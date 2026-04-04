package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-sql-driver/mysql"
)

// QueryTelemetry holds APM-style telemetry for a single query execution.
type QueryTelemetry struct {
	// QueryName is extracted from an embedded SQL comment (e.g. "-- BGWRITER_STATS")
	// or derived from the first two keywords of the query if no comment is present.
	QueryName    string
	DurationMs   float64
	HasError     bool
	ErrorCode    string
	ErrorMessage string
}

// queryNameRe matches the first single-line SQL comment token: -- SOME_NAME
var queryNameRe = regexp.MustCompile(`--\s+([A-Z][A-Z0-9_]+)`)

// dsnCredsRe matches the :password@ portion of a MySQL DSN string, anchored
// on the @tcp( or @unix( delimiter that always follows credentials. Uses a
// lazy quantifier so passwords containing @ are fully captured.
var dsnCredsRe = regexp.MustCompile(`:(.*?)@(tcp\(|unix\()`)

// sanitizeErrorMessage redacts credentials that may appear in error messages,
// particularly MySQL DSN strings in the form user:password@tcp(host:port)/db.
func sanitizeErrorMessage(msg string) string {
	return dsnCredsRe.ReplaceAllString(msg, ":***@${2}")
}

// extractQueryName pulls a name from an embedded SQL comment like "-- SLOW_QUERY_STATUS".
// Falls back to the first SQL keyword + next token (e.g. "SHOW server_version").
func extractQueryName(query string) string {
	if m := queryNameRe.FindStringSubmatch(query); len(m) == 2 {
		return m[1]
	}
	words := strings.Fields(strings.ReplaceAll(query, "\n", " "))
	if len(words) == 0 {
		return "UNKNOWN"
	}
	if len(words) == 1 {
		return strings.ToUpper(words[0])
	}
	return strings.ToUpper(words[0] + "_" + words[1])
}

// classifyError converts a raw error into a structured (code, message) pair.
func classifyError(err error) (code, message string) {
	if err == nil {
		return "", ""
	}

	message = sanitizeErrorMessage(err.Error())

	// Context deadline exceeded or cancellation.
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "timeout", message
	}

	// MySQL server error — structured error number.
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return fmt.Sprintf("mysql_error_%d", mysqlErr.Number), mysqlErr.Message
	}

	// Network / I/O errors.
	var netErr net.Error
	if errors.As(err, &netErr) {
		if netErr.Timeout() {
			return "timeout", message
		}
	}

	lower := strings.ToLower(message)
	switch {
	case strings.Contains(lower, "connection refused"):
		return "connection_refused", message
	case strings.Contains(lower, "eof"):
		return "server_closed_connection", message
	case strings.Contains(lower, "connection reset"):
		return "connection_reset", message
	case strings.Contains(lower, "no such host"),
		strings.Contains(lower, "dns"), strings.Contains(lower, "lookup"):
		return "dns_resolution_failed", message
	case strings.Contains(lower, "i/o timeout"), strings.Contains(lower, "io timeout"):
		return "io_timeout", message
	case strings.Contains(lower, "tls") || strings.Contains(lower, "certificate"):
		return "tls_error", message
	default:
		return "unknown_error", message
	}
}

// telemetryAccumulator is embedded in database to collect per-query telemetry.
type telemetryAccumulator struct {
	enabled bool
	mu      sync.Mutex
	entries []*QueryTelemetry
}

func (a *telemetryAccumulator) record(queryName string, duration time.Duration, err error) {
	if a == nil || !a.enabled {
		return
	}
	t := &QueryTelemetry{
		QueryName:  queryName,
		DurationMs: msec(duration),
	}
	if err != nil {
		t.HasError = true
		t.ErrorCode, t.ErrorMessage = classifyError(err)
	}
	a.mu.Lock()
	a.entries = append(a.entries, t)
	a.mu.Unlock()
}

// drainTelemetry returns all accumulated query telemetry and resets the internal slice.
func (a *telemetryAccumulator) drain() []*QueryTelemetry {
	if a == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	entries := a.entries
	a.entries = nil
	return entries
}
