package cmd

import "github.com/spf13/cobra"

func NewRootCommand() *cobra.Command {
	root := &cobra.Command{Use: "go-shortner", SilenceErrors: true, SilenceUsage: true}
	root.AddCommand(NewPublicServerCommand(), NewManagementServerCommand(), newMigrateCommand(), NewProfileCaptureCommand())

	return root
}
