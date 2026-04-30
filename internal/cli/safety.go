package cli

import (
	"fmt"
	"slices"
	"strings"
)

// dmlKeywords are SQL statement types that modify data.
var dmlKeywords = []string{"insert", "update", "delete", "truncate", "replace", "merge"}

// isDML returns true when the query's first keyword indicates a data-mutating
// statement. Used to gate --allow-dml and print rollback warnings.
func isDML(sql string) bool {
	fields := strings.Fields(strings.TrimSpace(sql))
	if len(fields) == 0 {
		return false
	}
	return slices.Contains(dmlKeywords, strings.ToLower(fields[0]))
}

// applyMaxRows appends "LIMIT n" to a SELECT query that has no existing LIMIT
// clause, capping runaway full-table scans during benchmarks.
// No-ops when maxRows <= 0 or the query is not a SELECT.
func applyMaxRows(sql string, maxRows int) string {
	if maxRows <= 0 {
		return sql
	}
	trimmed := strings.TrimSpace(sql)
	if !strings.EqualFold(trimmed[:min(6, len(trimmed))], "select") {
		return sql
	}
	lower := strings.ToLower(trimmed)
	if strings.Contains(lower, " limit ") || strings.HasSuffix(lower, "\nlimit") {
		return sql
	}
	return strings.TrimRight(trimmed, ";") + fmt.Sprintf(" LIMIT %d", maxRows)
}
