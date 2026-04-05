.DEFAULT_GOAL := help

# ── Helpers ───────────────────────────────────────────────────────────────────

.PHONY: help
help:
	@echo ""
	@echo "  Local MySQL testing (primary + replica + inventorydb):"
	@echo "    make setup                copy .env.example -> .env (edit before first run)"
	@echo "    make up                   build image + start all containers and agent"
	@echo "    make down                 stop and remove containers + volumes"
	@echo "    make logs                 tail infra agent logs"
	@echo "    make agent-shell          open a shell inside the agent container"
	@echo "    make run-once             exec integration against primary, print JSON"
	@echo "    make run-once-replica     exec integration against replica, print JSON"
	@echo "    make run-once-inventorydb exec integration against inventorydb, print JSON"
	@echo "    make repl-status          show replica replication status"
	@echo "    make test-availability    chaos test: trigger real failure scenarios (~6 min)"
	@echo "    make test-fast           same chaos test, 1 cycle per phase (~2 min)"
	@echo ""
	@echo "  RDS / Aurora MySQL testing:"
	@echo "    make up-rds             start infra agent only (no local mysql)"
	@echo "    make down-rds           stop rds stack"
	@echo ""
	@echo "  Rebuild after source changes:"
	@echo "    make rebuild            force-rebuild the agent image and restart"
	@echo ""

# ── Environment ──────────────────────────────────────────────────────────────

.PHONY: setup
setup:
	@if [ -f .env ]; then \
		echo ".env already exists -- skipping. Edit it directly if needed."; \
	else \
		cp .env.example .env; \
		echo "Created .env from .env.example."; \
		echo "Set NR_LICENSE_KEY as an environment variable (preferred) or edit .env."; \
	fi

# ── Local stack ──────────────────────────────────────────────────────────────

.PHONY: up
up:
	docker compose up -d --build
	@echo ""
	@echo "Stack is up (primary: e2e-mysql-1, replica: e2e-mysql-2)."
	@echo "Give the agent ~30s to send its first collection."
	@echo "Run 'make logs' to watch, or query New Relic directly."

.PHONY: down
down:
	docker compose down -v

.PHONY: rebuild
rebuild:
	docker compose down
	docker compose build --no-cache newrelic-infra
	docker compose up -d

.PHONY: logs
logs:
	docker compose logs -f newrelic-infra

.PHONY: agent-shell
agent-shell:
	docker compose exec newrelic-infra /bin/sh

# Run the integration binary directly inside the agent container and print the
# raw JSON payload — useful for inspecting output without waiting for NR ingest.
.PHONY: run-once
run-once:
	@docker compose exec newrelic-infra \
		/var/db/newrelic-infra/newrelic-integrations/bin/nri-mysql \
		-username root \
		-password e2e_test_password \
		-hostname e2e-mysql-1 \
		-port 3306 \
		-database demo \
		-collect_connection_timing=true \
		-availability_check_query="SELECT 1" \
		-availability_check_timeout_ms=5000 \
		-collect_query_telemetry=true \
		| python3 -m json.tool 2>/dev/null || \
		docker compose exec newrelic-infra \
		/var/db/newrelic-infra/newrelic-integrations/bin/nri-mysql \
		-username root \
		-password e2e_test_password \
		-hostname e2e-mysql-1 \
		-port 3306 \
		-database demo \
		-collect_connection_timing=true \
		-availability_check_query="SELECT 1" \
		-availability_check_timeout_ms=5000 \
		-collect_query_telemetry=true

.PHONY: run-once-replica
run-once-replica:
	@docker compose exec newrelic-infra \
		/var/db/newrelic-infra/newrelic-integrations/bin/nri-mysql \
		-username root \
		-password e2e_test_password \
		-hostname e2e-mysql-2 \
		-port 3306 \
		-database demo \
		-collect_connection_timing=true \
		-availability_check_query="SELECT 1" \
		-availability_check_timeout_ms=5000 \
		-collect_query_telemetry=true \
		| python3 -m json.tool 2>/dev/null || \
		docker compose exec newrelic-infra \
		/var/db/newrelic-infra/newrelic-integrations/bin/nri-mysql \
		-username root \
		-password e2e_test_password \
		-hostname e2e-mysql-2 \
		-port 3306 \
		-database demo \
		-collect_connection_timing=true \
		-availability_check_query="SELECT 1" \
		-availability_check_timeout_ms=5000 \
		-collect_query_telemetry=true

.PHONY: run-once-inventorydb
run-once-inventorydb:
	@docker compose exec newrelic-infra \
		/var/db/newrelic-infra/newrelic-integrations/bin/nri-mysql \
		-username root \
		-password e2e_test_password \
		-hostname e2e-inventorydb \
		-port 3306 \
		-database inventory \
		-collect_connection_timing=true \
		-availability_check_query="SELECT 1" \
		-availability_check_timeout_ms=5000 \
		-collect_query_telemetry=true \
		| python3 -m json.tool 2>/dev/null || \
		docker compose exec newrelic-infra \
		/var/db/newrelic-infra/newrelic-integrations/bin/nri-mysql \
		-username root \
		-password e2e_test_password \
		-hostname e2e-inventorydb \
		-port 3306 \
		-database inventory \
		-collect_connection_timing=true \
		-availability_check_query="SELECT 1" \
		-availability_check_timeout_ms=5000 \
		-collect_query_telemetry=true

# Show replication status on the replica.
.PHONY: repl-status
repl-status:
	@docker compose exec e2e-mysql-2 \
		mysql -u root -pe2e_test_password -e "SHOW REPLICA STATUS\G" 2>/dev/null

# Rebuild the integration binary inside the agent container so tests always
# run against the latest source.  Uses --build to recompile only when files
# have changed (Docker BuildKit cache).
.PHONY: rebuild-agent
rebuild-agent:
	@echo "Rebuilding agent image with latest source..."
	docker compose build newrelic-infra
	docker compose up -d newrelic-infra
	@echo "Agent container restarted with fresh binary."

# Run availability error classification tests against the running stack.
# Always rebuilds the agent binary first to avoid stale-binary surprises.
.PHONY: test-availability
test-availability: rebuild-agent
	@./test-availability.sh

# Fast mode — 2 fail cycles per phase (~3 min vs ~6 min).
.PHONY: test-fast
test-fast: rebuild-agent
	@FAST=1 ./test-availability.sh

# ── RDS / Aurora stack ───────────────────────────────────────────────────────

.PHONY: up-rds
up-rds:
	@if [ -z "$$RDS_HOST" ] && ! grep -q "^RDS_HOST=" .env 2>/dev/null; then \
		echo "ERROR: RDS_HOST is not set. Add RDS credentials to .env or export them."; \
		exit 1; \
	fi
	docker compose -f docker-compose.yml -f docker-compose.rds.yml up -d --build

.PHONY: down-rds
down-rds:
	docker compose -f docker-compose.yml -f docker-compose.rds.yml down -v
