BIN := bin/meowsql
PKG := ./cmd/meowsql

E2E_DSN       := postgres://meowsql:meowsql@localhost:55432/meowsql?sslmode=disable
E2E_MYSQL_DSN := mysql://meowsql:meowsql@localhost:53306/meowsql
PSQL          := docker compose exec -T postgres psql -U meowsql -d meowsql -v ON_ERROR_STOP=1
MYSQL         := docker compose exec -T mysql mysql -umeowsql -pmeowsql meowsql

.PHONY: build run test tidy fmt vet clean \
        e2e e2e-up e2e-wait e2e-seed e2e-run e2e-explain e2e-down \
        e2e-mysql e2e-mysql-up e2e-mysql-wait e2e-mysql-seed e2e-mysql-run e2e-mysql-explain e2e-mysql-down

build:
	CGO_ENABLED=1 go build -o $(BIN) $(PKG)

run:
	CGO_ENABLED=1 go run $(PKG) $(ARGS)

test:
	go test ./...

tidy:
	go mod tidy

fmt:
	go fmt ./...

vet:
	go vet ./...

clean:
	rm -rf bin dist

# -------- end-to-end demo against a throwaway PostgreSQL container --------

e2e: e2e-up e2e-seed e2e-run

e2e-up:
	docker compose up -d
	@$(MAKE) --no-print-directory e2e-wait

e2e-wait:
	@echo "Waiting for postgres on :55432..."
	@for i in $$(seq 1 30); do \
		if docker compose exec -T postgres pg_isready -U meowsql -d meowsql > /dev/null 2>&1; then \
			echo "Postgres ready."; exit 0; \
		fi; \
		sleep 1; \
	done; \
	echo "Postgres did not become ready in time." >&2; exit 1

e2e-seed:
	@echo "==> schema"
	@$(PSQL) < testdata/seed/01_schema.sql
	@echo "==> seed (100k users, 500k orders — takes ~15s)"
	@$(PSQL) < testdata/seed/02_seed.sql
	@echo "==> analyze"
	@$(PSQL) < testdata/seed/03_analyze.sql

# Run meowsql analyze against the dockerized DB. Needs ANTHROPIC_API_KEY.
# Pass extra flags via ARGS, e.g. `make e2e-run ARGS="--json"`.
e2e-run: build
	@if [ -z "$$ANTHROPIC_API_KEY" ]; then echo "ANTHROPIC_API_KEY not set" >&2; exit 1; fi
	./$(BIN) analyze --dsn "$(E2E_DSN)" --file testdata/examples/slow_orders.sql $(ARGS)

# Sanity check without the LLM: confirm the demo query actually Seq-Scans.
e2e-explain:
	@$(PSQL) -c "EXPLAIN (ANALYZE, BUFFERS) $$(cat testdata/examples/slow_orders.sql | tr -d ';')"

e2e-down:
	docker compose down -v

# -------- end-to-end demo against a throwaway MySQL 8 container --------

e2e-mysql: e2e-mysql-up e2e-mysql-seed e2e-mysql-run

e2e-mysql-up:
	docker compose up -d mysql
	@$(MAKE) --no-print-directory e2e-mysql-wait

e2e-mysql-wait:
	@echo "Waiting for mysql on :53306..."
	@for i in $$(seq 1 60); do \
		if docker compose exec -T mysql mysqladmin ping -h 127.0.0.1 -umeowsql -pmeowsql --silent > /dev/null 2>&1; then \
			echo "MySQL ready."; exit 0; \
		fi; \
		sleep 1; \
	done; \
	echo "MySQL did not become ready in time." >&2; exit 1

e2e-mysql-seed:
	@echo "==> schema"
	@$(MYSQL) < testdata/seed-mysql/01_schema.sql
	@echo "==> seed (100k users, 500k orders — may take a minute)"
	@$(MYSQL) < testdata/seed-mysql/02_seed.sql
	@echo "==> analyze"
	@$(MYSQL) < testdata/seed-mysql/03_analyze.sql

# Run meowsql analyze against the dockerized MySQL. Needs ANTHROPIC_API_KEY.
e2e-mysql-run: build
	@if [ -z "$$ANTHROPIC_API_KEY" ]; then echo "ANTHROPIC_API_KEY not set" >&2; exit 1; fi
	./$(BIN) analyze --dsn "$(E2E_MYSQL_DSN)" --file testdata/examples/slow_orders_mysql.sql $(ARGS)

# Sanity check without the LLM: confirm the demo query actually full-scans.
e2e-mysql-explain:
	@$(MYSQL) -e "EXPLAIN FORMAT=TREE $$(cat testdata/examples/slow_orders_mysql.sql | tr -d ';')"

e2e-mysql-down:
	docker compose rm -sfv mysql
