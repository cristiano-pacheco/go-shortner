//go:build e2e

package shortner_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
)

//nolint:paralleltest // The test scales and stops its isolated Compose application.
func TestAdministrativeMetrics(t *testing.T) {
	stack := start(t)

	scrape := func(base string) string {
		t.Helper()

		response, err := stack.client.Get(base + "/metrics")
		require.NoError(t, err)

		defer response.Body.Close()

		require.Equal(t, http.StatusOK, response.StatusCode)
		body, err := io.ReadAll(response.Body)
		require.NoError(t, err)

		return string(body)
	}
	metrics := scrape(stack.metricsBase)
	require.Contains(t, metrics, "go_goroutines ")
	require.Contains(t, metrics, "process_cpu_seconds_total ")
	require.NotContains(t, metrics, "promhttp_metric_handler_requests_total")

	stack.expect(
		t,
		http.MethodPost,
		"/api/v1/links",
		`{"destination":"https://example.com/","alias":"metrics"}`,
		apiKey,
		"application/json",
		http.StatusCreated,
	)
	headers, _ := stack.expect(t, http.MethodGet, "/metrics", "", "", "", http.StatusFound)
	require.Equal(t, "https://example.com/", headers.Get("Location"))

	out, err := stack.run("up", "-d", "--no-deps", "--scale", "app=2", "app")
	require.NoError(t, err, "%s", out)
	out, err = stack.run("port", "--index", "2", "app", "9090")
	require.NoError(t, err, "%s", out)
	second := "http://" + strings.TrimSpace(string(out))

	require.Eventually(t, func() bool {
		response, requestErr := stack.client.Get(second + "/metrics")
		if requestErr != nil {
			return false
		}
		defer response.Body.Close()

		return response.StatusCode == http.StatusOK
	}, 30*time.Second, 200*time.Millisecond)
	require.Contains(t, scrape(second), "go_goroutines ")

	out, err = stack.run("ps", "-q", "app")
	require.NoError(t, err, "%s", out)
	container := strings.Fields(string(out))[0]

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	inspect := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.Config.Image}}", container)
	image, err := inspect.Output()
	require.NoError(t, err)
	// The second process shares the first application's network namespace. Its
	// public port differs so the existing administrative port is the conflict.
	command := exec.CommandContext(ctx, "docker", "run", "--rm", "--network", "container:"+container,
		"-e", "DATABASE_HOST=postgres", "-e", "DATABASE_PORT=5432", "-e", "DATABASE_NAME=shortener",
		"-e", "DATABASE_USER=shortener", "-e", "DATABASE_PASSWORD=e2e-only", "-e", "DATABASE_SSLMODE=disable",
		"-e", "REDIS_URL=redis://redis:6379/0", "-e", "API_KEY="+apiKey,
		"-e", "PUBLIC_BASE_URL="+publicBase, "-e", "HTTP_ADDR=:8081", strings.TrimSpace(string(image)), "server")
	out, err = command.CombinedOutput()
	require.Error(t, err, "a second listener on port 9090 must fail")
	require.Contains(t, string(out), "address already in use")

	out, err = stack.run("stop", "app")
	require.NoError(t, err, "%s", out)
	_, err = stack.client.Get(stack.metricsBase + "/metrics")
	require.Error(t, err, "stopped listener must reject requests")
}

