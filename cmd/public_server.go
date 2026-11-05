package cmd

import (
	"github.com/spf13/cobra"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/httpserver"
)

func NewPublicServerCommand() *cobra.Command {
	return &cobra.Command{
		Use: "public-server", Short: "Serve public link redirects", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runServer(command, shortener.PublicModule, httpserver.PublicModule)
		},
	}
}
