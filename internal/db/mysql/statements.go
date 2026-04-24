package mysql

import (
	"context"
	"fmt"
)

// Statement is a row from performance_schema.events_statements_summary_by_digest.
type Statement struct {
	Query   string
	Calls   int64
	TotalMs float64
	MeanMs  float64
}

// Timer values in performance_schema are in picoseconds; divide by 1e9 for ms.
const stmtQuery = `
SELECT DIGEST_TEXT, COUNT_STAR,
       SUM_TIMER_WAIT / 1e9 AS total_ms,
       AVG_TIMER_WAIT / 1e9 AS mean_ms
FROM performance_schema.events_statements_summary_by_digest
WHERE COUNT_STAR  >= ?
  AND DIGEST_TEXT IS NOT NULL
  AND DIGEST_TEXT     LIKE 'SELECT %'
  AND DIGEST_TEXT NOT LIKE '%performance_schema%'
  AND DIGEST_TEXT NOT LIKE '%information_schema%'
ORDER BY AVG_TIMER_WAIT DESC
LIMIT ?`

// TopStatements returns the top n slowest queries from performance_schema.
// Requires performance_schema to be enabled (on by default in MySQL 8).
func (c *Collector) TopStatements(ctx context.Context, n int, minCalls int64) ([]Statement, error) {
	rows, err := c.db.QueryContext(ctx, stmtQuery, minCalls, n)
	if err != nil {
		return nil, fmt.Errorf("performance_schema: %w (ensure performance_schema=ON in my.cnf)", err)
	}
	defer rows.Close()

	var out []Statement
	for rows.Next() {
		var s Statement
		if err := rows.Scan(&s.Query, &s.Calls, &s.TotalMs, &s.MeanMs); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
