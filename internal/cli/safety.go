package cli

import (
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
