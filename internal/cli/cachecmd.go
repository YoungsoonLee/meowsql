package cli

import (
	"fmt"
	"io"

	"github.com/YoungsoonLee/meowsql/internal/cache"
	"github.com/spf13/cobra"
)

func newCacheCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cache",
		Short: "Manage the local analysis cache",
	}
	cmd.AddCommand(newCacheClearCmd())
	cmd.AddCommand(newCacheDirCmd())
	return cmd
}

func newCacheClearCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clear",
		Short: "Delete all cached analysis results",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCacheClear(cmd.OutOrStdout())
		},
	}
}

func newCacheDirCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "dir",
		Short: "Print the cache directory path",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := cache.Dir()
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), dir)
			return nil
		},
	}
}

func runCacheClear(out io.Writer) error {
	n, err := cache.Clear()
	if err != nil {
		return err
	}
	if n == 0 {
		fmt.Fprintln(out, "Cache is already empty.")
	} else {
		fmt.Fprintf(out, "Removed %d cached result(s).\n", n)
	}
	return nil
}
