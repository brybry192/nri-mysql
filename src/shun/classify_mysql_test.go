package shun

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsShunnableMySQL(t *testing.T) {
	tests := []struct {
		code     string
		expected bool
		reason   string
	}{
		// Shunnable: persistent errors that require human intervention.
		{"mysql_error_1045", true, "access denied — wrong password"},
		{"mysql_error_1044", true, "access denied for database — permission issue"},
		{"mysql_error_1049", true, "unknown database — config error"},
		{"mysql_error_1129", true, "host blocked — needs FLUSH HOSTS"},
		{"dns_resolution_failed", true, "hostname doesn't resolve"},
		{"connection_refused", true, "nothing listening on that port"},
		{"ssl_error", true, "TLS misconfiguration"},
		{"tls_error", true, "TLS misconfiguration (alternate code)"},

		// Not shunnable: transient errors that may self-resolve.
		{"timeout", false, "may be temporary network issue"},
		{"io_timeout", false, "transient I/O timeout"},
		{"invalid_connection", false, "connection was killed — transient"},
		{"connection_reset", false, "TCP reset — transient"},
		{"server_closed_connection", false, "server EOF — transient"},
		{"unknown_error", false, "unclassified — don't shun on uncertainty"},
		{"mysql_error_1040", false, "too many connections — may self-resolve"},
		{"mysql_error_1158", false, "network read error — transient"},
		{"mysql_error_1159", false, "network write timeout — transient"},
		{"", false, "empty code — no error"},
	}

	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			assert.Equal(t, tt.expected, IsShunnableMySQL(tt.code), tt.reason)
		})
	}
}
