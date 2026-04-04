package main

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOpenDB_StandardPath(t *testing.T) {
	// No observability flags → standard sql.Open path
	db, err := openDB("root:password@tcp(127.0.0.1:3306)/test", ObservabilityConfig{}, false)
	require.NoError(t, err)
	defer db.close()

	assert.Nil(t, db.Timing, "timing should be nil without CollectConnectionTiming")
	assert.NotNil(t, db.telemetry)
	assert.False(t, db.telemetry.enabled)
}

func TestOpenDB_WithTiming(t *testing.T) {
	db, err := openDB("root:password@tcp(127.0.0.1:3306)/test", ObservabilityConfig{
		CollectConnectionTiming: true,
	}, false)
	require.NoError(t, err)
	defer db.close()

	assert.NotNil(t, db.Timing, "timing should be set when CollectConnectionTiming is true")
	assert.Equal(t, 0.0, db.Timing.DNSLookupMs, "timing not triggered yet")
}

func TestOpenDB_WithAvailabilityCheckQuery(t *testing.T) {
	db, err := openDB("root:password@tcp(127.0.0.1:3306)/test", ObservabilityConfig{
		AvailabilityCheckQuery: "SELECT 1",
	}, false)
	require.NoError(t, err)
	defer db.close()

	assert.NotNil(t, db.Timing, "timing should be set when AvailabilityCheckQuery is set")
}

func TestOpenDB_UnixSocket_NoTiming(t *testing.T) {
	// Even with timing enabled, unix socket skips timing
	db, err := openDB("root:password@unix(/tmp/mysql.sock)/test", ObservabilityConfig{
		CollectConnectionTiming: true,
	}, true)
	require.NoError(t, err)
	defer db.close()

	assert.Nil(t, db.Timing, "timing should be nil for unix socket connections")
}

func TestOpenDB_WithTelemetry(t *testing.T) {
	db, err := openDB("root:password@tcp(127.0.0.1:3306)/test", ObservabilityConfig{
		CollectQueryTelemetry: true,
	}, false)
	require.NoError(t, err)
	defer db.close()

	assert.True(t, db.telemetry.enabled)
}

func TestOpenDB_InvalidDSN_TimingPath(t *testing.T) {
	_, err := openDB("not-a-valid-dsn", ObservabilityConfig{
		CollectConnectionTiming: true,
	}, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse DSN")
}

func TestOpenSQLDB(t *testing.T) {
	ds, err := openSQLDB("root:password@tcp(127.0.0.1:3306)/test")
	require.NoError(t, err)
	defer ds.close()
}

func TestDatabase_DrainTelemetry(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer db.Close()

	d := &database{
		source:    db,
		telemetry: &telemetryAccumulator{enabled: true},
	}

	mock.ExpectQuery("SHOW STATUS").WillReturnRows(
		sqlmock.NewRows([]string{"Variable_name", "Value"}).AddRow("Uptime", "12345"),
	)

	_, err = d.query("SHOW STATUS")
	require.NoError(t, err)

	entries := d.drainTelemetry()
	assert.Len(t, entries, 1)
	assert.Equal(t, "SHOW_STATUS", entries[0].QueryName)
	assert.False(t, entries[0].HasError)

	// Second drain should be empty
	assert.Empty(t, d.drainTelemetry())
}

func TestDatabase_QueryRecordsTelemetryOnError(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer db.Close()

	d := &database{
		source:    db,
		telemetry: &telemetryAccumulator{enabled: true},
	}

	mock.ExpectQuery("SELECT VERSION.*").WillReturnError(assert.AnError)

	_, err = d.query("SELECT VERSION() as version;")
	assert.Error(t, err)

	entries := d.drainTelemetry()
	assert.Len(t, entries, 1)
	assert.Equal(t, "SELECT_VERSION()", entries[0].QueryName)
	assert.True(t, entries[0].HasError)
}
