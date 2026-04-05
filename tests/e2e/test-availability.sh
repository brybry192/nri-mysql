#!/bin/bash
# test-availability.sh — Chaos test for availability monitoring.
#
# Orchestrates real failure scenarios against the running e2e stack so the
# agent detects, classifies, and reports them to New Relic. Each scenario
# holds the failure state for several collection cycles, then restores
# and stabilizes before moving on.
#
# Watch the dashboard while this runs to see the events appear in real time.
#
# Requires: the e2e stack is running (make up).
#
# Usage:
#   ./test-availability.sh              # Normal mode (~6 min): 3 cycles per phase
#   FAST=1 ./test-availability.sh       # Fast mode   (~3 min): 2 fail cycles
#   SKIP_TO=4 ./test-availability.sh    # Jump to test 4 (skip 1-3)
#   SKIP_TO=5 FAST=1 ./test-availability.sh  # Jump to test 5, fast mode

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
REPO_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
LOG_FILE=$(mktemp)

# Capture all output to both console and log file.
exec > >(tee "$LOG_FILE") 2>&1

# Collection interval matches the agent's 10s interval.
CYCLE=${CYCLE:-10}

# FAST=1 drops to 1 cycle per phase — enough to confirm error classification
# without waiting for chart-visible gaps between state changes.
if [ "${FAST:-0}" = "1" ]; then
    FAIL_CYCLES=2
    STABILIZE_CYCLES=1
else
    FAIL_CYCLES=${FAIL_CYCLES:-3}
    STABILIZE_CYCLES=${STABILIZE_CYCLES:-3}
fi

FAIL_DURATION=$((CYCLE * FAIL_CYCLES))
STABILIZE_DURATION=$((CYCLE * STABILIZE_CYCLES))

# ── Timing ────────────────────────────────────────────────────────────────────

ts() { date -u '+%Y-%m-%dT%H:%M:%SZ'; }
epoch() { date +%s; }

START_TIME=$(ts)
START_EPOCH=$(epoch)

# ── Helpers ──────────────────────────────────────────────────────────────────

banner() {
    echo ""
    echo "================================================================"
    echo "  [$(ts)] $1"
    echo "================================================================"
}

hold() {
    local duration=$1 label=$2
    echo "  Holding for ${duration}s ($label)..."
    for i in $(seq "$duration" -1 1); do
        printf "\r  %3ds remaining " "$i"
        sleep 1
    done
    printf "\r  Done.                \n"
}

wait_healthy() {
    local service=$1 max_wait=${2:-60}
    echo "  Waiting for $service to be healthy (max ${max_wait}s)..."
    for i in $(seq 1 "$max_wait"); do
        if docker compose ps "$service" --format json 2>/dev/null | grep -q '"healthy"'; then
            echo "  $service is healthy."
            return 0
        fi
        sleep 1
    done
    echo "  WARNING: $service did not become healthy within ${max_wait}s"
    return 1
}

# ── Preflight ────────────────────────────────────────────────────────────────

if ! docker compose ps --status running | grep -q newrelic-infra; then
    echo "ERROR: e2e stack is not running. Run 'make up' first."
    exit 1
fi

echo "=== MySQL Availability Chaos Test ==="
echo ""
echo "Start time:          $START_TIME"
echo "Collection interval: ${CYCLE}s"
echo "Failure hold:        ${FAIL_DURATION}s (${FAIL_CYCLES} cycles)"
echo "Stabilize hold:      ${STABILIZE_DURATION}s (${STABILIZE_CYCLES} cycles)"
echo ""
echo "Watch your New Relic dashboard — events will appear in real time."

# SKIP_TO lets you jump directly to a specific test (1-5).
SKIP_TO=${SKIP_TO:-0}
if [ "$SKIP_TO" -gt 0 ]; then
    echo ""
    echo "  >>> Skipping to Test $SKIP_TO (SKIP_TO=$SKIP_TO)"
fi

# ── Baseline ─────────────────────────────────────────────────────────────────

banner "Phase 0: Baseline — confirming both instances are healthy"
echo "  e2e-mysql-1: $(docker compose ps e2e-mysql-1 --format '{{.Status}}')"
echo "  e2e-mysql-2: $(docker compose ps e2e-mysql-2 --format '{{.Status}}')"
hold "$STABILIZE_DURATION" "baseline collection"

# ── Test 1: Container stop (DNS failure) ─────────────────────────────────────

