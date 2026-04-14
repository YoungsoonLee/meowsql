package postgres

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

// Explain runs EXPLAIN (FORMAT JSON, ...) against the query. When analyze is
// true, EXPLAIN (ANALYZE, BUFFERS) is run inside a transaction that is always
// rolled back — guarding against mutating queries persisting.
func (c *Collector) Explain(ctx context.Context, sql string, analyze bool) (*ExplainResult, error) {
	opts := "FORMAT JSON, VERBOSE, COSTS"
	if analyze {
		opts = "ANALYZE, BUFFERS, " + opts
	}
	q := fmt.Sprintf("EXPLAIN (%s) %s", opts, stripTrailingSemicolon(sql))

	var raw json.RawMessage
	if analyze {
		tx, err := c.conn.Begin(ctx)
		if err != nil {
			return nil, err
		}
		defer func() { _ = tx.Rollback(ctx) }()
		if err := tx.QueryRow(ctx, q).Scan(&raw); err != nil {
			return nil, err
		}
	} else {
		if err := c.conn.QueryRow(ctx, q).Scan(&raw); err != nil {
			return nil, err
		}
	}

	return &ExplainResult{Plan: raw, Tables: extractTablesFromPlan(raw)}, nil
}

func stripTrailingSemicolon(sql string) string {
	return strings.TrimRight(strings.TrimSpace(sql), ";")
}

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
			if rn, ok := t["Relation Name"].(string); ok && rn != "" {
				full := rn
				if s, ok := t["Schema"].(string); ok && s != "" {
					full = s + "." + rn
				}
				if _, dup := seen[full]; !dup {
					seen[full] = struct{}{}
					out = append(out, full)
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
