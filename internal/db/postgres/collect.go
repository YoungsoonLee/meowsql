package postgres

import (
	"context"
	"encoding/json"
)

type CollectOptions struct {
	RunAnalyze bool
	SchemaOnly bool
}

type ContextPack struct {
	Dialect     string          `json:"dialect"`
	Version     string          `json:"version"`
	SQL         string          `json:"sql"`
	Fingerprint string          `json:"fingerprint"`
	Explain     json.RawMessage `json:"explain,omitempty"`
	ExplainRan  bool            `json:"explain_ran"`
	Analyzed    bool            `json:"analyzed"`
	Tables      []TableInfo     `json:"tables"`
}

// Collect gathers every piece of grounding context the agent needs: version,
// validated SQL + fingerprint, EXPLAIN plan (unless SchemaOnly), and the
// schema/indexes/stats for every referenced table.
func (c *Collector) Collect(ctx context.Context, sql string, opts CollectOptions) (*ContextPack, error) {
	fp, err := ValidateSQL(sql)
	if err != nil {
		return nil, err
	}

	version, _ := c.serverVersion(ctx)

	pack := &ContextPack{
		Dialect:     "postgres",
		Version:     version,
		SQL:         sql,
		Fingerprint: fp,
	}

	var tableNames []string
	if !opts.SchemaOnly {
		plan, err := c.Explain(ctx, sql, opts.RunAnalyze)
		if err != nil {
			return nil, err
		}
		pack.Explain = plan.Plan
		pack.ExplainRan = true
		pack.Analyzed = opts.RunAnalyze
		tableNames = plan.Tables
	}

	if len(tableNames) == 0 {
		if names, err := parseTableNames(sql); err == nil {
			tableNames = names
		}
	}

	tables, err := c.DescribeTables(ctx, tableNames)
	if err != nil {
		return nil, err
	}
	pack.Tables = tables
	return pack, nil
}
