package mysql

import (
	"context"
	"crypto/sha1"
	"encoding/hex"

	"github.com/YoungsoonLee/meowsql/internal/target"
)

func (c *Collector) Collect(ctx context.Context, sql string, opts target.CollectOptions) (*target.ContextPack, error) {
	var fp string
	if opts.LenientParse {
		// Parameterized queries from performance_schema may not parse cleanly.
		// Use a raw sha1 of the text as the fingerprint instead.
		sum := sha1.Sum([]byte(sql))
		fp = hex.EncodeToString(sum[:])
	} else {
		var err error
		fp, err = ValidateSQL(sql)
		if err != nil {
			return nil, err
		}
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
		names, err := parseTableNames(sql)
		if err != nil && opts.LenientParse {
			names = extractTableNamesRegex(sql)
		} else if err != nil {
			// non-lenient: best-effort, ignore error
		}
		tableNames = names
	}

	tables, err := c.DescribeTables(ctx, tableNames)
	if err != nil {
		return nil, err
	}
	pack.Tables = tables
	return pack, nil
}
