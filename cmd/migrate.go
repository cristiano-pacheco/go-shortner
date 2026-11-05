package cmd

import (
	"errors"
	"net"
	"net/url"
	"strconv"

	bricksconfig "github.com/cristiano-pacheco/bricks/pkg/config"
	"github.com/cristiano-pacheco/bricks/pkg/database"
	bricksmigration "github.com/cristiano-pacheco/bricks/pkg/migration"
	"github.com/spf13/cobra"
	"go.uber.org/fx"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener"
	sharedmigration "github.com/cristiano-pacheco/go-shortner/internal/shared/migration"
)

//nolint:gocognit // Command constructs the migration URL from the database connection configuration.
func newMigrateCommand() *cobra.Command {
	return &cobra.Command{
		Use: "migrate", Short: "Apply pending database migrations", Args: cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if err := command.Context().Err(); err != nil {
				return err
			}

			app := fx.New(fx.NopLogger, shortener.MigrationModule,
				bricksconfig.Provide[database.Config]("app.database"),
				fx.Provide(sharedmigration.NewRunner),
				fx.Invoke(func(loaded bricksconfig.Config[database.Config], runner *bricksmigration.Runner) error {
					cfg := loaded.Get()
					if err := cfg.Validate(); err != nil {
						return err
					}

					sslmode := "disable"
					if cfg.SSLMode {
						sslmode = "require"
					}

					address := url.URL{
						Scheme: "postgres",
						User:   url.UserPassword(cfg.User, cfg.Password),
						Host:   net.JoinHostPort(cfg.Host, strconv.FormatUint(uint64(cfg.Port), 10)),
						Path:   "/" + cfg.Name,
						RawQuery: url.Values{
							"sslmode":           {sslmode},
							"connect_timeout":   {"5"},
							"statement_timeout": {"5000"},
						}.Encode(),
					}
					if cfg.SSLMode {
						query := address.Query()

						for key, value := range map[string]string{
							"sslcert": cfg.SSLCert, "sslkey": cfg.SSLKey, "sslrootcert": cfg.SSLRootCert,
						} {
							if value != "" {
								query.Set(key, value)
							}
						}

						address.RawQuery = query.Encode()
					}

					if err := runner.Up(address.String()); err != nil {
						// Driver errors can include a DSN and its credentials.
						return errors.New("database migration failed")
					}

					return nil
				}),
			)

			return app.Err()
		},
	}
}
