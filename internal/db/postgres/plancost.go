package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	pgx "github.com/jackc/pgx/v5"
)

// PlanCostResult holds the key metrics extracted from EXPLAIN FORMAT=JSON.
type PlanCostResult struct {
	TotalCost float64
	PlanType  string // top-level node type, e.g. "Seq Scan", "Index Scan"
}

// ExplainCost runs EXPLAIN (FORMAT JSON) and returns the root plan cost.
func (c *Collector) ExplainCost(ctx context.Context, sql string) (*PlanCostResult, error) {
	return explainCost(ctx, c.conn, sql)
}

// ExplainCostAfterDDL runs the migration DDL inside a transaction, captures the
// EXPLAIN cost for the query, then always rolls back — so no schema change persists.
// PostgreSQL supports transactional DDL (CREATE INDEX, DROP INDEX, ALTER TABLE),
// making this safe and side-effect-free.
func (c *Collector) ExplainCostAfterDDL(ctx context.Context, sql, ddl string) (*PlanCostResult, error) {
	tx, err := c.conn.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, stmt := range splitStatements(ddl) {
		if _, err := tx.Exec(ctx, stmt); err != nil {
			return nil, fmt.Errorf("apply ddl %q: %w", stmt[:min(len(stmt), 60)], err)
		}
	}
	return explainCost(ctx, tx, sql)
}

type querier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func explainCost(ctx context.Context, q querier, sql string) (*PlanCostResult, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(sql), ";")
	var raw string
	if err := q.QueryRow(ctx, "EXPLAIN (FORMAT JSON, COSTS true) "+trimmed).Scan(&raw); err != nil {
		return nil, fmt.Errorf("explain: %w", err)
	}

	// EXPLAIN FORMAT=JSON returns [{Plan:{...}}]
	var plans []struct {
		Plan struct {
			NodeType  string  `json:"Node Type"`
			TotalCost float64 `json:"Total Cost"`
		} `json:"Plan"`
	}
	if err := json.Unmarshal([]byte(raw), &plans); err != nil || len(plans) == 0 {
		return nil, fmt.Errorf("parse explain json: %w", err)
	}
	return &PlanCostResult{
		TotalCost: plans[0].Plan.TotalCost,
		PlanType:  plans[0].Plan.NodeType,
	}, nil
}

// splitStatements splits DDL on semicolons, skipping blank/comment-only lines.
func splitStatements(ddl string) []string {
	var out []string
	for _, s := range strings.Split(ddl, ";") {
		s = strings.TrimSpace(s)
		if s == "" || strings.HasPrefix(s, "--") {
			continue
		}
		out = append(out, s)
	}
	return out
}
