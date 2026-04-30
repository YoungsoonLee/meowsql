// Package cache stores meowsql analysis results on disk so that repeated
// calls with the same query and schema never hit the Claude API again.
//
// Cache key = SHA-256(dialect + normalised SQL + sorted schema JSON).
// EXPLAIN output and row estimates are intentionally excluded — they change
// with data, but the index/rewrite advice from the model stays valid as long
// as the schema itself hasn't changed.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/YoungsoonLee/meowsql/internal/agent"
	"github.com/YoungsoonLee/meowsql/internal/target"
)

const defaultTTL = 24 * time.Hour

type entry struct {
	Key       string       `json:"key"`
	CreatedAt time.Time    `json:"created_at"`
	TTL       string       `json:"ttl"`
	Result    agent.Result `json:"result"`
}

// Key derives a stable cache key from the collected context.
// It uses dialect, normalised SQL, and the schema (columns + indexes) so that
// adding or dropping an index automatically invalidates the cache entry.
func Key(pack *target.ContextPack) string {
	var b strings.Builder
	b.WriteString(pack.Dialect)
	b.WriteByte('\n')
	b.WriteString(normalise(pack.SQL))
	b.WriteByte('\n')
	b.WriteString(schemaDigest(pack.Tables))
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// Lookup returns a cached result if one exists and has not expired.
func Lookup(key string, ttl time.Duration) (*agent.Result, bool) {
	path, err := cachePath(key)
	if err != nil {
		return nil, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var e entry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, false
	}
	if ttl == 0 {
		ttl = defaultTTL
	}
	if time.Since(e.CreatedAt) > ttl {
		_ = os.Remove(path)
		return nil, false
	}
	return &e.Result, true
}

// Store writes a result to the cache. Errors are silently ignored — a failing
// cache write must never break a successful analysis.
func Store(key string, result *agent.Result, ttl time.Duration) {
	path, err := cachePath(key)
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	if ttl == 0 {
		ttl = defaultTTL
	}
	e := entry{
		Key:       key,
		CreatedAt: time.Now().UTC(),
		TTL:       ttl.String(),
		Result:    *result,
	}
	data, err := json.MarshalIndent(e, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

// Dir returns the cache directory path.
func Dir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "meowsql"), nil
}

// Clear removes all entries from the cache directory.
func Clear() (int, error) {
	dir, err := Dir()
	if err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	removed := 0
	for _, de := range entries {
		if !de.IsDir() && strings.HasSuffix(de.Name(), ".json") {
			if os.Remove(filepath.Join(dir, de.Name())) == nil {
				removed++
			}
		}
	}
	return removed, nil
}

func cachePath(key string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("%s.json", key)), nil
}

func normalise(sql string) string {
	return strings.ToLower(strings.Join(strings.Fields(sql), " "))
}

// schemaDigest produces a deterministic string from table schemas.
// It sorts tables and their indexes so column/index ordering doesn't matter.
func schemaDigest(tables []target.TableInfo) string {
	type colKey struct{ Name, Type string }
	type idxKey struct{ Name, Def string }
	type tblKey struct {
		Name    string
		Columns []colKey
		Indexes []idxKey
	}

	tbls := make([]tblKey, len(tables))
	for i, t := range tables {
		cols := make([]colKey, len(t.Columns))
		for j, c := range t.Columns {
			cols[j] = colKey{c.Name, c.Type}
		}
		sort.Slice(cols, func(a, b int) bool { return cols[a].Name < cols[b].Name })

		idxs := make([]idxKey, len(t.Indexes))
		for j, idx := range t.Indexes {
			idxs[j] = idxKey{idx.Name, idx.Definition}
		}
		sort.Slice(idxs, func(a, b int) bool { return idxs[a].Name < idxs[b].Name })

		tbls[i] = tblKey{Name: t.Schema + "." + t.Name, Columns: cols, Indexes: idxs}
	}
	sort.Slice(tbls, func(a, b int) bool { return tbls[a].Name < tbls[b].Name })

	data, _ := json.Marshal(tbls)
	return string(data)
}