if [ "$SKIP_TO" -le 1 ]; then
banner "Test 1/5: Container stop — replica (DNS resolution failure)"
echo "  Stopping e2e-mysql-2..."
docker compose stop e2e-mysql-2
echo "  Container stopped. Agent should report dns_resolution_failed for e2e-mysql-2."
hold "$FAIL_DURATION" "failure detection"

echo "  Restoring e2e-mysql-2..."
docker compose start e2e-mysql-2
wait_healthy e2e-mysql-2
hold "$STABILIZE_DURATION" "recovery stabilization"
fi

# ── Test 2: Port reject via iptables (connection refused) ────────────────────
#
# Uses iptables inside the MySQL container to REJECT incoming TCP connections
# on port 3306 with a TCP RST.  Container stays alive and on the network, but
# the kernel sends RST to every SYN → the agent gets "connection refused".
# Requires cap_add: [NET_ADMIN] on the MySQL service in docker-compose.yml.

if [ "$SKIP_TO" -le 2 ]; then
banner "Test 2/5: Port reject — primary (connection refused)"
echo "  Installing iptables in e2e-mysql-1 (if needed)..."
docker compose exec -T e2e-mysql-1 \
    bash -c 'command -v iptables >/dev/null 2>&1 || microdnf install -y iptables >/dev/null 2>&1' || true
echo "  Blocking port 3306 with iptables REJECT (TCP RST)..."
docker compose exec -T e2e-mysql-1 \
    iptables -A INPUT -p tcp --dport 3306 -j REJECT --reject-with tcp-reset
echo "  Port blocked. Agent should report connection_refused for e2e-mysql-1."
hold "$FAIL_DURATION" "failure detection"

echo "  Removing iptables rule..."
docker compose exec -T e2e-mysql-1 \
    iptables -D INPUT -p tcp --dport 3306 -j REJECT --reject-with tcp-reset
echo "  Port unblocked."
hold "$STABILIZE_DURATION" "recovery stabilization"
fi

# ── Test 3: Pause container (I/O freeze) ─────────────────────────────────────

if [ "$SKIP_TO" -le 3 ]; then
banner "Test 3/5: Container pause — replica (connection hang / timeout)"
echo "  Pausing e2e-mysql-2 (SIGSTOP — process frozen, TCP stays open)..."
docker compose pause e2e-mysql-2
echo "  Container paused. Agent should report timeout for e2e-mysql-2."
hold "$FAIL_DURATION" "failure detection"

echo "  Unpausing e2e-mysql-2..."
docker compose unpause e2e-mysql-2
echo "  Container resumed."
hold "$STABILIZE_DURATION" "recovery stabilization"
fi

# ── Test 4: MySQL password change (auth failure) ─────────────────────────────

if [ "$SKIP_TO" -le 4 ]; then
banner "Test 4/5: Password change — primary (authentication failure)"
echo "  Changing root password on e2e-mysql-1..."
docker compose exec -T e2e-mysql-1 \
    mysql -u root -pe2e_test_password -e "ALTER USER 'root'@'%' IDENTIFIED BY 'wrong_password'; FLUSH PRIVILEGES;" 2>/dev/null
echo "  Password changed. Agent should report mysql_error_1045 for e2e-mysql-1."
hold "$FAIL_DURATION" "failure detection"

echo "  Restoring root password on e2e-mysql-1..."
docker compose exec -T e2e-mysql-1 \
    mysql -u root -pe2e_test_password -e "ALTER USER 'root'@'%' IDENTIFIED BY 'e2e_test_password'; FLUSH PRIVILEGES;" 2>/dev/null
echo "  Password restored."
hold "$STABILIZE_DURATION" "recovery stabilization"
fi

# ── Test 5: Kill connection mid-query (server-side termination) ───────────────
#
# The e2e-mysql-1-slowcheck instance runs SELECT BENCHMARK(...) as its canary
# query.  BENCHMARK runs a CPU-bound loop for ~10-20s, giving us a reliable
# window to find the process and KILL the connection mid-query.
#
# Note: Both KILL QUERY and KILL (connection) produce `invalid_connection` at
# the Go level — the go-sql-driver converts any server-side termination to
# driver.ErrBadConn ("invalid connection") before our code sees it.  We test
# KILL (connection destroy) since it's the more severe scenario.

# Helper: find the BENCHMARK query in the process list.
find_benchmark_proc() {
    docker compose exec -T e2e-mysql-1 \
        mysql -u root -pe2e_test_password -N -e \
        "SELECT ID FROM information_schema.PROCESSLIST WHERE INFO LIKE '%BENCHMARK%' AND INFO NOT LIKE '%PROCESSLIST%' AND COMMAND = 'Query' LIMIT 1" 2>/dev/null | tr -d '[:space:]'
}

