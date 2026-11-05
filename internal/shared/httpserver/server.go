package httpserver

import (
	"context"
	"net/http"
	"sync"

	"github.com/cristiano-pacheco/bricks/pkg/http/server/chi"
	chiv5 "github.com/go-chi/chi/v5"
	"go.opentelemetry.io/otel/propagation"
	"go.uber.org/fx"
)

const maxRequestBodyBytes = 1 << 20

// ManagementRoute registers paths relative to /api/v1.
type ManagementRoute func(chiv5.Router)

// PublicRoute registers public paths relative to the root router.
type PublicRoute func(chiv5.Router)

type serverParams struct {
	fx.In

	Lifecycle        fx.Lifecycle
	Authentication   *APIKeyMiddleware
	Metrics          *MetricsMiddleware
	ManagementRoutes []ManagementRoute `group:"management_routes"`
	PublicRoutes     []PublicRoute     `group:"public_routes"`
}

type applicationRoutes struct {
	params         serverParams
	mu             sync.Mutex
	activeRequests int
	stopping       bool
	drained        chan struct{}
}

var _ chi.Route = (*applicationRoutes)(nil)

func newApplicationRoutes(params serverParams) *applicationRoutes {
	routes := &applicationRoutes{params: params, drained: make(chan struct{})}
	// This hook is registered before the Bricks server hook, so Fx stops the
	// listener first and waits for remaining handlers before stopping resources.
	params.Lifecycle.Append(fx.Hook{OnStop: func(ctx context.Context) error {
		routes.mu.Lock()

		routes.stopping = true
		if routes.activeRequests == 0 {
			close(routes.drained)
		}
		routes.mu.Unlock()

		select {
		case <-routes.drained:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}})

	return routes
}

func (routes *applicationRoutes) Setup(server *chi.Server) {
	router := chiv5.NewRouter()
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
	router.Route("/api/v1", func(r chiv5.Router) {
		r.Use(routes.params.Authentication.Authenticate)

		for _, register := range routes.params.ManagementRoutes {
			register(r)
		}
	})
	router.Group(func(r chiv5.Router) {
		for _, register := range routes.params.PublicRoutes {
			register(r)
		}
	})

	handler := routes.params.Metrics.Wrap(router)

	guarded := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		routes.mu.Lock()
		if routes.stopping {
			routes.mu.Unlock()
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		routes.activeRequests++
		routes.mu.Unlock()

		defer func() {
			routes.mu.Lock()

			routes.activeRequests--
			if routes.stopping && routes.activeRequests == 0 {
				close(routes.drained)
			}
			routes.mu.Unlock()
		}()

		handler.ServeHTTP(w, r)
	})

	server.Router().Mount("/", guarded)
	// Bricks registers /healthz before application routes. Keep it available
	// as a short code instead of shadowing previously created links.
	server.Router().Get("/healthz", guarded.ServeHTTP)
}
