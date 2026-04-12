package main

import (
	"errors"
	"testing"

	"github.com/newrelic/infra-integrations-sdk/v3/integration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	constants "github.com/newrelic/nri-mysql/src/query-performance-monitoring/constants"
	"github.com/newrelic/nri-mysql/src/shun"
)

func newTestEntity(t *testing.T, remote bool) *integration.Entity {
	t.Helper()
	i, err := integration.New(constants.IntegrationName, "0.0.0")
	require.NoError(t, err)
	if remote {
		e, err := i.Entity("localhost:3306", "mysql-node")
		require.NoError(t, err)
		return e
	}
	return i.LocalEntity()
}

func TestHealthSampleAttrs_Local(t *testing.T) {
	e := newTestEntity(t, false)
	attrs := healthSampleAttrs(e, "localhost", 3306, false)
	assert.Len(t, attrs, 1) // port only
}

func TestHealthSampleAttrs_Remote(t *testing.T) {
	e := newTestEntity(t, true)
	attrs := healthSampleAttrs(e, "localhost", 3306, true)
	assert.Len(t, attrs, 4) // displayName, entityName, hostname, port
}

func TestHealthSampleAttrs_NilMetadata(t *testing.T) {
	e := newTestEntity(t, false)
	// Simulate nil metadata on a local entity — should not panic
	attrs := healthSampleAttrs(e, "localhost", 3306, true)
	// Falls back to port-only because Metadata is nil
	assert.Len(t, attrs, 1)
}

func TestPublishImplicitHealthSample_Available(t *testing.T) {
	e := newTestEntity(t, false)
	timing := &ConnectionTiming{DNSLookupMs: 1.5, TCPConnectMs: 2.5}

	publishImplicitHealthSample(e, timing, 5.0, nil, "localhost", 3306, false)

	require.Len(t, e.Metrics, 1)
	ms := e.Metrics[0]
	assert.Equal(t, "MysqlHealthSample", ms.Metrics["event_type"])
	assert.Equal(t, "implicit", ms.Metrics["checkType"])
	assert.Equal(t, 1.0, ms.Metrics["available"])
	assert.Equal(t, 0.0, ms.Metrics["hasError"])
	assert.Equal(t, 5.0, ms.Metrics["durationMs"])
	assert.Equal(t, 1.5, ms.Metrics["dnsLookupMs"])
	assert.Equal(t, 2.5, ms.Metrics["tcpConnectMs"])
}

func TestPublishImplicitHealthSample_NoTiming(t *testing.T) {
	e := newTestEntity(t, false)

	publishImplicitHealthSample(e, nil, 0.0, nil, "localhost", 3306, false)

	require.Len(t, e.Metrics, 1)
	ms := e.Metrics[0]
	assert.Equal(t, 1.0, ms.Metrics["available"])
	assert.Equal(t, 0.0, ms.Metrics["hasError"])
	assert.Nil(t, ms.Metrics["dnsLookupMs"])
}

func TestPublishImplicitHealthSample_ConnErr(t *testing.T) {
	e := newTestEntity(t, false)

	publishImplicitHealthSample(e, nil, 2.5, errors.New("connection refused"), "localhost", 3306, false)

	require.Len(t, e.Metrics, 1)
	ms := e.Metrics[0]
	assert.Equal(t, 0.0, ms.Metrics["available"])
	assert.Equal(t, 1.0, ms.Metrics["hasError"])
	assert.Equal(t, "connection_refused", ms.Metrics["errorCode"])
}

func TestPublishExplicitHealthSample_Available(t *testing.T) {
	e := newTestEntity(t, false)
	result := &checkResult{
		available:  true,
		durationMs: 3.14,
		query:      "SELECT 1",
	}

	publishExplicitHealthSample(e, result, "localhost", 3306, false)

	require.Len(t, e.Metrics, 1)
	ms := e.Metrics[0]
	assert.Equal(t, "MysqlHealthSample", ms.Metrics["event_type"])
	assert.Equal(t, "explicit", ms.Metrics["checkType"])
	assert.Equal(t, 1.0, ms.Metrics["available"])
	assert.Equal(t, 0.0, ms.Metrics["hasError"])
	assert.Equal(t, 3.14, ms.Metrics["durationMs"])
	assert.Equal(t, "SELECT 1", ms.Metrics["query"])
}

func TestPublishExplicitHealthSample_Unavailable(t *testing.T) {
	e := newTestEntity(t, false)
	result := &checkResult{
		available:    false,
		durationMs:   5000.0,
		errorCode:    "timeout",
		errorMessage: "context deadline exceeded",
		query:        "SELECT 1",
	}

	publishExplicitHealthSample(e, result, "localhost", 3306, false)

	require.Len(t, e.Metrics, 1)
	ms := e.Metrics[0]
	assert.Equal(t, 0.0, ms.Metrics["available"])
	assert.Equal(t, 1.0, ms.Metrics["hasError"])
	assert.Equal(t, "timeout", ms.Metrics["errorCode"])
	assert.Equal(t, "context deadline exceeded", ms.Metrics["errorMessage"])
}

func TestPublishQueryHealthSamples_Empty(t *testing.T) {
	e := newTestEntity(t, false)
	publishQueryHealthSamples(e, nil, "localhost", 3306, false)
	assert.Empty(t, e.Metrics)
}

func TestPublishShunnedHealthSample(t *testing.T) {
	e := newTestEntity(t, false)
	st := shun.State{
		Shunned:          true,
		ErrorCode:        "mysql_error_1045",
		ConsecutiveFails: 3,
		BackoffCycles:    8,
	}
	connErr := &classifiedError{code: "mysql_error_1045", msg: "shunned: persistent failure"}

	publishShunnedHealthSample(e, st, connErr, "localhost", 3306, false)

	require.Len(t, e.Metrics, 1)
	ms := e.Metrics[0]
	assert.Equal(t, "MysqlHealthSample", ms.Metrics["event_type"])
	assert.Equal(t, "implicit", ms.Metrics["checkType"])
	assert.Equal(t, 0.0, ms.Metrics["available"])
	assert.Equal(t, 1.0, ms.Metrics["hasError"])
	assert.Equal(t, 1.0, ms.Metrics["shunned"])
	assert.Equal(t, 8.0, ms.Metrics["shunBackoffCycles"])
	assert.Equal(t, "mysql_error_1045", ms.Metrics["errorCode"])
}

func TestPublishQueryHealthSamples_Multiple(t *testing.T) {
	e := newTestEntity(t, false)
	entries := []*QueryTelemetry{
		{QueryName: "SHOW_STATUS", DurationMs: 1.2, HasError: false},
		{QueryName: "SELECT_VERSION()", DurationMs: 0.5, HasError: true, ErrorCode: "timeout", ErrorMessage: "deadline exceeded"},
	}

	publishQueryHealthSamples(e, entries, "localhost", 3306, false)

	require.Len(t, e.Metrics, 2)

	ms0 := e.Metrics[0]
	assert.Equal(t, "MysqlHealthSample", ms0.Metrics["event_type"])
	assert.Equal(t, "query", ms0.Metrics["checkType"])
	assert.Equal(t, "SHOW_STATUS", ms0.Metrics["queryName"])
	assert.Equal(t, 1.2, ms0.Metrics["durationMs"])
	assert.Equal(t, 0.0, ms0.Metrics["hasError"])

	ms1 := e.Metrics[1]
	assert.Equal(t, "SELECT_VERSION()", ms1.Metrics["queryName"])
	assert.Equal(t, 1.0, ms1.Metrics["hasError"])
	assert.Equal(t, "timeout", ms1.Metrics["errorCode"])
}
