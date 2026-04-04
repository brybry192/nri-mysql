package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/stretchr/testify/assert"
)

func TestExtractQueryName_WithComment(t *testing.T) {
	tests := []struct {
		query    string
		expected string
	}{
		{"-- SLOW_QUERY_STATUS\nSELECT * FROM foo", "SLOW_QUERY_STATUS"},
		{"-- BGWRITER_STATS\nSHOW STATUS", "BGWRITER_STATS"},
		{"--  MULTI_WORD_NAME\nSELECT 1", "MULTI_WORD_NAME"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractQueryName(tt.query))
		})
	}
}

func TestExtractQueryName_WithoutComment(t *testing.T) {
	tests := []struct {
		query    string
		expected string
	}{
		{"SHOW GLOBAL VARIABLES", "SHOW_GLOBAL"},
		{"SELECT VERSION()", "SELECT_VERSION()"},
		{"SHOW STATUS", "SHOW_STATUS"},
		{"SELECT 1", "SELECT_1"},
	}
	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, extractQueryName(tt.query))
		})
	}
}

func TestExtractQueryName_EdgeCases(t *testing.T) {
	assert.Equal(t, "UNKNOWN", extractQueryName(""))
	assert.Equal(t, "SELECT", extractQueryName("SELECT"))
}

func TestClassifyError_Nil(t *testing.T) {
	code, msg := classifyError(nil)
	assert.Equal(t, "", code)
	assert.Equal(t, "", msg)
}

func TestClassifyError_Timeout(t *testing.T) {
	code, _ := classifyError(context.DeadlineExceeded)
	assert.Equal(t, "timeout", code)

	code, _ = classifyError(context.Canceled)
	assert.Equal(t, "timeout", code)
}

func TestClassifyError_MySQLError(t *testing.T) {
	err := &mysql.MySQLError{Number: 1045, Message: "Access denied"}
	code, msg := classifyError(err)
	assert.Equal(t, "mysql_error_1045", code)
	assert.Equal(t, "Access denied", msg)
}

func TestClassifyError_NetworkTimeout(t *testing.T) {
	err := &net.OpError{
		Op:  "dial",
		Err: &timeoutError{},
	}
	code, _ := classifyError(err)
	assert.Equal(t, "timeout", code)
}

// timeoutError implements net.Error with Timeout() == true
type timeoutError struct{}

func (e *timeoutError) Error() string   { return "i/o timeout" }
func (e *timeoutError) Timeout() bool   { return true }
func (e *timeoutError) Temporary() bool { return false }

func TestClassifyError_StringMatches(t *testing.T) {
	tests := []struct {
		errMsg       string
		expectedCode string
	}{
		{"connection refused", "connection_refused"},
		{"dial tcp: connection refused", "connection_refused"},
		{"unexpected EOF", "server_closed_connection"},
		{"connection reset by peer", "connection_reset"},
		{"no such host", "dns_resolution_failed"},
		{"dns lookup failed", "dns_resolution_failed"},
		{"i/o timeout waiting for response", "io_timeout"},
		{"tls handshake failure", "tls_error"},
		{"x509: certificate signed by unknown authority", "tls_error"},
		{"something completely unexpected", "unknown_error"},
	}
	for _, tt := range tests {
		t.Run(tt.expectedCode, func(t *testing.T) {
			code, _ := classifyError(errors.New(tt.errMsg))
			assert.Equal(t, tt.expectedCode, code)
		})
	}
}

func TestTelemetryAccumulator_Disabled(t *testing.T) {
	a := &telemetryAccumulator{enabled: false}
	a.record("TEST", time.Second, nil)
	entries := a.drain()
	assert.Empty(t, entries)
}

func TestTelemetryAccumulator_RecordAndDrain(t *testing.T) {
	a := &telemetryAccumulator{enabled: true}

	a.record("QUERY_1", 50*time.Millisecond, nil)
	a.record("QUERY_2", 100*time.Millisecond, errors.New("connection refused"))

	entries := a.drain()
	assert.Len(t, entries, 2)

	assert.Equal(t, "QUERY_1", entries[0].QueryName)
	assert.InDelta(t, 50.0, entries[0].DurationMs, 0.1)
	assert.False(t, entries[0].HasError)

	assert.Equal(t, "QUERY_2", entries[1].QueryName)
	assert.InDelta(t, 100.0, entries[1].DurationMs, 0.1)
	assert.True(t, entries[1].HasError)
	assert.Equal(t, "connection_refused", entries[1].ErrorCode)

	// Drain again should be empty
	assert.Empty(t, a.drain())
}

func TestTelemetryAccumulator_NilDrain(t *testing.T) {
	var a *telemetryAccumulator
	assert.Nil(t, a.drain())
}

func TestTelemetryAccumulator_Concurrent(t *testing.T) {
	a := &telemetryAccumulator{enabled: true}
	var wg sync.WaitGroup

	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			a.record(fmt.Sprintf("Q_%d", i), time.Millisecond, nil)
		}(i)
	}
	wg.Wait()

	entries := a.drain()
	assert.Len(t, entries, 100)
}

func TestSanitizeErrorMessage(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "TCP DSN with password",
			input:    "error opening root:DBpwd1234@tcp(127.0.0.1:3306)/database?: dial tcp",
			expected: "error opening root:***@tcp(127.0.0.1:3306)/database?: dial tcp",
		},
		{
			name:     "Unix socket DSN",
			input:    "admin:s3cret@unix(/tmp/mysql.sock)/mydb failed",
			expected: "admin:***@unix(/tmp/mysql.sock)/mydb failed",
		},
		{
			name:     "Special chars in password",
			input:    "user:p@ss=w0rd!#$@tcp(host:3306)/db",
			expected: "user:***@tcp(host:3306)/db",
		},
		{
			name:     "No credentials",
			input:    "connection refused",
			expected: "connection refused",
		},
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "DSN with host:port in error tail",
			input:    "error opening root:pass1@tcp(host:3306)/db: dial tcp host:3306: connection refused",
			expected: "error opening root:***@tcp(host:3306)/db: dial tcp host:3306: connection refused",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, sanitizeErrorMessage(tt.input))
		})
	}
}

func TestClassifyError_SanitizesCredentials(t *testing.T) {
	err := errors.New("dial tcp root:DBpwd1234@tcp(127.0.0.1:3306)/db: connection refused")
	code, msg := classifyError(err)
	assert.Equal(t, "connection_refused", code)
	assert.NotContains(t, msg, "DBpwd1234")
	assert.Contains(t, msg, "root:***@")
}
