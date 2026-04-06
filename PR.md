## Summary

Add opt-in connection observability and availability monitoring to nri-mysql, following the pattern established in [nri-postgresql PR #1](https://github.com/brybry192/nri-postgresql/pull/1).

**Key changes:**

- **`MysqlHealthSample` event** with `checkType` attribute (`implicit`, `explicit`, `query`) and consistent `hasError` / `errorCode` / `errorMessage` schema across all check types.
- **Two-tier availability model**: Implicit availability via MySQL `COM_PING` (lightweight, protocol-level), or explicit via a configurable canary SQL query (`AvailabilityCheckQuery`) that exercises the full query path.
- **Connection timing** (`CollectConnectionTiming`): Custom `DialFunc` injected via `mysql.NewConnector` measures DNS lookup, TCP connect, and TLS handshake time independently.
- **TLS handshake timing** (`tlsHandshakeMs`): `VerifyConnection` callback on the `tls.Config` captures TLS handshake duration when TLS is enabled. Non-TLS connections report 0.
- **Query telemetry** (`CollectQueryTelemetry`): Per-query duration and structured error classification for all internal monitoring queries.
- **Credential sanitization**: `sanitizeErrorMessage()` redacts DSN passwords from error messages before publishing to New Relic.
- **Error resilience**: Health samples are published even when `getRawData` fails, so availability signals are delivered regardless of metric collection errors.
- **Ping timeout**: 5-second context deadline on implicit ping prevents agent from blocking indefinitely against a frozen server.
- **New Relic dashboard**: Multi-page MySQL Monitoring dashboard covering availability, connections, query performance, InnoDB internals, and replication health. Includes filter variables, operational context in each page's info panel, and links to MySQL documentation.
- **E2E test environment**: Docker Compose stack with primary, replica, and standalone MySQL instances. TLS enabled on primary/replica (MySQL 8.0 auto-generated certs), plain TCP on inventorydb for comparison. RDS/Aurora override support.
- **Chaos test suite**: Automated failure scenarios with NerdGraph verification of all 5 error codes.

All features default to disabled. No behavioral change when flags are not set.

### Change Breakdown

| Category | Files | Lines added | Lines deleted |
|---|---|---|---|
| Go source (non-test) | 7 | +592 | -27 |
| Go tests | 6 | +923 | -0 |
| E2E test infrastructure | 22 | +1,630 | -3 |
| Dashboard & docs | 3 | +2,120 | -0 |
| Build & config | 1 | +21 | -5 |
| **Total** | **39** | **+5,286** | **-35** |

The majority of the diff is test infrastructure and dashboard JSON. Core Go changes are ~592 lines of source + ~923 lines of tests.

### References

- [nri-postgresql availability monitoring PR](https://github.com/brybry192/nri-postgresql/pull/1) — sister implementation this PR follows
- [MySQL Server Error Reference](https://dev.mysql.com/doc/mysql-errors/8.0/en/server-error-reference.html) — upstream error codes mapped by `classifyError()`
- [MySQL Encrypted Connections](https://dev.mysql.com/doc/refman/8.0/en/encrypted-connections.html) — TLS configuration reference
- [MySQL COM_PING protocol](https://dev.mysql.com/doc/dev/mysql-server/latest/page_protocol_com_ping.html) — the implicit availability check method
- [New Relic MySQL integration docs](https://docs.newrelic.com/docs/infrastructure/host-integrations/host-integrations-list/mysql/mysql-integration/) — official integration guide
- [NRQL reference](https://docs.newrelic.com/docs/nrql/get-started/introduction-nrql-new-relics-query-language/) — for querying `MysqlHealthSample` events
- [go-sql-driver/mysql](https://github.com/go-sql-driver/mysql) — MySQL driver used for `NewConnector`, `ParseDSN`, TLS config

---

## Dashboard

Previously, nri-mysql shipped no example dashboards — customers had to build their own from scratch, often missing important metrics or lacking operational context. This PR includes a comprehensive, multi-page dashboard template (`dashboards/mysql-monitoring-template.json`) that encodes operational knowledge built from years of supporting database-backed applications.

The dashboard is designed as a **single shared resource** that multiple teams can use simultaneously. Integration labels (`instance`, `role`, `service_name`, `environment`, `availability_zone`) flow through to New Relic as event attributes, powering the dashboard's filter variables. An on-call engineer can filter to production, a team lead can scope to their service, and a DBA can view all replicas — all from the same dashboard, with no duplication or per-team maintenance.

Each page includes an information panel with metric explanations, troubleshooting guidance, and links to the relevant MySQL documentation, reducing the time from alert to diagnosis.

| Page | Focus |
|---|---|
| **Availability** | Uptime %, error classification, connection phase timing (DNS/TCP/TLS), fleet status |
| **Connections** | Active threads, connection rate, aborted connections, network throughput, thread cache |
| **Queries** | QPS by statement type, slow queries, temp tables, sort operations, table locks |
| **InnoDB** | Buffer pool utilization, row lock contention, I/O throughput, redo log, pending I/O |
| **Replication** | Replica lag, IO/SQL thread status, relay log space, replication errors |

<!-- Screenshots of each page will be added here -->

---

## New Configuration Flags

| Flag | Default | Description |
|---|---|---|
| `COLLECT_CONNECTION_TIMING` | `false` | Ping + DNS/TCP/TLS timing + implicit availability |
| `AVAILABILITY_CHECK_QUERY` | `""` (disabled) | SQL canary query for explicit availability check |
| `AVAILABILITY_CHECK_TIMEOUT_MS` | `5000` | Timeout for explicit availability check (ms) |
| `COLLECT_QUERY_TELEMETRY` | `false` | Per-query telemetry for internal monitoring queries |

---

## `MysqlHealthSample` Schema

All health samples share a unified schema:

| Attribute | Type | Description |
|---|---|---|
| `checkType` | string | `implicit`, `explicit`, or `query` |
| `available` | gauge (0/1) | Point-in-time availability signal |
| `hasError` | gauge (0/1) | Single field to find all errors |
| `errorCode` | string | Classified error type (e.g. `connection_refused`, `timeout`, `dns_resolution_failed`) |
| `errorMessage` | string | Sanitized error detail (credentials redacted) |
| `durationMs` | gauge | Execution time for the check |
| `dnsLookupMs` | gauge | DNS resolution time (implicit/explicit only) |
| `tcpConnectMs` | gauge | TCP handshake time (implicit/explicit only) |
| `tlsHandshakeMs` | gauge | TLS handshake time (0 when TLS is not configured) |
| `queryName` | string | Internal query identifier (query checks only) |
| `query` | string | Canary SQL text (explicit checks only) |

---

## Error Codes

| errorCode | Trigger |
|---|---|
| `dns_resolution_failed` | DNS lookup failure |
| `connection_refused` | TCP connection rejected |
| `timeout` | Connection or query timeout |
| `mysql_error_1045` | Access denied (wrong credentials) |
| `invalid_connection` | Server-side connection kill |
| `tls_error` | TLS handshake failure |
| `connection_reset` | TCP RST / connection reset |
| `server_closed_connection` | Server closed connection unexpectedly |
| `unknown_error` | Unclassified errors |

---

## New Files

| File | Purpose |
|---|---|
| `src/timing.go` | `timingDialFunc` closure measuring DNS + TCP phases; `wrapTLSConfig` for TLS handshake timing |
| `src/availability.go` | `explicitAvailabilityCheck` with context deadline |
| `src/query_telemetry.go` | Error classification, query name extraction, credential sanitization, thread-safe telemetry accumulator |
| `src/observability.go` | `ObservabilityConfig`, unified `MysqlHealthSample` publishing functions |
| `src/timing_test.go` | Unit tests for timing dial func and TLS handshake measurement |
| `src/availability_test.go` | Unit tests for explicit availability check |
| `src/query_telemetry_test.go` | Unit tests for error classification, sanitization, telemetry |
| `src/observability_test.go` | Unit tests for health sample publishing |
| `src/database_test.go` | Unit tests for database layer with telemetry |
| `tests/integration/availability_test.go` | Integration tests for availability monitoring |
| `tests/e2e/` | Full E2E Docker Compose stack (primary + replica + inventorydb) |
| `dashboards/mysql-monitoring-template.json` | Dashboard template (`YOUR_ACCOUNT_ID` placeholder) |
| `dashboards/sync-dashboard.sh` | NerdGraph create/update script |
| `dashboards/README.md` | Dashboard usage docs |

---

## Modified Files

| File | Changes |
|---|---|
| `src/args/argument_list.go` | 4 new flags |
| `src/database.go` | `openDB()` with optional timing dialer + TLS timing hook, telemetry wrapping, `ping()` with 5s timeout, `queryContext()`, `drainTelemetry()` |
| `src/mysql.go` | Observability probe orchestration, error resilience on `getRawData` failure |
| `Makefile` | Additional test targets (`test-verbose`, `test-coverage`, `test-integration`) |
| `tests/integration/README.md` | Testing instructions for availability monitoring |
| `tests/integration/docker-compose.yml` | `platform: linux/amd64` for MySQL 5.7 on ARM Macs |
| `tests/integration/mysql/versions/5.7.35/MasterDockerfile` | Updated for ARM compat |
| `tests/integration/mysql/versions/5.7.35/SlaveDockerfile` | Updated for ARM compat |

---

## E2E Test Environment

Three MySQL instances with different configurations to exercise all code paths:

| Instance | Role | TLS | Availability Check |
|---|---|---|---|
| `e2e-mysql-1` | primary | Yes (auto-generated certs) | Explicit (`SELECT 1`) |
| `e2e-mysql-2` | replica | Yes (auto-generated certs) | Implicit (ping only) |
| `e2e-inventorydb` | standalone | No (plain TCP baseline) | Explicit (`SELECT 1`) |

The `make test-chaos` target runs the full lifecycle: stack up, 10-min baseline, chaos tests, 10-min cool-down, stack down. Requires `NR_LICENSE_KEY`, `NR_ACCOUNT_ID`, and `NR_API_KEY`.

---

## Test Plan

- [ ] `make test` -- all unit tests pass with `-race`
- [ ] Verify no flags set -- no `MysqlHealthSample` emitted (backward compat)
- [ ] `-collect_connection_timing` against reachable MySQL -- `available=1`, `dnsLookupMs`/`tcpConnectMs`/`tlsHandshakeMs` present
- [ ] `-collect_connection_timing` against unreachable host -- `available=0`, `errorCode`/`errorMessage` present
- [ ] `-availability_check_query "SELECT 1"` -- explicit check with `checkType=explicit`
- [ ] `-collect_query_telemetry` -- per-query `MysqlHealthSample` with `checkType=query` and `queryName`
- [ ] `make test-integration` -- Docker integration suite passes
- [ ] E2E: `cd tests/e2e && make up && make run-once` -- full JSON output with all health samples
- [ ] E2E: `make test-availability` -- chaos test validates all 5 error codes via NerdGraph
- [ ] E2E: Compare TLS vs non-TLS response times on dashboard (inventorydb should show 0 for `tlsHandshakeMs`)
- [ ] Credential sanitization: connect with password, force error, confirm password not in output
- [ ] Import dashboard template -- verify billboard thresholds, filter variables, and TLS timing charts

---

## Availability Test Results

_Last run: 2026-04-05 19:37:07 (normal mode, 3 cycle(s) per phase)_

---

| Test | Expected Error Code | Method |
|---|---|---|
| 1 | `dns_resolution_failed` | Container stop (replica) |
| 2 | `connection_refused` | Port reject / iptables (primary) |
| 3 | `timeout` | Container pause (replica) |
| 4 | `mysql_error_1045` | Password change (primary) |
| 5 | `invalid_connection` | KILL connection (slowcheck) |

---

<details>

<summary>Full test output (click to expand)</summary>

```
=== MySQL Availability Chaos Test ===

Start time:          2026-04-06T02:30:29Z
Collection interval: 10s
Failure hold:        30s (3 cycles)
Stabilize hold:      30s (3 cycles)

Watch your New Relic dashboard — events will appear in real time.

================================================================
  [2026-04-06T02:30:29Z] Phase 0: Baseline — confirming both instances are healthy
================================================================
  e2e-mysql-1: Up 48 minutes (healthy)
  e2e-mysql-2: Up 48 minutes (healthy)
  Holding for 30s (baseline collection)...
   30s remaining    29s remaining    28s remaining    27s remaining    26s remaining    25s remaining    24s remaining    23s remaining    22s remaining    21s remaining    20s remaining    19s remaining    18s remaining    17s remaining    16s remaining    15s remaining    14s remaining    13s remaining    12s remaining    11s remaining    10s remaining     9s remaining     8s remaining     7s remaining     6s remaining     5s remaining     4s remaining     3s remaining     2s remaining     1s remaining   Done.                

================================================================
  [2026-04-06T02:30:59Z] Test 1/5: Container stop — replica (DNS resolution failure)
================================================================
  Stopping e2e-mysql-2...
 Container e2e-mysql-2 Stopping 
 Container e2e-mysql-2 Stopped 
  Container stopped. Agent should report dns_resolution_failed for e2e-mysql-2.
  Holding for 30s (failure detection)...
   30s remaining    29s remaining    28s remaining    27s remaining    26s remaining    25s remaining    24s remaining    23s remaining    22s remaining    21s remaining    20s remaining    19s remaining    18s remaining    17s remaining    16s remaining    15s remaining    14s remaining    13s remaining    12s remaining    11s remaining    10s remaining     9s remaining     8s remaining     7s remaining     6s remaining     5s remaining     4s remaining     3s remaining     2s remaining     1s remaining   Done.                
  Restoring e2e-mysql-2...
 Container e2e-mysql-1 Waiting 
 Container e2e-mysql-1 Healthy 
 Container e2e-mysql-2 Starting 
 Container e2e-mysql-2 Started 
  Waiting for e2e-mysql-2 to be healthy (max 60s)...
  e2e-mysql-2 is healthy.
  Holding for 30s (recovery stabilization)...
   30s remaining    29s remaining    28s remaining    27s remaining    26s remaining    25s remaining    24s remaining    23s remaining    22s remaining    21s remaining    20s remaining    19s remaining    18s remaining    17s remaining    16s remaining    15s remaining    14s remaining    13s remaining    12s remaining    11s remaining    10s remaining     9s remaining     8s remaining     7s remaining     6s remaining     5s remaining     4s remaining     3s remaining     2s remaining     1s remaining   Done.                

================================================================
  [2026-04-06T02:32:07Z] Test 2/5: Port reject — primary (connection refused)
================================================================
  Installing iptables in e2e-mysql-1 (if needed)...
  Blocking port 3306 with iptables REJECT (TCP RST)...
  Port blocked. Agent should report connection_refused for e2e-mysql-1.
  Holding for 30s (failure detection)...
   30s remaining    29s remaining    28s remaining    27s remaining    26s remaining    25s remaining    24s remaining    23s remaining    22s remaining    21s remaining    20s remaining    19s remaining    18s remaining    17s remaining    16s remaining    15s remaining    14s remaining    13s remaining    12s remaining    11s remaining    10s remaining     9s remaining     8s remaining     7s remaining     6s remaining     5s remaining     4s remaining     3s remaining     2s remaining     1s remaining   Done.                
  Removing iptables rule...
  Port unblocked.
  Holding for 30s (recovery stabilization)...
   30s remaining    29s remaining    28s remaining    27s remaining    26s remaining    25s remaining    24s remaining    23s remaining    22s remaining    21s remaining    20s remaining    19s remaining    18s remaining    17s remaining    16s remaining    15s remaining    14s remaining    13s remaining    12s remaining    11s remaining    10s remaining     9s remaining     8s remaining     7s remaining     6s remaining     5s remaining     4s remaining     3s remaining     2s remaining     1s remaining   Done.                

================================================================
  [2026-04-06T02:33:31Z] Test 3/5: Container pause — replica (connection hang / timeout)
================================================================
  Pausing e2e-mysql-2 (SIGSTOP — process frozen, TCP stays open)...
 Container e2e-mysql-2 Paused 
  Container paused. Agent should report timeout for e2e-mysql-2.
  Holding for 30s (failure detection)...
   30s remaining    29s remaining    28s remaining    27s remaining    26s remaining    25s remaining    24s remaining    23s remaining    22s remaining    21s remaining    20s remaining    19s remaining    18s remaining    17s remaining    16s remaining    15s remaining    14s remaining    13s remaining    12s remaining    11s remaining    10s remaining     9s remaining     8s remaining     7s remaining     6s remaining     5s remaining     4s remaining     3s remaining     2s remaining     1s remaining   Done.                
  Unpausing e2e-mysql-2...
 Container e2e-mysql-2 Unpaused 
  Container resumed.
  Holding for 30s (recovery stabilization)...
   30s remaining    29s remaining    28s remaining    27s remaining    26s remaining    25s remaining    24s remaining    23s remaining    22s remaining    21s remaining    20s remaining    19s remaining    18s remaining    17s remaining    16s remaining    15s remaining    14s remaining    13s remaining    12s remaining    11s remaining    10s remaining     9s remaining     8s remaining     7s remaining     6s remaining     5s remaining     4s remaining     3s remaining     2s remaining     1s remaining   Done.                

================================================================
  [2026-04-06T02:34:31Z] Test 4/5: Password change — primary (authentication failure)
================================================================
  Changing root password on e2e-mysql-1...
  Password changed. Agent should report mysql_error_1045 for e2e-mysql-1.
  Holding for 30s (failure detection)...
   30s remaining    29s remaining    28s remaining    27s remaining    26s remaining    25s remaining    24s remaining    23s remaining    22s remaining    21s remaining    20s remaining    19s remaining    18s remaining    17s remaining    16s remaining    15s remaining    14s remaining    13s remaining    12s remaining    11s remaining    10s remaining     9s remaining     8s remaining     7s remaining     6s remaining     5s remaining     4s remaining     3s remaining     2s remaining     1s remaining   Done.                
  Restoring root password on e2e-mysql-1...
  Password restored.
  Holding for 30s (recovery stabilization)...
   30s remaining    29s remaining    28s remaining    27s remaining    26s remaining    25s remaining    24s remaining    23s remaining    22s remaining    21s remaining    20s remaining    19s remaining    18s remaining    17s remaining    16s remaining    15s remaining    14s remaining    13s remaining    12s remaining    11s remaining    10s remaining     9s remaining     8s remaining     7s remaining     6s remaining     5s remaining     4s remaining     3s remaining     2s remaining     1s remaining   Done.                

================================================================
  [2026-04-06T02:35:31Z] Test 5/5: Kill connection — on-demand BENCHMARK (server-side termination)
================================================================

  Injecting slowcheck config, restarting agent...
  Expected error: invalid_connection

 Container mysql-e2e-newrelic-infra Restarting 
 Container mysql-e2e-newrelic-infra Started 
  Agent restarted. Waiting for first BENCHMARK cycle...
  Round 1/3: waiting for BENCHMARK query...
  Found BENCHMARK query on process 4886 — killing connection...
  Connection killed. Agent should report invalid_connection.
  Waiting for next collection cycle...
  Round 2/3: waiting for BENCHMARK query...
  Found BENCHMARK query on process 4896 — killing connection...
  Connection killed. Agent should report invalid_connection.
  Waiting for next collection cycle...
  Round 3/3: waiting for BENCHMARK query...
  Found BENCHMARK query on process 4912 — killing connection...
  Connection killed. Agent should report invalid_connection.
  Removing slowcheck config and restarting agent...
 Container mysql-e2e-newrelic-infra Restarting 
 Container mysql-e2e-newrelic-infra Started 
  Holding for 30s (recovery stabilization)...
   30s remaining    29s remaining    28s remaining    27s remaining    26s remaining    25s remaining    24s remaining    23s remaining    22s remaining    21s remaining    20s remaining    19s remaining    18s remaining    17s remaining    16s remaining    15s remaining    14s remaining    13s remaining    12s remaining    11s remaining    10s remaining     9s remaining     8s remaining     7s remaining     6s remaining     5s remaining     4s remaining     3s remaining     2s remaining     1s remaining   Done.                

================================================================
  [2026-04-06T02:36:36Z] Chaos test complete
================================================================

  Start:    2026-04-06T02:30:29Z
  End:      2026-04-06T02:36:36Z
  Duration: 6m 7s

  Scenarios executed:

  Test  Expected Error Code              Method
  ────  ─────────────────────────────    ──────────────────────────────
  1     dns_resolution_failed            Container stop (replica)
  2     connection_refused               Port reject / iptables (primary)
  3     timeout                          Container pause (replica)
  4     mysql_error_1045                 Password change (primary)
  5     invalid_connection               KILL connection (slowcheck)


================================================================
  [2026-04-06T02:36:36Z] Verifying results via NerdGraph
================================================================
  Waiting 30s for NR ingest pipeline...

  Verification NRQL:
    SELECT count(*) FROM MysqlHealthSample WHERE hasError = 1 FACET errorCode SINCE '2026-04-06T02:30:29Z' LIMIT 20


  Error Code                    Count   Status
  ─────────────────────────────────────────────
  mysql_error_1045                    6   PASS
  invalid_connection                  6   PASS
  connection_refused                  5   PASS
  dns_resolution_failed               2   PASS
  timeout                             1   PASS

  Result: All expected error codes found.
```

</details>

## Code Coverage

**Overall package coverage: 74.3%**

| File | Function | Coverage |
|---|---|---|
| `availability.go` | `explicitAvailabilityCheck` | 100% |
| `query_telemetry.go` | `sanitizeErrorMessage` | 100% |
| `query_telemetry.go` | `extractQueryName` | 100% |
| `query_telemetry.go` | `classifyError` | 100% |
| `query_telemetry.go` | `record` / `drain` | 100% |
| `observability.go` | `healthSampleAttrs` | 100% |
| `observability.go` | `publishImplicitHealthSample` | 100% |
| `observability.go` | `publishExplicitHealthSample` | 100% |
| `observability.go` | `publishQueryHealthSamples` | 100% |
| `timing.go` | `timingDialFunc` | 94.1% |
| `timing.go` | `wrapTLSConfig` | 100% |
| `timing.go` | `msec` | 100% |
| `database.go` | `openDB` | 88.2% |
| `database.go` | `query` / `queryInternal` | 100% / 82.8% |
| `database.go` | `openSQLDB` / `close` / `queryContext` / `drainTelemetry` | 100% |
| `database.go` | `ping` | 0% (one-liner, requires real DB) |
| `observability.go` | `setGauge` / `setAttribute` | 50% (error branch unreachable with valid metric types) |

New code averages **96%+ coverage** on core logic. Below-80% items are either trivial one-liners (`ping`) or SDK error branches that cannot be triggered with valid inputs.
