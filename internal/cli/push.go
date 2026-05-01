package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/YoungsoonLee/meowsql/internal/agent"
	"github.com/YoungsoonLee/meowsql/internal/cache"
	"github.com/YoungsoonLee/meowsql/internal/push"
	"github.com/YoungsoonLee/meowsql/internal/target"
	"github.com/spf13/cobra"
)

type pushOpts struct {
	dsn      string
	dialect  string
	top      int
	minCalls int64
	model    string
	noCache  bool
	cacheTTL time.Duration
	timeout  time.Duration
	interval time.Duration
	endpoint string
	apiKey   string
	dryRun   bool
}

func newPushCmd() *cobra.Command {
	var o pushOpts
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Analyze slow queries and push results to MeowSQL Cloud",
		Long: `push runs the same analysis as 'meowsql watch' and streams results to
MeowSQL Cloud for persistent storage, dashboards, and alerting.

Only normalized query text, execution stats, and AI analysis are sent to the
cloud. Your DSN, credentials, and row data never leave your machine.

Requires ANTHROPIC_API_KEY (for local analysis) and MEOWSQL_API_KEY (for the
cloud). Pass either via environment variable or --api-key.

Run once:
  meowsql push --dsn "$DATABASE_URL"

Run continuously every hour:
  meowsql push --dsn "$DATABASE_URL" --interval 1h`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPush(cmd.Context(), cmd.OutOrStdout(), o)
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.dsn, "dsn", "", "database connection string (required)")
	f.StringVar(&o.dialect, "dialect", "", "postgres|mysql (overrides DSN inference)")
	f.IntVar(&o.top, "top", 5, "number of slowest queries to analyze per cycle")
	f.Int64Var(&o.minCalls, "min-calls", 10, "ignore queries seen fewer than N times")
	f.StringVar(&o.model, "model", "claude-haiku-4-5-20251001", "Anthropic model id")
	f.BoolVar(&o.noCache, "no-cache", false, "skip local cache lookup")
	f.DurationVar(&o.cacheTTL, "cache-ttl", 24*time.Hour, "how long a cached result remains valid")
	f.DurationVar(&o.timeout, "timeout", 60*time.Second, "per-query timeout for schema + analysis")
	f.DurationVar(&o.interval, "interval", 0, "repeat on this schedule (e.g. 1h); 0 = run once")
	f.StringVar(&o.endpoint, "endpoint", push.DefaultEndpoint, "MeowSQL Cloud API base URL")
	f.StringVar(&o.apiKey, "api-key", "", "MeowSQL Cloud API key (or set MEOWSQL_API_KEY)")
	f.BoolVar(&o.dryRun, "dry-run", false, "analyze locally and print without sending to the cloud")
	_ = cmd.MarkFlagRequired("dsn")
	return cmd
}

func runPush(ctx context.Context, out io.Writer, o pushOpts) error {
	cloudKey := o.apiKey
	if cloudKey == "" {
		cloudKey = strings.TrimSpace(os.Getenv("MEOWSQL_API_KEY"))
	}
	if !o.dryRun && cloudKey == "" {
		return errors.New("MEOWSQL_API_KEY is not set (or pass --api-key)")
	}

	anthropicKey := strings.TrimSpace(os.Getenv("ANTHROPIC_API_KEY"))
	if anthropicKey == "" {
		return errors.New("ANTHROPIC_API_KEY is not set")
	}

	dialect, err := resolveDialect(o.dialect, o.dsn)
	if err != nil {
		return err
	}

	if o.dryRun {
		printDryRun(out, []drySection{
			{Title: "Connection", Lines: []string{
				"DSN:      " + maskDSN(o.dsn),
				"Dialect:  " + dialect,
				"Endpoint: " + o.endpoint,
			}},
			{Title: "Would analyze and push", Lines: []string{
				fmt.Sprintf("  Top %d slow queries (min-calls=%d)", o.top, o.minCalls),
				"  Schema collection + AI analysis (local — your data stays on-machine)",
				"  POST " + o.endpoint + "/v1/ingest",
				"  (normalized query text + stats + AI result only)",
			}},
		})
		return nil
	}

	dbLabel := dbLabelFromDSN(o.dsn)

	for {
		if err := pushOnce(ctx, out, o, dialect, dbLabel, anthropicKey, cloudKey); err != nil {
			fmt.Fprintf(out, "[error] %v\n", err)
		}
		if o.interval <= 0 {
			break
		}
		fmt.Fprintf(out, "Next push in %s. Ctrl-C to stop.\n\n", o.interval)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(o.interval):
		}
	}
	return nil
}

