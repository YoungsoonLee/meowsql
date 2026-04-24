package postgres

import (
	"context"
	"fmt"
)

// Statement is a row from pg_stat_statements.
type Statement struct {
	Query   string
	Calls   int64
	TotalMs float64
	MeanMs  float64
}

const stmtQuery = `
SELECT query, calls, total_exec_time, mean_exec_time
FROM pg_stat_statements
WHERE calls >= $1
  AND query ILIKE 'select%'
  AND query NOT LIKE '%pg_stat_statements%'
  AND query NOT LIKE '%pg_catalog%'
ORDER BY mean_exec_time DESC
LIMIT $2`

// TopStatements returns the top n slowest queries from pg_stat_statements.
// Requires the pg_stat_statements extension; returns a descriptive error if
// the extension is missing or the view is not accessible.
func (c *Collector) TopStatements(ctx context.Context, n int, minCalls int64) ([]Statement, error) {
	rows, err := c.conn.Query(ctx, stmtQuery, minCalls, n)
	if err != nil {
		return nil, fmt.Errorf("pg_stat_statements: %w (is the extension enabled? run: CREATE EXTENSION IF NOT EXISTS pg_stat_statements)", err)
	}
	var out []Statement
	for rows.Next() {
		var s Statement
		if err := rows.Scan(&s.Query, &s.Calls, &s.TotalMs, &s.MeanMs); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, s)
	}
	rows.Close()
	return out, rows.Err()
}
