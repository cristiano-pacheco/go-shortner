//go:build integration

package api_test

import (
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

func TestDeleteLinkAPI(t *testing.T) {
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

	sql := func(query string) string {
		t.Helper()
		result, err := deps.Exec("postgres", "psql", "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1",
			"-U", "shortener", "-d", "shortener", "-c", query)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	sql(`INSERT INTO links (id, code, destination, active, created_at, updated_at, deleted_at) VALUES
		(101, 'reserved-code', 'https://example.com', true, now(), now(), NULL),
		(102, 'already-deleted', 'https://example.com', true, now(), now(), now()),
		(103, 'database-failure', 'https://example.com', true, now(), now(), NULL)`)
	fixture.StartApplication(t, binary, env, "http://"+address+"/health")

	client := &http.Client{Timeout: 5 * time.Second}
	deleteLink := func(id, key string) (int, []byte) {
		t.Helper()
		request, err := http.NewRequest(http.MethodDelete, "http://"+address+"/api/v1/links/"+id, nil)
		if err != nil {
			t.Fatal(err)
		}
		if key != "" {
			request.Header.Set("X-API-Key", key)
		}
		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, body
	}
	key := "integration-only-key"
	for _, invalidKey := range []string{"", "wrong"} {
		status, body := deleteLink("101", invalidKey)
		if status != http.StatusUnauthorized {
			t.Errorf("key %q: want 401, got %d: %s", invalidKey, status, body)
		}
	}
	if got := sql("SELECT deleted_at IS NULL FROM links WHERE id = 101"); got != "t" {
		t.Fatalf("unauthorized deletion changed row: %q", got)
	}

	status, body := deleteLink("101", key)
	if status != http.StatusNoContent || len(body) != 0 {
		t.Fatalf("DELETE: want empty 204, got %d: %s", status, body)
	}
	if got := sql("SELECT count(*) FROM links WHERE id = 101 AND code = 'reserved-code' AND deleted_at IS NOT NULL"); got != "1" {
		t.Fatalf("deleted row not preserved: %q", got)
	}
	output, err := deps.Exec("postgres", "psql", "-X", "-v", "ON_ERROR_STOP=1", "-U", "shortener", "-d", "shortener", "-c",
		"INSERT INTO links (code, destination, active, created_at, updated_at) VALUES ('reserved-code', 'https://other.example', true, now(), now())")
	if err == nil || !strings.Contains(err.Error(), "duplicate key") {
		t.Fatalf("deleted code insertion: want unique constraint error, got %v: %s", err, output)
	}

	for _, id := range []string{"101", "102", "999999"} {
		status, body := deleteLink(id, key)
		if status != http.StatusNotFound {
			t.Errorf("ID %s: want 404, got %d: %s", id, status, body)
		}
	}
	for _, id := range []string{"abc", "0", "-1", "9223372036854775808"} {
		status, body := deleteLink(id, key)
		if status != http.StatusBadRequest {
			t.Errorf("ID %s: want 400, got %d: %s", id, status, body)
		}
	}

	// Rename the table to force a real PostgreSQL error without stopping Redis.
	sql("ALTER TABLE links RENAME TO links_unavailable")
	status, body = deleteLink("103", key)
	sql("ALTER TABLE links_unavailable RENAME TO links")
	if status < http.StatusInternalServerError || status >= 600 {
		t.Errorf("database failure: want 5xx, got %d: %s", status, body)
	}
	if got := sql("SELECT deleted_at IS NULL FROM links WHERE id = 103"); got != "t" {
		t.Fatalf("failed deletion changed row: %q", got)
	}
}
