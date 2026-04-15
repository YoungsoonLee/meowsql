package report

import (
	"encoding/json"
	"io"

	"github.com/YoungsoonLee/meowsql/internal/agent"
	"github.com/YoungsoonLee/meowsql/internal/target"
)

type envelope struct {
	Context *target.ContextPack `json:"context"`
	Result  *agent.Result       `json:"result"`
}

func WriteJSON(w io.Writer, pack *target.ContextPack, r *agent.Result) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(envelope{Context: pack, Result: r})
}
