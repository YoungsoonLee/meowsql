package postgres

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/YoungsoonLee/meowsql/internal/target"
)

// executor is satisfied by both *pgx.Conn and pgx.Tx.
type executor interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Bench measures wall-clock query latency before and (optionally) after
// applying DDL inside a rolled-back transaction.
// PostgreSQL supports transactional DDL, so no schema change ever persists.
func (c *Collector) Bench(ctx context.Context, opts target.BenchOptions) (*target.BenchResult, error) {
	if opts.Runs <= 0 {
		opts.Runs = 10
	}

	before, err := timePhase(ctx, c.conn, opts.SQL, opts.Runs, opts.Warmup)
	if err != nil {
		return nil, fmt.Errorf("before phase: %w", err)
	}

	result := &target.BenchResult{Before: before, After: before}

	if opts.DDL == "" {
		return result, nil
	}

	tx, err := c.conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, stmt := range splitStatements(opts.DDL) {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return nil, fmt.Errorf("apply ddl %q: %w", truncate(stmt, 60), err)
		}
	}

	after, err := timePhase(ctx, tx, opts.SQL, opts.Runs, opts.Warmup)
	if err != nil {
		return nil, fmt.Errorf("after phase: %w", err)
	}
	result.After = after
	if after.P50 > 0 && before.P50 > 0 {
		result.SpeedupX = float64(before.P50) / float64(after.P50)
	}
	return result, nil
}

func timePhase(ctx context.Context, ex executor, sql string, runs, warmup int) (target.RunStats, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(sql), ";")
	for range warmup {
		if err := drainQuery(ctx, ex, trimmed); err != nil {
			return target.RunStats{}, err
		}
	}
	durations := make([]time.Duration, runs)
	for i := range runs {
		t := time.Now()
		if err := drainQuery(ctx, ex, trimmed); err != nil {
			return target.RunStats{}, err
		}
		durations[i] = time.Since(t)
	}
	return computeStats(durations), nil
}

func drainQuery(ctx context.Context, ex executor, sql string) error {
	rows, err := ex.Query(ctx, sql)
	if err != nil {
		return fmt.Errorf("execute: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
	}
	return rows.Err()
}

func computeStats(d []time.Duration) target.RunStats {
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