//nolint:paralleltest,gocognit // Cases share state and verify multiple metric dimensions.
func TestHTTPMetrics(t *testing.T) {
	stack := start(t)

	type requestCase struct {
		method, path, body, key, media string
		route, metricMethod, outcome   string
		status                         int
	}

	cases := []requestCase{
		{
			http.MethodPost,
			"/api/v1/links",
			`{"destination":"https://example.com/private","alias":"metric-alpha"}`,
			apiKey,
			"application/json",
			"/api/v1/links",
			"POST",
			"success",
			201,
		},
		{
			http.MethodPost,
			"/api/v1/links",
			`{"destination":"https://example.com/private","alias":"metric-bravo"}`,
			apiKey,
			"application/json",
			"/api/v1/links",
			"POST",
			"success",
			201,
		},
		{http.MethodGet, "/metric-alpha", "", "", "", "/{code}", "GET", "success", 302},
		{http.MethodGet, "/metric-bravo", "", "", "", "/{code}", "GET", "success", 302},
		{http.MethodHead, "/metric-alpha", "", "", "", "/{code}", "HEAD", "success", 302},
		{http.MethodGet, "/api/v1/links/1", "", apiKey, "", "/api/v1/links/{id}", "GET", "success", 200},
		{http.MethodGet, "/api/v1/links/2", "", apiKey, "", "/api/v1/links/{id}", "GET", "success", 200},
		{http.MethodPost, "/api/v1/links", `{}`, "", "application/json", "/api/v1/links", "POST", "client_error", 401},
		{
			http.MethodPost,
			"/api/v1/links",
			`{"destination":`,
			apiKey,
			"application/json",
			"/api/v1/links",
			"POST",
			"client_error",
			400,
		},
		{http.MethodGet, "/unknown/route-one", "", "", "", "unmatched", "GET", "client_error", 404},
		{http.MethodGet, "/unknown/route-two", "", "", "", "unmatched", "GET", "client_error", 404},
		{http.MethodPut, "/api/v1/links", "", apiKey, "", "/api/v1/links", "PUT", "client_error", 405},
		{http.MethodPost, "/health", "", "", "", "/health", "POST", "client_error", 405},
		{"CUSTOM", "/api/v1/links", "", apiKey, "", "/api/v1/links", "OTHER", "client_error", 405},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s_%s_%d", tc.method, tc.path, tc.status), func(t *testing.T) {
			before := stack.httpMetrics(t)

			headers, body := stack.expect(t, tc.method, tc.path, tc.body, tc.key, tc.media, tc.status)
			if tc.status == http.StatusFound {
				require.Equal(t, "https://example.com/private", headers.Get("Location"))

				if tc.method == http.MethodHead {
					require.Empty(t, body)
				}
			}

			after := stack.httpMetrics(t)
			labels := map[string]string{
				"method":  tc.metricMethod,
				"route":   tc.route,
				"status":  strconv.Itoa(tc.status),
				"outcome": tc.outcome,
			}
			require.InDelta(
				t,
				before.value("shortener_http_requests_total", labels)+1,
				after.value("shortener_http_requests_total", labels),
				0,
			)
			require.InDelta(
				t,
				before.total("shortener_http_requests_total")+1,
				after.total("shortener_http_requests_total"),
				0,
			)

			durationLabels := map[string]string{"method": tc.metricMethod, "route": tc.route}
			require.InDelta(
				t,
				before.value("shortener_http_request_duration_seconds", durationLabels)+1,
				after.value("shortener_http_request_duration_seconds", durationLabels),
				0,
			)
			require.InDelta(
				t,
				before.total("shortener_http_request_duration_seconds")+1,
				after.total("shortener_http_request_duration_seconds"),
				0,
			)
			require.NotNil(t, after.find("shortener_http_requests_in_flight", durationLabels))
			require.Zero(t, after.value("shortener_http_requests_in_flight", durationLabels))
		})
	}

	before := stack.httpMetrics(t)
	stack.expect(t, http.MethodGet, "/health", "", "", "", http.StatusOK)
	after := stack.httpMetrics(t)
	require.InDelta(
		t,
		before.total("shortener_http_requests_total"),
		after.total("shortener_http_requests_total"),
		0,
		"health and scrape requests must not count as business traffic",
	)
	require.InDelta(
		t,
		before.total("shortener_http_request_duration_seconds"),
		after.total("shortener_http_request_duration_seconds"),
		0,
	)

	for _, family := range after {
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				for _, secret := range []string{"metric-alpha", "metric-bravo", "example.com", apiKey, "route-one", "route-two"} {
					require.NotContains(t, label.GetValue(), secret)
				}
			}
		}
	}
}

type httpMetricFamilies map[string]*dto.MetricFamily

func (s httpSuite) httpMetrics(t *testing.T) httpMetricFamilies {
	t.Helper()

	response, err := s.client.Get(s.metricsBase + "/metrics")
	require.NoError(t, err)

	defer response.Body.Close()

	require.Equal(t, http.StatusOK, response.StatusCode)

	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(response.Body)
	require.NoError(t, err)

	return families
}

