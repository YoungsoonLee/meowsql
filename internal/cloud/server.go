package cloud

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/YoungsoonLee/meowsql/internal/push"
)

// Server is the MeowSQL Cloud HTTP server.
type Server struct {
	store *Store
	mux   *http.ServeMux
}

// NewServer wires handlers onto a new ServeMux.
func NewServer(store *Store) *Server {
	s := &Server{store: store, mux: http.NewServeMux()}
	s.mux.HandleFunc("GET /v1/health", s.handleHealth)
	s.mux.HandleFunc("POST /v1/ingest", s.auth(s.handleIngest))
	s.mux.HandleFunc("GET /v1/queries", s.auth(s.handleListQueries))
	return s
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Printf("%s %s", r.Method, r.URL.Path)
	s.mux.ServeHTTP(w, r)
}

// --- handlers ---

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	keyID := keyIDFromCtx(r.Context())

	var payload push.PushPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if len(payload.Queries) == 0 {
		writeError(w, http.StatusBadRequest, "queries array is empty")
		return
	}

	if err := s.store.SaveSnapshot(r.Context(), keyID, &payload); err != nil {
		log.Printf("save snapshot: %v", err)
		writeError(w, http.StatusInternalServerError, "storage error")
		return
	}

	fingerprints := make([]string, len(payload.Queries))
	for i, q := range payload.Queries {
		fingerprints[i] = q.Fingerprint
	}
	writeJSON(w, http.StatusOK, push.IngestResponse{
		Received:     len(payload.Queries),
		Fingerprints: fingerprints,
	})
}

func (s *Server) handleListQueries(w http.ResponseWriter, r *http.Request) {
	keyID := keyIDFromCtx(r.Context())
	dbLabel := r.URL.Query().Get("db_label")
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))

	queries, err := s.store.ListQueries(r.Context(), keyID, dbLabel, limit)
	if err != nil {
		log.Printf("list queries: %v", err)
		writeError(w, http.StatusInternalServerError, "storage error")
		return
	}
	if queries == nil {
		queries = []push.QuerySummary{}
	}
	writeJSON(w, http.StatusOK, queries)
}

// --- auth middleware ---

type contextKey struct{}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := bearerToken(r)
		if token == "" {
			writeError(w, http.StatusUnauthorized, "missing Authorization header")
			return
		}
		keyID, err := s.store.ValidateAPIKey(r.Context(), token)
		if err != nil {
			log.Printf("validate key: %v", err)
			writeError(w, http.StatusInternalServerError, "auth error")
			return
		}
		if keyID == "" {
			writeError(w, http.StatusUnauthorized, "invalid API key")
			return
		}
		ctx := context.WithValue(r.Context(), contextKey{}, keyID)
		next(w, r.WithContext(ctx))
	}
}

func keyIDFromCtx(ctx context.Context) string {
	v, _ := ctx.Value(contextKey{}).(string)
	return v
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if after, ok := strings.CutPrefix(h, "Bearer "); ok {
		return strings.TrimSpace(after)
	}
	return ""
}

// --- response helpers ---

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// Addr returns the listen address string for a given port.
func Addr(port int) string { return fmt.Sprintf(":%d", port) }
