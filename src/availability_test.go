package main

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExplicitAvailabilityCheck_Success(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT 1").WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))

	d := &database{
		source:    db,
		telemetry: &telemetryAccumulator{enabled: false},
	}
	ctx := context.Background()
	result := explicitAvailabilityCheck(ctx, d, "SELECT 1", 0)

	assert.True(t, result.available)
	assert.Equal(t, "SELECT 1", result.query)
	assert.Empty(t, result.errorCode)
	assert.GreaterOrEqual(t, result.durationMs, 0.0)
}

func TestExplicitAvailabilityCheck_QueryError(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT 1").WillReturnError(assert.AnError)

	d := &database{
		source:    db,
		telemetry: &telemetryAccumulator{enabled: false},
	}
	ctx := context.Background()
	result := explicitAvailabilityCheck(ctx, d, "SELECT 1", 0)

	assert.False(t, result.available)
	assert.NotEmpty(t, result.errorCode)
	assert.NotEmpty(t, result.errorMessage)
}

func TestExplicitAvailabilityCheck_NoRows(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT 1").WillReturnRows(sqlmock.NewRows([]string{"1"}))

	d := &database{
		source:    db,
		telemetry: &telemetryAccumulator{enabled: false},
	}
	ctx := context.Background()
	result := explicitAvailabilityCheck(ctx, d, "SELECT 1", 0)

	assert.False(t, result.available)
}

func TestExplicitAvailabilityCheck_Timeout(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT 1").WillDelayFor(200 * time.Millisecond).WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))

	d := &database{
		source:    db,
		telemetry: &telemetryAccumulator{enabled: false},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	result := explicitAvailabilityCheck(ctx, d, "SELECT 1", 0)

	assert.False(t, result.available)
	assert.Equal(t, "timeout", result.errorCode)
}

func TestExplicitAvailabilityCheck_SetsMaxExecutionTime(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectExec("SET SESSION max_execution_time = 3000").WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectQuery("SELECT 1").WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))

	d := &database{
		source:    db,
		telemetry: &telemetryAccumulator{enabled: false},
	}
	ctx := context.Background()
	result := explicitAvailabilityCheck(ctx, d, "SELECT 1", 3000)

	assert.True(t, result.available)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestExplicitAvailabilityCheck_MaxExecutionTimeFailureNonFatal(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	require.NoError(t, err)
	defer db.Close()

	// SET fails (e.g. MySQL < 5.7.8) but the canary query still runs.
	mock.ExpectExec("SET SESSION max_execution_time = 5000").WillReturnError(assert.AnError)
	mock.ExpectQuery("SELECT 1").WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))

	d := &database{
		source:    db,
		telemetry: &telemetryAccumulator{enabled: false},
	}
	ctx := context.Background()
	result := explicitAvailabilityCheck(ctx, d, "SELECT 1", 5000)

	assert.True(t, result.available)
	assert.NoError(t, mock.ExpectationsWereMet())
}

func TestExplicitAvailabilityCheck_RowsErr(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherEqual))
	require.NoError(t, err)
	defer db.Close()

	mock.ExpectQuery("SELECT 1").WillReturnRows(
		sqlmock.NewRows([]string{"1"}).AddRow(1).RowError(0, assert.AnError),
	)

	d := &database{
		source:    db,
		telemetry: &telemetryAccumulator{enabled: false},
	}
	ctx := context.Background()
	result := explicitAvailabilityCheck(ctx, d, "SELECT 1", 0)

	assert.False(t, result.available)
	assert.NotEmpty(t, result.errorCode)
}
