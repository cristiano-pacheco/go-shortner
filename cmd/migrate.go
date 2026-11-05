package cmd

import (
	"net"
	"net/url"

	bricksmigration "github.com/cristiano-pacheco/bricks/pkg/migration"
	"github.com/spf13/cobra"
	"go.uber.org/fx"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
	sharederrs "github.com/cristiano-pacheco/go-shortner/internal/shared/errs"
	sharedmigration "github.com/cristiano-pacheco/go-shortner/internal/shared/migration"
)

func newMigrateCommand() *cobra.Command {
	return &cobra.Command{
		Use: "migrate", Short: "Apply pending database migrations", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := command.Context().Err(); err != nil {
				return err
			}

			app := fx.New(fx.NopLogger, shortener.Module,
				fx.Provide(config.Load, sharedmigration.NewRunner),
				fx.Invoke(func(cfg config.Config, runner *bricksmigration.Runner) error {
					address := url.URL{
						Scheme: "postgres",
						User:   url.UserPassword(cfg.DatabaseUser, cfg.DatabasePassword),
						Host:   net.JoinHostPort(cfg.DatabaseHost, cfg.DatabasePort),
						Path:   "/" + cfg.DatabaseName,
						RawQuery: url.Values{
							"sslmode":           {cfg.DatabaseSSLMode},
							"connect_timeout":   {"5"},
							"statement_timeout": {"5000"},
						}.Encode(),
					}
					if err := runner.Up(address.String()); err != nil {
						// Driver errors can include a DSN and its credentials.
						return sharederrs.ErrMigrationFailed
					}

					return nil
				}),
			)

			return app.Err()
		},
	}
}
