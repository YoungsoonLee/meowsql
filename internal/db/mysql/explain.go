package mysql

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type ExplainResult struct {
	Plan   json.RawMessage
	Tables []string
}

// Explain runs EXPLAIN FORMAT=JSON against the query. When analyze is true,
// EXPLAIN ANALYZE runs inside a transaction that is always rolled back so
// mutating queries do not persist. MySQL returns EXPLAIN ANALYZE output as
// plain text rather than JSON, so we wrap it in a JSON envelope for the agent.
func (c *Collector) Explain(ctx context.Context, sql string, analyze bool) (*ExplainResult, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(sql), ";")

	if analyze {
		tx, err := c.db.BeginTx(ctx, nil)
		if err != nil {
			return nil, err
		}
		defer func() { _ = tx.Rollback() }()

		rows, err := tx.QueryContext(ctx, "EXPLAIN ANALYZE "+trimmed)
		if err != nil {
			return nil, err
		}
		defer rows.Close()

		var lines []string
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				return nil, err
			}
			lines = append(lines, line)
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		wrap, _ := json.Marshal(map[string]any{
			"format":        "text",
			"analyze_lines": lines,
		})
		return &ExplainResult{Plan: wrap, Tables: nil}, nil
	}

	var raw string
	if err := c.db.QueryRowContext(ctx, "EXPLAIN FORMAT=JSON "+trimmed).Scan(&raw); err != nil {
		return nil, err
	}
	if !json.Valid([]byte(raw)) {
		return nil, fmt.Errorf("mysql returned non-JSON EXPLAIN output")
	}
	return &ExplainResult{Plan: json.RawMessage(raw), Tables: extractTablesFromPlan([]byte(raw))}, nil
}

// extractTablesFromPlan walks the EXPLAIN FORMAT=JSON tree looking for every
// "table_name" key. Matches MySQL's nested layout (query_block, nested_loop,
// subqueries, union_result, ...).
func extractTablesFromPlan(raw []byte) []string {
	var tree any
	if err := json.Unmarshal(raw, &tree); err != nil {
		return nil
	}
	seen := map[string]struct{}{}
	var out []string
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			if name, ok := t["table_name"].(string); ok && name != "" {
				if _, dup := seen[name]; !dup {
					seen[name] = struct{}{}
					out = append(out, name)
				}
			}
			for _, child := range t {
				walk(child)
			}
		case []any:
			for _, child := range t {
				walk(child)
			}
		}
	}
	walk(tree)
	return out
}