func (families httpMetricFamilies) value(name string, labels map[string]string) float64 {
	metric := families.find(name, labels)
	if metric == nil {
		return 0
	}

	return httpMetricValue(metric)
}

func (families httpMetricFamilies) find(name string, labels map[string]string) *dto.Metric {
	family := families[name]
	if family == nil {
		return nil
	}

	for _, metric := range family.GetMetric() {
		if len(metric.GetLabel()) != len(labels) {
			continue
		}

		matches := true

		for _, label := range metric.GetLabel() {
			if value, exists := labels[label.GetName()]; !exists || value != label.GetValue() {
				matches = false
				break
			}
		}

		if matches {
			return metric
		}
	}

	return nil
}

func (families httpMetricFamilies) total(name string) float64 {
	var total float64
	for _, metric := range families[name].GetMetric() {
		total += httpMetricValue(metric)
	}

	return total
}

func httpMetricValue(metric *dto.Metric) float64 {
	switch {
	case metric.GetCounter() != nil:
		return metric.GetCounter().GetValue()
	case metric.GetGauge() != nil:
		return metric.GetGauge().GetValue()
	case metric.GetHistogram() != nil:
		return float64(metric.GetHistogram().GetSampleCount())
	default:
		return 0
	}
}

//nolint:paralleltest // The cases share an isolated Compose stack and its database.
func TestUseCaseMetrics(t *testing.T) {
	stack := start(t)

	type requestCase struct {
		method, path, body, key, media string
		operation, category            string
		status                         int
	}
	cases := []requestCase{
		{http.MethodPost, "/api/v1/links", `{"destination":"https://example.com/private","alias":"usecase-metric"}`, apiKey, "application/json", "create", "", http.StatusCreated},
		{http.MethodGet, "/api/v1/links/1", "", apiKey, "", "get", "", http.StatusOK},
		{http.MethodGet, "/api/v1/links", "", apiKey, "", "list", "", http.StatusOK},
		{http.MethodPatch, "/api/v1/links/1", `{"destination":"https://example.org/updated"}`, apiKey, "application/json", "update", "", http.StatusOK},
		{http.MethodGet, "/usecase-metric", "", "", "", "resolve", "", http.StatusFound},
		{http.MethodHead, "/usecase-metric", "", "", "", "resolve", "", http.StatusFound},
		{http.MethodPost, "/api/v1/links", `{"destination":"https://example.com/private","alias":"usecase-metric"}`, apiKey, "application/json", "create", "conflict", http.StatusConflict},
		{http.MethodPost, "/api/v1/links", `{"destination":"not-a-url"}`, apiKey, "application/json", "create", "invalid_argument", http.StatusBadRequest},
		{http.MethodGet, "/api/v1/links/999999", "", apiKey, "", "get", "not_found", http.StatusNotFound},
		{http.MethodGet, "/absent-metric", "", "", "", "resolve", "not_found", http.StatusNotFound},
		{http.MethodDelete, "/api/v1/links/1", "", apiKey, "", "delete", "", http.StatusNoContent},
		{http.MethodPost, "/api/v1/links", `{}`, "", "application/json", "", "", http.StatusUnauthorized},
		{http.MethodPost, "/api/v1/links", `{"destination":`, apiKey, "application/json", "", "", http.StatusBadRequest},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s_%s_%d", tc.method, tc.path, tc.status), func(t *testing.T) {
			before := stack.httpMetrics(t)
			stack.expect(t, tc.method, tc.path, tc.body, tc.key, tc.media, tc.status)
			after := stack.httpMetrics(t)
			assertUseCaseMetricDelta(t, before, after, tc.operation, tc.category)
		})
	}

	out, err := stack.run("stop", "postgres")
	require.NoError(t, err, "%s", out)
	before := stack.httpMetrics(t)
	stack.expect(t, http.MethodGet, "/api/v1/links/999998", "", apiKey, "", http.StatusInternalServerError)
	after := stack.httpMetrics(t)
	assertUseCaseMetricDelta(t, before, after, "get", "internal")

	for name, family := range after {
		if !strings.HasPrefix(name, "shortener_usecase_") {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				for _, secret := range []string{"usecase-metric", "absent-metric", "example.com", "example.org", apiKey} {
					require.NotContains(t, label.GetValue(), secret)
				}
			}
		}
	}
}

