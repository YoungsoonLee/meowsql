package push

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// DefaultEndpoint is the production MeowSQL Cloud API base URL.
const DefaultEndpoint = "https://api.meowsql.dev"

var httpClient = &http.Client{Timeout: 30 * time.Second}

// Send posts payload to <endpoint>/v1/ingest, authenticated with apiKey.
// Returns the server's IngestResponse on success.
func Send(ctx context.Context, endpoint, apiKey string, payload *PushPayload) (*IngestResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		endpoint+"/v1/ingest", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		if errBody.Error != "" {
			return nil, fmt.Errorf("server %d: %s", resp.StatusCode, errBody.Error)
		}
		return nil, fmt.Errorf("server returned %d", resp.StatusCode)
	}

	var out IngestResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &out, nil
}

// ListQueries fetches the query list from <endpoint>/v1/queries.
func ListQueries(ctx context.Context, endpoint, apiKey, dbLabel string, limit int) ([]QuerySummary, error) {
	u, err := url.Parse(endpoint + "/v1/queries")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	if dbLabel != "" {
		q.Set("db_label", dbLabel)
	}
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errBody struct{ Error string `json:"error"` }
		_ = json.NewDecoder(resp.Body).Decode(&errBody)
		return nil, fmt.Errorf("server %d: %s", resp.StatusCode, errBody.Error)
	}

	var out []QuerySummary
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return out, nil
}
