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

set -euo pipefail

# Collection interval is 10s. Hold failures for 3 cycles, stabilize for 3.
CYCLE=10
FAIL_CYCLES=3
STABILIZE_CYCLES=3
FAIL_DURATION=$((CYCLE * FAIL_CYCLES))
STABILIZE_DURATION=$((CYCLE * STABILIZE_CYCLES))

# ── Helpers ──────────────────────────────────────────────────────────────────

banner() {
    echo ""
    echo "================================================================"
    echo "  $1"
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
echo "Collection interval: ${CYCLE}s"
echo "Failure hold:        ${FAIL_DURATION}s (${FAIL_CYCLES} cycles)"
echo "Stabilize hold:      ${STABILIZE_DURATION}s (${STABILIZE_CYCLES} cycles)"
echo ""
echo "Watch your New Relic dashboard — events will appear in real time."

# ── Baseline ─────────────────────────────────────────────────────────────────

banner "Phase 0: Baseline — confirming both instances are healthy"
echo "  e2e-mysql-1: $(docker compose ps e2e-mysql-1 --format '{{.Status}}')"
echo "  e2e-mysql-2: $(docker compose ps e2e-mysql-2 --format '{{.Status}}')"
hold "$STABILIZE_DURATION" "baseline collection"

# ── Test 1: Container stop (DNS failure) ─────────────────────────────────────

banner "Test 1/4: Container stop — replica (DNS resolution failure)"
echo "  Stopping e2e-mysql-2..."
docker compose stop e2e-mysql-2
echo "  Container stopped. Agent should report dns_resolution_failed for e2e-mysql-2."
hold "$FAIL_DURATION" "failure detection"

echo "  Restoring e2e-mysql-2..."
docker compose start e2e-mysql-2
wait_healthy e2e-mysql-2
hold "$STABILIZE_DURATION" "recovery stabilization"

# ── Test 2: Network disconnect (connection timeout) ──────────────────────────

banner "Test 2/4: Network disconnect — primary (connection timeout)"
echo "  Disconnecting e2e-mysql-1 from the network..."
docker network disconnect e2e_default e2e-mysql-1 2>/dev/null || \
    docker network disconnect "$(docker compose ps e2e-mysql-1 --format '{{.Networks}}')" e2e-mysql-1
echo "  Network severed. Agent should report timeout or connection errors for e2e-mysql-1."
hold "$FAIL_DURATION" "failure detection"

echo "  Reconnecting e2e-mysql-1 to the network..."
docker network connect e2e_default e2e-mysql-1 2>/dev/null || \
    docker compose up -d e2e-mysql-1
wait_healthy e2e-mysql-1
hold "$STABILIZE_DURATION" "recovery stabilization"

# ── Test 3: Pause container (I/O freeze) ─────────────────────────────────────

banner "Test 3/4: Container pause — replica (connection hang / timeout)"
echo "  Pausing e2e-mysql-2 (SIGSTOP — process frozen, TCP stays open)..."
docker compose pause e2e-mysql-2
echo "  Container paused. Agent should report timeout for e2e-mysql-2."
hold "$FAIL_DURATION" "failure detection"

echo "  Unpausing e2e-mysql-2..."
docker compose unpause e2e-mysql-2
echo "  Container resumed."
hold "$STABILIZE_DURATION" "recovery stabilization"

# ── Test 4: MySQL password change (auth failure) ─────────────────────────────

banner "Test 4/4: Password change — primary (authentication failure)"
echo "  Changing root password on e2e-mysql-1..."
docker compose exec -T e2e-mysql-1 \
    mysql -u root -pe2e_test_password -e "ALTER USER 'root'@'%' IDENTIFIED BY 'wrong_password'; FLUSH PRIVILEGES;" 2>/dev/null
echo "  Password changed. Agent should report mysql_error_1045 for e2e-mysql-1."
hold "$FAIL_DURATION" "failure detection"

echo "  Restoring root password on e2e-mysql-1..."
docker compose exec -T e2e-mysql-1 \
    mysql -u root -pwrong_password -e "ALTER USER 'root'@'%' IDENTIFIED BY 'e2e_test_password'; FLUSH PRIVILEGES;" 2>/dev/null
echo "  Password restored."
hold "$STABILIZE_DURATION" "recovery stabilization"

# ── Summary ──────────────────────────────────────────────────────────────────

banner "Chaos test complete"
echo ""
echo "  All scenarios executed. Check your New Relic dashboard for:"
echo ""
echo "  Test 1 — dns_resolution_failed     (replica container stopped)"
echo "  Test 2 — connection_refused/timeout (primary network disconnected)"
echo "  Test 3 — timeout                    (replica container paused)"
echo "  Test 4 — mysql_error_1045           (primary password changed)"
echo ""
echo "  Use NRQL:"
echo "    SELECT * FROM MysqlHealthSample"
echo "      WHERE hasError = 1"
echo "      SINCE 30 minutes ago"
echo ""
