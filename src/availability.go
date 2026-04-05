package main

import (
	"context"
	"time"
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
func explicitAvailabilityCheck(ctx context.Context, db *database, query string) *checkResult {
	result := &checkResult{query: query}

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
