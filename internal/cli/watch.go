package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/YoungsoonLee/meowsql/internal/agent"
	"github.com/YoungsoonLee/meowsql/internal/cache"
	"github.com/YoungsoonLee/meowsql/internal/db/mysql"
	"github.com/YoungsoonLee/meowsql/internal/db/postgres"
	"github.com/YoungsoonLee/meowsql/internal/report"
	"github.com/YoungsoonLee/meowsql/internal/target"
	"github.com/spf13/cobra"
)

type watchOpts struct {
	dsn      string
	dialect  string
	top      int
	minCalls int64
	jsonOut  bool
	model    string
	noCache  bool
	cacheTTL time.Duration
	timeout  time.Duration
	dryRun   bool
}

// watchStatement is the shared type returned by both dialect collectors.
type watchStatement struct {
	Query   string
	Calls   int64
	TotalMs float64
	MeanMs  float64
}

func newWatchCmd() *cobra.Command {
	var o watchOpts
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Surface and analyze the top-N slowest queries from pg_stat_statements / performance_schema",
		Long: `Watch reads slow-query telemetry from the database itself:

  PostgreSQL  pg_stat_statements view  (CREATE EXTENSION IF NOT EXISTS pg_stat_statements)
  MySQL 8     performance_schema.events_statements_summary_by_digest  (on by default)

For each query it runs the same schema-level analysis as 'meowsql analyze',
grounded in the real table schemas and indexes — without needing EXPLAIN
(the stored queries are parameterized, so EXPLAIN is skipped automatically).

Requires ANTHROPIC_API_KEY.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWatch(cmd.Context(), cmd.OutOrStdout(), o)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.dsn, "dsn", "", "database connection string (required)")
	f.StringVar(&o.dialect, "dialect", "", "postgres|mysql (overrides DSN inference)")
	f.IntVar(&o.top, "top", 5, "number of slowest queries to analyze")
	f.Int64Var(&o.minCalls, "min-calls", 10, "ignore queries seen fewer than this many times")
	f.BoolVar(&o.jsonOut, "json", false, "emit JSON instead of human-readable text")
	f.StringVar(&o.model, "model", "claude-haiku-4-5-20251001", "Anthropic model id")
	f.BoolVar(&o.noCache, "no-cache", false, "skip cache lookup and do not write a new entry")
	f.DurationVar(&o.cacheTTL, "cache-ttl", 24*time.Hour, "how long a cached result remains valid")
	f.DurationVar(&o.timeout, "timeout", 60*time.Second, "per-query timeout for schema collection and analysis (0 = no limit)")
	f.BoolVar(&o.dryRun, "dry-run", false, "print what would be executed without connecting to the database")
	_ = cmd.MarkFlagRequired("dsn")
	return cmd
}

func runWatch(ctx context.Context, out io.Writer, o watchOpts) error {
	dialect, err := resolveDialect(o.dialect, o.dsn)
	if err != nil {
		return err
	}

	if o.dryRun {
		statView := "pg_stat_statements"
		if dialect == "mysql" {
			statView = "performance_schema.events_statements_summary_by_digest"
		}
		printDryRun(out, []drySection{
			{Title: "Connection", Lines: []string{
				"DSN:     " + maskDSN(o.dsn),
				"Dialect: " + dialect,
				"Mode:    read-only session",
			}},
			{Title: "Would query from database", Lines: []string{
				fmt.Sprintf("  SELECT from %s", statView),
				fmt.Sprintf("  WHERE calls >= %d", o.minCalls),
				fmt.Sprintf("  ORDER BY total_exec_time DESC LIMIT %d", o.top),
				"",
				"  Schema queries on pg_catalog / information_schema",
				"  for every table referenced in each slow query",
			}},
			{Title: "Would send to Claude API (per query)", Lines: []string{
				"  Query text + schema fragments",
				"  (credentials and row data never leave your machine)",
			}},
		})
		return nil
	}

	apiKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if apiKey == "" {
		return errors.New("ANTHROPIC_API_KEY is not set")
	}

	stmts, col, validate, err := fetchStatements(ctx, dialect, o.dsn, o.top, o.minCalls)
	if err != nil {
		return err
	}
	defer col.Close()

	if len(stmts) == 0 {
		fmt.Fprintf(out, "No queries found (min-calls=%d). Try lowering --min-calls.\n", o.minCalls)
		return nil
	}

	fmt.Fprintf(out, "Analyzing top %d slow queries from %s...\n\n", len(stmts), dialect)

	for i, s := range stmts {
		fmt.Fprintf(out, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n")
		fmt.Fprintf(out, "  Query %d/%d  calls=%d  mean=%.1fms  total=%.1fms\n",
			i+1, len(stmts), s.Calls, s.MeanMs, s.TotalMs)
		fmt.Fprintf(out, "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n\n")

		// Apply per-query timeout for schema collection + agent call.
		qctx := ctx
		if o.timeout > 0 {
			var cancel context.CancelFunc
			qctx, cancel = context.WithTimeout(ctx, o.timeout)
			defer cancel()
		}

		pack, err := col.Collect(qctx, s.Query, target.CollectOptions{SchemaOnly: true, LenientParse: true})
		if err != nil {
			fmt.Fprintf(out, "  [skip] collect failed: %v\n\n", err)
			continue
		}

		var result *agent.Result
		cacheKey := cache.Key(pack)
		if !o.noCache {
			if cached, ok := cache.Lookup(cacheKey, o.cacheTTL); ok {
				if !o.jsonOut {
					fmt.Fprintln(out, "  (from cache)")
				}
				result = cached
			}
		}

		if result == nil {
			result, err = agent.Analyze(qctx, agent.Request{
				APIKey:   apiKey,
				Model:    o.model,
				Context:  pack,
				Validate: validate,
			})
			if err != nil {
				fmt.Fprintf(out, "  [skip] agent failed: %v\n\n", err)
				continue
			}
			if !o.noCache {
				cache.Store(cacheKey, result, o.cacheTTL)
			}
		}

		if o.jsonOut {
			if err := report.WriteJSON(out, pack, result); err != nil {
				return err
			}
		} else {
			if err := report.WriteText(out, pack, result); err != nil {
				return err
			}
		}
		fmt.Fprintln(out)
	}

	return nil
}

// pgWatchCollector wraps postgres.Collector to implement collector + TopStatements.
type pgWatchCollector struct{ *postgres.Collector }

func (w *pgWatchCollector) TopStatements(ctx context.Context, n int, minCalls int64) ([]watchStatement, error) {
	raw, err := w.Collector.TopStatements(ctx, n, minCalls)
	if err != nil {
		return nil, err
	}
	out := make([]watchStatement, len(raw))
	for i, s := range raw {
		out[i] = watchStatement{Query: s.Query, Calls: s.Calls, TotalMs: s.TotalMs, MeanMs: s.MeanMs}
	}
	return out, nil
}

// myWatchCollector wraps mysql.Collector to implement watchCollector.
type myWatchCollector struct{ *mysql.Collector }

func (w *myWatchCollector) TopStatements(ctx context.Context, n int, minCalls int64) ([]watchStatement, error) {
	raw, err := w.Collector.TopStatements(ctx, n, minCalls)
	if err != nil {
		return nil, err
	}
	out := make([]watchStatement, len(raw))
	for i, s := range raw {
		out[i] = watchStatement{Query: s.Query, Calls: s.Calls, TotalMs: s.TotalMs, MeanMs: s.MeanMs}
	}
	return out, nil
}

func fetchStatements(ctx context.Context, dialect, dsn string, top int, minCalls int64) ([]watchStatement, collector, agent.Validator, error) {
	switch dialect {
	case "postgres":
		c, err := postgres.Open(ctx, dsn)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("connect: %w", err)
		}
		_ = c.SetReadOnly(ctx)
		wc := &pgWatchCollector{c}
		stmts, err := wc.TopStatements(ctx, top, minCalls)
		if err != nil {
			c.Close()
			return nil, nil, nil, err
		}
		return stmts, wc, postgres.ValidateOnly, nil
	case "mysql":
		c, err := mysql.Open(ctx, dsn)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("connect: %w", err)
		}
		_ = c.SetReadOnly(ctx)
		wc := &myWatchCollector{c}
		stmts, err := wc.TopStatements(ctx, top, minCalls)
		if err != nil {
			c.Close()
			return nil, nil, nil, err
		}
		return stmts, wc, mysql.ValidateOnly, nil
	}
	return nil, nil, nil, fmt.Errorf("unsupported dialect %q", dialect)
}
