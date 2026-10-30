package httpserver

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/infrastructure"
	"github.com/go-chi/chi/v5"
	"go.uber.org/fx"
)

func Register(lc fx.Lifecycle, shutdown fx.Shutdowner, cfg config.Config, _ *infrastructure.Resources) {
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
			next.ServeHTTP(w, r)
		})
	})
	router.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	server := &http.Server{Handler: router, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 15 * time.Second, IdleTimeout: 60 * time.Second}
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			listener, err := net.Listen("tcp", cfg.HTTPAddr)
			if err != nil {
				return err
			}
			go func() {
				if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
					_ = shutdown.Shutdown(fx.ExitCode(1))
				}
			}()
			return nil
		},
		OnStop: server.Shutdown,
	})
}
