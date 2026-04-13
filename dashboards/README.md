# MySQL Monitoring Dashboard

A comprehensive, multi-page New Relic dashboard for monitoring MySQL instances using data collected by nri-mysql. Covers availability, connections, query performance, InnoDB engine internals, and replication health — all from a single shared dashboard.

## Pages

The dashboard is organized into five pages, each focused on a different operational concern:

| Page | Event type | Focus |
|---|---|---|
| **Availability** | `MysqlHealthSample` | Uptime %, error classification, connection phase timing (DNS/TCP/TLS), fleet status |
| **Connections** | `MysqlSample` | Active threads, connection rate, aborted connections, network throughput, thread cache |
| **Queries** | `MysqlSample` | QPS by statement type, slow queries, temp tables, sort operations, table locks |
| **InnoDB** | `MysqlSample` | Buffer pool utilization, row lock contention, I/O throughput, redo log, pending I/O |
| **Replication** | `MysqlSample` | Replica lag, IO/SQL thread status, relay log space, replication errors |

Each page includes an information panel with metric explanations, troubleshooting guidance, and links to the relevant MySQL documentation.

## Why a shared dashboard?

Most teams end up building their own MySQL dashboards from scratch, duplicating effort and missing important metrics. This template encodes operational knowledge from supporting database-backed applications at scale:

- **One dashboard, many teams**: Filter variables let different teams view the same dashboard scoped to their instances, services, or environments — no need for per-team copies.
- **Labels as a filtering primitive**: Integration labels (`instance`, `role`, `service_name`, `environment`, `availability_zone`) flow through to New Relic as attributes, enabling powerful grouping and filtering. A single dashboard can serve a fleet of hundreds of instances across environments by leveraging these labels.
- **Operational context built in**: Each page's markdown panel explains what the metrics mean, what to look for, and how to troubleshoot — reducing the time from alert to diagnosis.

## Required integration flags

The **Availability** page requires opt-in flags. The other four pages (Connections, Queries, InnoDB, Replication) work with the default `MysqlSample` events that nri-mysql always collects.

```yaml
env:
  # Required for the Availability page
  COLLECT_CONNECTION_TIMING: "true"          # Implicit availability + DNS/TCP/TLS timing
  AVAILABILITY_CHECK_QUERY: "SELECT 1"       # Explicit canary query (optional)
  AVAILABILITY_CHECK_TIMEOUT_MS: "5000"      # Canary query timeout (optional)
  COLLECT_QUERY_TELEMETRY: "true"            # Per-query telemetry (optional)

  # Required for the InnoDB page
  EXTENDED_INNODB_METRICS: "true"            # Buffer pool, row locks, I/O, logging

  # Recommended for full coverage
  EXTENDED_METRICS: "true"                   # Handler, table cache, thread pool, sorts
```

## How to import

### Option 1: Sync script (recommended)

Use `sync-dashboard.sh` to create or update the dashboard via the NerdGraph API:

```bash
# First time — creates a new dashboard and prints the GUID:
NR_ACCOUNT_ID=12345 NR_API_KEY=NRAK-xxx ./sync-dashboard.sh

# Update an existing dashboard:
NR_DASHBOARD_GUID=MzA3... NR_ACCOUNT_ID=12345 NR_API_KEY=NRAK-xxx ./sync-dashboard.sh
```

| Variable | Required | Description |
|---|---|---|
| `NR_ACCOUNT_ID` | Yes | Your New Relic account ID |
| `NR_API_KEY` | Yes | User API key (`NRAK-...`), **not** the ingest license key |
| `NR_DASHBOARD_GUID` | No | Set to update an existing dashboard; omit to create new |

Generate a User API key at **New Relic > API Keys > Create a key > User key type**.

### Option 2: Manual import

1. Open `mysql-monitoring-template.json`
2. Find-and-replace `YOUR_ACCOUNT_ID` with your New Relic account ID
3. In New Relic: **Dashboards > Import dashboard** > paste the JSON
4. Use the filter variable dropdowns to scope to your instances

## Labels

Labels are the key to making a single dashboard work across teams, services, and environments. They are set in the integration config and flow through to New Relic as event attributes, enabling the dashboard's filter variables and FACET groupings.

```yaml
labels:
  instance: my-db-host          # Required — instance identifier
  role: primary                 # primary, replica, standalone
  service_name: my-service      # Service or application grouping
  availability_zone: us-east-1a # AZ grouping for regional views
  environment: production       # Environment tag for filtering
```

**How labels enable shared dashboards:**
- An on-call engineer filters to `environment: production` to see only prod instances
- A team lead filters to `service_name: payments-db` to see only their service's databases
- A DBA uses `role: replica` to check replication health across all replicas
- An incident responder uses `availability_zone: us-east-1a` to scope to an affected AZ

All from the same dashboard — no duplication, no maintenance overhead.

## Dashboard Variables

The template includes five filter variables in the top bar. All default to `*` (show everything).

| Variable | Populates from | Used on | Use case |
|---|---|---|---|
| `instance` | `label.instance` | All pages | Filter to specific MySQL instances |
| `service_name` | `label.service_name` | Availability | Filter by service grouping |
| `role` | `label.role` | Availability | Filter by primary / replica / standalone |
| `checkType` | `checkType` | Availability | Filter by implicit / explicit / query |
| `environment` | `label.environment` | Availability | Filter by environment |

The **Availability** page uses all five variables for fine-grained filtering. The other pages (Connections, Queries, InnoDB, Replication) filter by `instance` only to minimize dependencies on label configuration — they work out of the box even if no labels are set.

The variable dropdown queries respect the dashboard time picker, so they only show values from instances that reported data within the selected time window.

## Files

| File | Description |
|---|---|
| `mysql-monitoring-template.json` | Dashboard template with `YOUR_ACCOUNT_ID` placeholder |
| `sync-dashboard.sh` | NerdGraph create/update script |
| `README.md` | This file |

## Note

The canonical upstream location for New Relic dashboards is the [newrelic-quickstarts](https://github.com/newrelic/newrelic-quickstarts) repository. This template is provided here for convenience alongside the integration code.
