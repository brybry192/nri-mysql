# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Build binary to bin/nri-mysql
make compile

# Run unit tests
make test

# Run a single test
go test -race ./src/... -run TestName -count=1

# Run integration tests (requires Docker)
make test-integration

# Build + test (default)
make
```

## Architecture

**nri-mysql** is a New Relic Infrastructure integration that collects MySQL performance metrics and inventory data. It queries MySQL status/configuration variables and publishes them to New Relic.

### Main Flow

`mysql.go` (entry point) -> parse args -> open DB connection -> observability probes -> collect metrics -> publish to New Relic

1. **`src/args/`** -- CLI argument definitions (hostname, port, socket, credentials, TLS, feature flags)
2. **`src/dbutils/`** -- Builds MySQL DSN supporting TCP, Unix socket, TLS, old-password auth
3. **`src/database.go`** -- `dataSource` interface + `database` struct with optional timing/telemetry; `openDB()` creates connections with or without the timing dialer via `mysql.NewConnector`
4. **`src/metrics_parse.go`** -- Converts raw MySQL status strings to typed metric values (GAUGE/RATE/ATTRIBUTE)
5. **`src/default_metrics.go`**, **`src/extended_metrics.go`** -- Metric definitions mapping MySQL variable names to NR metric names and types
6. **`src/slave_metrics.go`** -- Replication/replica metrics (handles MySQL 8.4+ `Replica_` rename)
7. **`src/infrautils/`** -- New Relic SDK helpers for creating entities and metric sets
8. **`src/query-performance-monitoring/`** -- Optional module (flag: `EnableQueryMonitoring`) for slow queries, execution plans, wait events, blocking sessions

### Observability Layer (availability monitoring)

Opt-in features that emit `MysqlHealthSample` events. All disabled by default for backward compatibility. Every health sample carries a `checkType` attribute (`implicit`, `explicit`, or `query`) and a unified `hasError` / `errorCode` / `errorMessage` schema.

- **`src/timing.go`** -- `timingDialFunc` closure injected into `mysql.Config.DialFunc` to measure DNS lookup and TCP connect time separately. Fires on the first real connection (lazy pool).
- **`src/availability.go`** -- `explicitAvailabilityCheck()` runs a user-configured SQL query (e.g. `SELECT 1`) with a context deadline and returns a `checkResult` with availability, duration, and classified error.
- **`src/query_telemetry.go`** -- Per-query telemetry accumulator (thread-safe). `classifyError()` maps errors to structured codes (timeout, mysql_error_N, connection_refused, dns_resolution_failed, etc.). `sanitizeErrorMessage()` redacts DSN credentials from error strings before publishing.
- **`src/observability.go`** -- `ObservabilityConfig` struct and publishing functions: `publishImplicitHealthSample` (ping + DNS/TCP timing), `publishExplicitHealthSample` (canary query), `publishQueryHealthSamples` (per-query telemetry). All emit `MysqlHealthSample`.

**Two-tier availability model:**
- Implicit (`checkType=implicit`): `CollectConnectionTiming=true` triggers `db.ping()` (MySQL COM_PING protocol-level check)
- Explicit (`checkType=explicit`): `AvailabilityCheckQuery="SELECT 1"` runs a canary SQL query with timeout

**Observability flags:**

| Flag | Default | Purpose |
|---|---|---|
| `CollectConnectionTiming` | `false` | Ping + DNS/TCP timing + implicit availability |
| `AvailabilityCheckQuery` | `""` (disabled) | If set, runs this SQL as explicit canary check |
| `AvailabilityCheckTimeoutMs` | `5000` | Timeout for the explicit check |
| `CollectQueryTelemetry` | `false` | Per-query telemetry for internal monitoring queries |

### Key Patterns

- **`dataSource` interface** enables mock injection in unit tests -- all DB access goes through it
- **`go-sqlmock`** is used for unit testing database code; `testdb` struct for lightweight mock
- **Version-conditional metrics**: Query Cache metrics excluded for MySQL 8.0+; replica naming differs in 8.4+
- **Metric pipeline**: raw status vars -> map -> typed metrics -> New Relic SDK publish
- **Credential safety**: `sanitizeErrorMessage()` in `query_telemetry.go` redacts passwords from error messages before they reach New Relic. All error messages flow through `classifyError()` which applies this sanitization.
- **Local vs remote entities**: local entities have nil `Metadata` -- always guard access (see `healthSampleAttrs` pattern in `observability.go`)

### Testing

- Unit tests use `go-sqlmock` and `testify/assert`. Run with `-race` flag.
- Integration tests in `tests/integration/` use Docker Compose with MySQL 5.7, 8.0, and 9.1. The binary runs inside the `nri-mysql` container and connects to MySQL containers by hostname within the Docker network. Output is validated against JSON schemas in `tests/integration/json-schema-files-*/`.
- MySQL 5.7 containers need `platform: linux/amd64` on Apple Silicon Macs.

### PR.md

- Do not include a "Commit Messages" section — commits are already visible in the PR.
- The `## Availability Test Results` section is auto-populated by `tests/e2e/test-availability.sh`. Do not write it manually; run `make test-availability` or `make test-chaos` to populate it.
- The auto-populate script replaces content between `## Availability Test Results` and the next `## ` heading. Ensure there is always a blank line and `---` separator after the closing `</details>` tag so markdown renders correctly.
- Include a `### Change Breakdown` table in the Summary section showing lines added/deleted by category (Go source, Go tests, E2E infrastructure, dashboard/docs, build/config).
- Include a `### References` section with links to upstream MySQL docs, the sister nri-postgresql PR, New Relic docs, and the go-sql-driver repo.
- **After every commit**, refresh PR.md: recalculate the `### Change Breakdown` table using `git diff --numstat master..HEAD`, update totals and the summary line. Do not leave stale stats from previous commits.

### Reference

- nri-postgresql availability monitoring PR: https://github.com/brybry192/nri-postgresql/pull/1
