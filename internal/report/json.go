package report

import (
	"encoding/json"
	"io"

	"github.com/YoungsoonLee/meowsql/internal/agent"
	"github.com/YoungsoonLee/meowsql/internal/db/postgres"
)

type envelope struct {
	Context *postgres.ContextPack `json:"context"`
	Result  *agent.Result         `json:"result"`
}

func WriteJSON(w io.Writer, pack *postgres.ContextPack, r *agent.Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(envelope{Context: pack, Result: r})
}
