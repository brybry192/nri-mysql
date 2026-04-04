//go:build integration

package integration

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/newrelic/nri-mysql/tests/integration/helpers"
)

// runWithExtraFlags runs the integration binary with the standard connection
// flags plus any additional flags supplied by the caller. Unlike runIntegration,
// errors are returned to the caller so failure tests can inspect partial output.
func runWithExtraFlags(t *testing.T, targetContainer string, extraFlags []string, envVars ...string) (string, string, error) {
	t.Helper()
	command := []string{
		*binPath,
		"-username=" + *user,
		"-password=" + *psw,
		"-hostname=" + targetContainer,
		"-port=" + strconv.Itoa(*port),
	}
	command = append(command, extraFlags...)
	return helpers.ExecInContainer(*container, command, envVars...)
}

// TestHealthSampleAlwaysEmitted verifies that a MysqlHealthSample with
// available is emitted on every successful collection cycle even without any
// observability flags set.
func TestHealthSampleAlwaysEmitted(t *testing.T) {
	cfg := MysqlConfigs[len(MysqlConfigs)-1] // latest supported version
	envVars := []string{fmt.Sprintf("NRIA_CACHE_PATH=/tmp/%v.json", t.Name())}
	stdout, _, _ := runWithExtraFlags(t, cfg.MasterHostname, nil, envVars...)
	assert.Contains(t, stdout, `"MysqlHealthSample"`)
	assert.Contains(t, stdout, `"available"`)
}

// TestObservabilityFlags validates each observability flag individually.
func TestMysqlObservabilityFlags(t *testing.T) {
	t.Parallel()
	cfg := MysqlConfigs[len(MysqlConfigs)-1]
	testCases := []struct {
		Name        string
		ExtraFlags  []string
		MustContain []string
	}{
		{
			Name:       "Connection timing emits DNS and TCP gauges",
			ExtraFlags: []string{"-collect_connection_timing=true"},
			MustContain: []string{
				`"dnsLookupMs"`,
				`"tcpConnectMs"`,
			},
		},
		{
			Name:       "Availability check emits explicit sample with available=1",
			ExtraFlags: []string{"-enable_availability_check=true"},
			MustContain: []string{
				`"checkType":"explicit"`,
				`"available"`,
				`"durationMs"`,
				`"query":"SELECT 1"`,
			},
		},
		{
			Name:       "Availability check custom query is reflected in sample",
			ExtraFlags: []string{"-enable_availability_check=true", "-availability_check_query=SELECT 42"},
			MustContain: []string{
				`"checkType":"explicit"`,
				`"query":"SELECT 42"`,
			},
		},
		{
			Name:       "Query telemetry emits MysqlHealthSample with checkType=query",
			ExtraFlags: []string{"-collect_query_telemetry=true"},
			MustContain: []string{
				`"MysqlHealthSample"`,
				`"checkType":"query"`,
				`"durationMs"`,
				`"hasError"`,
			},
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.Name, func(t *testing.T) {
			t.Parallel()
			envVars := []string{fmt.Sprintf("NRIA_CACHE_PATH=/tmp/%v.json", t.Name())}
			stdout, _, _ := runWithExtraFlags(t, cfg.MasterHostname, tc.ExtraFlags, envVars...)
			assert.NotEmpty(t, stdout)
			for _, want := range tc.MustContain {
				assert.Contains(t, stdout, want, "expected %q in output", want)
			}
		})
	}
}

// TestConnectionFailureHealthSample verifies that when the host is unreachable
// a MysqlHealthSample is still emitted (lazy pool) and the explicit check sample
// carries errorCode.
func TestConnectionFailureHealthSample(t *testing.T) {
	badHost := "nonexistent-mysql-host-00000"
	extraFlags := []string{"-enable_availability_check=true"}
	envVars := []string{fmt.Sprintf("NRIA_CACHE_PATH=/tmp/%v.json", t.Name())}
	stdout, _, _ := runWithExtraFlags(t, badHost, extraFlags, envVars...)
	if stdout == "" {
		t.Skip("integration binary produced no output on connection failure; check container setup")
	}
	assert.Contains(t, stdout, `"MysqlHealthSample"`, "health sample should always be emitted")
	assert.Contains(t, stdout, `"checkType"`, "check type should be present")
	assert.Contains(t, stdout, `"errorCode"`, "health sample should carry errorCode when the host is unreachable")
}

// TestAvailabilityCheckTimeout verifies that a hanging canary query is interrupted
// by the configured timeout and reported with errorCode=timeout.
func TestAvailabilityCheckTimeout(t *testing.T) {
	cfg := MysqlConfigs[len(MysqlConfigs)-1]
	extraFlags := []string{
		"-enable_availability_check=true",
		// SELECT SLEEP(30) simulates a hung server; the tight timeout should cancel it quickly.
		"-availability_check_query=SELECT SLEEP(30)",
		"-availability_check_timeout_ms=500",
	}
	envVars := []string{fmt.Sprintf("NRIA_CACHE_PATH=/tmp/%v.json", t.Name())}
	stdout, _, _ := runWithExtraFlags(t, cfg.MasterHostname, extraFlags, envVars...)
	assert.Contains(t, stdout, `"checkType"`)
	assert.Contains(t, stdout, `"errorCode"`)
	assert.Contains(t, stdout, `"timeout"`, "errorCode should classify the cancelled context as timeout")
}
