// meowsql-cloud is the MeowSQL Cloud API server.
//
// Environment variables:
//
//	DATABASE_URL  Postgres DSN for the cloud database (required for serve)
//	PORT          HTTP listen port (default 8080)
//
// Usage:
//
//	meowsql-cloud serve                  # start the API server
//	meowsql-cloud keygen --label prod    # create and print a new API key
//	meowsql-cloud keys                   # list all API keys (redacted)
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/YoungsoonLee/meowsql/internal/cloud"
	"github.com/spf13/cobra"
)

func main() {
	root := &cobra.Command{
		Use:           "meowsql-cloud",
		Short:         "MeowSQL Cloud API server",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	root.AddCommand(newServeCmd())
	root.AddCommand(newKeygenCmd())
	root.AddCommand(newKeysCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

// --- serve ---

func newServeCmd() *cobra.Command {
	var port int
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the MeowSQL Cloud API server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runServe(port)
		},
	}
	cmd.Flags().IntVar(&port, "port", defaultPort(), "HTTP listen port")
	return cmd
}

func runServe(port int) error {
	ctx := context.Background()
	store, err := openStore(ctx)
	if err != nil {
		return err
	}
	defer store.Close()

	srv := &http.Server{
		Addr:         cloud.Addr(port),
		Handler:      cloud.NewServer(store),
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		log.Printf("meowsql-cloud listening on %s", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-done
	log.Println("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(ctx)
}

// --- keygen ---

func newKeygenCmd() *cobra.Command {
	var label string
	cmd := &cobra.Command{
		Use:   "keygen",
		Short: "Create a new API key",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			store, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer store.Close()

			key, err := store.GenerateAPIKey(ctx, label)
			if err != nil {
				return fmt.Errorf("generate key: %w", err)
			}
			fmt.Printf("API key created (shown once — store it securely):\n\n  %s\n\n", key)
			fmt.Printf("Use it with:\n  meowsql push --api-key %s ...\n", key)
			fmt.Printf("  or: export MEOWSQL_API_KEY=%s\n", key)
			return nil
		},
	}
	cmd.Flags().StringVar(&label, "label", "", "human-readable label for this key")
	return cmd
}

// --- keys list ---

func newKeysCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "keys",
		Short: "List all API keys (redacted — hashes not shown)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := context.Background()
			store, err := openStore(ctx)
			if err != nil {
				return err
			}
			defer store.Close()

			keys, err := store.ListAPIKeys(ctx)
			if err != nil {
				return err
			}
			if len(keys) == 0 {
				fmt.Println("No API keys found. Run 'meowsql-cloud keygen' to create one.")
				return nil
			}
			fmt.Printf("%-26s  %-20s  %s\n", "ID", "Label", "Created")
			for _, k := range keys {
				fmt.Printf("%-26s  %-20s  %s\n", k.ID, k.Label, k.CreatedAt.Format(time.RFC3339))
			}
			return nil
		},
	}
}

// --- helpers ---

func openStore(ctx context.Context) (*cloud.Store, error) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is not set")
	}
	return cloud.NewStore(ctx, dbURL)
}

func defaultPort() int {
	if s := os.Getenv("PORT"); s != "" {
		if p, err := strconv.Atoi(s); err == nil {
			return p
		}
	}
	return 8080
}
