package mysql

import (
	"context"
	"fmt"
	"strings"

	"github.com/YoungsoonLee/meowsql/internal/target"
)

const colQuery = `
SELECT column_name, column_type, is_nullable
FROM information_schema.columns
WHERE table_schema = ? AND table_name = ?
ORDER BY ordinal_position`

const idxQuery = `
SELECT index_name,
       MAX(non_unique)                                AS non_unique,
       GROUP_CONCAT(column_name ORDER BY seq_in_index) AS columns
FROM information_schema.statistics
WHERE table_schema = ? AND table_name = ?
GROUP BY index_name`

const statsQuery = `
SELECT COALESCE(table_rows, 0)
FROM information_schema.tables
WHERE table_schema = ? AND table_name = ?`

// DescribeTables returns schema + indexes + row-count estimates for each name.
// Names may be bare ("users") or schema-qualified ("otherdb.users"); bare
// names resolve against the current database (from SELECT DATABASE()).
func (c *Collector) DescribeTables(ctx context.Context, names []string) ([]target.TableInfo, error) {
	out := make([]target.TableInfo, 0, len(names))
	for _, n := range names {
		schema, table := c.splitName(n)
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

func (c *Collector) splitName(n string) (string, string) {
	if schema, name, ok := strings.Cut(n, "."); ok {
		return schema, name
	}
	return c.dbName, n
}

func (c *Collector) describeOne(ctx context.Context, schema, table string) (*target.TableInfo, error) {
	info := &target.TableInfo{Schema: schema, Name: table}

	rows, err := c.db.QueryContext(ctx, colQuery, schema, table)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var col target.ColumnInfo
		var nullable string
		if err := rows.Scan(&col.Name, &col.Type, &nullable); err != nil {
			rows.Close()
			return nil, err
		}
		col.Nullable = strings.EqualFold(nullable, "YES")
		info.Columns = append(info.Columns, col)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(info.Columns) == 0 {
		return nil, nil
	}

	rows, err = c.db.QueryContext(ctx, idxQuery, schema, table)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var name, cols string
		var nonUnique int
		if err := rows.Scan(&name, &nonUnique, &cols); err != nil {
			rows.Close()
			return nil, err
		}
		info.Indexes = append(info.Indexes, target.IndexInfo{
			Name:       name,
			Definition: synthesizeDef(schema, table, name, cols, nonUnique == 0),
			IsUnique:   nonUnique == 0,
			IsPrimary:  name == "PRIMARY",
		})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := c.db.QueryRowContext(ctx, statsQuery, schema, table).Scan(&info.EstimatedRows); err != nil {
		return nil, err
	}

	return info, nil
}

// MySQL's information_schema does not expose a ready-made DDL per index, so we
// synthesize a CREATE-style definition the agent can read without ambiguity.
func synthesizeDef(schema, table, name, cols string, unique bool) string {
	if name == "PRIMARY" {
		return fmt.Sprintf("PRIMARY KEY (%s)", cols)
	}
	u := ""
	if unique {
		u = "UNIQUE "
	}
	return fmt.Sprintf("CREATE %sINDEX %s ON %s.%s (%s)", u, name, schema, table, cols)
}
