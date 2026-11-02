//go:build e2e

package shortner_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/cristiano-pacheco/go-shortner/cmd"
)

//nolint:paralleltest // The test sets process environment and registers a global sentinel.
func TestMetricsRegistryIsolationAcrossSequentialApplications(t *testing.T) {
	stack := start(t)
	out, err := stack.run("port", "postgres", "5432")
	require.NoError(t, err, "%s", out)
	postgresHost, postgresPort, err := net.SplitHostPort(strings.TrimSpace(string(out)))
	require.NoError(t, err)
	out, err = stack.run("port", "redis", "6379")
	require.NoError(t, err, "%s", out)
	redisAddress := strings.TrimSpace(string(out))

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	metricsAddress := listener.Addr().(*net.TCPAddr)
	metricsPort := metricsAddress.Port

	require.NoError(t, listener.Close())

	_, file, _, ok := runtime.Caller(0)
	require.True(t, ok)

	configPath := filepath.Join(filepath.Dir(file), "../../../..", "config", "base.yaml")
	baseConfig, err := os.ReadFile(configPath)
	require.NoError(t, err)

	configured := strings.Replace(string(baseConfig), "address: \"0.0.0.0\"", "address: \"127.0.0.1\"", 1)
	configured = strings.Replace(configured, "port: 9090", fmt.Sprintf("port: %d", metricsPort), 1)
	configDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "base.yaml"), []byte(configured), 0o600))

	t.Setenv("APP_CONFIG_DIR", configDir)
	t.Setenv("APP_ENV", "local")
	t.Setenv("DATABASE_HOST", postgresHost)
	t.Setenv("DATABASE_PORT", postgresPort)
	t.Setenv("DATABASE_NAME", "shortener")
	t.Setenv("DATABASE_USER", "shortener")
	t.Setenv("DATABASE_PASSWORD", "e2e-only")
	t.Setenv("DATABASE_SSLMODE", "disable")
	t.Setenv("REDIS_URL", "redis://"+redisAddress+"/0")
	t.Setenv("API_KEY", apiKey)
	t.Setenv("PUBLIC_BASE_URL", publicBase)
	t.Setenv("HTTP_ADDR", "127.0.0.1:0")

	sentinel := prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "shortner_test_global_registry_sentinel",
			Help: "Global registry isolation sentinel.",
		},
	)
	require.NoError(t, prometheus.Register(sentinel))
	t.Cleanup(func() {
		prometheus.Unregister(sentinel)
	})
	sentinel.Set(42)

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	found := false

	for _, family := range families {
		if family.GetName() == "shortner_test_global_registry_sentinel" {
			found = true
		}
	}

	require.True(t, found, "sentinel must be present in global gatherer")

	for iteration := range 2 {
		t.Run(fmt.Sprintf("instance_%d", iteration+1), func(t *testing.T) {
			stack.assertMetricsRegistry(t, metricsPort)
		})
	}
}

func (s httpSuite) assertMetricsRegistry(t *testing.T, metricsPort int) {
	t.Helper()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	var runErr error

	command := cmd.NewRootCommand()

	command.SetArgs([]string{"server"})
	go func() {
		runErr = command.ExecuteContext(ctx)

		close(done)
	}()

	t.Cleanup(func() {
		cancel()

		select {
		case <-done:
			if runErr != nil {
				t.Errorf("application stopped: %v", runErr)
			}
		case <-time.After(15 * time.Second):
			t.Error("application did not shut down")
		}
	})

	var body []byte

	require.Eventually(t, func() bool {
		select {
		case <-done:
			return true
		default:
		}

		response, requestErr := s.client.Get(fmt.Sprintf("http://127.0.0.1:%d/metrics", metricsPort))
		if requestErr != nil {
			return false
		}
		defer response.Body.Close()

		if response.StatusCode != http.StatusOK {
			return false
		}

		body, requestErr = io.ReadAll(response.Body)

		return requestErr == nil
	}, 30*time.Second, 100*time.Millisecond)

	select {
	case <-done:
		t.Fatalf("application stopped during startup: %v", runErr)
	default:
	}

	require.Contains(t, string(body), "go_goroutines ")
	require.Contains(t, string(body), "process_cpu_seconds_total ")
	require.NotContains(t, string(body), "shortner_test_global_registry_sentinel")
}
