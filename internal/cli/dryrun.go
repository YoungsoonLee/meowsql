package cli

import (
	"fmt"
	"io"
	"net/url"
	"strings"

	"github.com/fatih/color"
)

// maskDSN hides the password from a DSN before printing it.
func maskDSN(dsn string) string {
	if u, err := url.Parse(dsn); err == nil && u.User != nil {
		masked := *u
		masked.User = url.User(u.User.Username())
		return masked.String()
	}
	// Native go-sql-driver form: user:pass@tcp(host)/db
	if colon := strings.Index(dsn, ":"); colon != -1 {
		if at := strings.Index(dsn[colon:], "@"); at != -1 {
			return dsn[:colon+1] + "***" + dsn[colon+at:]
		}
	}
	return dsn
}

type drySection struct {
	Title string
	Lines []string
}

// firstLine returns the first non-empty line of sql (trimmed).
func firstLine(sql string) string {
	for _, line := range strings.Split(strings.TrimSpace(sql), "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return sql
}

func printDryRun(out io.Writer, sections []drySection) {
	hdr := color.New(color.FgHiCyan, color.Bold).SprintFunc()
	sec := color.New(color.FgHiYellow, color.Bold).SprintFunc()
	cod := color.New(color.FgGreen).SprintFunc()
	dim := color.New(color.FgHiBlack).SprintFunc()

	fmt.Fprintf(out, "%s\n\n", hdr("🐾 MeowSQL — dry run"))
	fmt.Fprintf(out, "%s\n\n", dim("Nothing will be executed. Remove --dry-run to connect and run."))

	for _, s := range sections {
		fmt.Fprintf(out, "%s\n", sec(s.Title))
		for _, line := range s.Lines {
			// Lines starting with "  " are code — print in green.
			if strings.HasPrefix(line, "  ") {
				fmt.Fprintf(out, "%s\n", cod(line))
			} else {
				fmt.Fprintf(out, "  %s\n", line)
			}
		}
		fmt.Fprintln(out)
	}
}
