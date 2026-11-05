package cmd

import (
	"context"
	"errors"
	"time"

	"github.com/cristiano-pacheco/bricks/pkg/database"
	bricksmetrics "github.com/cristiano-pacheco/bricks/pkg/http/server/metrics"
	"github.com/cristiano-pacheco/bricks/pkg/otel/trace"
	"github.com/cristiano-pacheco/bricks/pkg/redis"
	"github.com/cristiano-pacheco/bricks/pkg/validator"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"
	"go.uber.org/fx"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/httpserver"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/metrics"
)

func runServer(cmd *cobra.Command, _ []string) (runErr error) {
	const (
		startupTimeout  = 10 * time.Second
		shutdownTimeout = 30 * time.Second
	)

	app := fx.New(
		fx.NopLogger,
		fx.Provide(config.Load),
		trace.Module,
		validator.Module,
		fx.Provide(fx.Annotate(metrics.NewRegistry,
			fx.As(new(prometheus.Gatherer)),
			fx.As(new(prometheus.Registerer)),
		)),
		fx.Provide(metrics.New),
		bricksmetrics.Module,
		database.Module,
		redis.ClientModule,
		fx.Invoke(metrics.RegisterPoolMetrics),
		shortener.Module,
		httpserver.Module,
	)
	if err := app.Err(); err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(cmd.Context(), startupTimeout)
	err := app.Start(ctx)

	cancel()

	if err != nil {
		return err
	}

	defer func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), shutdownTimeout)
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
