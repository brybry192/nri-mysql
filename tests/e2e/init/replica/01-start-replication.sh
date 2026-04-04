#!/bin/bash
# Wait for the primary to be ready and then configure replication.
# This runs inside the replica's entrypoint-initdb phase, so mysqld is
# already running locally. We connect to the primary over the network.

set -e

PRIMARY_HOST="e2e-mysql-1"
PRIMARY_PORT=3306
REPL_USER="repl"
REPL_PASSWORD="repl_password"

echo "[replica-init] Waiting for primary at ${PRIMARY_HOST}:${PRIMARY_PORT}..."
for i in $(seq 1 30); do
    if mysqladmin ping -h "$PRIMARY_HOST" -P "$PRIMARY_PORT" -u root -p"$MYSQL_ROOT_PASSWORD" --silent 2>/dev/null; then
        echo "[replica-init] Primary is ready."
        break
    fi
    echo "[replica-init] Attempt $i/30 — primary not ready yet, retrying..."
    sleep 2
done

# Get the current binary log position from the primary.
MASTER_STATUS=$(mysql -h "$PRIMARY_HOST" -P "$PRIMARY_PORT" -u root -p"$MYSQL_ROOT_PASSWORD" -e "SHOW MASTER STATUS\G" 2>/dev/null)
LOG_FILE=$(echo "$MASTER_STATUS" | grep "File:" | awk '{print $2}')
LOG_POS=$(echo "$MASTER_STATUS" | grep "Position:" | awk '{print $2}')

if [ -z "$LOG_FILE" ] || [ -z "$LOG_POS" ]; then
    echo "[replica-init] ERROR: Could not read master status from primary."
    exit 1
fi

echo "[replica-init] Primary log_file=${LOG_FILE}, log_pos=${LOG_POS}"

# Configure and start replication (MySQL 8.0.23+ syntax).
mysql -u root -p"$MYSQL_ROOT_PASSWORD" <<-EOSQL
    CHANGE REPLICATION SOURCE TO
        SOURCE_HOST='${PRIMARY_HOST}',
        SOURCE_PORT=${PRIMARY_PORT},
        SOURCE_USER='${REPL_USER}',
        SOURCE_PASSWORD='${REPL_PASSWORD}',
        SOURCE_LOG_FILE='${LOG_FILE}',
        SOURCE_LOG_POS=${LOG_POS};
    START REPLICA;
EOSQL

echo "[replica-init] Replication started."