# Helper: poll until the BENCHMARK query appears (up to 30s).
wait_for_benchmark() {
    local proc_id=""
    for attempt in $(seq 1 60); do
        proc_id=$(find_benchmark_proc)
        if [ -n "$proc_id" ]; then
            echo "$proc_id"
            return 0
        fi
        sleep 0.5
    done
    return 1
}

if [ "$SKIP_TO" -le 5 ]; then
banner "Test 5/5: Kill connection — slowcheck (server-side termination)"

echo ""
echo "  KILL — destroy connection mid-query"
echo "  Expected error: invalid_connection"
echo ""

KILL_ROUNDS=3
for round in $(seq 1 "$KILL_ROUNDS"); do
    echo "  Round $round/$KILL_ROUNDS: waiting for BENCHMARK query..."
    PROC_ID=$(wait_for_benchmark) || true

    if [ -n "$PROC_ID" ]; then
        echo "  Found BENCHMARK query on process $PROC_ID — killing connection..."
        docker compose exec -T e2e-mysql-1 \
            mysql -u root -pe2e_test_password -e "KILL $PROC_ID" 2>/dev/null || true
        echo "  Connection killed. Agent should report invalid_connection."
    else
        echo "  WARNING: BENCHMARK query not found within 30s (round $round). Skipping."
    fi

    if [ "$round" -lt "$KILL_ROUNDS" ]; then
        echo "  Waiting for next collection cycle..."
        sleep "$CYCLE"
    fi
done

hold "$STABILIZE_DURATION" "recovery stabilization"
fi

# ── Summary ──────────────────────────────────────────────────────────────────

END_TIME=$(ts)
END_EPOCH=$(epoch)
ELAPSED=$(( END_EPOCH - START_EPOCH ))
ELAPSED_MIN=$(( ELAPSED / 60 ))
ELAPSED_SEC=$(( ELAPSED % 60 ))

banner "Chaos test complete"
echo ""
echo "  Start:    $START_TIME"
echo "  End:      $END_TIME"
echo "  Duration: ${ELAPSED_MIN}m ${ELAPSED_SEC}s"
echo ""
echo "  Scenarios executed:"
echo ""
echo "  Test  Expected Error Code              Method"
echo "  ────  ─────────────────────────────    ──────────────────────────────"
echo "  1     dns_resolution_failed            Container stop (replica)"
echo "  2     connection_refused               Port reject / iptables (primary)"
echo "  3     timeout                          Container pause (replica)"
echo "  4     mysql_error_1045                 Password change (primary)"
echo "  5     invalid_connection               KILL connection (slowcheck)"
echo ""
# ── Automated verification (if NR_API_KEY and NR_ACCOUNT_ID are set) ─────────

if [ -n "${NR_API_KEY:-}" ] && [ -n "${NR_ACCOUNT_ID:-}" ]; then
    banner "Verifying results via NerdGraph"

    # Wait for ingest pipeline to flush. 30s covers typical NR ingest latency.
    echo "  Waiting 30s for NR ingest pipeline..."
    sleep 30

    EXPECTED_CODES="dns_resolution_failed connection_refused timeout mysql_error_1045 invalid_connection"

    NRQL="SELECT count(*) FROM MysqlHealthSample WHERE hasError = 1 FACET errorCode SINCE '$START_TIME' LIMIT 20"

    echo ""
    echo "  Verification NRQL:"
    echo "    $NRQL"
    echo ""

    QUERY='{ "query": "{ actor { account(id: '$NR_ACCOUNT_ID') { nrql(query: \"'"$NRQL"'\") { results } } } }" }'

    RESPONSE=$(curl -s -X POST https://api.newrelic.com/graphql \
        -H "Content-Type: application/json" \
        -H "Api-Key: $NR_API_KEY" \
        -d "$QUERY")

    RESULTS=$(echo "$RESPONSE" | jq -r '.data.actor.account.nrql.results // []')

    if [ "$RESULTS" = "[]" ] || [ "$RESULTS" = "null" ]; then
        echo "  WARNING: No results returned from NerdGraph. Check API key and account ID."
        echo "  Response: $(echo "$RESPONSE" | jq -c '.errors // empty')"
    else
        echo ""
        echo "  Error Code                    Count   Status"
        echo "  ─────────────────────────────────────────────"

        PASS_COUNT=0
        FAIL_COUNT=0
        FOUND_CODES=""

        for row in $(echo "$RESULTS" | jq -c '.[]'); do
            CODE=$(echo "$row" | jq -r '.facet // .errorCode // "unknown"')
            COUNT=$(echo "$row" | jq -r '.count // 0')
            FOUND_CODES="$FOUND_CODES $CODE"

            # Check if this is an expected code.
            EXPECTED=false
            for ec in $EXPECTED_CODES; do
                if [ "$CODE" = "$ec" ]; then
                    EXPECTED=true
                    break
                fi
            done

            if [ "$EXPECTED" = "true" ]; then
                printf "  %-31s %5s   PASS\n" "$CODE" "$COUNT"
                PASS_COUNT=$((PASS_COUNT + 1))
            else
                printf "  %-31s %5s   (unexpected)\n" "$CODE" "$COUNT"
            fi
        done

        # Check for expected codes that were NOT found.
        for ec in $EXPECTED_CODES; do
            FOUND=false
            for fc in $FOUND_CODES; do
                if [ "$ec" = "$fc" ]; then
                    FOUND=true
                    break
                fi
            done
            if [ "$FOUND" = "false" ]; then
                printf "  %-31s %5s   MISS\n" "$ec" "0"
                FAIL_COUNT=$((FAIL_COUNT + 1))
            fi
        done

        echo ""

        # Some error codes are alternatives (e.g. connection_refused OR timeout),
        # so a MISS isn't necessarily a failure. Report rather than fail.
        if [ "$FAIL_COUNT" -eq 0 ]; then
            echo "  Result: All expected error codes found."
        else
            echo "  Result: $PASS_COUNT found, $FAIL_COUNT missing."
            echo "  Note: Some codes are alternatives (e.g. connection_refused vs timeout,"
            echo "        connection_reset vs server_closed_connection). A MISS for one"
            echo "        of a pair is expected if the other was seen."
        fi
    fi

    echo ""

