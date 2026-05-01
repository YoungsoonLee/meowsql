# MeowSQL

> Paste a slow query. Get a faster one. For PostgreSQL and MySQL.

MeowSQL is an open-source CLI agent that diagnoses slow SQL, recommends missing
indexes, and rewrites queries — grounded in your real schema and execution plan,
not hallucinated guesses.

It is built for the workflow you already have: a slow query, a connection
string, and five minutes before your next standup.

![meowsql demo](demo/demo.gif)

```bash
$ meowsql analyze --dsn "$DATABASE_URL" --file slow.sql

🐾 MeowSQL — PostgreSQL 15.4

Diagnosis
  Seq Scan on orders (cost=0..184210) — filter on customer_id is non-SARGable
  because of the lower(email) call, preventing index use.

Suggested index
  CREATE INDEX CONCURRENTLY idx_orders_customer_lower_email
    ON orders (customer_id, lower(email));

Rewritten query
  -- (diff omitted in preview)

Estimated impact
  ~180x faster on the current plan.
```

---

## Why MeowSQL

Large-model SQL tools are obsessed with *generating* SQL. The expensive problem
— the one teams still page a DBA for — is **optimizing** SQL that already
exists and runs too slowly.

MeowSQL is deliberately narrow:

- **Two databases, done well.** PostgreSQL and MySQL only. No "12 dialects,
  badly."
- **Grounded, not hallucinated.** Every suggestion is conditioned on the real
  `EXPLAIN` output, the real schema, and the real indexes in your database.
- **Seconds, not sprints.** The value is legible in one command: the query runs
  N× faster or it doesn't.
- **Yours to run.** CLI-first, single binary, BYO Claude API key. No data
  leaves your machine except the query, schema snippet, and plan.

---

## Safe to run on production

Every MeowSQL command is designed to be read-only or explicitly roll back any
writes. You can point it at a live database without fear.

| Protection | `analyze` | `analyze --analyze` | `watch` | `plan-diff` | `bench` |
|--|:--:|:--:|:--:|:--:|:--:|
| `--dry-run` — no DB connection at all | ✅ | ✅ | ✅ | ✅ | ✅ |
| Read-only session (no writes possible) | ✅ | — | ✅ | — | — |
| Query never executes (EXPLAIN only) | ✅ | — | ✅ | ✅ | — |
| DML wrapped in `BEGIN` / `ROLLBACK` | — | ✅ always | — | — | ✅ always |
| DML guard — requires `--allow-dml` | — | ⚠️ warning | — | — | ✅ blocked |
| `--max-rows` auto-LIMIT on SELECT | — | — | — | — | ✅ 10 000 |
| Statement timeout (`--timeout`) | ✅ 60s | ✅ 60s | ✅ 60s | ✅ 60s | ✅ 30s |
| Lock timeout (PostgreSQL, 5 s) | ✅ | ✅ | ✅ | ✅ | ✅ per run |
| MySQL index named `meowsql_bench_*` | — | — | — | — | ✅ |

**`--dry-run`** — Every command accepts `--dry-run`. It prints exactly what
would be sent to the database and to the Claude API, then exits — no connection
is ever opened. Use it to audit or demo before pointing at a production host.

**Read-only session** — `analyze` (without `--analyze`) and `watch` open their
connection in `READ ONLY` mode at the session level
(`SET SESSION CHARACTERISTICS AS TRANSACTION READ ONLY` / `SET SESSION TRANSACTION READ ONLY`).
Any attempt to write data is rejected by the server, not just by MeowSQL.

**Query rollback** — `analyze --analyze` runs `EXPLAIN ANALYZE` inside a
`BEGIN` / `ROLLBACK` transaction. `bench` wraps every individual execution the
same way. `UPDATE`, `DELETE`, and `INSERT` run for timing or plan purposes but
are **never committed** to the database.

**`--max-rows`** (`bench`) — SELECT queries without an explicit `LIMIT` have
`LIMIT 10000` appended automatically, preventing a full-table scan from
saturating the network during a benchmark run. Disable with `--max-rows 0`.

**Statement timeout** — Every command accepts `--timeout` (default 60 s for
analysis commands, 30 s for bench). When the limit is hit, the database cancels
the query so it never blocks other sessions.  
PostgreSQL: `SET LOCAL statement_timeout`. MySQL bench: `MAX_EXECUTION_TIME`
optimizer hint + Go context deadline.

