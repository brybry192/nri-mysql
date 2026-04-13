package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"
)

// ConnectionTiming holds DNS, TCP, and TLS timing for a new database connection.
// DNS and TCP values are captured inside timingDialFunc, which fires on the first
// real query issued against the connection. TLS handshake time is captured via a
// VerifyConnection callback on the tls.Config — it fires only when TLS is active.
// When only COLLECT_CONNECTION_TIMING is true, a lightweight Ping is issued to
// trigger the dialer before the implicit connection sample is published.
type ConnectionTiming struct {
	DNSLookupMs    float64
	TCPConnectMs   float64
	TLSHandshakeMs float64 // 0 when TLS is not used

	// tcpDone records when the TCP connect completed. Used internally by
	// wrapTLSConfig to compute TLS handshake duration. Unexported because
	// it's an implementation detail, not a publishable metric.
	tcpDone time.Time
}

// timingDialFunc returns a mysql DialFunc that captures DNS resolution and TCP
// connection time into timing. It is set on mysql.Config.DialFunc before opening
// the connection via mysql.NewConnector. The network parameter is ignored since
// MySQL always uses TCP for named-host connections.
func timingDialFunc(timing *ConnectionTiming) func(ctx context.Context, network, addr string) (net.Conn, error) {
	return func(ctx context.Context, _ string, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil {
			return nil, fmt.Errorf("invalid address %q: %w", addr, err)
		}

		// DNS resolution
		dnsStart := time.Now()
		addrs, err := net.DefaultResolver.LookupHost(ctx, host)
		timing.DNSLookupMs = msec(time.Since(dnsStart))
		if err != nil {
			return nil, fmt.Errorf("dns lookup for %q failed: %w", host, err)
		}
		if len(addrs) == 0 {
			return nil, fmt.Errorf("dns returned no addresses for %q", host)
		}

		// TCP connection (use first resolved address)
		tcpAddr := net.JoinHostPort(addrs[0], port)
		dialer := &net.Dialer{}
		tcpStart := time.Now()
		conn, err := dialer.DialContext(ctx, "tcp", tcpAddr)
		timing.TCPConnectMs = msec(time.Since(tcpStart))
		if err == nil {
			timing.tcpDone = time.Now()
		}
		return conn, err
	}
}

// wrapTLSConfig installs a VerifyConnection callback on the tls.Config to
// measure TLS handshake duration. The callback fires after the handshake
// completes; the elapsed time since tcpDone (set by timingDialFunc) gives
// the TLS phase duration. Any existing VerifyConnection callback is preserved.
//
// Note: the interval between TCP connect and TLS handshake includes the MySQL
// initial handshake packet exchange (server greeting + client TLS request),
// which is typically sub-millisecond on local networks.
func wrapTLSConfig(tlsCfg *tls.Config, timing *ConnectionTiming) {
	origVerify := tlsCfg.VerifyConnection
	tlsCfg.VerifyConnection = func(cs tls.ConnectionState) error {
		if !timing.tcpDone.IsZero() {
			timing.TLSHandshakeMs = msec(time.Since(timing.tcpDone))
		}
		if origVerify != nil {
			return origVerify(cs)
		}
		return nil
	}
}

func msec(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