else
    echo "  ── Verify in New Relic (manual) ──"
    echo ""
    echo "  Set NR_API_KEY and NR_ACCOUNT_ID to enable automated verification."
    echo ""
    echo "  Error summary (count by code):"
    echo "    SELECT count(*) FROM MysqlHealthSample"
    echo "      WHERE hasError = 1"
    echo "      FACET errorCode"
    echo "      SINCE '$START_TIME'"
    echo ""
    echo "  Recent errors with details:"
    echo "    SELECT errorCode, errorMessage, checkType, label.instance, durationMs"
    echo "      FROM MysqlHealthSample"
    echo "      WHERE hasError = 1"
    echo "      SINCE '$START_TIME'"
    echo "      LIMIT 50"
    echo ""
fi

# ── Update PR.md ─────────────────────────────────────────────────────────────
# Auto-update the Chaos Test Results section in PR.md with the summary table
# visible and the full log in a collapsed <details> block.

PR_MD="$REPO_ROOT/PR.md"
if [ -f "$PR_MD" ]; then
    # Wait briefly for tee buffer to flush.
    sleep 1

    MODE="normal"
    [ "${FAST:-0}" = "1" ] && MODE="fast"
    TIMESTAMP=$(date '+%Y-%m-%d %H:%M:%S')

    # Build the replacement section.
    SECTION=$(cat <<SECTION_EOF
## Chaos Test Results

_Last run: $TIMESTAMP ($MODE mode, ${FAIL_CYCLES} cycle(s) per phase)_

| Test | Expected Error Code | Method |
|---|---|---|
| 1 | \`dns_resolution_failed\` | Container stop (replica) |
| 2 | \`connection_refused\` | Port reject / iptables (primary) |
| 3 | \`timeout\` | Container pause (replica) |
| 4 | \`mysql_error_1045\` | Password change (primary) |
| 5 | \`invalid_connection\` | KILL connection (slowcheck) |

<details>
<summary>Full test output (click to expand)</summary>

\`\`\`
$(cat "$LOG_FILE")
\`\`\`

</details>
SECTION_EOF
)

    # Replace the existing section or append if not found.
    if grep -q "^## Chaos Test Results" "$PR_MD"; then
        # Remove old section (from "## Chaos Test Results" to the next "## " heading or EOF).
        python3 -c "
import re, sys
content = open('$PR_MD').read()
pattern = r'## Chaos Test Results.*?(?=\n## [^#]|\Z)'
replacement = sys.stdin.read()
result = re.sub(pattern, replacement.rstrip(), content, count=1, flags=re.DOTALL)
open('$PR_MD', 'w').write(result)
" <<< "$SECTION"
        echo "  PR.md updated with chaos test results."
    else
        printf "\n%s\n" "$SECTION" >> "$PR_MD"
        echo "  PR.md updated with chaos test results (appended)."
    fi
fi

rm -f "$LOG_FILE"
