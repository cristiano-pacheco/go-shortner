package httpserver

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/cristiano-pacheco/go-shortner/internal/shared/metrics"
)

type MetricsMiddleware struct {
	applicationMetrics *metrics.Metrics
}

func NewMetricsMiddleware(applicationMetrics *metrics.Metrics) *MetricsMiddleware {
	return &MetricsMiddleware{applicationMetrics: applicationMetrics}
}

func (m *MetricsMiddleware) Wrap(router chi.Router) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" && r.Method == http.MethodGet {
			router.ServeHTTP(w, r)
			return
		}

		method, route := m.method(r.Method), m.route(router, r)
		writer := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()

		m.applicationMetrics.StartHTTPRequest(method, route)

		completed := false

		defer func() {
			status := writer.Status()
			if status == 0 {
				status = http.StatusOK
				if !completed {
					status = http.StatusInternalServerError
				}
			}

			m.applicationMetrics.FinishHTTPRequest(method, route, status, m.outcome(status), start)
		}()

		router.ServeHTTP(writer, r)

		completed = true
	})
}

func (m *MetricsMiddleware) outcome(status int) string {
	if status >= http.StatusInternalServerError {
		return "server_error"
	}

	if status >= http.StatusBadRequest {
		return "client_error"
	}

	return "success"
}

func (m *MetricsMiddleware) method(method string) string {
	switch method {
	case http.MethodGet,
		http.MethodHead,
		http.MethodPost,
		http.MethodPut,
		http.MethodPatch,
		http.MethodDelete,
		http.MethodOptions,
		http.MethodTrace,
		http.MethodConnect:
		return method
	default:
		return "OTHER"
	}
}

func (m *MetricsMiddleware) route(router chi.Router, request *http.Request) string {
	for _, method := range []string{request.Method, http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		context := chi.NewRouteContext()
		if pattern := router.Find(context, method, request.URL.Path); pattern != "" {
			return pattern
		}
	}

	return "unmatched"
}
