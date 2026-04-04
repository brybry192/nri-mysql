# MySQL Availability Dashboard

Pre-built New Relic dashboard for visualizing `MysqlHealthSample` events emitted by the nri-mysql availability monitoring features.

![MySQL Availability Dashboard](MySQL-Availability-Dashboard.png)

## What it shows

| Section | Description |
|---|---|
| **Uptime %** | Percentage of successful availability checks over time, faceted by instance |
| **Error Rate** | Health check error rate across all check types |
| **Fleet Status** | Total instances reporting, how many are up vs down |
| **Response Time** | Connection phase breakdown: DNS resolution, TCP handshake, total duration |
| **Errors Over Time** | Error count by classified error code |
| **Error Breakdown** | Pie chart of error distribution |
| **Recent Errors** | Table of latest errors with code, message, instance, and check type |
| **Health Check Config** | Active check types and canary queries per instance |
| **Availability by AZ / Service** | Uptime grouped by availability zone or service name |
| **Query Performance** | Slowest internal monitoring queries and duration trends |

## Required integration flags

Enable these in your nri-mysql configuration to populate the dashboard:

```yaml
env:
  COLLECT_CONNECTION_TIMING: "true"          # Implicit availability + DNS/TCP timing
  AVAILABILITY_CHECK_QUERY: "SELECT 1"       # Explicit canary query (optional)
  AVAILABILITY_CHECK_TIMEOUT_MS: "5000"      # Canary query timeout (optional)
  COLLECT_QUERY_TELEMETRY: "true"            # Per-query telemetry (optional)
```

At minimum, `COLLECT_CONNECTION_TIMING` must be enabled. The other flags add additional dashboard sections.

## How to import

1. Open `mysql-availability-template.json`
2. Find-and-replace `YOUR_ACCOUNT_ID` with your New Relic account ID (14 occurrences)
3. In New Relic: **Dashboards > Import dashboard** > paste the JSON
4. Use the **Instance** variable dropdown to filter by specific MySQL instances

## Labels

The dashboard filters and facets use integration labels. Add these to your integration config for full functionality:

```yaml
labels:
  instance: my-db-host          # Required — instance identifier
  role: primary                 # primary, replica, standalone
  service_name: my-service      # Service grouping
  availability_zone: us-east-1a # AZ grouping
  environment: production       # Environment tag
```

## Note

The canonical upstream location for New Relic dashboards is the [newrelic-quickstarts](https://github.com/newrelic/newrelic-quickstarts) repository. This template is provided here for convenience alongside the integration code.
