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
//
// Every individual query execution — SELECT or DML — is wrapped in its own
// BEGIN / ROLLBACK (before phase) or SAVEPOINT / ROLLBACK TO SAVEPOINT (after
// phase, which is already inside the outer DDL transaction). This means the
// bench is safe to run against production: UPDATE / DELETE / INSERT results
// are never committed.
func (c *Collector) Bench(ctx context.Context, opts target.BenchOptions) (*target.BenchResult, error) {
	if opts.Runs <= 0 {
		opts.Runs = 10
	}

	trimmed := strings.TrimRight(strings.TrimSpace(opts.SQL), ";")

	// Before phase: each run gets its own BEGIN / ROLLBACK.
	before, err := timePhase(ctx, opts.Runs, opts.Warmup, func(ctx context.Context) (time.Duration, error) {
		return runInTx(ctx, c.conn, trimmed, opts.Timeout)
	})
	if err != nil {
		return nil, fmt.Errorf("before phase: %w", err)
	}

	result := &target.BenchResult{Before: before, After: before}
	if opts.DDL == "" {
		return result, nil
	}

	// Outer DDL transaction — never committed.
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

	// After phase: outer tx holds the index; each run uses a savepoint so
	// DML is still rolled back within the already-open transaction.
	after, err := timePhase(ctx, opts.Runs, opts.Warmup, func(ctx context.Context) (time.Duration, error) {
		return runInSavepoint(ctx, tx, trimmed, opts.Timeout)
	})
	if err != nil {
		return nil, fmt.Errorf("after phase: %w", err)
	}

	result.After = after
	if after.P50 > 0 && before.P50 > 0 {
		result.SpeedupX = float64(before.P50) / float64(after.P50)
	}
	return result, nil
}

// runInTx executes sql inside a fresh BEGIN/ROLLBACK transaction.
// Timing covers only the query + row fetch — not the transaction management.
// This makes UPDATE/DELETE safe: results are always rolled back.
// statement_timeout and lock_timeout are applied locally so they never affect
// other sessions.
func runInTx(ctx context.Context, conn *pgx.Conn, sql string, timeout time.Duration) (time.Duration, error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := applyTimeouts(ctx, tx, timeout); err != nil {
		return 0, err
	}

	t := time.Now()
	if err := drainQuery(ctx, tx, sql); err != nil {
		return 0, err
	}
	return time.Since(t), nil
}

// runInSavepoint executes sql inside a SAVEPOINT / ROLLBACK TO SAVEPOINT scope
// within an existing transaction. Used for the after-DDL phase where the outer
// tx must stay open to keep the index alive.
func runInSavepoint(ctx context.Context, tx pgx.Tx, sql string, timeout time.Duration) (time.Duration, error) {
	if _, err := tx.Exec(ctx, "SAVEPOINT bench_sp"); err != nil {
		return 0, fmt.Errorf("savepoint: %w", err)
	}
	defer func() { _, _ = tx.Exec(ctx, "ROLLBACK TO SAVEPOINT bench_sp") }()

	if err := applyTimeouts(ctx, tx, timeout); err != nil {
		return 0, err
	}

	t := time.Now()
	if err := drainQuery(ctx, tx, sql); err != nil {
		return 0, err
	}
	return time.Since(t), nil
}

// applyTimeouts sets statement_timeout and lock_timeout for the current
// transaction only (SET LOCAL). Zero timeout means no limit.
func applyTimeouts(ctx context.Context, tx pgx.Tx, timeout time.Duration) error {
	if timeout > 0 {
		ms := timeout.Milliseconds()
		if _, err := tx.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", ms)); err != nil {
			return fmt.Errorf("set statement_timeout: %w", err)
		}
	}
	// Always cap lock waits at 5 s so a locked table doesn't stall the bench.
	if _, err := tx.Exec(ctx, "SET LOCAL lock_timeout = '5s'"); err != nil {
		return fmt.Errorf("set lock_timeout: %w", err)
	}
	return nil
}

// timePhase runs warmup iterations (unmeasured) then runs measured iterations,
// calling run for each. run returns the duration of one query execution.
func timePhase(
	ctx context.Context,
	runs, warmup int,
	run func(context.Context) (time.Duration, error),
) (target.RunStats, error) {
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
