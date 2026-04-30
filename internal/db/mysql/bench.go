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
// Every individual query execution — SELECT or DML — is wrapped in its own
// BEGIN / ROLLBACK transaction so that UPDATE / DELETE / INSERT results are
// never committed. Timing covers only the query + row fetch, not the
// transaction management overhead.
//
// MySQL DDL (CREATE INDEX) is NOT transactional, so the index is created for
// real, measurements are taken, then the index is dropped. If DROP fails, the
// error message names the index so the caller can clean up manually.
func (c *Collector) Bench(ctx context.Context, opts target.BenchOptions) (*target.BenchResult, error) {
	if opts.Runs <= 0 {
		opts.Runs = 10
	}

	trimmed := strings.TrimRight(strings.TrimSpace(opts.SQL), ";")

	before, err := mysqlTimePhase(ctx, c.db, trimmed, opts.Runs, opts.Warmup, opts.Timeout)
	if err != nil {
		return nil, fmt.Errorf("before phase: %w", err)
	}

	result := &target.BenchResult{Before: before, After: before}
	if opts.DDL == "" {
		return result, nil
	}

	indexName, tableName, err := parseCreateIndex(opts.DDL)
	if err != nil {
		return nil, err
	}

	// Apply DDL — commits immediately in MySQL.
	for s := range strings.SplitSeq(opts.DDL, ";") {
		s = strings.TrimSpace(s)
		if s == "" || strings.HasPrefix(s, "--") {
			continue
		}
		if _, err := c.db.ExecContext(ctx, s); err != nil {
			return nil, fmt.Errorf("apply ddl: %w", err)
		}
	}

	after, err := mysqlTimePhase(ctx, c.db, trimmed, opts.Runs, opts.Warmup, opts.Timeout)

	// Always drop the index, even if the after phase failed.
	dropSQL := fmt.Sprintf("DROP INDEX `%s` ON `%s`", indexName, tableName)
	if _, dropErr := c.db.ExecContext(ctx, dropSQL); dropErr != nil {
		return nil, fmt.Errorf(
			"bench completed but DROP INDEX failed — index `%s` on `%s` still exists: %w",
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

// mysqlTimePhase runs warmup then measured iterations. Each run is wrapped in
// its own BEGIN/ROLLBACK so DML never commits.
func mysqlTimePhase(ctx context.Context, db *sql.DB, query string, runs, warmup int, timeout time.Duration) (target.RunStats, error) {
	run := func(ctx context.Context) (time.Duration, error) {
		return mysqlRunInTx(ctx, db, query, timeout)
	}
	for range warmup {
		if _, err := run(ctx); err != nil {
			return target.RunStats{}, err
		}
	}
	durations := make([]time.Duration, runs)
	for i := range runs {
		d, err := run(ctx)
		if err != nil {
			return target.RunStats{}, err
		}
		durations[i] = d
	}
	return mysqlComputeStats(durations), nil
}

// mysqlRunInTx executes query inside a fresh transaction that is always
// rolled back. Timing covers only the query + row fetch.
// MAX_EXECUTION_TIME hint is injected into SELECT queries when a timeout is
// set; for DML the timeout relies on the context deadline.
func mysqlRunInTx(ctx context.Context, db *sql.DB, query string, timeout time.Duration) (time.Duration, error) {
	runCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	tx, err := db.BeginTx(runCtx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	// Inject MAX_EXECUTION_TIME hint for SELECT queries (MySQL 5.7.8+).
	// This enforces the timeout server-side, independent of the Go context.
	q := query
	if timeout > 0 && isSelect(query) {
		ms := timeout.Milliseconds()
		q = fmt.Sprintf("SELECT /*+ MAX_EXECUTION_TIME(%d) */ %s", ms,
			query[len("SELECT"):])
	}

	t := time.Now()
	rows, err := tx.QueryContext(runCtx, q)
	if err != nil {
		return 0, fmt.Errorf("execute: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return time.Since(t), nil
}

func isSelect(sql string) bool {
	return len(sql) >= 6 && strings.EqualFold(sql[:6], "select")
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
			"--index must be a CREATE INDEX statement for MySQL " +
				"(e.g. CREATE INDEX idx_name ON tbl (col)); got: %s",
			truncateMySQL(ddl, 80),
		)
	}
	return m[1], m[2], nil
}

func truncateMySQL(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
