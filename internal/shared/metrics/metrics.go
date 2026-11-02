package metrics

import (
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

type Metrics struct {
	httpRequests    *prometheus.CounterVec
	httpDuration    *prometheus.HistogramVec
	httpInFlight    *prometheus.GaugeVec
	createSuccess   prometheus.Counter
	createErrors    *prometheus.CounterVec
	createDuration  prometheus.Histogram
	getSuccess      prometheus.Counter
	getErrors       *prometheus.CounterVec
	getDuration     prometheus.Histogram
	listSuccess     prometheus.Counter
	listErrors      *prometheus.CounterVec
	listDuration    prometheus.Histogram
	updateSuccess   prometheus.Counter
	updateErrors    *prometheus.CounterVec
	updateDuration  prometheus.Histogram
	deleteSuccess   prometheus.Counter
	deleteErrors    *prometheus.CounterVec
	deleteDuration  prometheus.Histogram
	resolveSuccess  prometheus.Counter
	resolveErrors   *prometheus.CounterVec
	resolveDuration prometheus.Histogram
}

func NewRegistry() (*prometheus.Registry, error) {
	registry := prometheus.NewRegistry()
	if err := registry.Register(collectors.NewGoCollector()); err != nil {
		return nil, err
	}

	if err := registry.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})); err != nil {
		return nil, err
	}

	return registry, nil
}

//nolint:funlen // Each metric is declared explicitly in the shared registry.
func New(registerer prometheus.Registerer) (*Metrics, error) {
	const (
		methodLabel   = "method"
		routeLabel    = "route"
		categoryLabel = "category"
	)

	useCaseBuckets := []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 15, 30}

	metrics := &Metrics{
		httpRequests: prometheus.NewCounterVec(
			prometheus.CounterOpts{Name: "shortener_http_requests_total", Help: "Completed business HTTP requests."},
			[]string{methodLabel, routeLabel, "status", "outcome"},
		),
		httpDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    "shortener_http_request_duration_seconds",
				Help:    "Business HTTP request duration in seconds.",
				Buckets: []float64{0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 15},
			},
			[]string{methodLabel, routeLabel},
		),
		httpInFlight: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "shortener_http_requests_in_flight",
				Help: "Business HTTP requests currently in progress.",
			},
			[]string{methodLabel, routeLabel},
		),
		createSuccess: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "shortener_usecase_create_success_total",
			Help: "Successful create executions.",
		}),
		createErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "shortener_usecase_create_errors_total",
			Help: "Failed create executions by bounded error category.",
		}, []string{categoryLabel}),
		createDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "shortener_usecase_create_duration_seconds",
			Help:    "Create execution duration in seconds.",
			Buckets: useCaseBuckets,
		}),
		getSuccess: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "shortener_usecase_get_success_total",
			Help: "Successful get executions.",
		}),
		getErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "shortener_usecase_get_errors_total",
			Help: "Failed get executions by bounded error category.",
		}, []string{categoryLabel}),
		getDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "shortener_usecase_get_duration_seconds",
			Help:    "Get execution duration in seconds.",
			Buckets: useCaseBuckets,
		}),
		listSuccess: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "shortener_usecase_list_success_total",
			Help: "Successful list executions.",
		}),
		listErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "shortener_usecase_list_errors_total",
			Help: "Failed list executions by bounded error category.",
		}, []string{categoryLabel}),
		listDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "shortener_usecase_list_duration_seconds",
			Help:    "List execution duration in seconds.",
			Buckets: useCaseBuckets,
		}),
		updateSuccess: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "shortener_usecase_update_success_total",
			Help: "Successful update executions.",
		}),
		updateErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "shortener_usecase_update_errors_total",
			Help: "Failed update executions by bounded error category.",
		}, []string{categoryLabel}),
		updateDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "shortener_usecase_update_duration_seconds",
			Help:    "Update execution duration in seconds.",
			Buckets: useCaseBuckets,
		}),
		deleteSuccess: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "shortener_usecase_delete_success_total",
			Help: "Successful delete executions.",
		}),
		deleteErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "shortener_usecase_delete_errors_total",
			Help: "Failed delete executions by bounded error category.",
		}, []string{categoryLabel}),
		deleteDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "shortener_usecase_delete_duration_seconds",
			Help:    "Delete execution duration in seconds.",
			Buckets: useCaseBuckets,
		}),
		resolveSuccess: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "shortener_usecase_resolve_success_total",
			Help: "Successful resolve executions.",
		}),
		resolveErrors: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "shortener_usecase_resolve_errors_total",
			Help: "Failed resolve executions by bounded error category.",
		}, []string{categoryLabel}),
		resolveDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "shortener_usecase_resolve_duration_seconds",
			Help:    "Resolve execution duration in seconds.",
			Buckets: useCaseBuckets,
		}),
	}

	collectors := []prometheus.Collector{
		metrics.httpRequests,
		metrics.httpDuration,
		metrics.httpInFlight,
		metrics.createSuccess,
		metrics.createErrors,
		metrics.createDuration,
		metrics.getSuccess,
		metrics.getErrors,
		metrics.getDuration,
		metrics.listSuccess,
		metrics.listErrors,
		metrics.listDuration,
		metrics.updateSuccess,
		metrics.updateErrors,
		metrics.updateDuration,
		metrics.deleteSuccess,
		metrics.deleteErrors,
		metrics.deleteDuration,
		metrics.resolveSuccess,
		metrics.resolveErrors,
		metrics.resolveDuration,
	}
	for index, collector := range collectors {
		if err := registerer.Register(collector); err != nil {
			for _, registered := range collectors[:index] {
				registerer.Unregister(registered)
			}

			return nil, err
		}
	}

	return metrics, nil
}

