package cli

import "github.com/spf13/cobra"

func NewRoot() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "meowsql",
		Short:         "SQL performance tuning agent for PostgreSQL and MySQL",
		SilenceUsage:  true,
		SilenceErrors: false,
	}
	cmd.AddCommand(newAnalyzeCmd())
	return cmd
}
