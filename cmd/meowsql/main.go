package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/YoungsoonLee/meowsql/internal/cli"
)

// version is set at build time via -ldflags "-X main.version=v0.1.0".
var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cli.NewRoot(version).ExecuteContext(ctx); err != nil {
		os.Exit(1)
	}
}
