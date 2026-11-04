package cmd

import "github.com/spf13/cobra"

func NewRootCommand() *cobra.Command {
	root := &cobra.Command{Use: "go-shortner", SilenceErrors: true, SilenceUsage: true}
	root.AddCommand(&cobra.Command{Use: "server", Args: cobra.NoArgs, RunE: runServer}, newMigrateCommand())

	return root
}
