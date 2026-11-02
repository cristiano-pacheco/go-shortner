//go:build e2e

package shortner_test

import (
	"context"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

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
