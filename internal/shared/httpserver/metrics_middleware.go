package httpserver

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	methodLabel = "method"
	routeLabel  = "route"
)

type MetricsMiddleware struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
	inFlight *prometheus.GaugeVec
}

func NewMetricsMiddleware(registerer prometheus.Registerer) (*MetricsMiddleware, error) {
	metrics := &MetricsMiddleware{
		requests: prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "shortener_http_requests_total", Help: "Completed business HTTP requests."},
			[]string{methodLabel, routeLabel, "status", "outcome"},
		),
		duration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "shortener_http_request_duration_seconds",
				Help:    "Business HTTP request duration in seconds.",
				Buckets: []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 15},
			},
			[]string{methodLabel, routeLabel},
		),
		inFlight: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "shortener_http_requests_in_flight",
				Help: "Business HTTP requests currently in progress.",
			},
			[]string{methodLabel, routeLabel},
		),
	}
	for _, collector := range []prometheus.Collector{metrics.requests, metrics.duration, metrics.inFlight} {
		if err := registerer.Register(collector); err != nil {
			return nil, err
		}
	}

	return metrics, nil
}

func (m *MetricsMiddleware) Wrap(router chi.Router) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			router.ServeHTTP(w, r)
			return
		}

		method, route := m.method(r.Method), m.route(router, r)
		writer := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()

		m.inFlight.WithLabelValues(method, route).Inc()

		completed := false

		defer func() {
			m.inFlight.WithLabelValues(method, route).Dec()

			status := writer.Status()
			if status == 0 {
				status = http.StatusOK
				if !completed {
					status = http.StatusInternalServerError
				}
			}

			m.requests.WithLabelValues(method, route, strconv.Itoa(status), m.outcome(status)).Inc()
			m.duration.WithLabelValues(method, route).Observe(time.Since(start).Seconds())
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