func (m *Metrics) StartHTTPRequest(method, route string) {
	m.httpInFlight.WithLabelValues(method, route).Inc()
}

func (m *Metrics) FinishHTTPRequest(method, route string, status int, outcome string, startedAt time.Time) {
	m.httpInFlight.WithLabelValues(method, route).Dec()
	m.httpRequests.WithLabelValues(method, route, strconv.Itoa(status), outcome).Inc()
	m.httpDuration.WithLabelValues(method, route).Observe(time.Since(startedAt).Seconds())
}

func (m *Metrics) ObserveCreateDuration(startedAt time.Time) {
	m.createDuration.Observe(time.Since(startedAt).Seconds())
}

func (m *Metrics) IncrementCreateSuccess() {
	m.createSuccess.Inc()
}

func (m *Metrics) IncrementCreateError(category string) {
	m.createErrors.WithLabelValues(m.errorCategory(category)).Inc()
}

func (m *Metrics) ObserveGetDuration(startedAt time.Time) {
	m.getDuration.Observe(time.Since(startedAt).Seconds())
}

func (m *Metrics) IncrementGetSuccess() {
	m.getSuccess.Inc()
}

func (m *Metrics) IncrementGetError(category string) {
	m.getErrors.WithLabelValues(m.errorCategory(category)).Inc()
}

func (m *Metrics) ObserveListDuration(startedAt time.Time) {
	m.listDuration.Observe(time.Since(startedAt).Seconds())
}

func (m *Metrics) IncrementListSuccess() {
	m.listSuccess.Inc()
}

func (m *Metrics) IncrementListError(category string) {
	m.listErrors.WithLabelValues(m.errorCategory(category)).Inc()
}

func (m *Metrics) ObserveUpdateDuration(startedAt time.Time) {
	m.updateDuration.Observe(time.Since(startedAt).Seconds())
}

func (m *Metrics) IncrementUpdateSuccess() {
	m.updateSuccess.Inc()
}

func (m *Metrics) IncrementUpdateError(category string) {
	m.updateErrors.WithLabelValues(m.errorCategory(category)).Inc()
}

func (m *Metrics) ObserveDeleteDuration(startedAt time.Time) {
	m.deleteDuration.Observe(time.Since(startedAt).Seconds())
}

func (m *Metrics) IncrementDeleteSuccess() {
	m.deleteSuccess.Inc()
}

func (m *Metrics) IncrementDeleteError(category string) {
	m.deleteErrors.WithLabelValues(m.errorCategory(category)).Inc()
}

func (m *Metrics) ObserveResolveDuration(startedAt time.Time) {
	m.resolveDuration.Observe(time.Since(startedAt).Seconds())
}

func (m *Metrics) IncrementResolveSuccess() {
	m.resolveSuccess.Inc()
}

func (m *Metrics) IncrementResolveError(category string) {
	m.resolveErrors.WithLabelValues(m.errorCategory(category)).Inc()
}

func (m *Metrics) errorCategory(category string) string {
	switch category {
	case "invalid_argument", "conflict", "not_found", "canceled":
		return category
	default:
		return "internal"
	}
}
