# MeowSQL

> Paste a slow query. Get a faster one. For PostgreSQL and MySQL.

MeowSQL is an open-source CLI agent that diagnoses slow SQL, recommends missing
indexes, and rewrites queries — grounded in your real schema and execution plan,
not hallucinated guesses.

It is built for the workflow you already have: a slow query, a connection
string, and five minutes before your next standup.

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

## Install

```bash
# Build from source (requires Go 1.23+ and a C toolchain — pg_query_go uses cgo)
git clone https://github.com/YoungsoonLee/meowsql.git
cd meowsql
make build
./bin/meowsql --help

# Or via `go install` (also needs cgo enabled)
CGO_ENABLED=1 go install github.com/YoungsoonLee/meowsql/cmd/meowsql@latest
```

> Release binaries and a Homebrew tap land with v0.1. Until then, build
> from source as above.

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

Flags you will actually use:

| Flag | What it does |
|------|-------------|
| `--dsn` | Database connection string (PostgreSQL or MySQL, auto-detected). |
| `--dialect` | Force `postgres` or `mysql` when the DSN is ambiguous. |
| `--query` / `--file` / stdin | Where the slow SQL comes from. Pick one. |
| `--analyze` | Run `EXPLAIN (ANALYZE, BUFFERS)` — actually executes the query. Off by default. |
| `--schema-only` | Skip `EXPLAIN`; use schema + stats only. Safe on prod read-replicas. |
| `--json` | Machine-readable output. |
| `--model` | Override the Claude model. Defaults to a fast/cheap one. |

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
- [ ] Homebrew tap + GitHub Releases binaries
- [ ] Asciinema demo in the README

### Phase 2 — Developer workflow (v0.2 – v0.3)

- [ ] `meowsql watch` — read `pg_stat_statements` / `performance_schema`,
      surface the top-N most expensive queries, auto-analyze each
- [ ] GitHub Action: comment on PRs when a migration or query changes a plan
      for the worse
- [ ] VS Code extension: inline "optimize this query" action
- [ ] Query cache so repeated analyses are free
- [ ] `meowsql bench` — before/after timing harness

### Phase 3 — SaaS (MeowSQL Cloud)

- [ ] Hosted continuous monitoring against pg_stat_statements / P_S
- [ ] Cost dashboard: dollars and seconds burned per query family
- [ ] Slack / Teams digests: "your 5 most expensive queries this week"
- [ ] Multi-tenant, SSO, audit log
- [ ] Optional self-hosted edition for regulated workloads

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
cmd/meowsql/            thin CLI entry point
internal/cli/           cobra commands (root, analyze)
internal/db/postgres/   connect, parse (pg_query_go), EXPLAIN, schema, stats
internal/agent/         Claude prompt + HTTP client, JSON result decoding
internal/report/        pretty terminal + JSON renderers
testdata/examples/      sample slow queries used in demos
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
