# nri-mysql E2E Validation

End-to-end testing environment for the availability monitoring features on the
`feat/availability-monitoring` branch. Runs the New Relic Infrastructure Agent
and a local MySQL instance entirely in Docker.

## What We're Validating

| Flag | What it reports | Event type |
|---|---|---|
| `COLLECT_CONNECTION_TIMING` | DNS lookup + TCP connect time + response time | `MysqlConnectionSample` |
| `AVAILABILITY_CHECK_QUERY` | Canary query result + duration + error code | `MysqlConnectionSample` (checkType=explicit) |
| `AVAILABILITY_CHECK_TIMEOUT_MS` | Bounds the canary query so it can't block the next cycle | -- |
| `COLLECT_QUERY_TELEMETRY` | Per-internal-query duration + error code | `MysqlQueryTelemetrySample` |

---

## Prerequisites

- **Docker Desktop** or **Rancher Desktop** running
- **New Relic account** with a license key
- The `feat/availability-monitoring` branch checked out

---

## Local MySQL Testing

### 1. First-time setup

```bash
cd tests/e2e

# Copy the env template
make setup
```

Set your license key as an environment variable (preferred):
```bash
export NR_LICENSE_KEY=your_license_key_here
```

### 2. Start the stack

```bash
make up
```

This will:
- Build the infra agent image (compiles `nri-mysql` from the current branch)
- Start a MySQL 8.0 container seeded with test tables and data
- Start the New Relic Infrastructure Agent, running the integration every 30s

### 3. Verify output immediately

```bash
make run-once
```

Look for:
- `"event_type": "MysqlConnectionSample"` -- appears twice per cycle:
  once without `checkType` (implicit) and once with `"checkType": "explicit"`
- `"db.available": 1` in the implicit sample
- `"db.responseTimeMs"` -- total response time
- `"db.connection.dnsLookupMs"` and `"db.connection.tcpConnectMs"` (timing)
- `"db.availabilityCheck.available": 1` in the explicit sample
- `"event_type": "MysqlQueryTelemetrySample"` -- one per internal query

### 4. Watch agent logs

```bash
make logs
```

### 5. Query in New Relic

Allow 1-2 minutes for data to appear, then open **Query your data** (NRQL).

#### Implicit availability signal

```sql
FROM MysqlConnectionSample
SELECT db.available, db.responseTimeMs, db.connection.dnsLookupMs, db.connection.tcpConnectMs
WHERE checkType IS NULL
SINCE 10 minutes ago
LIMIT 20
```

#### Explicit availability check

```sql
FROM MysqlConnectionSample
SELECT
  db.availabilityCheck.available,
  db.availabilityCheck.durationMs,
  db.availabilityCheck.query,
  db.availabilityCheck.errorCode
WHERE checkType = 'explicit'
SINCE 10 minutes ago
LIMIT 20
```

#### Query telemetry

```sql
FROM MysqlQueryTelemetrySample
SELECT queryName, durationMs, hasError, errorCode
SINCE 10 minutes ago
LIMIT 50
```

#### Availability over time (alerting use case)

```sql
FROM MysqlConnectionSample
SELECT latest(db.availabilityCheck.available)
WHERE checkType = 'explicit'
TIMESERIES 1 minute
SINCE 30 minutes ago
```

### 6. Tear down

```bash
make down
```

---

## Simulating Failure Scenarios

### Connection failure

Stop MySQL while the agent is running:

```bash
docker compose stop mysql
```

Wait ~30s, then query:

```sql
FROM MysqlConnectionSample
SELECT db.available, db.connection.errorCode
WHERE checkType IS NULL
SINCE 5 minutes ago
ORDER BY timestamp DESC
LIMIT 5
```

**Expected:** `db.available = 0` with `errorCode` like `connection_refused`.

Restart when done:
```bash
docker compose start mysql
```

### Timeout scenario

Run manually with a sleep query and tight timeout:

```bash
docker compose exec newrelic-infra \
  /var/db/newrelic-infra/newrelic-integrations/bin/nri-mysql \
  -username root -password e2e_test_password \
  -hostname mysql -port 3306 -database demo \
  -availability_check_query="SELECT SLEEP(10)" \
  -availability_check_timeout_ms=500 \
  | python3 -m json.tool
```

**Expected:** `db.availabilityCheck.errorCode = "timeout"` and returns in ~500ms.

---

## RDS / Aurora MySQL Testing

### Setup

Set RDS credentials as environment variables or add to `.env`:

```bash
export RDS_HOST=your-cluster.cluster-xxxx.us-east-1.rds.amazonaws.com
export RDS_PORT=3306
export RDS_USER=your_db_user
export RDS_PASSWORD=your_db_password
export RDS_DB=mysql
export RDS_ENABLE_TLS=true
```

### Start

```bash
make up-rds
```

Starts only the infra agent (no local MySQL). Uses `config/integrations.d/mysql-rds.yml`.

### Tear down

```bash
make down-rds
```

---

## After Source Changes

```bash
make rebuild
```

Forces a clean image rebuild and restarts the stack.

---

## File Reference

```
tests/e2e/
├── .env.example                    <- template; copy to .env
├── .env                            <- your secrets (gitignored)
├── .gitignore
├── Makefile                        <- convenience targets (make help)
├── Dockerfile.agent                <- builds infra agent + custom nri-mysql
├── docker-compose.yml              <- local mysql + infra agent
├── docker-compose.rds.yml          <- override for RDS/Aurora testing
├── config/
│   ├── newrelic-infra.yml          <- minimal agent config
│   └── integrations.d/
│       ├── mysql-local.yml         <- all new flags enabled, targets local mysql
│       └── mysql-rds.yml           <- RDS config, reads from env vars
├── init/
│   └── 01-schema.sql              <- test tables + seed data
└── README.md
```
