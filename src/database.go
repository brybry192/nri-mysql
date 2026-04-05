package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/go-sql-driver/mysql"
	"github.com/newrelic/infra-integrations-sdk/v3/log"
)

type dataSource interface {
	close()
	query(string) (map[string]interface{}, error)
}

type database struct {
	source    *sql.DB
	Timing    *ConnectionTiming    // non-nil when COLLECT_CONNECTION_TIMING is enabled
	telemetry *telemetryAccumulator // pointer so all method calls share the same storage
}

// openDB creates a database connection with optional observability support.
// When timing is enabled and a TCP connection is used (not Unix socket), it attaches
// a timingDialer via mysql.NewConnector so the first connection's DNS and TCP phases
// are measured. When telemetry is enabled, each query call records its duration.
// The returned *database is always non-nil when err is nil; the underlying sql.DB
// is lazy and does not dial until the first query or Ping is issued.
func openDB(dsn string, obs ObservabilityConfig, isUnixSocket bool) (*database, error) {
	var timing *ConnectionTiming
	if (obs.CollectConnectionTiming || obs.AvailabilityCheckQuery != "") && !isUnixSocket {
		timing = &ConnectionTiming{}
	}

	telemetry := &telemetryAccumulator{enabled: obs.CollectQueryTelemetry}

	if timing != nil {
		// Use mysql.ParseDSN + NewConnector + sql.OpenDB to attach the timing dialer.
		// This is equivalent to sql.Open("mysql", dsn) but allows customising the
		// dial function before the pool is created. The pool is still lazy — no
		// actual network connection is made here.
		cfg, err := mysql.ParseDSN(dsn)
		if err != nil {
			return nil, fmt.Errorf("failed to parse DSN for instrumented connection: %w", err)
		}
		cfg.DialFunc = timingDialFunc(timing)
		connector, err := mysql.NewConnector(cfg)
		if err != nil {
			return nil, fmt.Errorf("failed to create MySQL connector: %w", err)
		}
		return &database{
			source:    sql.OpenDB(connector),
			Timing:    timing,
			telemetry: telemetry,
		}, nil
	}

	// Standard path — no custom dialer needed.
	source, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("error opening database connection: %w", err)
	}
	return &database{
		source:    source,
		Timing:    timing,
		telemetry: telemetry,
	}, nil
}

// openSQLDB creates a basic database connection without observability features.
// Kept for backwards compatibility; mysql.go now calls openDB directly.
func openSQLDB(dsn string) (dataSource, error) {
	return openDB(dsn, ObservabilityConfig{}, false)
}

func (db *database) close() {
	db.source.Close()
}

// ping issues a lightweight Ping to trigger the timing dialer on the first real
// connection. Call this before reading db.Timing when only COLLECT_CONNECTION_TIMING
// is set (and AVAILABILITY_CHECK_QUERY is not). Uses a 5-second timeout so a
// frozen or unresponsive server produces a timeout error rather than blocking
// indefinitely.
func (db *database) ping() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return db.source.PingContext(ctx)
}

// queryContext runs the given query with context support, returning *sql.Rows.
// Used by the availability check to support context deadlines.
func (db *database) queryContext(ctx context.Context, query string) (*sql.Rows, error) {
	return db.source.QueryContext(ctx, query)
}

// drainTelemetry returns and resets all accumulated query telemetry.
func (db *database) drainTelemetry() []*QueryTelemetry {
	return db.telemetry.drain()
}

/*
query executes provided as an argument query and parses the output to the map structure.
It is only possible to parse two types of query:
1. output of the query consists of two columns. Names of the columns are ignored. Values from the first
column are used as keys, and from the second as corresponding values of the map. Number of rows can be greater than 1;
2. output of the query consists of multiple columns, but only single row.
In this case, each column name is a key, and corresponding value is a map value.
*/
func (db *database) query(q string) (map[string]interface{}, error) {
	start := time.Now()
	result, err := db.queryInternal(q)
	db.telemetry.record(extractQueryName(q), time.Since(start), err)
	return result, err
}

func (db *database) queryInternal(q string) (map[string]interface{}, error) {
	log.Debug("executing query: " + q)
	rows, err := db.source.Query(q)
	if err != nil {
		return nil, fmt.Errorf("error executing `%s`: %v", q, err)
	}
	defer func() {
		if err := rows.Close(); err != nil {
			log.Warn(fmt.Sprintf("error closing rows: %v", err))
		}
	}()

	rawData := make(map[string]interface{})

	columns, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("error getting columns from query: %v", err)
	}

	values := make([]sql.RawBytes, len(columns))
	scanArgs := make([]interface{}, len(values))
	for i := range values {
		scanArgs[i] = &values[i]
	}

	rowIndex := 0
	for rows.Next() {
		err = rows.Scan(scanArgs...)
		if err != nil {
			return nil, fmt.Errorf("error scanning rows[%d]: %v", rowIndex, err)
		}

		if len(values) == 2 {
			rawData[string(values[0])] = asValue(string(values[1]))
		} else {
			if rowIndex != 0 {
				log.Debug("Cannot process query: %s, for query output with more than 2 columns only single row expected", q)
				break
			}

			for i, value := range values {
				rawData[columns[i]] = asValue(string(value))
			}
			rowIndex++
		}
	}

	return rawData, nil
}
