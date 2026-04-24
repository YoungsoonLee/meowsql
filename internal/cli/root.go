package cli

import "github.com/spf13/cobra"

func NewRoot(version string) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "meowsql",
		Short:         "SQL performance tuning agent for PostgreSQL and MySQL",
		Version:       version,
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	cmd.AddCommand(newAnalyzeCmd())
	cmd.AddCommand(newWatchCmd())
	return cmd
}
