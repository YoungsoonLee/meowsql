package target

import "time"

// BenchOptions controls a Bench run for any dialect.
type BenchOptions struct {
	SQL     string
	DDL     string        // index DDL; empty means baseline only
	Runs    int
	Warmup  int
	Timeout time.Duration // per-query statement timeout; 0 = no limit
}

// RunStats holds timing percentiles for one benchmark phase.
type RunStats struct {
	Mean time.Duration
	P50  time.Duration
	P99  time.Duration
	Min  time.Duration
	Max  time.Duration
}

// BenchResult is returned by both postgres.Collector.Bench and
// mysql.Collector.Bench.
type BenchResult struct {
	Before   RunStats
	After    RunStats
	SpeedupX float64 // before.P50 / after.P50; zero when no DDL was applied
}
