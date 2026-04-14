package report

import (
	"fmt"
	"io"
	"strings"

	"github.com/YoungsoonLee/meowsql/internal/agent"
	"github.com/YoungsoonLee/meowsql/internal/db/postgres"
	"github.com/fatih/color"
)

var (
	header  = color.New(color.FgHiCyan, color.Bold).SprintFunc()
	section = color.New(color.FgHiYellow, color.Bold).SprintFunc()
	dim     = color.New(color.FgHiBlack).SprintFunc()
	code    = color.New(color.FgGreen).SprintFunc()
)

func WriteText(w io.Writer, pack *postgres.ContextPack, r *agent.Result) error {
	version := pack.Version
	if version == "" {
		version = "unknown"
	}
	fmt.Fprintf(w, "%s %s\n\n", header("🐾 MeowSQL"), dim(fmt.Sprintf("PostgreSQL %s", version)))

	fmt.Fprintln(w, section("Diagnosis"))
	fmt.Fprintln(w, indent(r.Diagnosis, "  "))
	if len(r.RootCauses) > 0 {
		fmt.Fprintln(w)
		for _, rc := range r.RootCauses {
			fmt.Fprintf(w, "  • %s\n", rc)
		}
	}
	fmt.Fprintln(w)

	if len(r.IndexSuggestions) > 0 {
		fmt.Fprintln(w, section("Suggested indexes"))
		for _, s := range r.IndexSuggestions {
			fmt.Fprintf(w, "  %s\n", code(s.Statement))
			if s.Rationale != "" {
				fmt.Fprintf(w, "    %s\n", dim(s.Rationale))
			}
		}
		fmt.Fprintln(w)
	}

	if len(r.Rewrites) > 0 {
		fmt.Fprintln(w, section("Rewrites"))
		for _, rw := range r.Rewrites {
			fmt.Fprintln(w, indent(rw.SQL, "  "))
			if rw.Rationale != "" {
				fmt.Fprintf(w, "  %s\n\n", dim(rw.Rationale))
			}
		}
	}

	if r.EstimatedImpact != "" {
		fmt.Fprintln(w, section("Estimated impact"))
		fmt.Fprintf(w, "  %s\n\n", r.EstimatedImpact)
	}

	if len(r.Caveats) > 0 {
		fmt.Fprintln(w, section("Caveats"))
		for _, c := range r.Caveats {
			fmt.Fprintf(w, "  • %s\n", c)
		}
	}
	return nil
}

func indent(s, pad string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = pad + l
	}
	return strings.Join(lines, "\n")
}
