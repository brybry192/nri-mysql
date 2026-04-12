package main

import (
	"strconv"

	"github.com/newrelic/infra-integrations-sdk/v3/data/attribute"
	"github.com/newrelic/infra-integrations-sdk/v3/data/metric"
	"github.com/newrelic/infra-integrations-sdk/v3/integration"
	"github.com/newrelic/infra-integrations-sdk/v3/log"
	"github.com/newrelic/nri-mysql/src/shun"
)

const healthSampleEventType = "MysqlHealthSample"

// ObservabilityConfig holds the optional APM-style observability settings.
type ObservabilityConfig struct {
	CollectConnectionTiming    bool
	AvailabilityCheckQuery     string // empty = disabled
	AvailabilityCheckTimeoutMs int
	CollectQueryTelemetry      bool
}

// healthSampleAttrs returns the standard attributes for a MysqlHealthSample.
// Follows the same local-vs-remote pattern as infrautils.MetricSet: local entities
// have nil Metadata, so we only use hostname/port args — never e.Metadata directly.
func healthSampleAttrs(e *integration.Entity, hostname string, port int, remote bool) []attribute.Attribute {
	strPort := strconv.Itoa(port)
	attrs := []attribute.Attribute{}
	if remote && e.Metadata != nil {
		attrs = append(attrs,
			attribute.Attr("displayName", e.Metadata.Name),
			attribute.Attr("entityName", e.Metadata.Namespace+":"+e.Metadata.Name),
			attribute.Attr("hostname", hostname),
			attribute.Attr("port", strPort),
		)
	} else {
		attrs = append(attrs, attribute.Attr("port", strPort))
	}
	return attrs
}

// publishImplicitHealthSample emits a MysqlHealthSample with checkType=implicit
// capturing the ping-based availability signal and connection phase timings.
func publishImplicitHealthSample(e *integration.Entity, timing *ConnectionTiming, responseTimeMs float64, connErr error, hostname string, port int, remote bool) {
	attrs := healthSampleAttrs(e, hostname, port, remote)
	attrs = append(attrs, attribute.Attr("checkType", "implicit"))
	ms := e.NewMetricSet(healthSampleEventType, attrs...)

	hasError := 0.0
	if connErr != nil {
		hasError = 1.0
		setGauge(ms, "available", 0)
		errCode, errMsg := classifyError(connErr)
		setAttribute(ms, "errorCode", errCode)
		setAttribute(ms, "errorMessage", errMsg)
	} else {
		setGauge(ms, "available", 1)
	}
	setGauge(ms, "hasError", hasError)

	setGauge(ms, "durationMs", responseTimeMs)

	if timing != nil {
		setGauge(ms, "dnsLookupMs", timing.DNSLookupMs)
		setGauge(ms, "tcpConnectMs", timing.TCPConnectMs)
		if timing.TLSHandshakeMs > 0 {
			setGauge(ms, "tlsHandshakeMs", timing.TLSHandshakeMs)
		}
	}
}

// publishExplicitHealthSample emits a MysqlHealthSample with checkType=explicit
// carrying the canary query result.
func publishExplicitHealthSample(e *integration.Entity, result *checkResult, hostname string, port int, remote bool) {
	attrs := healthSampleAttrs(e, hostname, port, remote)
	attrs = append(attrs, attribute.Attr("checkType", "explicit"))
	ms := e.NewMetricSet(healthSampleEventType, attrs...)

	available := 0.0
	hasError := 0.0
	if result.available {
		available = 1.0
	} else {
		hasError = 1.0
	}
	setGauge(ms, "available", available)
	setGauge(ms, "hasError", hasError)
	setGauge(ms, "durationMs", result.durationMs)
	setAttribute(ms, "query", result.query)
	if result.errorCode != "" {
		setAttribute(ms, "errorCode", result.errorCode)
		setAttribute(ms, "errorMessage", result.errorMessage)
	}
}

// publishQueryHealthSamples emits one MysqlHealthSample per internal monitoring
// query with checkType=query.
func publishQueryHealthSamples(e *integration.Entity, entries []*QueryTelemetry, hostname string, port int, remote bool) {
	baseAttrs := healthSampleAttrs(e, hostname, port, remote)
	for _, t := range entries {
		// Clone baseAttrs to avoid slice aliasing — append may reuse the
		// backing array across loop iterations, corrupting earlier entries.
		attrs := make([]attribute.Attribute, len(baseAttrs), len(baseAttrs)+2)
		copy(attrs, baseAttrs)
		entryAttrs := append(attrs,
			attribute.Attr("checkType", "query"),
			attribute.Attr("queryName", t.QueryName),
		)
		ms := e.NewMetricSet(healthSampleEventType, entryAttrs...)
		setGauge(ms, "durationMs", t.DurationMs)
		hasError := 0.0
		if t.HasError {
			hasError = 1.0
		}
		setGauge(ms, "hasError", hasError)
		if t.ErrorCode != "" {
			setAttribute(ms, "errorCode", t.ErrorCode)
			setAttribute(ms, "errorMessage", t.ErrorMessage)
		}
	}
}

// publishShunnedHealthSample emits a MysqlHealthSample with checkType=implicit
// when the instance is shunned. No connection is attempted; the sample reports
// available=0 and shunned=true so dashboards reflect the ongoing outage.
func publishShunnedHealthSample(e *integration.Entity, st shun.State, connErr error, hostname string, port int, remote bool) {
	attrs := healthSampleAttrs(e, hostname, port, remote)
	attrs = append(attrs, attribute.Attr("checkType", "implicit"))
	ms := e.NewMetricSet(healthSampleEventType, attrs...)

	setGauge(ms, "available", 0)
	setGauge(ms, "hasError", 1)
	setGauge(ms, "shunned", 1)
	setGauge(ms, "shunBackoffCycles", float64(st.BackoffCycles))

	errCode, errMsg := classifyError(connErr)
	setAttribute(ms, "errorCode", errCode)
	setAttribute(ms, "errorMessage", errMsg)
}

func setGauge(ms *metric.Set, name string, val float64) {
	if err := ms.SetMetric(name, val, metric.GAUGE); err != nil {
		log.Warn("Failed to set metric %s: %s", name, err)
	}
}

func setAttribute(ms *metric.Set, name, val string) {
	if err := ms.SetMetric(name, val, metric.ATTRIBUTE); err != nil {
		log.Warn("Failed to set attribute %s: %s", name, err)
	}
}
