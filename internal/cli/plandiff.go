package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/YoungsoonLee/meowsql/internal/db/postgres"
	"github.com/spf13/cobra"
)

// DiffResult is the machine-readable output of plan-diff.
type DiffResult struct {
	QueryFile     string  `json:"query_file"`
	MigrationFile string  `json:"migration_file,omitempty"`
	Before        CostRow `json:"before"`
	After         CostRow `json:"after"`
	PctChange     float64 `json:"pct_change"` // positive = more expensive
	Regression    bool    `json:"regression"`
	Error         string  `json:"error,omitempty"`
}

// CostRow is a single EXPLAIN cost snapshot.
type CostRow struct {
	Cost     float64 `json:"cost"`
	PlanType string  `json:"plan_type"`
}

type planDiffOpts struct {
	dsn           string
	queryFile     string
	migrationFile string
	threshold     float64
	exitCode      bool
	timeout       time.Duration
	dryRun        bool
}

func newPlanDiffCmd() *cobra.Command {
	var o planDiffOpts
	cmd := &cobra.Command{
		Use:   "plan-diff",
		Short: "Compare EXPLAIN costs before and after a DDL migration (PostgreSQL only)",
		Long: `plan-diff measures whether a DDL change makes a query's plan more expensive.

Steps:
  1. EXPLAIN query → baseline cost
  2. Apply migration DDL inside a BEGIN / ROLLBACK transaction
  3. EXPLAIN query again → after cost
  4. Roll back — the schema change never persists

PostgreSQL supports transactional DDL (CREATE INDEX, DROP INDEX, ALTER TABLE),
so this is safe on a shared test database.

Exit codes with --exit-code:
  0  no regression
  1  cost rose beyond --threshold`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPlanDiff(cmd.Context(), cmd.OutOrStdout(), o)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.dsn, "dsn", "", "PostgreSQL connection string (required)")
	f.StringVar(&o.queryFile, "file", "", "SQL query file to evaluate (required)")
	f.StringVar(&o.migrationFile, "migration", "", "DDL file to apply inside a rolled-back transaction")
	f.Float64Var(&o.threshold, "threshold", 10.0, "regression threshold in percent")
	f.BoolVar(&o.exitCode, "exit-code", false, "exit 1 when a regression is detected")
	f.DurationVar(&o.timeout, "timeout", 60*time.Second, "EXPLAIN timeout per phase (0 = no limit)")
	f.BoolVar(&o.dryRun, "dry-run", false, "print what would be executed without connecting to the database")
	_ = cmd.MarkFlagRequired("dsn")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func runPlanDiff(ctx context.Context, out io.Writer, o planDiffOpts) error {
	querySql, err := os.ReadFile(o.queryFile)
	if err != nil {
		return fmt.Errorf("read --file: %w", err)
	}

	if o.dryRun {
		migLines := []string{"  EXPLAIN query → baseline cost"}
		if o.migrationFile != "" {
			migLines = append(migLines,
				"  BEGIN",
				"    Apply migration from: "+o.migrationFile,
				"    EXPLAIN query → after cost",
				"  ROLLBACK  ← schema change never persists",
			)
		}
		printDryRun(out, []drySection{
			{Title: "Connection", Lines: []string{
				"DSN:     " + maskDSN(o.dsn),
				"Dialect: postgres",
			}},
			{Title: "Would execute against the database", Lines: migLines},
		})
		return nil
	}

	if o.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, o.timeout)
		defer cancel()
	}

	col, err := postgres.Open(ctx, o.dsn)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer col.Close()

	// Prevent EXPLAIN from blocking behind long-held table locks.
	_ = col.ApplySafetySettings(ctx, 5*time.Second)

	before, err := col.ExplainCost(ctx, string(querySql))
	if err != nil {
		return fmt.Errorf("baseline explain: %w", err)
	}

	result := DiffResult{
		QueryFile: o.queryFile,
		Before:    CostRow{Cost: before.TotalCost, PlanType: before.PlanType},
		After:     CostRow{Cost: before.TotalCost, PlanType: before.PlanType},
	}

	if o.migrationFile != "" {
		result.MigrationFile = o.migrationFile
		ddl, err := os.ReadFile(o.migrationFile)
		if err != nil {
			return fmt.Errorf("read --migration: %w", err)
		}
		after, err := col.ExplainCostAfterDDL(ctx, string(querySql), string(ddl))
		if err != nil {
			result.Error = err.Error()
		} else {
			result.After = CostRow{Cost: after.TotalCost, PlanType: after.PlanType}
		}
	}

	if result.Before.Cost > 0 {
		result.PctChange = (result.After.Cost - result.Before.Cost) / result.Before.Cost * 100
	}
	result.Regression = result.PctChange > o.threshold

	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(result); err != nil {
		return err
	}

	if o.exitCode && result.Regression {
		// Use os.Exit so cobra doesn't print an error message; the JSON output
		// already contains all the information the caller needs.
		os.Exit(1)
	}
	return nil
}
