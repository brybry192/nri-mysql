package main

import (
	"context"
	"fmt"
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
	assert.Greater(t, timing.TCPConnectMs, 0.0)
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
	assert.Greater(t, timing.DNSLookupMs, 0.0)
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
	assert.Greater(t, timing.TCPConnectMs, 0.0)
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