func assertUseCaseMetricDelta(t *testing.T, before, after httpMetricFamilies, operation, category string) {
	t.Helper()

	for _, candidate := range []string{"create", "get", "list", "update", "delete", "resolve"} {
		success, failures, duration := 0.0, 0.0, 0.0
		if operation == candidate {
			duration = 1
			if category == "" {
				success = 1
			} else {
				failures = 1
			}
		}
		prefix := "shortener_usecase_" + candidate
		require.Equal(t, before.total(prefix+"_success_total")+success, after.total(prefix+"_success_total"))
		require.Equal(t, before.total(prefix+"_errors_total")+failures, after.total(prefix+"_errors_total"))
		require.Equal(t, before.total(prefix+"_duration_seconds")+duration, after.total(prefix+"_duration_seconds"))
		if failures != 0 {
			labels := map[string]string{"category": category}
			require.Equal(t, before.value(prefix+"_errors_total", labels)+1, after.value(prefix+"_errors_total", labels))
		}
	}
}

//nolint:paralleltest // Requests share an isolated Compose stack.
func TestCodeGenerationMetrics(t *testing.T) {
	stack := start(t)
	before := stack.httpMetrics(t)
	_, body := stack.expect(t, http.MethodPost, "/api/v1/links", `{"destination":"https://example.com/generated"}`, apiKey, "application/json", http.StatusCreated)
	generated := decodeLink(t, body)
	require.Regexp(t, `^[A-Za-z0-9]{8}$`, generated.Code)
	after := stack.httpMetrics(t)
	success := map[string]string{"result": "success"}
	require.Equal(t, before.value("shortener_code_generation_attempts_total", success)+1, after.value("shortener_code_generation_attempts_total", success))
	require.Equal(t, before.value("shortener_code_generation_duration_seconds", success)+1, after.value("shortener_code_generation_duration_seconds", success))
	require.Equal(t, before.total("shortener_code_creation_events_total"), after.total("shortener_code_creation_events_total"))

	stack.expect(t, http.MethodPost, "/api/v1/links", `{"destination":"https://example.com/original","alias":"metric-duplicate"}`, apiKey, "application/json", http.StatusCreated)
	before = stack.httpMetrics(t)
	stack.expect(t, http.MethodPost, "/api/v1/links", `{"destination":"https://example.com/other","alias":"metric-duplicate"}`, apiKey, "application/json", http.StatusConflict)
	after = stack.httpMetrics(t)
	require.Equal(t, before.total("shortener_code_generation_attempts_total"), after.total("shortener_code_generation_attempts_total"))
	require.Equal(t, before.total("shortener_code_generation_duration_seconds"), after.total("shortener_code_generation_duration_seconds"))
	labels := map[string]string{"event": "alias_conflict"}
	require.Equal(t, before.value("shortener_code_creation_events_total", labels)+1, after.value("shortener_code_creation_events_total", labels))
	require.Equal(t, before.total("shortener_code_creation_events_total")+1, after.total("shortener_code_creation_events_total"))
	for name, family := range after {
		if !strings.HasPrefix(name, "shortener_code_") {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				for _, secret := range []string{generated.Code, "metric-duplicate", "example.com", apiKey} {
					require.NotContains(t, label.GetValue(), secret)
				}
			}
		}
	}
}

