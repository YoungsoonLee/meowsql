package postgres

import (
	"encoding/json"

	pgquery "github.com/pganalyze/pg_query_go/v6"
)

// ValidateSQL confirms the input parses as PostgreSQL and returns a fingerprint
// suitable for identification or caching.
func ValidateSQL(sql string) (string, error) {
	if _, err := pgquery.Parse(sql); err != nil {
		return "", err
	}
	return pgquery.Fingerprint(sql)
}

// ValidateOnly matches the agent.Validator signature for CLI wiring.
func ValidateOnly(sql string) error {
	_, err := ValidateSQL(sql)
	return err
}

// parseTableNames walks the parse tree for every RangeVar and returns
// schema-qualified names when the schema is present. Used as a fallback when
// an EXPLAIN plan is unavailable (e.g. schema-only mode).
func parseTableNames(sql string) ([]string, error) {
	raw, err := pgquery.ParseToJSON(sql)
	if err != nil {
		return nil, err
	}
	var tree any
	if err := json.Unmarshal([]byte(raw), &tree); err != nil {
		return nil, err
	}

	seen := map[string]struct{}{}
	var out []string
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			if rv, ok := t["RangeVar"].(map[string]any); ok {
				name, _ := rv["relname"].(string)
				schema, _ := rv["schemaname"].(string)
				if name != "" {
					full := name
					if schema != "" {
						full = schema + "." + name
					}
					if _, dup := seen[full]; !dup {
						seen[full] = struct{}{}
						out = append(out, full)
					}
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
	return out, nil
}
