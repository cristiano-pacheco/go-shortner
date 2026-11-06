package cmd

import (
	"context"
	"errors"
	"time"

	"github.com/cristiano-pacheco/bricks/pkg/database"
	"github.com/cristiano-pacheco/bricks/pkg/http/server/chi"
	bricksmetrics "github.com/cristiano-pacheco/bricks/pkg/http/server/metrics"
	"github.com/cristiano-pacheco/bricks/pkg/logger"
	"github.com/cristiano-pacheco/bricks/pkg/otel/trace"
	"github.com/cristiano-pacheco/bricks/pkg/redis"
	"github.com/cristiano-pacheco/bricks/pkg/validator"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/spf13/cobra"
	"go.uber.org/fx"

	"github.com/cristiano-pacheco/go-shortner/internal/shared/metrics"
)

func runServer(cmd *cobra.Command, options ...fx.Option) (runErr error) {
	const (
		startupTimeout  = 10 * time.Second
		shutdownTimeout = 30 * time.Second
	)

	app := fx.New(fx.Options(options...),
		fx.NopLogger,
		fx.Provide(logger.DefaultConfig),
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
		chi.Module,
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

	profileServer, err := startProfiling()
	if err != nil {
		return err
	}
	if profileServer != nil {
		defer func() {
			runErr = errors.Join(runErr, profileServer.Close())
		}()
	}

	select {
	case <-cmd.Context().Done():
	case signal := <-app.Wait():
		if signal.ExitCode != 0 {
			return errors.New("HTTP server stopped unexpectedly")
		}
	}

	return nil
}
