package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/YoungsoonLee/meowsql/internal/target"
)

// reCreateIndex matches: CREATE [UNIQUE] INDEX idx ON tbl (...)
var reCreateIndex = regexp.MustCompile(
	`(?i)CREATE\s+(?:UNIQUE\s+)?INDEX\s+` + "`?" + `(\w+)` + "`?" + `\s+ON\s+` + "`?" + `(\w+)` + "`?",
)

// Bench measures wall-clock query latency before and after applying an index.
//
// Unlike PostgreSQL, MySQL DDL is NOT transactional — CREATE INDEX commits
// immediately. Bench therefore follows a real create → measure → drop cycle.
// If the DROP fails the error message names the index so the caller can clean
// up manually.
func (c *Collector) Bench(ctx context.Context, opts target.BenchOptions) (*target.BenchResult, error) {
	if opts.Runs <= 0 {
		opts.Runs = 10
	}

	before, err := mysqlTimePhase(ctx, c.db, opts.SQL, opts.Runs, opts.Warmup)
	if err != nil {
		return nil, fmt.Errorf("before phase: %w", err)
	}

	result := &target.BenchResult{Before: before, After: before}

	if opts.DDL == "" {
		return result, nil
	}

	// Parse CREATE INDEX so we know what to DROP afterward.
	indexName, tableName, err := parseCreateIndex(opts.DDL)
	if err != nil {
		return nil, err
	}

	// Apply DDL (commits immediately in MySQL).
	for _, stmt := range splitMySQL(opts.DDL) {
		if _, err := c.db.ExecContext(ctx, stmt); err != nil {
			return nil, fmt.Errorf("apply ddl: %w", err)
		}
	}

	after, err := mysqlTimePhase(ctx, c.db, opts.SQL, opts.Runs, opts.Warmup)

	// Always attempt cleanup, even if the after phase failed.
	dropSQL := fmt.Sprintf("DROP INDEX `%s` ON `%s`", indexName, tableName)
	if _, dropErr := c.db.ExecContext(ctx, dropSQL); dropErr != nil {
		// If the after phase also failed, surface the original error.
		// If it succeeded, the drop failure is the most important thing to surface.
		return nil, fmt.Errorf(
			"bench completed but DROP INDEX failed — index `%s` still exists on `%s`: %w",
			indexName, tableName, dropErr,
		)
	}

	if err != nil {
		return nil, fmt.Errorf("after phase: %w", err)
	}

	result.After = after
	if after.P50 > 0 && before.P50 > 0 {
		result.SpeedupX = float64(before.P50) / float64(after.P50)
	}
	return result, nil
}

func mysqlTimePhase(ctx context.Context, db *sql.DB, sql string, runs, warmup int) (target.RunStats, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(sql), ";")
	for range warmup {
		if err := mysqlDrainQuery(ctx, db, trimmed); err != nil {
			return target.RunStats{}, err
		}
	}
	durations := make([]time.Duration, runs)
	for i := range runs {
		t := time.Now()
		if err := mysqlDrainQuery(ctx, db, trimmed); err != nil {
			return target.RunStats{}, err
		}
		durations[i] = time.Since(t)
	}
	return mysqlComputeStats(durations), nil
}

func mysqlDrainQuery(ctx context.Context, db *sql.DB, query string) error {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("execute: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}

func mysqlComputeStats(d []time.Duration) target.RunStats {
	n := len(d)
	if n == 0 {
		return target.RunStats{}
	}
	slices.Sort(d)
	var sum time.Duration
	for _, v := range d {
		sum += v
	}
	return target.RunStats{
		Mean: sum / time.Duration(n),
		P50:  d[n/2],
		P99:  d[max(0, int(float64(n)*0.99)-1)],
		Min:  d[0],
		Max:  d[n-1],
	}
}

func parseCreateIndex(ddl string) (indexName, tableName string, err error) {
	m := reCreateIndex.FindStringSubmatch(ddl)
	if m == nil {
		return "", "", fmt.Errorf(
			"--index DDL must be a CREATE INDEX statement for MySQL bench " +
				"(e.g. CREATE INDEX idx_name ON tbl (col)); got: %s",
			truncateMySQL(ddl, 80),
		)
	}
	return m[1], m[2], nil
}

// splitMySQL splits DDL on semicolons, skipping blank/comment-only chunks.
func splitMySQL(ddl string) []string {
	var out []string
	for s := range strings.SplitSeq(ddl, ";") {
		s = strings.TrimSpace(s)
		if s == "" || strings.HasPrefix(s, "--") {
			continue
		}
		out = append(out, s)
	}
	return out
}

func truncateMySQL(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
