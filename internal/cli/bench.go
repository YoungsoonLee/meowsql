package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/YoungsoonLee/meowsql/internal/db/mysql"
	"github.com/YoungsoonLee/meowsql/internal/db/postgres"
	"github.com/YoungsoonLee/meowsql/internal/target"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

type benchOpts struct {
	dsn           string
	dialect       string
	queryFile     string
	indexDDL      string
	migrationFile string
	runs          int
	warmup        int
	timeout       time.Duration
	allowDML      bool
	jsonOut       bool
}


// BenchReport is the machine-readable output of bench.
type BenchReport struct {
	QueryFile string         `json:"query_file"`
	Dialect   string         `json:"dialect"`
	IndexDDL  string         `json:"index_ddl,omitempty"`
	Runs      int            `json:"runs"`
	Warmup    int            `json:"warmup"`
	Before    BenchStatsJSON `json:"before"`
	After     BenchStatsJSON `json:"after"`
	SpeedupX  float64        `json:"speedup_x,omitempty"`
}

type BenchStatsJSON struct {
	MeanMs float64 `json:"mean_ms"`
	P50Ms  float64 `json:"p50_ms"`
	P99Ms  float64 `json:"p99_ms"`
	MinMs  float64 `json:"min_ms"`
	MaxMs  float64 `json:"max_ms"`
}

// bencher is implemented by both postgres.Collector and mysql.Collector.
type bencher interface {
	Bench(ctx context.Context, opts target.BenchOptions) (*target.BenchResult, error)
	Close() error
}

