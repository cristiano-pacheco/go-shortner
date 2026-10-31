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
		"DATABASE_PASSWORD=integration-only", "DATABASE_SSLMODE=disable", "REDIS_URL=redis://"+deps.RedisAddress+"/0",
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

	expiresAt := time.Now().UTC().Add(3 * time.Second)
	sql(fmt.Sprintf(`INSERT INTO links (code, destination, active, expires_at) VALUES ('short-lived', 'https://example.com/short', true, '%s')`, expiresAt.Format(time.RFC3339Nano)))
	var status int
	var body string
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

	sql("ALTER TABLE links RENAME TO links_unavailable")
	status, _, body = request(http.MethodGet, "/CaseSensitive")
	sql("ALTER TABLE links_unavailable RENAME TO links")
	if status < http.StatusInternalServerError || status >= 600 {
		t.Errorf("database failure: want 5xx, got %d: %s", status, body)
	}
}
