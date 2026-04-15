# CLAUDE.md

Guidance for Claude Code (and other AI coding agents) working in this repo.

## What this is

MeowSQL is an open-source CLI that analyzes a slow SQL query against a real
PostgreSQL or MySQL database and returns a grounded diagnosis, index
suggestion, and rewrite. The goal is a narrow, viral developer tool
("paste query, get 10x speedup") on the path to a SaaS monitoring product.

Long-form product context — goals, non-goals, exit thesis — lives in
`goal.md` and `README.md`. Read both before making product-shape decisions.

## Architecture (Phase 1)

```
cmd/meowsql/            thin main — wires signals + root cobra command
internal/cli/           cobra commands. root.go exposes NewRoot;
                        analyze.go owns the end-to-end flow and the
                        DSN-prefix → dialect dispatch (postgres vs mysql)
internal/target/        dialect-agnostic types shared by the collectors and
                        the agent/report layers (ContextPack, TableInfo,
                        ColumnInfo, IndexInfo, CollectOptions)
internal/db/postgres/   conn.go     pgx connection + server_version
                        parse.go    pg_query_go wrappers (Validate, walk)
                        explain.go  EXPLAIN (FORMAT JSON), --analyze in tx
                        schema.go   pg_catalog: columns, indexes, reltuples
                        collect.go  orchestrates the above into ContextPack
internal/db/mysql/      conn.go     database/sql + go-sql-driver, DSN
                                    translator (mysql:// → user:pass@tcp(..))
                        parse.go    pingcap/tidb parser — ValidateSQL +
                                    TableName AST visitor
                        explain.go  EXPLAIN FORMAT=JSON; EXPLAIN ANALYZE
                                    inside a rolled-back tx, text wrapped
                                    in a JSON envelope for the agent
                        schema.go   information_schema: columns, statistics,
                                    table_rows; synthesizes CREATE INDEX
                                    strings because MySQL has no indexdef
                        collect.go  orchestrates into the shared ContextPack
internal/agent/         prompt.go     system prompt (strict JSON contract,
                                      dialect-aware DDL rules)
                        anthropic.go  plain net/http client, no SDK
                        agent.go      Analyze(); Request.Validate is a
                                      dialect-supplied callback used to drop
                                      un-parseable or identical-to-input
                                      rewrites before returning
internal/report/        text.go pretty terminal output (fatih/color);
                        json.go machine-readable envelope
testdata/examples/      sample slow queries (PG + MySQL variants)
testdata/seed/          PostgreSQL demo: schema + 100k users + 500k orders
testdata/seed-mysql/    MySQL 8 demo: same shape, recursive-CTE generator
```

### The pipeline

`analyze.go` drives one linear flow:

1. Read SQL (`--query` / `--file` / stdin).
2. Resolve dialect from `--dialect` or DSN prefix (`postgres://`,
   `postgresql://`, `mysql://`, or `@tcp(...)`).
3. Open the dialect's `Collector` (postgres or mysql) and pick its
   `ValidateOnly` callback for the agent.
4. `Collector.Collect(sql, opts)` returns a `*target.ContextPack` with
   version, validated SQL, EXPLAIN plan (optional), and per-table schema.
5. `agent.Analyze(ctx, Request{APIKey, Model, Context, Validate})` sends
   the pack to Claude and decodes strict JSON into `agent.Result`.
6. `report.WriteText` / `WriteJSON` emits the output.

### Design rules worth repeating

- **No hallucinated schema.** The agent only sees columns/indexes that
  `pg_catalog` / `information_schema` actually returned. The system prompt
  in `prompt.go` forbids inventing identifiers — keep it strict if you
  edit it.
- **`--analyze` must always run inside a transaction that is rolled back.**
  See `Collector.Explain` in both dialects. Do not remove the
  `defer tx.Rollback`.
- **Pre-fetch, don't tool-loop.** v0.1 sends one prompt with the full
  context. A true Anthropic tool-use loop (`get_schema`, `get_explain`,
  `test_index`) is the v0.2 upgrade path.
- **One package per dialect; types live in `internal/target`.** The CLI,
  agent, and report packages must never import `internal/db/postgres` or
  `internal/db/mysql` directly for types — only for `Open` and
  `ValidateOnly`. A third dialect (if ever) slots in the same way.
- **Dialect-aware DDL in the prompt.** Rule 4 tells the model to use
  `CREATE INDEX CONCURRENTLY` for Postgres and plain `CREATE INDEX` for
  MySQL. If you add a new dialect, extend that rule in the same shape.
- **No SDK for Anthropic.** `internal/agent/anthropic.go` is ~80 lines of
  `net/http`. This avoids SDK-version churn and keeps the dep surface tiny.
  Revisit only if we need streaming or tool-use.

## Build & run

```bash
make build                      # bin/meowsql (CGO_ENABLED=1)
make run ARGS="analyze --help"
make test
make tidy
```

Environment:

- `ANTHROPIC_API_KEY` — required.
- DSN — pass via `--dsn`. Examples:
  - Postgres: `postgres://user:pass@localhost:5432/dbname?sslmode=disable`
  - MySQL (URL): `mysql://user:pass@localhost:3306/dbname`
  - MySQL (go-sql-driver native): `user:pass@tcp(localhost:3306)/dbname`

`pg_query_go` uses cgo. Builds need a C toolchain. We pin v6 because v5 fails
to compile on current macOS SDKs (duplicate `strchrnul` declaration). Do not
downgrade.

`pingcap/tidb/pkg/parser` is pure Go and covers MySQL 8 (CTEs, window
functions, JSON). We rejected `dolthub/vitess` (broken transitive go.mod in
current releases) and `xwb1989/sqlparser` (no CTE/window support).

## Coding conventions

- Standard Go style. `go fmt` on save, `go vet` clean.
- Errors: wrap with `fmt.Errorf("step: %w", err)` at package boundaries; do
  not decorate inside leaf functions.
- Comments: only when the "why" is non-obvious (see the rollback note in
  `explain.go`, the SDK-less rationale above). No "what-does-this-do"
  comments.
- Keep files focused: one responsibility per file, roughly <150 lines.
- Prefer adding a targeted `testdata/examples/*.sql` over embedding SQL in
  tests.

## What not to do

- Don't add a Python sidecar for `sqlglot`. We explicitly chose Go for
  single-binary distribution. Revisit only when a concrete feature
  demonstrably needs sqlglot-level cross-dialect rewriting.
- Don't add Snowflake/BigQuery/SQL Server support before PostgreSQL + MySQL
  feel uncompromised.
- Don't add telemetry. User data should not leave their machine except as
  documented (query text + schema fragments + EXPLAIN → Anthropic).
- Don't broaden scope into SQL generation, BI, or coding-assistant land —
  those were considered and rejected in `goal.md`.

## Phase 2 pointers (not yet built)

- `meowsql watch` — poll `pg_stat_statements`, pick top-N by total time,
  auto-analyze each.
- True Claude tool-use loop with tools: `describe_table`, `explain`,
  `try_index` (inside a rolled-back tx).
- GitHub Action that diffs EXPLAIN plans on PRs touching `.sql` files.

When extending, keep each addition shippable as its own demo-able unit.
Every feature should answer "what would make someone tweet about this?"
