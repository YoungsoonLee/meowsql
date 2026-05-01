// Package cloud is the MeowSQL Cloud API server — receives pushed payloads
// from `meowsql push` agents, stores them in PostgreSQL, and exposes a
// read API for dashboards and alerting.
package cloud

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/YoungsoonLee/meowsql/internal/push"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps a Postgres connection pool and owns all DB access.
type Store struct {
	db *pgxpool.Pool
}

// NewStore connects to Postgres at databaseURL, runs schema migrations,
// and returns a ready Store.
func NewStore(ctx context.Context, databaseURL string) (*Store, error) {
	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, fmt.Errorf("connect cloud db: %w", err)
	}
	if err := db.Ping(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping cloud db: %w", err)
	}
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func (s *Store) Close() { s.db.Close() }

const ddl = `
CREATE TABLE IF NOT EXISTS api_keys (
    id         TEXT        PRIMARY KEY,
    key_hash   TEXT        NOT NULL UNIQUE,
    label      TEXT        NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS query_snapshots (
    id            TEXT        PRIMARY KEY,
    api_key_id    TEXT        NOT NULL REFERENCES api_keys(id),
    dialect       TEXT        NOT NULL,
    db_label      TEXT        NOT NULL,
    fingerprint   TEXT        NOT NULL,
    query_text    TEXT        NOT NULL,
    calls         BIGINT      NOT NULL,
    total_ms      FLOAT8      NOT NULL,
    mean_ms       FLOAT8      NOT NULL,
    analysis_json JSONB,
    collected_at  TIMESTAMPTZ NOT NULL,
    received_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_snapshots_key_time
    ON query_snapshots (api_key_id, collected_at DESC);
CREATE INDEX IF NOT EXISTS idx_snapshots_key_fp
    ON query_snapshots (api_key_id, fingerprint, collected_at DESC);
`

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.Exec(ctx, ddl)
	return err
}

// GenerateAPIKey creates a new API key, stores its hash, and returns the
// plaintext key (shown only once — the store retains only the hash).
func (s *Store) GenerateAPIKey(ctx context.Context, label string) (plaintext string, err error) {
	raw := make([]byte, 24)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	plaintext = "msk_" + hex.EncodeToString(raw)
	keyHash := hashKey(plaintext)
	id := newID()

	_, err = s.db.Exec(ctx,
		`INSERT INTO api_keys (id, key_hash, label) VALUES ($1, $2, $3)`,
		id, keyHash, label,
	)
	return plaintext, err
}

// ValidateAPIKey looks up an API key by its hash and returns the key's row ID.
// Returns ("", nil) if the key is not found (caller should return 401).
func (s *Store) ValidateAPIKey(ctx context.Context, plaintext string) (keyID string, err error) {
	h := hashKey(plaintext)
	err = s.db.QueryRow(ctx,
		`SELECT id FROM api_keys WHERE key_hash = $1`, h,
	).Scan(&keyID)
	if err != nil && err.Error() == "no rows in result set" {
		return "", nil
	}
	return keyID, err
}

// SaveSnapshot persists all queries in payload under keyID.
func (s *Store) SaveSnapshot(ctx context.Context, keyID string, payload *push.PushPayload) error {
	for _, q := range payload.Queries {
		var analysisJSON []byte
		if q.Analysis != nil {
			var err error
			analysisJSON, err = json.Marshal(q.Analysis)
			if err != nil {
				return err
			}
		}
		_, err := s.db.Exec(ctx, `
			INSERT INTO query_snapshots
			    (id, api_key_id, dialect, db_label, fingerprint, query_text,
			     calls, total_ms, mean_ms, analysis_json, collected_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
			newID(), keyID,
			payload.Dialect, payload.DBLabel,
			q.Fingerprint, q.Query,
			q.Calls, q.TotalMs, q.MeanMs,
			nullableJSON(analysisJSON),
			payload.CollectedAt,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

// ListQueries returns the most recent snapshot for each unique fingerprint
// belonging to keyID, ordered by total_ms descending (most expensive first).
func (s *Store) ListQueries(ctx context.Context, keyID, dbLabel string, limit int) ([]push.QuerySummary, error) {
	if limit <= 0 {
		limit = 50
	}
	args := []any{keyID, limit}
	dbFilter := ""
	if dbLabel != "" {
		dbFilter = "AND db_label = $3"
		args = append(args, dbLabel)
	}

	rows, err := s.db.Query(ctx, fmt.Sprintf(`
		SELECT DISTINCT ON (fingerprint)
		    fingerprint, db_label, dialect, query_text,
		    calls, total_ms, mean_ms,
		    analysis_json, collected_at,
		    COUNT(*) OVER (PARTITION BY fingerprint) AS seen_count
		FROM query_snapshots
		WHERE api_key_id = $1 %s
		ORDER BY fingerprint, collected_at DESC
		LIMIT $2
	`, dbFilter), args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []push.QuerySummary
	for rows.Next() {
		var qs push.QuerySummary
		var analysisJSON []byte
		if err := rows.Scan(
			&qs.Fingerprint, &qs.DBLabel, &qs.Dialect, &qs.Query,
			&qs.Calls, &qs.TotalMs, &qs.MeanMs,
			&analysisJSON, &qs.LastSeen, &qs.SeenCount,
		); err != nil {
			return nil, err
		}
		if len(analysisJSON) > 0 {
			qs.Analysis = &push.Analysis{}
			_ = json.Unmarshal(analysisJSON, qs.Analysis)
		}
		out = append(out, qs)
	}
	return out, rows.Err()
}

// ListAPIKeys returns all api_keys rows (for the keygen list command).
func (s *Store) ListAPIKeys(ctx context.Context) ([]APIKeyRow, error) {
	rows, err := s.db.Query(ctx, `SELECT id, label, created_at FROM api_keys ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []APIKeyRow
	for rows.Next() {
		var r APIKeyRow
		if err := rows.Scan(&r.ID, &r.Label, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// APIKeyRow is a redacted view of an api_keys row (no hash).
type APIKeyRow struct {
	ID        string    `json:"id"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"created_at"`
}

func hashKey(plaintext string) string {
	h := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(h[:])
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func nullableJSON(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return b
}