func pushOnce(ctx context.Context, out io.Writer, o pushOpts, dialect, dbLabel, anthropicKey, cloudKey string) error {
	stmts, col, validate, err := fetchStatements(ctx, dialect, o.dsn, o.top, o.minCalls)
	if err != nil {
		return err
	}
	defer col.Close()

	if len(stmts) == 0 {
		fmt.Fprintf(out, "No queries found (min-calls=%d). Try lowering --min-calls.\n", o.minCalls)
		return nil
	}

	payload := &push.PushPayload{
		Dialect:     dialect,
		DBLabel:     dbLabel,
		CollectedAt: time.Now().UTC(),
	}

	fmt.Fprintf(out, "Analyzing %d queries from %s...\n", len(stmts), dialect)

	for i, s := range stmts {
		qctx := ctx
		if o.timeout > 0 {
			var cancel context.CancelFunc
			qctx, cancel = context.WithTimeout(ctx, o.timeout)
			defer cancel()
		}

		entry := push.QueryEntry{
			Fingerprint: push.Fingerprint(s.Query),
			Query:       s.Query,
			Calls:       s.Calls,
			TotalMs:     s.TotalMs,
			MeanMs:      s.MeanMs,
		}

		pack, err := col.Collect(qctx, s.Query, target.CollectOptions{SchemaOnly: true, LenientParse: true})
		if err != nil {
			fmt.Fprintf(out, "  [%d/%d] collect error: %v — pushing stats only\n", i+1, len(stmts), err)
			payload.Queries = append(payload.Queries, entry)
			continue
		}

		cacheKey := cache.Key(pack)
		var result *agent.Result
		if !o.noCache {
			if cached, ok := cache.Lookup(cacheKey, o.cacheTTL); ok {
				result = cached
			}
		}

		if result == nil {
			result, err = agent.Analyze(qctx, agent.Request{
				APIKey:   anthropicKey,
				Model:    o.model,
				Context:  pack,
				Validate: validate,
			})
			if err != nil {
				fmt.Fprintf(out, "  [%d/%d] agent error: %v — pushing stats only\n", i+1, len(stmts), err)
				payload.Queries = append(payload.Queries, entry)
				continue
			}
			if !o.noCache {
				cache.Store(cacheKey, result, o.cacheTTL)
			}
		}

		entry.Analysis = agentResultToAnalysis(result)
		payload.Queries = append(payload.Queries, entry)
		fmt.Fprintf(out, "  [%d/%d] %-16s  mean=%.1fms  calls=%d\n",
			i+1, len(stmts), entry.Fingerprint, s.MeanMs, s.Calls)
	}

	resp, err := push.Send(ctx, o.endpoint, cloudKey, payload)
	if err != nil {
		return fmt.Errorf("send to %s: %w", o.endpoint, err)
	}
	fmt.Fprintf(out, "✓ Pushed %d queries to %s\n", resp.Received, o.endpoint)
	return nil
}

func agentResultToAnalysis(r *agent.Result) *push.Analysis {
	a := &push.Analysis{
		Diagnosis:       r.Diagnosis,
		RootCauses:      r.RootCauses,
		EstimatedImpact: r.EstimatedImpact,
	}
	if len(r.IndexSuggestions) > 0 {
		a.IndexDDL = r.IndexSuggestions[0].Statement
	}
	if len(r.Rewrites) > 0 {
		a.Rewrite = r.Rewrites[0].SQL
	}
	return a
}

// dbLabelFromDSN extracts host+dbname from a DSN for use as a display label.
// The password is never included.
func dbLabelFromDSN(dsn string) string {
	if u, err := url.Parse(dsn); err == nil && u.Host != "" {
		return u.Host + u.Path
	}
	// Native mysql DSN: user:pass@tcp(host:port)/db
	if at := strings.Index(dsn, "@"); at != -1 {
		return strings.TrimLeft(dsn[at+1:], "/")
	}
	return dsn
}
