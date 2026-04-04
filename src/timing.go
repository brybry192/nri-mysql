package main

import (
	"context"
	"fmt"
	"net"
	"time"
)

// ConnectionTiming holds DNS and TCP timing for a new database connection.
// Both values are captured inside the timingDialFunc, which fires on the first
// real query issued against the connection — no extra Ping is used when
// AVAILABILITY_CHECK_QUERY is set. When only COLLECT_CONNECTION_TIMING is
// true, a lightweight Ping is issued to trigger the dialer before the
// implicit connection sample is published.
type ConnectionTiming struct {
	DNSLookupMs  float64
	TCPConnectMs float64
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
		return conn, err
	}
}

func msec(d time.Duration) float64 {
	return float64(d) / float64(time.Millisecond)
}