//nolint:paralleltest // Requests share an isolated Compose stack and stop its PostgreSQL service.
func TestPersistenceMetrics(t *testing.T) {
	stack := start(t)
	initialMetrics := stack.httpMetrics(t)

	type requestCase struct {
		method, path, body, key, media string
		operation, result              string
		status                         int
	}
	cases := []requestCase{
		{http.MethodPost, "/api/v1/links", `{"destination":"https://example.com/private","alias":"persistence-metric"}`, apiKey, "application/json", "create", "success", http.StatusCreated},
		{http.MethodPost, "/api/v1/links", `{"destination":"https://example.com/private","alias":"persistence-metric"}`, apiKey, "application/json", "create", "duplicate", http.StatusConflict},
		{http.MethodGet, "/api/v1/links/1", "", apiKey, "", "find_by_id", "success", http.StatusOK},
		{http.MethodGet, "/api/v1/links/999999", "", apiKey, "", "find_by_id", "not_found", http.StatusNotFound},
		{http.MethodGet, "/persistence-metric", "", "", "", "find_by_code", "success", http.StatusFound},
		{http.MethodGet, "/missing-persistence-metric", "", "", "", "find_by_code", "not_found", http.StatusNotFound},
		{http.MethodGet, "/api/v1/links", "", apiKey, "", "list", "success", http.StatusOK},
		{http.MethodPatch, "/api/v1/links/1", `{"destination":"https://example.org/updated"}`, apiKey, "application/json", "update", "success", http.StatusOK},
		{http.MethodPatch, "/api/v1/links/999999", `{"active":false}`, apiKey, "application/json", "update", "not_found", http.StatusNotFound},
		{http.MethodDelete, "/api/v1/links/1", "", apiKey, "", "delete", "success", http.StatusNoContent},
		{http.MethodDelete, "/api/v1/links/1", "", apiKey, "", "delete", "not_found", http.StatusNotFound},
	}

	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s_%s_%d", tc.method, tc.path, tc.status), func(t *testing.T) {
			before := stack.httpMetrics(t)
			stack.expect(t, tc.method, tc.path, tc.body, tc.key, tc.media, tc.status)
			after := stack.httpMetrics(t)
			assertPersistenceMetricDelta(t, before, after, tc.operation, tc.result)
		})
	}

	poolMetrics := stack.httpMetrics(t)
	postgresLabels := map[string]string{"db_name": "postgres"}
	for _, name := range []string{
		"go_sql_max_open_connections", "go_sql_open_connections", "go_sql_in_use_connections",
		"go_sql_idle_connections", "go_sql_wait_count_total", "go_sql_wait_duration_seconds_total",
	} {
		require.NotNil(t, poolMetrics.find(name, postgresLabels), name)
	}
	require.Equal(t, 10.0, poolMetrics.value("go_sql_max_open_connections", postgresLabels))
	require.Greater(t, poolMetrics.value("go_sql_open_connections", postgresLabels), 0.0)
	require.Equal(t, poolMetrics.value("go_sql_open_connections", postgresLabels),
		poolMetrics.value("go_sql_in_use_connections", postgresLabels)+poolMetrics.value("go_sql_idle_connections", postgresLabels))

	for _, name := range []string{
		"shortener_redis_pool_connections", "shortener_redis_pool_idle_connections", "shortener_redis_pool_pending_requests",
		"shortener_redis_pool_hits_total", "shortener_redis_pool_misses_total", "shortener_redis_pool_timeouts_total",
	} {
		require.NotNil(t, poolMetrics.find(name, nil), name)
	}
	require.Greater(t, poolMetrics.value("shortener_redis_pool_connections", nil), 0.0)
	require.Greater(t, poolMetrics.value("shortener_redis_pool_hits_total", nil), initialMetrics.value("shortener_redis_pool_hits_total", nil),
		"application Redis operations must use the observed connection pool")

	out, err := stack.run("stop", "postgres")
	require.NoError(t, err, "%s", out)
	before := stack.httpMetrics(t)
	stack.expect(t, http.MethodGet, "/api/v1/links/999998", "", apiKey, "", http.StatusInternalServerError)
	after := stack.httpMetrics(t)
	assertPersistenceMetricDelta(t, before, after, "find_by_id", "error")

	for name, family := range after {
		if !strings.HasPrefix(name, "shortener_repository_") {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				for _, secret := range []string{"persistence-metric", "example.com", "example.org", apiKey, "999998"} {
					require.NotContains(t, label.GetValue(), secret)
				}
			}
		}
	}
}

func assertPersistenceMetricDelta(t *testing.T, before, after httpMetricFamilies, operation, result string) {
	t.Helper()

	labels := map[string]string{"operation": operation, "result": result}
	for _, name := range []string{"shortener_repository_operations_total", "shortener_repository_operation_duration_seconds"} {
		require.Equal(t, before.value(name, labels)+1, after.value(name, labels), name)
		require.Equal(t, before.total(name)+1, after.total(name), name)
	}
}
