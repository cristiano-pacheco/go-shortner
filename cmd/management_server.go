package cmd

import (
	"github.com/spf13/cobra"
	"go.uber.org/fx"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/httpserver"
)

func NewManagementServerCommand() *cobra.Command {
	return &cobra.Command{
		Use: "management-server", Short: "Serve authenticated link management", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			return runServer(command, fx.Provide(config.Load), shortener.ManagementModule, httpserver.ManagementModule)
		},
	}
}
