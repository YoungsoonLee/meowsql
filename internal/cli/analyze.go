package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/YoungsoonLee/meowsql/internal/agent"
	"github.com/YoungsoonLee/meowsql/internal/db/postgres"
	"github.com/YoungsoonLee/meowsql/internal/report"
	"github.com/spf13/cobra"
)

type analyzeOpts struct {
	dsn        string
	query      string
	file       string
	runAnalyze bool
	schemaOnly bool
	jsonOut    bool
	model      string
}

func newAnalyzeCmd() *cobra.Command {
	var o analyzeOpts
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Diagnose a slow SQL query and suggest fixes",
		Long: `Analyze connects to your database, parses the given SQL with the real
PostgreSQL parser, collects the schema and EXPLAIN plan for referenced tables,
and asks Claude to propose indexes and rewrites — grounded in that real context.

SQL input (pick one):
  --query "SELECT ..."     inline
  --file path/to.sql       from a file
  (stdin)                  piped in

Safety:
  --analyze runs EXPLAIN (ANALYZE, BUFFERS) inside a transaction that is
  always rolled back, so writes do not persist. Still, treat --analyze as
  "executes the query" and avoid it on production primaries.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runAnalyze(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout(), o)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.dsn, "dsn", "", "PostgreSQL connection string (required)")
	f.StringVar(&o.query, "query", "", "inline SQL to analyze")
	f.StringVar(&o.file, "file", "", "path to SQL file")
	f.BoolVar(&o.runAnalyze, "analyze", false, "run EXPLAIN (ANALYZE, BUFFERS) inside a rolled-back transaction")
	f.BoolVar(&o.schemaOnly, "schema-only", false, "skip EXPLAIN; use schema + stats only")
	f.BoolVar(&o.jsonOut, "json", false, "emit JSON instead of human-readable text")
	f.StringVar(&o.model, "model", "claude-haiku-4-5-20251001", "Anthropic model id")
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

	apiKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if apiKey == "" {
		return errors.New("ANTHROPIC_API_KEY is not set")
	}

	col, err := postgres.Open(ctx, o.dsn)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer col.Close()

	pack, err := col.Collect(ctx, sql, postgres.CollectOptions{
		RunAnalyze: o.runAnalyze,
		SchemaOnly: o.schemaOnly,
	})
	if err != nil {
		return fmt.Errorf("collect context: %w", err)
	}

	result, err := agent.Analyze(ctx, agent.Request{
		APIKey:  apiKey,
		Model:   o.model,
		Context: pack,
	})
	if err != nil {
		return fmt.Errorf("agent: %w", err)
	}

	if o.jsonOut {
		return report.WriteJSON(out, pack, result)
	}
	return report.WriteText(out, pack, result)
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
