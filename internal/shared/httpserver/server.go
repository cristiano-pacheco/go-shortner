package httpserver

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/fx"

	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
)

const (
	maxRequestBodyBytes = 1 << 20
	readHeaderTimeout   = 5 * time.Second
	readTimeout         = 15 * time.Second
	writeTimeout        = 15 * time.Second
	idleTimeout         = 60 * time.Second
	drainTimeout        = 20 * time.Second
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

func newRouter(params serverParams) chi.Router {
	router := chi.NewRouter()
	router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Body = http.MaxBytesReader(w, r.Body, maxRequestBodyBytes)
			ctx := (propagation.TraceContext{}).Extract(r.Context(), propagation.HeaderCarrier(r.Header))
			next.ServeHTTP(w, r.WithContext(ctx))
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

	return router
}

func startServer(ctx context.Context, server *http.Server, params serverParams) error {
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
}

func Register(params serverParams) {
	var mutex sync.Mutex

	activeRequests := 0
	stopping := false
	drained := make(chan struct{})

	handler := params.Metrics.Wrap(newRouter(params))
	server := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			mutex.Lock()
			if stopping {
				mutex.Unlock()
				w.WriteHeader(http.StatusServiceUnavailable)

				return
			}

			activeRequests++
			mutex.Unlock()

			defer func() {
				mutex.Lock()

				activeRequests--
				if stopping && activeRequests == 0 {
					close(drained)
				}
				mutex.Unlock()
			}()

			handler.ServeHTTP(w, r)
		}),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	params.Lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			return startServer(ctx, server, params)
		},
		OnStop: func(ctx context.Context) error {
			mutex.Lock()
			stopping = true

			if activeRequests == 0 {
				close(drained)
			}
			mutex.Unlock()

			drainCtx, cancel := context.WithTimeout(ctx, drainTimeout)
			defer cancel()

			err := server.Shutdown(drainCtx)
			if err != nil {
				err = errors.Join(err, server.Close())
			}

			select {
			case <-drained:
				return err
			case <-ctx.Done():
				return errors.Join(err, ctx.Err())
			}
		},
	})
}
