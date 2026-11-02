package cmd

import (
	"context"
	"errors"
	"time"

	bricksmetrics "github.com/cristiano-pacheco/bricks/pkg/http/server/metrics"
	bricksvalidator "github.com/cristiano-pacheco/bricks/pkg/validator"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/spf13/cobra"
	"go.uber.org/fx"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/database"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/httpserver"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/redis"
)

func NewRootCommand() *cobra.Command {
	root := &cobra.Command{Use: "go-shortner", SilenceErrors: true, SilenceUsage: true}
	root.AddCommand(&cobra.Command{Use: "server", Args: cobra.NoArgs, RunE: runServer}, newMigrateCommand())

	return root
}

func newMetricsRegistry() (*prometheus.Registry, error) {
	registry := prometheus.NewRegistry()
	if err := registry.Register(collectors.NewGoCollector()); err != nil {
		return nil, err
	}

	if err := registry.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})); err != nil {
		return nil, err
	}

	return registry, nil
}

func runServer(cmd *cobra.Command, _ []string) (runErr error) {
	const lifecycleTimeout = 10 * time.Second

	app := fx.New(
		fx.NopLogger,
		fx.Provide(config.Load),
		bricksvalidator.Module,
		fx.Provide(newMetricsRegistry),
		fx.Provide(func(registry *prometheus.Registry) prometheus.Gatherer {
			return registry
		}),
		fx.Provide(func(registry *prometheus.Registry) prometheus.Registerer {
			return registry
		}),
		bricksmetrics.Module,
		database.Module,
		redis.Module,
		shortener.Module,
		httpserver.Module,
	)
	if err := app.Err(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), lifecycleTimeout)
	err := app.Start(ctx)

	cancel()

	if err != nil {
		return err
	}

	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), lifecycleTimeout)
		defer stopCancel()

		runErr = errors.Join(runErr, app.Stop(stopCtx))
	}()

	select {
	case <-cmd.Context().Done():
	case signal := <-app.Wait():
		if signal.ExitCode != 0 {
			return errors.New("HTTP server stopped unexpectedly")
		}
	}

	return nil
}
