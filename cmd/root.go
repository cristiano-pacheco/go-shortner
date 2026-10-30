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
	app := fx.New(fx.NopLogger, fx.Provide(config.Load, infrastructure.NewResources), shortener.Module, fx.Invoke(httpserver.Register))
	if err := app.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
	err := app.Start(ctx)
	cancel()
	if err != nil {
		return err
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		runErr = errors.Join(runErr, app.Stop(ctx))
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
