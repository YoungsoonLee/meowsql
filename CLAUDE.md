# CLAUDE.md

Guidance for Claude Code (and other AI coding agents) working in this repo.

## What this is

MeowSQL is an open-source CLI that analyzes a slow SQL query against a real
PostgreSQL (and, later, MySQL) database and returns a grounded diagnosis,
index suggestion, and rewrite. The goal is a narrow, viral developer tool
("paste query, get 10x speedup") on the path to a SaaS monitoring product.

Long-form product context — goals, non-goals, exit thesis — lives in
`goal.md` and `README.md`. Read both before making product-shape decisions.

## Architecture (Phase 1)

```
cmd/meowsql/            thin main — wires signals + root cobra command
internal/cli/           cobra commands. root.go exposes NewRoot;
                        analyze.go owns the end-to-end flow
internal/db/postgres/   everything database-side, kept in one package
                          conn.go     pgx connection + server_version
                          parse.go    pg_query_go wrappers (Validate, walk)
                          explain.go  EXPLAIN (FORMAT JSON), --analyze in tx
                          schema.go   pg_catalog: columns, indexes, reltuples
                          collect.go  orchestrates the above into ContextPack
internal/agent/         Claude integration
                          prompt.go     system prompt (strict JSON contract)
                          anthropic.go  plain net/http client, no SDK
                          agent.go      Analyze(): marshal pack → call → decode
internal/report/        text.go pretty terminal output (fatih/color);
                        json.go machine-readable envelope
testdata/examples/      sample slow queries
```

### The pipeline

`analyze.go` drives one linear flow:

1. Read SQL (`--query` / `--file` / stdin).
2. `postgres.Open(dsn)` → `pgx.Conn`.
3. `Collector.Collect(sql, opts)` returns a `*postgres.ContextPack` with
   version, validated SQL, EXPLAIN plan (optional), and per-table schema.
4. `agent.Analyze(ctx, Request{APIKey, Model, Context})` sends the pack to
   Claude and decodes strict JSON into `agent.Result`.
5. `report.WriteText` / `WriteJSON` emits the output.

### Design rules worth repeating

- **No hallucinated schema.** The agent only sees columns/indexes that
  `pg_catalog` actually returned. The system prompt in `prompt.go` forbids
  inventing identifiers — keep it strict if you edit it.
- **`--analyze` must always run inside a transaction that is rolled back.**
  See `Collector.Explain`. Do not remove the `defer tx.Rollback`.
- **Pre-fetch, don't tool-loop.** v0.1 sends one prompt with the full
  context. A true Anthropic tool-use loop (`get_schema`, `get_explain`,
  `test_index`) is the v0.2 upgrade path.
- **One package per dialect.** `internal/db/postgres/`. When MySQL arrives,
  add `internal/db/mysql/` and introduce a shared `ContextPack` type in a
  new `internal/target` package — not before.
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
- PostgreSQL DSN — pass via `--dsn`. Local examples usually use
  `postgres://user:pass@localhost:5432/dbname?sslmode=disable`.

`pg_query_go` uses cgo. Builds need a C toolchain. We pin v6 because v5 fails
to compile on current macOS SDKs (duplicate `strchrnul` declaration). Do not
downgrade.

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