func newBenchCmd() *cobra.Command {
	var o benchOpts
	cmd := &cobra.Command{
		Use:   "bench",
		Short: "Measure query latency before and after an index (PostgreSQL and MySQL)",
		Long: `bench executes a query N times and records wall-clock latency before and
after applying an index — giving you concrete numbers to prove a speedup.

PostgreSQL: DDL is applied inside BEGIN / ROLLBACK — no schema change persists.

MySQL: DDL is NOT transactional. bench creates the index, measures, then drops
it. The index name is parsed from the CREATE INDEX statement automatically.
If the DROP fails (e.g. server crash), the error message names the index so
you can clean up manually.

Use --index for a one-liner or --index-file for a larger DDL file.
Omit both to record a baseline without a comparison.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runBench(cmd.Context(), cmd.OutOrStdout(), o)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.dsn, "dsn", "", "database connection string (required)")
	f.StringVar(&o.dialect, "dialect", "", "postgres|mysql (overrides DSN inference)")
	f.StringVar(&o.queryFile, "file", "", "SQL query file to benchmark (required)")
	f.StringVar(&o.indexDDL, "index", "", "index DDL to apply (CREATE INDEX ...)")
	f.StringVar(&o.migrationFile, "index-file", "", "file containing DDL (alternative to --index)")
	f.IntVar(&o.runs, "runs", 10, "number of measured executions per phase")
	f.IntVar(&o.warmup, "warmup", 2, "number of unmeasured warm-up executions per phase")
	f.DurationVar(&o.timeout, "timeout", 30*time.Second, "per-query statement timeout (0 = no limit)")
	f.BoolVar(&o.allowDML, "allow-dml", false, "required when the query is UPDATE/DELETE/INSERT/TRUNCATE")
	f.BoolVar(&o.jsonOut, "json", false, "machine-readable JSON output")
	_ = cmd.MarkFlagRequired("dsn")
	_ = cmd.MarkFlagRequired("file")
	return cmd
}

func runBench(ctx context.Context, out io.Writer, o benchOpts) error {
	querySql, err := os.ReadFile(o.queryFile)
	if err != nil {
		return fmt.Errorf("read --file: %w", err)
	}

	ddl := strings.TrimSpace(o.indexDDL)
	if o.migrationFile != "" {
		if ddl != "" {
			return fmt.Errorf("--index and --index-file are mutually exclusive")
		}
		b, err := os.ReadFile(o.migrationFile)
		if err != nil {
			return fmt.Errorf("read --index-file: %w", err)
		}
		ddl = strings.TrimSpace(string(b))
	}

	// DML guard: require explicit opt-in so users are never surprised by
	// accidental writes. Each run IS rolled back, but the flag ensures the
	// user has read the warning.
	if isDML(string(querySql)) && !o.allowDML {
		return fmt.Errorf(
			"the query appears to be a DML statement (UPDATE/DELETE/INSERT/TRUNCATE).\n" +
				"Each run is wrapped in BEGIN/ROLLBACK so data is never committed,\n" +
				"but pass --allow-dml to confirm you understand this and proceed.",
		)
	}

	dialect, err := resolveDialect(o.dialect, o.dsn)
	if err != nil {
		return err
	}

	b, err := openBencher(ctx, dialect, o.dsn)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer b.Close()

	if !o.jsonOut {
		dim := color.New(color.FgHiBlack).SprintFunc()
		timeoutNote := ""
		if o.timeout > 0 {
			timeoutNote = fmt.Sprintf(" timeout=%s.", o.timeout)
		}
		if dialect == "mysql" && ddl != "" {
			fmt.Fprintf(out, "%s\n\n",
				dim(fmt.Sprintf("Note: each run is wrapped in BEGIN/ROLLBACK — DML is safe.%s "+
					"The index will be created then dropped (MySQL DDL is not transactional).", timeoutNote)),
			)
		} else {
			fmt.Fprintf(out, "%s\n\n",
				dim(fmt.Sprintf("Note: each run is wrapped in BEGIN/ROLLBACK — UPDATE/DELETE/INSERT are never committed.%s", timeoutNote)),
			)
		}
	}

	result, err := b.Bench(ctx, target.BenchOptions{
		SQL:     string(querySql),
		DDL:     ddl,
		Runs:    o.runs,
		Warmup:  o.warmup,
		Timeout: o.timeout,
	})
	if err != nil {
		return fmt.Errorf("bench: %w", err)
	}

	rep := BenchReport{
		QueryFile: o.queryFile,
		Dialect:   dialect,
		IndexDDL:  ddl,
		Runs:      o.runs,
		Warmup:    o.warmup,
		Before:    toJSON(result.Before),
		After:     toJSON(result.After),
		SpeedupX:  result.SpeedupX,
	}

	if o.jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		return enc.Encode(rep)
	}
	return writeBenchText(out, dialect, rep)
}

func openBencher(ctx context.Context, dialect, dsn string) (bencher, error) {
	switch dialect {
	case "postgres":
		return postgres.Open(ctx, dsn)
	case "mysql":
		return mysql.Open(ctx, dsn)
	}
	return nil, fmt.Errorf("unsupported dialect %q", dialect)
}

func writeBenchText(out io.Writer, dialect string, r BenchReport) error {
	hdr := color.New(color.FgHiCyan, color.Bold).SprintFunc()
	sec := color.New(color.FgHiYellow, color.Bold).SprintFunc()
	dim := color.New(color.FgHiBlack).SprintFunc()
	grn := color.New(color.FgGreen).SprintFunc()

	dbLabel := "PostgreSQL"
	if dialect == "mysql" {
		dbLabel = "MySQL"
	}

	fmt.Fprintf(out, "%s\n\n", hdr("🐾 MeowSQL Bench — "+dbLabel))
	fmt.Fprintf(out, "  %-10s %s\n", dim("Query"), r.QueryFile)
	fmt.Fprintf(out, "  %-10s %d (+ %d warmup)\n", dim("Runs"), r.Runs, r.Warmup)
	if r.IndexDDL != "" {
		short := r.IndexDDL
		if len(short) > 72 {
			short = short[:72] + "…"
		}
		fmt.Fprintf(out, "  %-10s %s\n", dim("Index"), short)
	}
	fmt.Fprintln(out)

	const colW = 10
	fmt.Fprintf(out, "  %s  %s  %s  %s  %s  %s\n",
		pad("", 8),
		pad("mean", colW), pad("p50", colW), pad("p99", colW),
		pad("min", colW), pad("max", colW),
	)
	fmt.Fprintf(out, "  %s\n", strings.Repeat("─", 10+(colW+2)*5))

	fmt.Fprintf(out, "  %s  %s  %s  %s  %s  %s\n",
		sec(pad("Before", 8)),
		pad(fmtMs(r.Before.MeanMs), colW),
		pad(fmtMs(r.Before.P50Ms), colW),
		pad(fmtMs(r.Before.P99Ms), colW),
		pad(fmtMs(r.Before.MinMs), colW),
		pad(fmtMs(r.Before.MaxMs), colW),
	)

	if r.IndexDDL != "" {
		fmt.Fprintf(out, "  %s  %s  %s  %s  %s  %s\n",
			sec(pad("After", 8)),
			pad(fmtMs(r.After.MeanMs), colW),
			pad(fmtMs(r.After.P50Ms), colW),
			pad(fmtMs(r.After.P99Ms), colW),
			pad(fmtMs(r.After.MinMs), colW),
			pad(fmtMs(r.After.MaxMs), colW),
		)
		fmt.Fprintln(out)
		if r.SpeedupX >= 2 {
			fmt.Fprintf(out, "  %s  %s  %s\n",
				sec("Speedup"),
				grn(fmt.Sprintf("%.0f× faster", r.SpeedupX)),
				dim("(p50 basis)"),
			)
		} else if r.SpeedupX > 0 {
			fmt.Fprintf(out, "  %s  %s  %s\n",
				sec("Speedup"),
				fmt.Sprintf("%.2f×", r.SpeedupX),
				dim("(p50 basis — no significant improvement)"),
			)
		}
	}
	fmt.Fprintln(out)
	return nil
}

func toJSON(s target.RunStats) BenchStatsJSON {
	ms := func(d interface{ Seconds() float64 }) float64 { return d.Seconds() * 1000 }
	return BenchStatsJSON{
		MeanMs: ms(s.Mean),
		P50Ms:  ms(s.P50),
		P99Ms:  ms(s.P99),
		MinMs:  ms(s.Min),
		MaxMs:  ms(s.Max),
	}
}

func fmtMs(ms float64) string {
	if ms < 1 {
		return fmt.Sprintf("%.2fms", ms)
	}
	if ms < 1000 {
		return fmt.Sprintf("%.1fms", ms)
	}
	return fmt.Sprintf("%.2fs", ms/1000)
}

func pad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}
