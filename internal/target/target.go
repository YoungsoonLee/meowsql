// Package target holds the dialect-agnostic types that every database
// collector produces and every downstream component (agent, report) consumes.
//
// Moving these out of internal/db/postgres lets us add MySQL (and, later,
// other dialects) without every consumer importing every collector.
package target

import "encoding/json"

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
