//go:build e2e

package shortner_test

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	collectortrace "go.opentelemetry.io/proto/otlp/collector/trace/v1"
	tracepb "go.opentelemetry.io/proto/otlp/trace/v1"
	"google.golang.org/protobuf/proto"
)

//nolint:gocognit,paralleltest // Cases own isolated stacks and verify complete trace lifecycles.
func TestApplicationTracing(t *testing.T) {
	for _, tc := range []struct {
		name       string
		enabled    bool
		sampleRate int
	}{
		{name: "sampled", enabled: true, sampleRate: 1},
		{name: "zero sampling", enabled: true, sampleRate: 0},
		{name: "disabled", enabled: false, sampleRate: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var (
				mu    sync.Mutex
				spans []*tracepb.Span
			)

			receiver := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				data, err := io.ReadAll(r.Body)
				if err != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}

				var request collectortrace.ExportTraceServiceRequest
				if decodeErr := proto.Unmarshal(data, &request); decodeErr != nil {
					w.WriteHeader(http.StatusBadRequest)
					return
				}

				mu.Lock()
				for _, resource := range request.GetResourceSpans() {
					for _, scope := range resource.GetScopeSpans() {
						spans = append(spans, scope.GetSpans()...)
					}
				}
				mu.Unlock()
				w.Header().Set("Content-Type", "application/x-protobuf")
				w.WriteHeader(http.StatusOK)
			}))
			listener, err := net.Listen("tcp", "0.0.0.0:0")
			require.NoError(t, err)

			receiver.Listener = listener
			receiver.Start()
			t.Cleanup(receiver.Close)

			_, file, _, ok := runtime.Caller(0)
			require.True(t, ok)

			root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../../.."))
			// Colima cannot mount the system test temporary directory into containers.
			dir, err := os.MkdirTemp(
				root,
				".tracing-e2e-",
			) //nolint:usetesting // The repository is shared with the Docker VM.
			require.NoError(t, err)
			t.Cleanup(func() {
				require.NoError(t, os.RemoveAll(dir))
			})

			port := listener.Addr().(*net.TCPAddr).Port
			config := fmt.Sprintf(
				"app:\n  open-telemetry:\n    trace_enabled: %t\n    sample_rate: %d\n    trace_url: host.docker.internal:%d\n    exporter_type: http\n    batch_timeout: 100ms\n",
				tc.enabled,
				tc.sampleRate,
				port,
			)
			configFile := filepath.Join(dir, "tracing.yaml")
			require.NoError(t, os.WriteFile(configFile, []byte(config), 0o644))

			composeFile := filepath.Join(dir, "compose.yaml")
			override := fmt.Sprintf(
				"services:\n  app:\n    environment:\n      APP_ENV: tracing\n    volumes:\n      - %q\n",
				configFile+":/app/config/tracing.yaml:ro",
			)
			if runtime.GOOS == "linux" {
				override += "    extra_hosts:\n      - host.docker.internal:host-gateway\n"
			}
			require.NoError(t, os.WriteFile(composeFile, []byte(override), 0o600))
			app := startConfiguredStack(t, false, composeFile)

			app.expect(
				t,
				http.MethodPost,
				"/api/v1/links",
				`{"destination":"https://example.com/private-path","alias":"private-alias"}`,
				apiKey,
				"application/json",
				http.StatusCreated,
			)
			app.expect(t, http.MethodGet, "/private-alias", "", "", "", http.StatusFound)

			// Parent-based sampling must follow the remote parent without creating an HTTP span.
			for _, parent := range []string{
				"00-11111111111111111111111111111111-1111111111111111-01",
				"00-22222222222222222222222222222222-2222222222222222-00",
			} {
				req, requestErr := http.NewRequestWithContext(t.Context(), http.MethodGet, app.base+"/private-alias", nil)
				require.NoError(t, requestErr)
				req.Header.Set("traceparent", parent)
				req.Header.Set("baggage", "private-key=private-value")
				resp, requestErr := app.client.Do(req)
				require.NoError(t, requestErr)
				require.Equal(t, http.StatusFound, resp.StatusCode)
				require.NoError(t, resp.Body.Close())
			}

			if tc.enabled && tc.sampleRate == 1 {
				require.Eventually(t, func() bool {
					mu.Lock()
					defer mu.Unlock()

					for _, span := range spans {
						if bytes.Equal(bytes.Repeat([]byte{0x11}, 16), span.GetTraceId()) {
							return len(spans) >= 6
						}
					}

					return false
				}, 10*time.Second, 100*time.Millisecond)
				mu.Lock()

				collected := append([]*tracepb.Span(nil), spans...)
				mu.Unlock()

				byName := make(map[string]*tracepb.Span)
				for _, span := range collected {
					if !bytes.Equal(bytes.Repeat([]byte{0x11}, 16), span.GetTraceId()) {
						byName[span.GetName()] = span
					}
					require.NotContains(t, span.String(), "private-alias")
					require.NotContains(t, span.String(), "private-path")
					require.NotContains(t, span.String(), apiKey)
				}

				var sampledParentFound bool
				for _, span := range collected {
					require.NotEqual(t, bytes.Repeat([]byte{0x22}, 16), span.GetTraceId(), "unsampled parent must not export spans")
					require.NotContains(t, span.String(), "private-value")
					if bytes.Equal(bytes.Repeat([]byte{0x11}, 16), span.GetTraceId()) && span.GetName() == "LinkResolveUseCase.Execute" {
						require.Equal(t, bytes.Repeat([]byte{0x11}, 8), span.GetParentSpanId())
						sampledParentFound = true
					}
				}
				require.True(t, sampledParentFound, "sampled remote parent must be propagated")

				parent := byName["LinkResolveUseCase.Execute"]
				require.NotNil(t, parent)

				for _, name := range []string{"RedirectCache.Get", "LinkRepository.FindByCode", "RedirectCache.Set"} {
					child := byName[name]
					require.NotNil(t, child, name)
					require.True(t, bytes.Equal(parent.GetTraceId(), child.GetTraceId()), name)
					require.True(t, bytes.Equal(parent.GetSpanId(), child.GetParentSpanId()), name)
				}

				for name := range byName {
					require.Contains(
						t,
						[]string{
							"LinkCreateUseCase.Execute",
							"LinkRepository.Create",
							"LinkResolveUseCase.Execute",
							"RedirectCache.Get",
							"LinkRepository.FindByCode",
							"RedirectCache.Set",
						},
						name,
					)
				}

				// A long batch interval makes the next export depend on shutdown flushing.
				longBatch := strings.Replace(config, "batch_timeout: 100ms", "batch_timeout: 30s", 1)
				require.NoError(t, os.WriteFile(configFile, []byte(longBatch), 0o644))
				out, err := app.run("restart", "app")
				require.NoError(t, err, string(out))
				app.base = app.composeAddress(t, "8080")
				app.waitReady(t)
				mu.Lock()
				spans = nil
				mu.Unlock()
				app.expect(t, http.MethodGet, "/private-alias", "", "", "", http.StatusFound)
				conn, err := net.DialTimeout("tcp", strings.TrimPrefix(app.base, "http://"), time.Second)
				require.NoError(t, err)
				t.Cleanup(func() {
					_ = conn.Close()
				})
				_, err = fmt.Fprintf(conn, "POST /api/v1/links HTTP/1.1\r\nHost: localhost\r\nX-Api-Key: %s\r\nContent-Type: application/json\r\nContent-Length: 100\r\n\r\n{", apiKey)
				require.NoError(t, err)
				time.Sleep(200 * time.Millisecond)
				out, err = app.run("stop", "-t", "35", "app")
				require.NoError(t, err, string(out))
				require.Eventually(t, func() bool {
					mu.Lock()
					defer mu.Unlock()
					return len(spans) >= 2
				}, 5*time.Second, 100*time.Millisecond, "shutdown must flush pending spans")
				out, err = app.run("up", "-d", "--no-deps", "app")
				require.NoError(t, err, string(out))
				app.base = app.composeAddress(t, "8080")
				app.waitReady(t)
				receiver.Close()
				app.expect(t, http.MethodGet, "/private-alias", "", "", "", http.StatusFound)
			} else if tc.enabled {
				require.Eventually(t, func() bool {
					mu.Lock()
					defer mu.Unlock()
					return len(spans) > 0
				}, 10*time.Second, 100*time.Millisecond)
				mu.Lock()
				for _, span := range spans {
					require.Equal(t, bytes.Repeat([]byte{0x11}, 16), span.GetTraceId())
				}
				mu.Unlock()
			} else {
				time.Sleep(time.Second)
				mu.Lock()
				count := len(spans)
				mu.Unlock()
				require.Zero(t, count)
			}

			started := time.Now()
			out, err := app.run("stop", "-t", strconv.Itoa(12), "app")
			require.NoError(t, err, string(out))
			require.Less(t, time.Since(started), 12*time.Second)
			if !tc.enabled {
				mu.Lock()
				count := len(spans)
				mu.Unlock()
				require.Zero(t, count, "shutdown must not export unsampled spans")
			}
		})
	}
}
