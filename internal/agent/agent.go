package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/YoungsoonLee/meowsql/internal/db/postgres"
)

type Request struct {
	APIKey  string
	Model   string
	Context *postgres.ContextPack
}

type Result struct {
	Diagnosis        string       `json:"diagnosis"`
	RootCauses       []string     `json:"root_causes"`
	IndexSuggestions []Suggestion `json:"index_suggestions"`
	Rewrites         []Rewrite    `json:"rewrites"`
	EstimatedImpact  string       `json:"estimated_impact"`
	Caveats          []string     `json:"caveats"`
}

type Suggestion struct {
	Statement string `json:"statement"`
	Rationale string `json:"rationale"`
}

type Rewrite struct {
	SQL       string `json:"sql"`
	Rationale string `json:"rationale"`
}

func Analyze(ctx context.Context, req Request) (*Result, error) {
	payload, err := json.MarshalIndent(req.Context, "", "  ")
	if err != nil {
		return nil, err
	}
	user := "CONTEXT (JSON):\n" + string(payload) + "\n\nReturn the JSON response per the rules."

	text, err := callAnthropic(ctx, req.APIKey, req.Model, systemPrompt, user)
	if err != nil {
		return nil, err
	}

	text = stripFences(text)
	var out Result
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, fmt.Errorf("model did not return valid JSON: %w\nresponse:\n%s", err, text)
	}
	return &out, nil
}

func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
