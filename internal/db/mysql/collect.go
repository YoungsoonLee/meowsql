package mysql

import (
	"context"

	"github.com/YoungsoonLee/meowsql/internal/target"
)

func (c *Collector) Collect(ctx context.Context, sql string, opts target.CollectOptions) (*target.ContextPack, error) {
	fp, err := ValidateSQL(sql)
	if err != nil {
		return nil, err
	}

	version, _ := c.serverVersion(ctx)

	pack := &target.ContextPack{
		Dialect:     "mysql",
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
