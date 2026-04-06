#!/bin/bash
# generate-load.sh — Generate mixed read/write MySQL activity for dashboard demos.
# Not checked in — local use only.
#
# Usage:
#   ./generate-load.sh           # 60 seconds of activity
#   ./generate-load.sh 300       # 5 minutes of activity
#   ./generate-load.sh 0         # run until ctrl-c

DURATION=${1:-60}

sql() {
    docker compose exec -T e2e-mysql-1 mysql -u root -pe2e_test_password demo -N -s >/dev/null 2>/dev/null
}

echo "Generating MySQL load for ${DURATION}s (0=unlimited)..."

# Setup: create a scratch table if it doesn't exist.
sql <<'SQL'
CREATE TABLE IF NOT EXISTS load_test (
    id INT AUTO_INCREMENT PRIMARY KEY,
    category VARCHAR(50),
    value DECIMAL(10,2),
    payload TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    INDEX idx_category (category),
    INDEX idx_created (created_at)
) ENGINE=InnoDB;
SQL

# Open idle connections that persist for the duration of the load test.
# These show up as threadsConnected on the Connections dashboard page.
IDLE_COUNT=${IDLE_CONNECTIONS:-30}
IDLE_PIDS=""
echo "Opening $IDLE_COUNT idle connections..."
for i in $(seq 1 "$IDLE_COUNT"); do
    docker compose exec -T e2e-mysql-1 \
        mysql -u root -pe2e_test_password demo -e "SELECT SLEEP($((DURATION + 120)))" \
        >/dev/null 2>&1 &
    IDLE_PIDS="$IDLE_PIDS $!"
done
echo "  $IDLE_COUNT idle connections opened."

cleanup_idle() {
    if [ -n "$IDLE_PIDS" ]; then
        kill $IDLE_PIDS 2>/dev/null || true
        wait $IDLE_PIDS 2>/dev/null || true
    fi
}
trap cleanup_idle EXIT

START=$(date +%s)
CYCLE=0

while true; do
    CYCLE=$((CYCLE + 1))
    NOW=$(date +%s)
    ELAPSED=$((NOW - START))

    if [ "$DURATION" -gt 0 ] && [ "$ELAPSED" -ge "$DURATION" ]; then
        break
    fi

    printf "\r  Cycle %d (%ds elapsed) " "$CYCLE" "$ELAPSED"

    # ── Writes: batch inserts
    sql <<'SQL'
INSERT INTO load_test (category, value, payload) VALUES
    (ELT(FLOOR(RAND()*5)+1, 'orders','users','products','logs','events'), ROUND(RAND()*1000,2), REPEAT(MD5(RAND()),2)),
    (ELT(FLOOR(RAND()*5)+1, 'orders','users','products','logs','events'), ROUND(RAND()*1000,2), REPEAT(MD5(RAND()),2)),
    (ELT(FLOOR(RAND()*5)+1, 'orders','users','products','logs','events'), ROUND(RAND()*1000,2), REPEAT(MD5(RAND()),2)),
    (ELT(FLOOR(RAND()*5)+1, 'orders','users','products','logs','events'), ROUND(RAND()*1000,2), REPEAT(MD5(RAND()),2)),
    (ELT(FLOOR(RAND()*5)+1, 'orders','users','products','logs','events'), ROUND(RAND()*1000,2), REPEAT(MD5(RAND()),2));
SQL

    # ── Reads: various select patterns
    sql <<'SQL'
SELECT COUNT(*) FROM load_test;
SELECT category, COUNT(*), AVG(value), MAX(value) FROM load_test GROUP BY category;
SELECT * FROM load_test ORDER BY created_at DESC LIMIT 20;
SELECT * FROM load_test WHERE category = 'orders' AND value > 500 LIMIT 10;
SELECT a.category, COUNT(*) FROM load_test a JOIN load_test b ON a.category = b.category WHERE a.id != b.id LIMIT 50;
SQL

    # ── Updates
    sql <<'SQL'
UPDATE load_test SET value = value * 1.01, updated_at = NOW() WHERE id IN (SELECT id FROM (SELECT id FROM load_test ORDER BY RAND() LIMIT 3) t);
SQL

    # ── Deletes (keep table from growing forever)
    sql <<'SQL'
DELETE FROM load_test WHERE id IN (SELECT id FROM (SELECT id FROM load_test ORDER BY id ASC LIMIT 2) t);
SQL

    # ── Transactions
    sql <<'SQL'
START TRANSACTION;
INSERT INTO load_test (category, value, payload) VALUES ('txn_test', RAND()*100, 'transaction payload');
UPDATE load_test SET value = value + 1 WHERE category = 'txn_test' LIMIT 1;
COMMIT;
SQL

    sleep 0.5
done

printf "\r  Done — %d cycles, %ds elapsed.          \n" "$CYCLE" "$ELAPSED"
