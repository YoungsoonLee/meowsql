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
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

type analyzeOpts struct {
	dsn        string
	dialect    string
	query      string
	file       string
	runAnalyze bool
	schemaOnly bool
	jsonOut    bool
	model      string
	noCache    bool
	cacheTTL   time.Duration
	timeout    time.Duration
	dryRun     bool
}

type collector interface {
	Collect(ctx context.Context, sql string, opts target.CollectOptions) (*target.ContextPack, error)
	Close() error
}

func newAnalyzeCmd() *cobra.Command {
	var o analyzeOpts
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Diagnose a slow SQL query and suggest fixes",
		Long: `Analyze connects to your database, parses the given SQL, collects the schema
and EXPLAIN plan for referenced tables, and asks Claude to propose indexes and
rewrites — grounded in that real context.

Dialect is inferred from the DSN (postgres://, postgresql://, mysql://, or
user:pass@tcp(host)/db) and can be overridden with --dialect.

SQL input (pick one):
  --query "SELECT ..."     inline
  --file path/to.sql       from a file
  (stdin)                  piped in

Safety:
  --analyze runs EXPLAIN (ANALYZE) inside a transaction that is always rolled
  back, so writes do not persist. Still, treat --analyze as "executes the
  query" and avoid it on production primaries.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAnalyze(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), o)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.dsn, "dsn", "", "database connection string (required)")
	f.StringVar(&o.dialect, "dialect", "", "postgres|mysql (overrides DSN inference)")
	f.StringVar(&o.query, "query", "", "inline SQL to analyze")
	f.StringVar(&o.file, "file", "", "path to SQL file")
	f.BoolVar(&o.runAnalyze, "analyze", false, "run EXPLAIN ANALYZE inside a rolled-back transaction")
	f.BoolVar(&o.schemaOnly, "schema-only", false, "skip EXPLAIN; use schema + stats only")
	f.BoolVar(&o.jsonOut, "json", false, "emit JSON instead of human-readable text")
	f.StringVar(&o.model, "model", "claude-haiku-4-5-20251001", "Anthropic model id")
	f.BoolVar(&o.noCache, "no-cache", false, "skip cache lookup and do not write a new entry")
	f.DurationVar(&o.cacheTTL, "cache-ttl", 24*time.Hour, "how long a cached result remains valid")
	f.DurationVar(&o.timeout, "timeout", 60*time.Second, "overall query timeout for EXPLAIN and schema collection (0 = no limit)")
	f.BoolVar(&o.dryRun, "dry-run", false, "print what would be executed without connecting to the database")
	_ = cmd.MarkFlagRequired("dsn")
	return cmd
}

func runAnalyze(ctx context.Context, in io.Reader, out io.Writer, o analyzeOpts) error {
	if o.runAnalyze && o.schemaOnly {
		return errors.New("--analyze and --schema-only are mutually exclusive")
	}

	sql, err := readSQL(in, o)
	if err != nil {
		return err
	}

	dialect, err := resolveDialect(o.dialect, o.dsn)
	if err != nil {
		return err
	}

	if o.dryRun {
		explainClause := "EXPLAIN (FORMAT JSON, VERBOSE, COSTS)"
		if o.runAnalyze {
			explainClause = "EXPLAIN (ANALYZE, BUFFERS, FORMAT JSON, VERBOSE, COSTS)"
		}
		sections := []drySection{
			{Title: "Connection", Lines: []string{
				"DSN:     " + maskDSN(o.dsn),
				"Dialect: " + dialect,
				"Mode:    read-only session (no writes possible)",
			}},
			{Title: "Would execute against the database", Lines: []string{
				"  " + explainClause,
				"  " + firstLine(sql),
				"  ...",
				"",
				"  Schema queries on pg_catalog / information_schema",
				"  for every table referenced in the query",
			}},
			{Title: "Would send to Claude API", Lines: []string{
				"  Query text + EXPLAIN plan + schema fragments",
				"  (credentials and row data never leave your machine)",
			}},
		}
		if o.runAnalyze && isDML(sql) {
			sections[1].Lines = append([]string{
				"⚠  DML detected — runs inside BEGIN/ROLLBACK, never committed",
				"",
			}, sections[1].Lines...)
		}
		printDryRun(out, sections)
		return nil
	}

	// Warn when --analyze will actually execute a DML query (rolled back, but runs).
	if o.runAnalyze && isDML(sql) && !o.jsonOut {
		dim := color.New(color.FgHiBlack).SprintFunc()
		fmt.Fprintf(out, "%s\n\n",
			dim("Note: --analyze will execute this DML query inside a rolled-back transaction. "+
				"The statement runs for real (to produce EXPLAIN ANALYZE output) but no data is committed."),
		)
	}

	if o.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.timeout)
		defer cancel()
	}

	apiKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if apiKey == "" {
		return errors.New("ANTHROPIC_API_KEY is not set")
	}

	// Read-only session for commands that only need EXPLAIN/schema — never write.
	readOnly := !o.runAnalyze && !o.schemaOnly
	col, validate, err := openCollector(ctx, dialect, o.dsn, readOnly)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer col.Close()

	pack, err := col.Collect(ctx, sql, target.CollectOptions{
		RunAnalyze: o.runAnalyze,
		SchemaOnly: o.schemaOnly,
	})
	if err != nil {
		return fmt.Errorf("collect context: %w", err)
	}

	cacheKey := cache.Key(pack)
	if !o.noCache {
		if cached, ok := cache.Lookup(cacheKey, o.cacheTTL); ok {
			if !o.jsonOut {
				fmt.Fprintln(out, "(from cache)")
			}
			return reportResult(out, pack, cached, o.jsonOut)
		}
	}

	result, err := agent.Analyze(ctx, agent.Request{
		APIKey:   apiKey,
		Model:    o.model,
		Context:  pack,
		Validate: validate,
	})
	if err != nil {
		return fmt.Errorf("agent: %w", err)
	}

	if !o.noCache {
		cache.Store(cacheKey, result, o.cacheTTL)
	}

	return reportResult(out, pack, result, o.jsonOut)
}

func resolveDialect(override, dsn string) (string, error) {
	if override != "" {
		switch override {
		case "postgres", "postgresql":
			return "postgres", nil
		case "mysql":
			return "mysql", nil
		default:
			return "", fmt.Errorf("unknown dialect %q (want postgres|mysql)", override)
		}
	}
	switch {
	case strings.HasPrefix(dsn, "postgres://"), strings.HasPrefix(dsn, "postgresql://"):
		return "postgres", nil
	case strings.HasPrefix(dsn, "mysql://"), strings.Contains(dsn, "@tcp("):
		return "mysql", nil
	}
	return "", errors.New("cannot infer dialect from DSN; pass --dialect postgres|mysql")
}

func openCollector(ctx context.Context, dialect, dsn string, readOnly bool) (collector, agent.Validator, error) {
	switch dialect {
	case "postgres":
		c, err := postgres.Open(ctx, dsn)
		if err != nil {
			return nil, nil, err
		}
		_ = c.ApplySafetySettings(ctx, 5*time.Second)
		if readOnly {
			_ = c.SetReadOnly(ctx)
		}
		return c, postgres.ValidateOnly, nil
	case "mysql":
		c, err := mysql.Open(ctx, dsn)
		if err != nil {
			return nil, nil, err
		}
		if readOnly {
			_ = c.SetReadOnly(ctx)
		}
		return c, mysql.ValidateOnly, nil
	}
	return nil, nil, fmt.Errorf("unsupported dialect %q", dialect)
}

func readSQL(in io.Reader, o analyzeOpts) (string, error) {
	switch {
	case o.query != "" && o.file != "":
		return "", errors.New("--query and --file are mutually exclusive")
	case o.query != "":
		return o.query, nil
	case o.file != "":
		b, err := os.ReadFile(o.file)
		if err != nil {
			return "", fmt.Errorf("read --file: %w", err)
		}
		return string(b), nil
	}

	b, err := io.ReadAll(in)
	if err != nil {
		return "", fmt.Errorf("read stdin: %w", err)
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", errors.New("no SQL provided: use --query, --file, or pipe via stdin")
	}
	return s, nil
}

func reportResult(out io.Writer, pack *target.ContextPack, result *agent.Result, jsonOut bool) error {
	if jsonOut {
		return report.WriteJSON(out, pack, result)
	}
	return report.WriteText(out, pack, result)
}
