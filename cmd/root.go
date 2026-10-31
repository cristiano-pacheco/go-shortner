package cmd

import (
	"context"
	"errors"
	"time"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/httpserver"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/infrastructure"
	"github.com/spf13/cobra"
	"go.uber.org/fx"
)

func NewRootCommand() *cobra.Command {
	root := &cobra.Command{Use: "go-shortner", SilenceErrors: true, SilenceUsage: true}
	root.AddCommand(&cobra.Command{Use: "server", Args: cobra.NoArgs, RunE: runServer}, newMigrateCommand())
	return root
}

func runServer(cmd *cobra.Command, _ []string) (runErr error) {
	const lifecycleTimeout = 10 * time.Second
	app := fx.New(
		fx.NopLogger,
		fx.Provide(config.Load, infrastructure.NewResources),
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
