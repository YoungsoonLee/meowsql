package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

type TableInfo struct {
	Schema        string       `json:"schema"`
	Name          string       `json:"name"`
	EstimatedRows int64        `json:"estimated_rows"`
	Columns       []ColumnInfo `json:"columns"`
	Indexes       []IndexInfo  `json:"indexes"`
}

type ColumnInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
}

type IndexInfo struct {
	Name       string `json:"name"`
	Definition string `json:"definition"`
	IsUnique   bool   `json:"is_unique"`
	IsPrimary  bool   `json:"is_primary"`
}

const colQuery = `
SELECT a.attname,
       format_type(a.atttypid, a.atttypmod),
       NOT a.attnotnull
FROM pg_attribute a
JOIN pg_class c     ON c.oid = a.attrelid
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relname = $2
  AND a.attnum > 0 AND NOT a.attisdropped
ORDER BY a.attnum`

const idxQuery = `
SELECT i.indexname,
       i.indexdef,
       ix.indisunique,
       ix.indisprimary
FROM pg_indexes i
JOIN pg_class c  ON c.relname = i.indexname
JOIN pg_index ix ON ix.indexrelid = c.oid
WHERE i.schemaname = $1 AND i.tablename = $2`

const statsQuery = `
SELECT COALESCE(c.reltuples, 0)::bigint
FROM pg_class c
JOIN pg_namespace n ON n.oid = c.relnamespace
WHERE n.nspname = $1 AND c.relname = $2`

// DescribeTables returns schema + indexes + row-count estimates for each given
// name. Names may be bare ("users") or schema-qualified ("auth.users"); bare
// names are resolved against the "public" schema.
func (c *Collector) DescribeTables(ctx context.Context, names []string) ([]TableInfo, error) {
	out := make([]TableInfo, 0, len(names))
	for _, n := range names {
		schema, table := splitName(n)
		info, err := c.describeOne(ctx, schema, table)
		if err != nil {
			return nil, err
		}
		if info != nil {
			out = append(out, *info)
		}
	}
	return out, nil
}

func splitName(n string) (string, string) {
	if i := strings.Index(n, "."); i >= 0 {
		return n[:i], n[i+1:]
	}
	return "public", n
}

func (c *Collector) describeOne(ctx context.Context, schema, table string) (*TableInfo, error) {
	info := &TableInfo{Schema: schema, Name: table}

	rows, err := c.conn.Query(ctx, colQuery, schema, table)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var col ColumnInfo
		if err := rows.Scan(&col.Name, &col.Type, &col.Nullable); err != nil {
			rows.Close()
			return nil, err
		}
		info.Columns = append(info.Columns, col)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(info.Columns) == 0 {
		// Not a real table, or not visible under the current role.
		return nil, nil
	}

	rows, err = c.conn.Query(ctx, idxQuery, schema, table)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var idx IndexInfo
		if err := rows.Scan(&idx.Name, &idx.Definition, &idx.IsUnique, &idx.IsPrimary); err != nil {
			rows.Close()
			return nil, err
		}
		info.Indexes = append(info.Indexes, idx)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := c.conn.QueryRow(ctx, statsQuery, schema, table).Scan(&info.EstimatedRows); err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}

	return info, nil
}
