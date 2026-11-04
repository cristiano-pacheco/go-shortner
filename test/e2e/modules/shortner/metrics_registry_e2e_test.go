//go:build e2e

package shortner_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

//nolint:paralleltest // The test restarts its isolated Compose application.
func TestMetricsAcrossApplicationRestart(t *testing.T) {
	stack := start(t)
	stack.expect(
		t,
		http.MethodPost,
		"/api/v1/links",
		`{"destination":"https://example.com/","alias":"restart-metrics"}`,
		apiKey,
		"application/json",
		http.StatusCreated,
	)
	stack.expect(t, http.MethodGet, "/restart-metrics", "", "", "", http.StatusFound)
	require.NotEmpty(t, stack.httpMetrics(t)["shortener_http_requests_total"])

	out, err := stack.run("restart", "app")
	require.NoError(t, err, "%s", out)
	stack.base = stack.composeAddress(t, "8080")
	stack.metricsBase = stack.composeAddress(t, "9090")
	require.Eventually(t, func() bool {
		response, requestErr := stack.client.Get(stack.base + "/health")
		if requestErr != nil {
			return false
		}
		defer response.Body.Close()

		return response.StatusCode == http.StatusOK
	}, 45*time.Second, 200*time.Millisecond)

	metrics := stack.httpMetrics(t)
	require.Contains(t, metrics, "go_goroutines")
	require.Contains(t, metrics, "process_cpu_seconds_total")
	require.NotContains(t, metrics, "shortener_http_requests_total")
	headers, _ := stack.expect(t, http.MethodGet, "/restart-metrics", "", "", "", http.StatusFound)
	require.Equal(t, "https://example.com/", headers.Get("Location"))
	require.NotEmpty(t, stack.httpMetrics(t)["shortener_http_requests_total"])
}
