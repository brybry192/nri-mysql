package main

import (
	"context"
	"fmt"
	"time"

	"github.com/newrelic/infra-integrations-sdk/v3/log"
)

// checkResult holds the outcome of an explicit availability check.
type checkResult struct {
	available    bool
	durationMs   float64
	errorCode    string
	errorMessage string
	query        string
}

// explicitAvailabilityCheck runs the given SQL query against db and returns a checkResult.
// The context should carry a deadline so the check is bounded. A non-error result
// with at least one row is considered available.
//
// Before running the canary query, a SET SESSION max_execution_time is issued so
// MySQL will kill the query server-side if it exceeds the timeout. This prevents
// orphaned queries when the client context is cancelled but the server continues
// executing.
func explicitAvailabilityCheck(ctx context.Context, db *database, query string, timeoutMs int) *checkResult {
	result := &checkResult{query: query}

	// Set server-side execution timeout (MySQL 5.7.8+). Best-effort: older
	// versions or non-SELECT statements will ignore this, but the client-side
	// context deadline still provides a hard bound.
	if timeoutMs > 0 {
		_, err := db.source.ExecContext(ctx, fmt.Sprintf("SET SESSION max_execution_time = %d", timeoutMs))
		if err != nil {
			log.Debug("Could not set max_execution_time (MySQL <5.7.8?): %v", err)
		}
	}

	start := time.Now()
	rows, err := db.queryContext(ctx, query)
	result.durationMs = msec(time.Since(start))

	if err != nil {
		result.available = false
		// Prefer the context error when present: database/sql may return an opaque
		// "canceling query" string rather than wrapping context.DeadlineExceeded.
		if ctxErr := ctx.Err(); ctxErr != nil {
			result.errorCode, result.errorMessage = classifyError(ctxErr)
		} else {
			result.errorCode, result.errorMessage = classifyError(err)
		}
		return result
	}
	defer rows.Close()

	result.available = rows.Next()
	if rowErr := rows.Err(); rowErr != nil {
		result.available = false
		result.errorCode, result.errorMessage = classifyError(rowErr)
	}

	return result
}
