package httpserver

import (
	"context"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"go.uber.org/fx"

	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
)

const (
	maxRequestBodyBytes = 1 << 20
	readHeaderTimeout   = 5 * time.Second
	readTimeout         = 15 * time.Second
	writeTimeout        = 15 * time.Second
	idleTimeout         = 60 * time.Second
)

// ManagementRoute registers paths relative to /api/v1.
type ManagementRoute func(chi.Router)

// PublicRoute registers public paths relative to the root router.
type PublicRoute func(chi.Router)

type serverParams struct {
	fx.In

	Lifecycle        fx.Lifecycle
	Shutdown         fx.Shutdowner
	Config           config.Config
	Authentication   *APIKeyMiddleware
	Metrics          *MetricsMiddleware
	ManagementRoutes []ManagementRoute `group:"management_routes"`
	PublicRoutes     []PublicRoute     `group:"public_routes"`
}

func Register(params serverParams) {
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
			next.ServeHTTP(w, r)
		})
	})
	router.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	router.Route("/api/v1", func(r chi.Router) {
		r.Use(params.Authentication.Authenticate)

		for _, register := range params.ManagementRoutes {
			register(r)
		}
	})
	router.Group(func(r chi.Router) {
		for _, register := range params.PublicRoutes {
			register(r)
		}
	})
	server := &http.Server{
		Handler:           params.Metrics.Wrap(router),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}
	params.Lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", params.Config.HTTPAddr)
			if err != nil {
				return err
			}

			go func() {
				if serveErr := server.Serve(listener); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
					_ = params.Shutdown.Shutdown(fx.ExitCode(1))
				}
			}()

			return nil
		},
		OnStop: server.Shutdown,
	})
}
