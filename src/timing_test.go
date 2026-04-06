package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"math/big"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTimingDialFunc_Localhost(t *testing.T) {
	// Start a TCP listener to connect to
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	addr := ln.Addr().String()

	timing := &ConnectionTiming{}
	dial := timingDialFunc(timing)

	conn, err := dial(context.Background(), "tcp", addr)
	require.NoError(t, err)
	defer conn.Close()

	// DNS for 127.0.0.1 should be very fast (just IP parsing)
	assert.GreaterOrEqual(t, timing.DNSLookupMs, 0.0)
	assert.GreaterOrEqual(t, timing.TCPConnectMs, 0.0)
	// tcpDone should be set after a successful connect
	assert.False(t, timing.tcpDone.IsZero())
	// No TLS configured, so TLSHandshakeMs stays 0
	assert.Equal(t, 0.0, timing.TLSHandshakeMs)
}

func TestTimingDialFunc_InvalidAddress(t *testing.T) {
	timing := &ConnectionTiming{}
	dial := timingDialFunc(timing)

	_, err := dial(context.Background(), "tcp", "not-a-valid-address")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid address")
}

func TestTimingDialFunc_DNSFailure(t *testing.T) {
	timing := &ConnectionTiming{}
	dial := timingDialFunc(timing)

	_, err := dial(context.Background(), "tcp", "this-host-does-not-exist-12345.invalid:3306")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "dns lookup")
	// DNS timing should still be recorded even on failure
	assert.GreaterOrEqual(t, timing.DNSLookupMs, 0.0)
}

func TestTimingDialFunc_ConnectionRefused(t *testing.T) {
	timing := &ConnectionTiming{}
	dial := timingDialFunc(timing)

	// Connect to a port that's definitely not listening
	_, err := dial(context.Background(), "tcp", "127.0.0.1:1")
	assert.Error(t, err)
	// DNS lookup for an IP should be near-instant
	assert.GreaterOrEqual(t, timing.DNSLookupMs, 0.0)
	// TCP timing should still be recorded even on failure
	assert.GreaterOrEqual(t, timing.TCPConnectMs, 0.0)
	// tcpDone should NOT be set after a failed connect
	assert.True(t, timing.tcpDone.IsZero())
}

func TestWrapTLSConfig(t *testing.T) {
	// Generate a self-signed cert for the test TLS server.
	serverTLS := generateTestTLSConfig(t)

	tlsLn, err := tls.Listen("tcp", "127.0.0.1:0", serverTLS)
	require.NoError(t, err)
	defer tlsLn.Close()

	// Accept one connection in a goroutine and complete the TLS handshake.
	go func() {
		conn, err := tlsLn.Accept()
		if err != nil {
			return
		}
		// Force the server-side TLS handshake to complete before closing.
		if tc, ok := conn.(*tls.Conn); ok {
			_ = tc.Handshake()
		}
		conn.Close()
	}()

	addr := tlsLn.Addr().String()

	timing := &ConnectionTiming{}
	dial := timingDialFunc(timing)

	// Establish TCP connection via our timing dialer.
	conn, err := dial(context.Background(), "tcp", addr)
	require.NoError(t, err)

	// Wrap a client TLS config with our timing callback.
	clientTLS := &tls.Config{InsecureSkipVerify: true}
	wrapTLSConfig(clientTLS, timing)

	// Perform TLS handshake.
	tlsConn := tls.Client(conn, clientTLS)
	err = tlsConn.Handshake()
	require.NoError(t, err)
	defer tlsConn.Close()

	assert.GreaterOrEqual(t, timing.TLSHandshakeMs, 0.0)
	assert.GreaterOrEqual(t, timing.DNSLookupMs, 0.0)
	assert.GreaterOrEqual(t, timing.TCPConnectMs, 0.0)
}

func TestWrapTLSConfig_PreservesExistingCallback(t *testing.T) {
	callbackCalled := false
	tlsCfg := &tls.Config{
		InsecureSkipVerify: true,
		VerifyConnection: func(cs tls.ConnectionState) error {
			callbackCalled = true
			return nil
		},
	}

	timing := &ConnectionTiming{tcpDone: time.Now()}
	wrapTLSConfig(tlsCfg, timing)

	// Simulate the callback being invoked.
	err := tlsCfg.VerifyConnection(tls.ConnectionState{})
	assert.NoError(t, err)
	assert.True(t, callbackCalled, "original VerifyConnection callback should be preserved")
	assert.GreaterOrEqual(t, timing.TLSHandshakeMs, 0.0)
}

// generateTestTLSConfig creates a self-signed TLS config for localhost tests.
func generateTestTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{Organization: []string{"Test"}},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	require.NoError(t, err)

	return &tls.Config{
		Certificates: []tls.Certificate{{
			Certificate: [][]byte{certDER},
			PrivateKey:  key,
		}},
	}
}

func TestMsec(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected float64
	}{
		{0, 0.0},
		{time.Millisecond, 1.0},
		{500 * time.Microsecond, 0.5},
		{time.Second, 1000.0},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%v", tt.input), func(t *testing.T) {
			assert.InDelta(t, tt.expected, msec(tt.input), 0.001)
		})
	}
}