**Lock timeout** (PostgreSQL) — `SET lock_timeout = '5s'` is applied on every
PostgreSQL connection immediately after opening. Schema and EXPLAIN queries
never wait more than 5 seconds for a table lock held by another session.

**DML guard** (`bench` only) — `UPDATE` / `DELETE` / `INSERT` / `TRUNCATE`
queries are blocked unless you pass `--allow-dml`. Even then, every run is
rolled back. For `analyze --analyze`, a notice is printed automatically.

**MySQL index naming** (`bench`) — When `bench` creates an index on MySQL it
is renamed to `meowsql_bench_<original>_<timestamp>` so leftover indexes from
a crash are immediately identifiable. Run `bench --cleanup` to drop them all.

---

## Install

### Homebrew (macOS + Linux)

```bash
brew tap YoungsoonLee/meowsql
brew install meowsql
```

### Pre-built binaries

Download from [GitHub Releases](https://github.com/YoungsoonLee/meowsql/releases).
Builds are available for macOS (arm64/amd64) and Linux (amd64/arm64).

```bash
# Example — replace version and platform as needed
curl -L https://github.com/YoungsoonLee/meowsql/releases/download/v0.1.0/meowsql_darwin_arm64.tar.gz \
  | tar -xz
sudo mv meowsql /usr/local/bin/
```

### Build from source

Requires Go 1.23+ and a C toolchain (`xcode-select --install` on macOS).

```bash
git clone https://github.com/YoungsoonLee/meowsql.git
cd meowsql
make build
./bin/meowsql --help

# Or via go install
CGO_ENABLED=1 go install github.com/YoungsoonLee/meowsql/cmd/meowsql@latest
```

Set your Anthropic API key:

```bash
export ANTHROPIC_API_KEY=sk-ant-...
```

---

## 30-Second Example

```bash
# PostgreSQL
meowsql analyze \
  --dsn "postgres://user:pass@localhost:5432/shop" \
  --query "SELECT * FROM orders WHERE lower(email) = 'a@b.com'"

# MySQL (URL form, or the native go-sql-driver DSN user:pass@tcp(host:port)/db)
meowsql analyze \
  --dsn "mysql://user:pass@localhost:3306/shop" \
  --query "SELECT * FROM orders WHERE LOWER(email) = 'a@b.com'"
```

Other ways to feed it:

```bash
# From a file
meowsql analyze --dsn "$DATABASE_URL" --file slow.sql

# From stdin (great in pipelines)
pbpaste | meowsql analyze --dsn "$DATABASE_URL"

# JSON output for CI / scripts
meowsql analyze --dsn "$DATABASE_URL" --file slow.sql --json
```

### `analyze` flags

| Flag | What it does |
|------|-------------|
| `--dsn` | Database connection string (PostgreSQL or MySQL, auto-detected). |
| `--dialect` | Force `postgres` or `mysql` when the DSN is ambiguous. |
| `--query` / `--file` / stdin | Where the slow SQL comes from. Pick one. |
| `--analyze` | Run `EXPLAIN (ANALYZE, BUFFERS)` — actually executes the query. Off by default. |
| `--schema-only` | Skip `EXPLAIN`; use schema + stats only. Safe on prod read-replicas. |
| `--json` | Machine-readable output. |
| `--model` | Override the Claude model. Defaults to a fast/cheap one. |
| `--no-cache` | Skip cache lookup and do not write a new entry. |
| `--cache-ttl` | How long a cached result remains valid (default `24h`). |
| `--timeout` | Overall timeout for EXPLAIN + schema collection (default `60s`, `0` = no limit). |
| `--dry-run` | Print what would be executed without connecting to the database. |

Results are cached in `~/.cache/meowsql/` (macOS: `~/Library/Caches/meowsql/`).
The cache key is derived from the query text and the live schema (columns + indexes),
so adding or dropping an index automatically invalidates the relevant entries.

```bash
meowsql cache dir    # print cache directory path
meowsql cache clear  # delete all cached results
```

---

## `meowsql watch` — auto-surface slow queries

Don't have a specific query to paste? `watch` reads slow-query telemetry
directly from the database and analyzes the top-N worst offenders
automatically.

| Database | Source |
|----------|--------|
| PostgreSQL | `pg_stat_statements` (needs `CREATE EXTENSION IF NOT EXISTS pg_stat_statements`) |
| MySQL 8 | `performance_schema.events_statements_summary_by_digest` (on by default) |

```bash
# Analyze the 5 slowest queries seen at least 10 times
meowsql watch --dsn "$DATABASE_URL"

# Show top 10, ignore one-off queries
meowsql watch --dsn "$DATABASE_URL" --top 10 --min-calls 5

# MySQL — same flags
meowsql watch --dsn "mysql://user:pass@localhost:3306/shop"

# JSON output (great for piping into jq or CI dashboards)
meowsql watch --dsn "$DATABASE_URL" --json
```

### `watch` flags

| Flag | What it does |
|------|-------------|
| `--dsn` | Database connection string (required). |
| `--top` | Number of slowest queries to analyze (default 5). |
| `--min-calls` | Skip queries seen fewer than N times — filters out one-off noise (default 10). |
| `--dialect` | Force `postgres` or `mysql` when the DSN is ambiguous. |
| `--json` | Machine-readable output per query. |
| `--model` | Override the Claude model. |
| `--no-cache` | Skip cache lookup and do not write a new entry. |
| `--cache-ttl` | How long a cached result remains valid (default `24h`). |
| `--timeout` | Per-query timeout for schema collection and analysis (default `60s`). |
| `--dry-run` | Print what would be queried without connecting to the database. |

> **Note — PostgreSQL:** `pg_stat_statements` must be loaded before server start:
> add `shared_preload_libraries = 'pg_stat_statements'` to `postgresql.conf`, restart,
> then `CREATE EXTENSION IF NOT EXISTS pg_stat_statements;` once per database.
> The Docker Compose file in this repo already sets this up.

> **Note — MySQL:** The watch user needs `SELECT` on `performance_schema`:
> `GRANT SELECT ON performance_schema.* TO 'youruser'@'%';`

---

## `meowsql plan-diff` — catch plan regressions in CI

`plan-diff` compares the EXPLAIN cost of a query **before and after** a DDL
migration, without permanently changing your schema. It uses PostgreSQL's
transactional DDL: the migration is applied inside a `BEGIN` / `ROLLBACK`
block, so the database state is always restored.

```bash
# Compare a query's plan before and after a migration
meowsql plan-diff \
  --dsn "postgres://user:pass@localhost:5432/mydb" \
  --file queries/slow_orders.sql \
  --migration migrations/0042_drop_email_index.sql

# Fail with exit code 1 if cost increases more than 10% (for CI)
meowsql plan-diff \
  --dsn "$DATABASE_URL" \
  --file queries/slow_orders.sql \
  --migration migrations/0042_drop_email_index.sql \
  --exit-code

# Just record baseline cost (no migration)
meowsql plan-diff --dsn "$DATABASE_URL" --file queries/slow_orders.sql
```

Example output:

```json
{
  "query_file": "queries/slow_orders.sql",
  "migration_file": "migrations/0042_drop_email_index.sql",
  "before": { "cost": 28.1,    "plan_type": "Index Scan" },
  "after":  { "cost": 9393.7,  "plan_type": "Seq Scan"   },
  "pct_change": 33377,
  "regression": true
}
```

### GitHub Action

The included `.github/workflows/plan-check.yml` runs automatically on every
PR that touches a `.sql` file. It:

1. Starts a PostgreSQL test container and seeds it
2. Runs `meowsql plan-diff` for every query in `testdata/examples/` against
   the combined DDL changes in the PR (applied inside a rolled-back transaction)
3. Posts a sticky PR comment with a before/after cost table
4. Fails the check if any query regressed by more than 10%

Example PR comment:

| Query | Before | After | Change | Plan (after) |
|-------|--------|-------|--------|--------------|
| `slow_orders.sql` | 28.1 | 9.4k | ❌ +33377% | Seq Scan |
| `user_lookup.sql` | 1.0k | 1.0k | ✅ — | Index Scan |

To use it in your own repo, copy `.github/workflows/plan-check.yml` and put
your tracked queries in `testdata/examples/`, migrations in `migrations/` or
`testdata/seed/`.

### `plan-diff` flags

| Flag | What it does |
|------|-------------|
| `--dsn` | PostgreSQL connection string (required). |
| `--file` | SQL query file to evaluate (required). |
| `--migration` | DDL file to apply inside a rolled-back transaction. |
| `--threshold` | Regression threshold in percent (default 10). |
| `--exit-code` | Exit 1 when a regression is detected — for CI pipelines. |
| `--timeout` | EXPLAIN timeout per phase (default `60s`, `0` = no limit). |
| `--dry-run` | Print what would be executed without connecting to the database. |

---

## `meowsql bench` — prove the speedup with real numbers

`plan-diff` compares EXPLAIN costs (estimates). `bench` actually **runs the
query** and measures wall-clock latency, giving you concrete before/after
numbers to put in a PR or incident report.

Every execution is wrapped in `BEGIN` / `ROLLBACK` — see [Safe to run on
production](#safe-to-run-on-production) for the full safety story.

| | PostgreSQL | MySQL |
|--|--|--|
| Index DDL | Applied in outer `BEGIN` / `ROLLBACK` — never persists | Created for real, then `DROP INDEX` after bench |
| Index name | As-written in your DDL | Renamed to `meowsql_bench_<name>_<timestamp>` |
| Index cleanup failure | N/A | Run `bench --cleanup` to drop leftover `meowsql_bench_*` indexes |

```bash
# PostgreSQL — baseline only
meowsql bench \
  --dsn "postgres://user:pass@localhost:5432/mydb" \
  --file queries/slow_orders.sql

# PostgreSQL — before/after with inline DDL
meowsql bench \
  --dsn "$DATABASE_URL" \
  --file queries/slow_orders.sql \
  --index "CREATE INDEX idx_orders_lower_email ON orders (lower(email))"

# MySQL — same flags, dialect auto-detected from DSN
meowsql bench \
  --dsn "mysql://user:pass@localhost:3306/shop" \
  --file queries/slow_orders.sql \
  --index "CREATE INDEX idx_orders_email ON orders (email)"

# DDL from a file, 20 runs
meowsql bench \
  --dsn "$DATABASE_URL" \
  --file queries/slow_orders.sql \
  --index-file migrations/0043_add_email_index.sql \
  --runs 20

# JSON output (pipe into jq)
meowsql bench --dsn "$DATABASE_URL" --file queries/slow_orders.sql \
  --index "CREATE INDEX ..." --json | jq '{speedup: .speedup_x}'
```

Example output:

```
🐾 MeowSQL Bench — PostgreSQL

  Query      queries/slow_orders.sql
  Runs       10 (+ 2 warmup)
  Index      CREATE INDEX idx_orders_lower_email ON orders (lower(email))

             mean        p50         p99         min         max
  ──────────────────────────────────────────────────────────────
  Before     85.3ms      84.1ms     102.8ms      81.2ms     104.5ms
  After       0.4ms       0.4ms       0.8ms       0.4ms       0.9ms

  Speedup    213× faster  (p50 basis)
```

### `bench` flags

| Flag | What it does |
|------|-------------|
| `--dsn` | Database connection string (required). |
| `--dialect` | Force `postgres` or `mysql` when the DSN is ambiguous. |
| `--file` | SQL query file to benchmark (required). |
| `--index` | Index DDL to apply (one-liner). |
| `--index-file` | File containing DDL (alternative to `--index`). |
| `--runs` | Number of measured executions per phase (default 10). |
| `--warmup` | Unmeasured warm-up runs before each phase (default 2). |
| `--timeout` | Per-query statement timeout (default `30s`, `0` = no limit). |
| `--allow-dml` | Required when the query is `UPDATE`/`DELETE`/`INSERT`/`TRUNCATE`. |
| `--max-rows` | Auto-append `LIMIT N` to SELECT queries with no LIMIT (default 10000, `0` = disabled). |
| `--cleanup` | Drop leftover `meowsql_bench_*` indexes from a prior interrupted run (MySQL only). |
| `--json` | Machine-readable output. |
| `--dry-run` | Print what would be executed without connecting to the database. |

---

## `meowsql push` — continuous monitoring (MeowSQL Cloud)

`push` is the bridge between your database and MeowSQL Cloud. It runs the same
analysis as `watch`, then streams results to the cloud API for persistent
storage, dashboards, and alerting — automatically, on a schedule.

### How the measurements work

A common question: does MeowSQL need to execute queries against my production
database to get accurate numbers?

**No. And that's the point.**

```
[Inside your database]
  Real user queries execute normally
          ↓
  Your DB engine records execution stats automatically:
    PostgreSQL → pg_stat_statements (cumulative since last reset)
    MySQL 8    → performance_schema.events_statements_summary_by_digest

  meowsql push reads this telemetry (SELECT only, read-only session)
          ↓
  Normalized query text + stats + AI analysis ──▶ MeowSQL Cloud
```

MeowSQL never re-executes your slow queries. Instead it reads the statistics
your database has already accumulated from **real production traffic** — often
millions of executions. That makes the numbers more accurate than any synthetic
benchmark, because they reflect your actual data distribution, cache state, and
concurrent load.

| What MeowSQL accesses | How |
|---|---|
| Execution stats (calls, mean ms, total ms) | `SELECT` from `pg_stat_statements` / `performance_schema` |
| Schema (columns, indexes, table sizes) | `SELECT` from `pg_catalog` / `information_schema` |
| Actual row data | ❌ Never |
| Your DSN / credentials | ❌ Never sent to cloud — stays on your machine |

### Architecture

```
[Your infrastructure]              [MeowSQL Cloud]
  DB ──(read-only SELECT)──▶
  meowsql push                ──▶  POST /v1/ingest  ──▶  cloud-db
  (runs on your side)              (meowsql-cloud)        ↓
                                                     dashboards
                                                     alerts
```

`meowsql push` is the only process that touches your database. The cloud server
never receives a connection string and never opens a connection to your DB.

### Usage

```bash
# Run once
export ANTHROPIC_API_KEY=sk-ant-...
export MEOWSQL_API_KEY=msk_...
meowsql push --dsn "$DATABASE_URL"

# Run every hour (drop into cron or a systemd unit)
meowsql push --dsn "$DATABASE_URL" --interval 1h

# MySQL
meowsql push --dsn "mysql://user:pass@host:3306/db" --interval 1h

# Preview what would be sent without connecting
meowsql push --dsn "$DATABASE_URL" --dry-run
```

### `push` flags

| Flag | What it does |
|------|-------------|
| `--dsn` | Database connection string (required). |
| `--dialect` | Force `postgres` or `mysql` when the DSN is ambiguous. |
| `--top` | Number of slowest queries to analyze per cycle (default 5). |
| `--min-calls` | Skip queries seen fewer than N times (default 10). |
| `--interval` | Repeat on this schedule (e.g. `1h`); `0` = run once. |
| `--endpoint` | MeowSQL Cloud API base URL (default `https://api.meowsql.dev`). |
| `--api-key` | Cloud API key (or set `MEOWSQL_API_KEY`). |
| `--timeout` | Per-query timeout for schema + analysis (default `60s`). |
| `--model` | Claude model override. |
| `--no-cache` | Skip local analysis cache. |
| `--dry-run` | Analyze locally and print without sending to the cloud. |

### Self-hosting MeowSQL Cloud

The `meowsql-cloud` binary in this repo is the full cloud server. Run it
yourself if you need data to stay entirely within your own infrastructure
(regulated workloads, air-gapped environments).

```bash
# Start the cloud database
docker compose up -d cloud-db   # Postgres on :55433

# Generate an API key (shown once — store it securely)
make cloud-keygen LABEL=production
# → msk_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx

# Start the server
make cloud-serve                # listens on :8080

# Point the push agent at your self-hosted server
meowsql push \
  --dsn "$DATABASE_URL" \
  --endpoint "http://localhost:8080" \
  --api-key "msk_..." \
  --interval 1h
```

API endpoints exposed by `meowsql-cloud`:

| Method | Path | Description |
|--------|------|-------------|
| `GET` | `/v1/health` | Health check (no auth) |
| `POST` | `/v1/ingest` | Receive a push payload |
| `GET` | `/v1/queries` | List queries sorted by cost |

---

## VS Code Extension

The `vscode-meowsql` extension adds an inline **"🐾 Optimize with MeowSQL"** CodeLens
above every SQL statement in `.sql` files. Click it and the extension runs
`meowsql analyze` in the background and shows the diagnosis, index suggestion,
and rewritten query in a side panel — no terminal required.

### Install

```bash
# From the vscode-meowsql/ directory in this repo:
cd vscode-meowsql
npm install
npm run package          # produces meowsql-0.1.0.vsix
code --install-extension meowsql-0.1.0.vsix
```

> The extension will be published to the VS Code Marketplace once the CLI
> reaches v0.1.0 stable. Until then, install from source as above.

### Usage

1. Open any `.sql` file.
2. Click **🐾 Optimize with MeowSQL** above a query — or right-click a
   selection and choose **MeowSQL: Optimize Selected SQL**.
3. Enter your database DSN when prompted (or set it in settings to skip the
   prompt).
4. Results appear in a side panel: diagnosis, suggested index DDL, rewritten
   query, and estimated speedup.

### Settings

| Setting | Default | Description |
|---------|---------|-------------|
| `meowsql.dsn` | `""` | DSN to use without prompting each time. |
| `meowsql.binaryPath` | `"meowsql"` | Path to the binary (must be on `PATH`). |
| `meowsql.model` | `""` | Claude model override. |
| `meowsql.anthropicApiKey` | `""` | API key override (defaults to env var). |

---

## How It Works

```
   ┌────────────┐    ┌─────────────┐    ┌────────────────┐    ┌──────────────┐
   │ slow query │──▶ │ plan + meta │──▶ │ Claude (agent) │──▶ │ diagnosis +  │
   │   + DSN    │    │ collection  │    │  tool-use loop │    │ fix + rewrite│
   └────────────┘    └─────────────┘    └────────────────┘    └──────────────┘
```

1. **Parse.** The query is parsed with the real database parser
   (`libpg_query` for PostgreSQL, Vitess's parser for MySQL) to extract
   referenced tables, columns, and predicates.
2. **Collect.** MeowSQL pulls only what's relevant: column types, existing
   indexes, table/row statistics, and the `EXPLAIN` plan.
3. **Reason.** The collected context is sent to Claude with a strict system
   prompt. The model returns JSON with a diagnosis, root causes, index
   suggestions, and query rewrites — grounded in the provided schema, not
   invented. (A true tool-use loop is planned for v0.2.)
4. **Report.** Human-friendly terminal output, or `--json` for automation.

What stays local: your connection string, your rows, your credentials.
What leaves: the query text, referenced schema fragments, and the `EXPLAIN`
output — sent only to the Claude API you configured.

---

## Roadmap

MeowSQL is built in three phases. The goal of Phase 1 is a CLI that earns
GitHub stars on the strength of one undeniable demo. Phase 2 and 3 are what
turns that into a product.

### Phase 1 — MVP (v0.1, the "paste and win" CLI)

- [x] `meowsql analyze` for PostgreSQL (`pg_query_go` + `pgx`)
- [x] Schema + index + stats collection from `pg_catalog`
- [x] `EXPLAIN` (FORMAT JSON) ingestion, plus `--analyze` inside a
      rolled-back transaction
- [x] Claude-powered diagnosis, index suggestion, and rewrite (JSON output)
- [x] Pretty terminal output + `--json`
- [x] `meowsql analyze` for MySQL (`pingcap/tidb` parser + `go-sql-driver`)
- [x] Homebrew tap + GitHub Releases binaries
- [x] Asciinema demo in the README

### Phase 2 — Developer workflow (v0.2 – v0.3)

- [x] `meowsql watch` — read `pg_stat_statements` / `performance_schema`,
      surface the top-N most expensive queries, auto-analyze each
- [x] GitHub Action: comment on PRs when a migration or query changes a plan
      for the worse
- [x] VS Code extension: inline "optimize this query" action
- [x] Query cache so repeated analyses are free
- [x] `meowsql bench` — before/after timing harness

### Phase 3 — SaaS (MeowSQL Cloud)

- [x] `meowsql push` — agent that reads DB telemetry locally and streams
      results to the cloud (credentials never leave your machine)
- [x] `meowsql-cloud` — self-hostable API server (`serve`, `keygen`, `keys`)
- [x] `POST /v1/ingest` + `GET /v1/queries` cloud API
- [ ] Cost dashboard: dollars and seconds burned per query family
- [ ] Regression alerts: Slack / email when a query degrades week-over-week
- [ ] Slack / Teams digests: "your 5 most expensive queries this week"
- [ ] Multi-tenant web UI, SSO, audit log
- [ ] Hosted version at api.meowsql.dev

Why this order: the CLI proves the wedge. The developer-workflow layer creates
daily surface area on a team. The SaaS layer is where the recurring revenue
— and an acquirer's interest — lives.

---

## Design Principles

1. **Narrow beats broad.** Two databases, one job.
2. **Grounded beats fluent.** Never suggest an index on a column that doesn't
   exist. Never rewrite to syntax the target database can't run.
3. **Copy-paste is the happy path.** If the output can't be pasted straight
   into a migration or a query editor, it's not done.
4. **Your data is yours.** No telemetry by default. You supply the model key.

---

## Non-Goals (for now)

- Generating SQL from natural language. Lots of tools do that.
- Supporting every dialect. Snowflake, BigQuery, SQL Server — maybe later,
  once PostgreSQL and MySQL feel uncompromised.
- Replacing your DBA. MeowSQL is a fast first pass, not a final say.

---

## Project Layout

```
cmd/meowsql/            CLI binary (analyze, watch, bench, plan-diff, push, cache)
cmd/meowsql-cloud/      Cloud server binary (serve, keygen, keys)
internal/cli/           cobra commands
internal/db/postgres/   connect, parse (pg_query_go), EXPLAIN, schema, stats
internal/db/mysql/      connect, parse (pingcap/tidb), EXPLAIN, schema, stats
internal/agent/         Claude prompt + HTTP client, JSON result decoding
internal/report/        pretty terminal + JSON renderers
internal/push/          wire types + HTTP client shared between CLI and server
internal/cloud/         cloud API handlers + Postgres store (for meowsql-cloud)
internal/cache/         disk-based result cache (SHA256 key, TTL expiry)
internal/target/        dialect-agnostic types (ContextPack, BenchOptions, …)
testdata/examples/      sample slow queries used in demos
vscode-meowsql/         VS Code extension (TypeScript)
```

## Development

```bash
make build      # CGO_ENABLED=1 go build -o bin/meowsql ./cmd/meowsql
make run ARGS="analyze --dsn '$DATABASE_URL' --file testdata/examples/slow_orders.sql"
make test
make tidy
```

Requirements: Go 1.23+, a C toolchain (`xcode-select --install` on macOS),
an `ANTHROPIC_API_KEY`, and a reachable PostgreSQL instance.

### End-to-End Demo (Docker)

A one-command demo runs MeowSQL against a throwaway PostgreSQL 16 container
with an intentionally under-indexed `orders` table (100k users, 500k orders).
The canonical slow query (`WHERE lower(email) = ...`) is designed to trigger
a Seq Scan — exactly the kind of pathology MeowSQL should catch.

```bash
export ANTHROPIC_API_KEY=sk-ant-...

make e2e-up      # start PostgreSQL on :55432
make e2e-seed    # schema + seed + VACUUM ANALYZE (~5s)
make e2e-run     # build + meowsql analyze against the dockerized DB
make e2e-down    # tear down

# Or the one-shot:
make e2e         # up → seed → run

# Sanity check the plan without spending on the LLM:
make e2e-explain
```

Extra flags pass through `ARGS`:

```bash
make e2e-run ARGS="--json" | jq .result
make e2e-run ARGS="--analyze"   # EXPLAIN ANALYZE inside a rolled-back tx
```

## Contributing

MeowSQL is pre-v0.1. The fastest way to help right now:

- Open an issue with a slow query you'd love an agent to solve (sanitize it
  first). Real queries shape the prompts.
- File bugs with `EXPLAIN` output attached.
- Try the CLI against a schema you know well and tell us where it lied.

A full `CONTRIBUTING.md` will land with v0.1.

---

## License

Licensed under the [Apache License 2.0](./LICENSE). The open-source CLI in
this repository is Apache-2.0; future hosted SaaS components will live in a
separate repository under a commercial license.

---

## Project Name

`meowsql` is the binary. The agent purrs when it
finds a missing index. That part is not configurable.
