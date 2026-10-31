//go:build integration

package api_test

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cristiano-pacheco/go-shortner/test/integration/fixture"
)

func TestPublicRedirectAPI(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate repository")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../../../.."))
	binary := filepath.Join(t.TempDir(), "shortener")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build application: %v\n%s", err, output)
	}

	deps := fixture.StartDependencies(t)
	redisProxy := newRedirectCacheProxy(t, deps.RedisAddress)
	host, port, err := net.SplitHostPort(deps.PostgresAddress)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "APP_CONFIG_DIR="+filepath.Join(root, "config"), "APP_ENV=local",
		"DATABASE_HOST="+host, "DATABASE_PORT="+port, "DATABASE_NAME=shortener", "DATABASE_USER=shortener",
		"DATABASE_PASSWORD=integration-only", "DATABASE_SSLMODE=disable", "REDIS_URL=redis://"+redisProxy.listener.Addr().String()+"/0",
		"API_KEY=integration-only-key", "PUBLIC_BASE_URL=https://short.example", "HTTP_ADDR="+address)
	migrate := exec.Command(binary, "migrate")
	migrate.Env = env
	if output, err := migrate.CombinedOutput(); err != nil {
		t.Fatalf("migrate: %v\n%s", err, output)
	}
	sql := func(query string) {
		t.Helper()
		output, err := deps.Exec("postgres", "psql", "-X", "-v", "ON_ERROR_STOP=1", "-U", "shortener", "-d", "shortener", "-c", query)
		if err != nil {
			t.Fatalf("SQL: %v: %s", err, output)
		}
	}
	sql(`INSERT INTO links (code, destination, active, expires_at, deleted_at) VALUES
		('CaseSensitive', 'https://example.com/a?x=1&y=two', true, NULL, NULL),
		('inactive', 'https://example.com/inactive', false, NULL, NULL),
		('expired', 'https://example.com/expired', true, now() - interval '1 second', NULL),
		('deleted', 'https://example.com/deleted', true, NULL, now()),
		('health', 'https://example.com/not-health', true, NULL, NULL),
		('api', 'https://example.com/not-api', true, NULL, NULL)`)
	fixture.StartApplication(t, binary, env, "http://"+address+"/health")

	client := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	request := func(method, path string) (int, http.Header, string) {
		t.Helper()
		req, err := http.NewRequest(method, "http://"+address+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, resp.Header, string(body)
	}
	sql(`INSERT INTO links (id, code, destination, active) VALUES (999999, 'validation-fixture', 'https://example.com', true)`)
	for _, operation := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/links"},
		{http.MethodPatch, "/api/v1/links/999999"},
	} {
		req, err := http.NewRequest(operation.method, "http://"+address+operation.path, strings.NewReader(`{"destination":"https://example.com/path "}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "integration-only-key")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s whitespace destination: want 400, got %d", operation.method, resp.StatusCode)
		}
	}

	const destination = "https://example.com/a?x=1&y=two"
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		status, headers, body := request(method, "/CaseSensitive")
		if status != http.StatusFound || headers.Get("Location") != destination || headers.Get("Cache-Control") != "no-store" {
			t.Errorf("%s redirect: status %d, headers %v, body %q", method, status, headers, body)
		}
		if method == http.MethodHead && body != "" {
			t.Errorf("HEAD redirect returned body %q", body)
		}
	}

	var notFoundBody string
	for i, code := range []string{"unknown", "casesensitive", "inactive", "expired", "deleted"} {
		status, _, body := request(http.MethodGet, "/"+code)
		if status != http.StatusNotFound {
			t.Errorf("%s: want 404, got %d: %s", code, status, body)
		}
		if i == 0 {
			notFoundBody = body
		} else if body != notFoundBody {
			t.Errorf("%s: not-found body %q differs from unknown body %q", code, body, notFoundBody)
		}
		status, _, body = request(http.MethodHead, "/"+code)
		if status != http.StatusNotFound || body != "" {
			t.Errorf("HEAD %s: want empty 404, got %d: %q", code, status, body)
		}
	}
	for _, path := range []string{"/health", "/api"} {
		status, headers, _ := request(http.MethodGet, path)
		if status == http.StatusFound || headers.Get("Location") != "" {
			t.Errorf("infrastructure path %s was captured: %d %v", path, status, headers)
		}
	}

	expiresAt := time.Now().UTC().Add(5 * time.Second)
	sql(fmt.Sprintf(`INSERT INTO links (code, destination, active, expires_at) VALUES ('short-lived', 'https://example.com/short', true, '%s')`, expiresAt.Format(time.RFC3339Nano)))
	var status int
	var body string
	status, _, body = request(http.MethodGet, "/short-lived")
	if status != http.StatusFound {
		t.Fatalf("warm short-lived cache: want 302, got %d: %s", status, body)
	}
	cacheKey := "shortener:redirect:v1:short-lived"
	if output, err := deps.Exec("redis", "redis-cli", "PERSIST", cacheKey); err != nil || strings.TrimSpace(output) != "1" {
		t.Fatalf("retain cached short-lived snapshot: %v: %s", err, output)
	}
	checkedBeforeExpiry := false
	for time.Now().Before(expiresAt.Add(-250 * time.Millisecond)) {
		status, _, body = request(http.MethodGet, "/short-lived")
		if status != http.StatusFound {
			t.Fatalf("short-lived link before expiry: want 302, got %d: %s", status, body)
		}
		checkedBeforeExpiry = true
		time.Sleep(50 * time.Millisecond)
	}
	if !checkedBeforeExpiry {
		t.Fatal("fixture setup left no time to check availability before expiration")
	}
	deadline := expiresAt.Add(3 * time.Second)
	for {
		status, _, body = request(http.MethodGet, "/short-lived")
		if status == http.StatusNotFound {
			break
		}
		if status != http.StatusFound || time.Now().After(deadline) {
			t.Fatalf("short-lived link after expiry: got %d: %s", status, body)
		}
		time.Sleep(50 * time.Millisecond)
	}

	if output, err := deps.Exec("redis", "redis-cli", "EXISTS", cacheKey); err != nil || strings.TrimSpace(output) != "1" {
		t.Fatalf("expired snapshot should still be present in Redis: %v: %s", err, output)
	}

	// A warm entry must resolve without PostgreSQL, while an uncached code must
	// report a dependency failure rather than a false not-found response.
	status, _, body = request(http.MethodGet, "/CaseSensitive")
	if status != http.StatusFound {
		t.Fatalf("warm cache: want 302, got %d: %s", status, body)
	}
	sql("ALTER TABLE links RENAME TO links_unavailable")
	cachedStatus, cachedHeaders, cachedBody := request(http.MethodGet, "/CaseSensitive")
	missingStatus, _, missingBody := request(http.MethodGet, "/never-cached")
	sql("ALTER TABLE links_unavailable RENAME TO links")
	if cachedStatus != http.StatusFound || cachedHeaders.Get("Location") != destination {
		t.Errorf("cached redirect during database failure: got %d, %v, %s", cachedStatus, cachedHeaders, cachedBody)
	}
	if missingStatus < http.StatusInternalServerError || missingStatus >= 600 {
		t.Errorf("uncached database failure: want 5xx, got %d: %s", missingStatus, missingBody)
	}

	sql(`INSERT INTO links (id, code, destination, active) VALUES
		(999998, 'cache-lifecycle', 'https://old.example/path', true)`)
	management := func(method, payload string) {
		t.Helper()
		req, err := http.NewRequest(method, "http://"+address+"/api/v1/links/999998", strings.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", "integration-only-key")
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		result, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		want := http.StatusOK
		if method == http.MethodDelete {
			want = http.StatusNoContent
		}
		if resp.StatusCode != want {
			t.Fatalf("%s %s: want %d, got %d: %s", method, payload, want, resp.StatusCode, result)
		}
	}
	expectRedirect := func(wantStatus int, wantLocation string) {
		t.Helper()
		got, headers, result := request(http.MethodGet, "/cache-lifecycle")
		if got != wantStatus || headers.Get("Location") != wantLocation {
			t.Fatalf("cache-lifecycle: want %d %q, got %d %q: %s", wantStatus, wantLocation, got, headers.Get("Location"), result)
		}
	}
	expectRedirect(http.StatusFound, "https://old.example/path")
	management(http.MethodPatch, `{"destination":"https://new.example/path"}`)
	expectRedirect(http.StatusFound, "https://new.example/path")
	management(http.MethodPatch, `{"active":false}`)
	expectRedirect(http.StatusNotFound, "")
	management(http.MethodPatch, `{"active":true}`)
	expectRedirect(http.StatusFound, "https://new.example/path")

	expiration := time.Now().UTC().Add(3 * time.Second)
	management(http.MethodPatch, fmt.Sprintf(`{"expires_at":%q}`, expiration.Format(time.RFC3339Nano)))
	expectRedirect(http.StatusFound, "https://new.example/path")
	for time.Now().Before(expiration.Add(3 * time.Second)) {
		status, _, body = request(http.MethodGet, "/cache-lifecycle")
		if status == http.StatusNotFound {
			break
		}
		if status != http.StatusFound {
			t.Fatalf("expiring cached link: got %d: %s", status, body)
		}
		time.Sleep(50 * time.Millisecond)
	}
	if status != http.StatusNotFound {
		t.Fatalf("cached link remained available after expiration: %d: %s", status, body)
	}
	management(http.MethodPatch, `{"expires_at":null}`)
	expectRedirect(http.StatusFound, "https://new.example/path")
	// Pause Redis long enough for a write and a cache miss to encounter the
	// outage. The committed write must not depend on invalidation succeeding.
	sql(`INSERT INTO links (code, destination, active) VALUES ('outage-miss', 'https://fallback.example', true)`)
	pauseStarted := time.Now()
	if output, err := deps.Exec("redis", "redis-cli", "CLIENT", "PAUSE", "8000", "ALL"); err != nil {
		t.Fatalf("pause Redis: %v: %s", err, output)
	}
	management(http.MethodPatch, `{"destination":"https://after-outage.example"}`)
	status, headers, body := request(http.MethodGet, "/outage-miss")
	if status != http.StatusFound || headers.Get("Location") != "https://fallback.example" {
		t.Fatalf("Redis outage fallback: got %d %v: %s", status, headers, body)
	}
	if time.Since(pauseStarted) >= 8*time.Second {
		t.Fatal("operations did not complete while Redis was paused")
	}
	if output, err := deps.Exec("redis", "redis-cli", "PING"); err != nil || strings.TrimSpace(output) != "PONG" {
		t.Fatalf("Redis recovery: %v: %s", err, output)
	}
	// A new lookup after recovery must populate Redis and survive database loss.
	status, _, body = request(http.MethodGet, "/outage-miss")
	if status != http.StatusFound {
		t.Fatalf("lookup after Redis recovery: got %d: %s", status, body)
	}
	sql("ALTER TABLE links RENAME TO links_unavailable")
	status, headers, body = request(http.MethodGet, "/outage-miss")
	sql("ALTER TABLE links_unavailable RENAME TO links")
	if status != http.StatusFound || headers.Get("Location") != "https://fallback.example" {
		t.Fatalf("cache after Redis recovery: got %d %v: %s", status, headers, body)
	}
	management(http.MethodDelete, "")
	expectRedirect(http.StatusNotFound, "")

	// A failed invalidation leaves the old cached destination available only
	// until the original snapshot's freshness limit.
	sql(`INSERT INTO links (id, code, destination, active) VALUES (999996, 'failed-invalidation', 'https://before-failure.example', true)`)
	status, headers, body = request(http.MethodGet, "/failed-invalidation")
	if status != http.StatusFound || headers.Get("Location") != "https://before-failure.example" {
		t.Fatalf("warm invalidation fixture: got %d %v: %s", status, headers, body)
	}
	rejected := redisProxy.rejectDelete("failed-invalidation")
	failurePatch, err := http.NewRequest(http.MethodPatch, "http://"+address+"/api/v1/links/999996", strings.NewReader(`{"destination":"https://after-failure.example"}`))
	if err != nil {
		t.Fatal(err)
	}
	failurePatch.Header.Set("Content-Type", "application/json")
	failurePatch.Header.Set("X-API-Key", "integration-only-key")
	failureResponse, err := client.Do(failurePatch)
	if err != nil {
		t.Fatal(err)
	}
	failureResponse.Body.Close()
	if failureResponse.StatusCode != http.StatusOK {
		t.Fatalf("PATCH with failed invalidation: got %d", failureResponse.StatusCode)
	}
	failureCommittedBy := time.Now()
	select {
	case <-rejected:
	case <-time.After(3 * time.Second):
		t.Fatal("proxy did not reject invalidation")
	}
	status, headers, body = request(http.MethodGet, "/failed-invalidation")
	if status != http.StatusFound || headers.Get("Location") != "https://before-failure.example" {
		t.Fatalf("failed invalidation did not retain stale redirect: got %d %v: %s", status, headers, body)
	}

	// Pause a cache fill after its database read. A committed PATCH invalidates
	// the empty cache before the old snapshot is allowed to arrive in Redis.
	sql(`INSERT INTO links (id, code, destination, active) VALUES (999997, 'stale-fill', 'https://stale.example', true)`)
	held, release, delivered := redisProxy.hold("stale-fill")
	defer close(release)
	firstDone := make(chan error, 1)
	go func() {
		resp, err := client.Get("http://" + address + "/stale-fill")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode != http.StatusFound || resp.Header.Get("Location") != "https://stale.example" {
				err = fmt.Errorf("initial redirect: got %d %q", resp.StatusCode, resp.Header.Get("Location"))
			}
		}
		firstDone <- err
	}()
	select {
	case <-held:
	case <-time.After(5 * time.Second):
		t.Fatal("cache fill did not reach the proxy")
	}
	req, err := http.NewRequest(http.MethodPatch, "http://"+address+"/api/v1/links/999997", strings.NewReader(`{"destination":"https://current.example"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "integration-only-key")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("PATCH during stale fill: got %d", resp.StatusCode)
	}
	committedBy := time.Now()
	// Let the original request time out, so an incorrect implementation that
	// starts a new 60-second lifetime on fill would remain stale past the bound.
	select {
	case err := <-firstDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("initial redirect did not finish with Redis unavailable")
	}
	time.Sleep(2 * time.Second)
	release <- struct{}{}
	select {
	case <-delivered:
	case <-time.After(3 * time.Second):
		t.Fatal("delayed cache fill was not sent to Redis")
	}
	time.Sleep(100 * time.Millisecond)
	status, headers, body = request(http.MethodGet, "/stale-fill")
	if status != http.StatusFound || headers.Get("Location") != "https://stale.example" {
		t.Fatalf("delayed stale fill was not observable: got %d %v: %s", status, headers, body)
	}
	if wait := time.Until(failureCommittedBy.Add(60*time.Second + 200*time.Millisecond)); wait > 0 {
		time.Sleep(wait)
	}
	status, headers, body = request(http.MethodGet, "/failed-invalidation")
	if status != http.StatusFound || headers.Get("Location") != "https://after-failure.example" {
		t.Fatalf("failed invalidation exceeded the 60-second bound: got %d %v: %s", status, headers, body)
	}
	// This deadline is after the commit plus the allowed 60 seconds, but well
	// before a fresh 60-second TTL started by the delayed fill would expire.
	if wait := time.Until(committedBy.Add(60*time.Second + 200*time.Millisecond)); wait > 0 {
		time.Sleep(wait)
	}
	status, headers, body = request(http.MethodGet, "/stale-fill")
	if status != http.StatusFound || headers.Get("Location") != "https://current.example" {
		t.Fatalf("stale destination survived the 60-second bound: got %d %v: %s", status, headers, body)
	}
}
