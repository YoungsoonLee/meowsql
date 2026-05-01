// Package push defines the wire format exchanged between `meowsql push`
// (the CLI agent) and the MeowSQL Cloud API.
//
// Only normalized query text, execution statistics, and AI analysis are
// transmitted. The customer's DSN, credentials, and row data stay local.
package push

import "time"

// PushPayload is the JSON body sent by `meowsql push` to POST /v1/ingest.
type PushPayload struct {
	Dialect     string       `json:"dialect"`
	DBLabel     string       `json:"db_label"`    // host+dbname derived from DSN — never the password
	CollectedAt time.Time    `json:"collected_at"` // when stats were read from the DB
	Queries     []QueryEntry `json:"queries"`
}

// QueryEntry holds one slow-query record plus its optional AI analysis.
type QueryEntry struct {
	Fingerprint string    `json:"fingerprint"` // SHA256[:8] of normalised query text
	Query       string    `json:"query"`
	Calls       int64     `json:"calls"`
	TotalMs     float64   `json:"total_ms"`
	MeanMs      float64   `json:"mean_ms"`
	Analysis    *Analysis `json:"analysis,omitempty"`
}

// Analysis is the subset of agent.Result that is stored in the cloud.
type Analysis struct {
	Diagnosis       string   `json:"diagnosis"`
	RootCauses      []string `json:"root_causes,omitempty"`
	IndexDDL        string   `json:"index_ddl,omitempty"` // first suggestion
	Rewrite         string   `json:"rewrite,omitempty"`   // first rewrite
	EstimatedImpact string   `json:"estimated_impact,omitempty"`
}

// IngestResponse is returned by POST /v1/ingest.
type IngestResponse struct {
	Received     int      `json:"received"`
	Fingerprints []string `json:"fingerprints"`
}

// QuerySummary is one row returned by GET /v1/queries.
type QuerySummary struct {
	Fingerprint string    `json:"fingerprint"`
	DBLabel     string    `json:"db_label"`
	Dialect     string    `json:"dialect"`
	Query       string    `json:"query"`
	Calls       int64     `json:"calls"`
	TotalMs     float64   `json:"total_ms"`
	MeanMs      float64   `json:"mean_ms"`
	Analysis    *Analysis `json:"analysis,omitempty"`
	LastSeen    time.Time `json:"last_seen"`
	SeenCount   int       `json:"seen_count"` // number of push snapshots received
}
