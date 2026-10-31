//go:build integration

package api_test

import (
	"encoding/json"
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

type listLink struct {
	ID          int64      `json:"id"`
	Code        string     `json:"code"`
	Destination string     `json:"destination"`
	ShortURL    string     `json:"short_url"`
	Active      bool       `json:"active"`
	ExpiresAt   *time.Time `json:"expires_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type listResponse struct {
	Data struct {
		Items    []listLink `json:"items"`
		Page     int        `json:"page"`
		PageSize int        `json:"page_size"`
		Total    int        `json:"total"`
	} `json:"data"`
}

// TestListLinksAPI starts the production command and exercises its HTTP API
// against PostgreSQL and Redis. Fixtures do not depend on link creation.
func TestListLinksAPI(t *testing.T) {
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
	env := append(os.Environ(),
		"APP_CONFIG_DIR="+filepath.Join(root, "config"), "APP_ENV=local",
		"DATABASE_HOST="+host, "DATABASE_PORT="+port, "DATABASE_NAME=shortener",
		"DATABASE_USER=shortener", "DATABASE_PASSWORD=integration-only", "DATABASE_SSLMODE=disable",
		"REDIS_URL=redis://"+deps.RedisAddress+"/0", "API_KEY=integration-only-key",
		"PUBLIC_BASE_URL=https://short.example/base", "HTTP_ADDR="+address,
	)
	migrate := exec.Command(binary, "migrate")
	migrate.Env = env
	if output, err := migrate.CombinedOutput(); err != nil {
		t.Fatalf("migrate: %v\n%s", err, output)
	}
	fixture.StartApplication(t, binary, env, "http://"+address+"/health")
	client := &http.Client{Timeout: 5 * time.Second}
	get := func(query, key string) (int, []byte) {
		t.Helper()
		request, err := http.NewRequest(http.MethodGet, "http://"+address+"/api/v1/links", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.URL.RawQuery = strings.TrimPrefix(query, "?")
		request.Host = "untrusted.example"
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
		status, body := get("", invalidKey)
		if status != http.StatusUnauthorized {
			t.Errorf("key %q: status %d, body %s", invalidKey, status, body)
		}
	}
	decode := func(query string, page, pageSize, total, count int) listResponse {
		t.Helper()
		status, body := get(query, key)
		if status != http.StatusOK {
			t.Fatalf("GET %q: status %d, body %s", query, status, body)
		}
		var response listResponse
		if err := json.Unmarshal(body, &response); err != nil {
			t.Fatalf("decode %s: %v", body, err)
		}
		if response.Data.Items == nil || response.Data.Page != page || response.Data.PageSize != pageSize || response.Data.Total != total || len(response.Data.Items) != count {
			t.Fatalf("GET %q: unexpected pagination: %s", query, body)
		}
		return response
	}
	decode("", 1, 20, 0, 0)

	// Adjacent pairs share creation timestamps, so their order depends on id.
	seed := `INSERT INTO links (code, destination, active, expires_at, created_at, updated_at, deleted_at)
		SELECT 'link-' || n, 'https://destination.example/' || n, n <> 24,
		CASE WHEN n = 23 THEN '2020-01-01 00:00:00+00'::timestamptz ELSE NULL END,
		'2021-01-01 00:00:00+00'::timestamptz + ((n - 1) / 2) * interval '1 second',
		'2021-02-01 00:00:00+00'::timestamptz,
		CASE WHEN n = 25 THEN '2021-03-01 00:00:00+00'::timestamptz ELSE NULL END
		FROM generate_series(1, 25) AS n ORDER BY n`
	if _, err := deps.Exec("postgres", "psql", "-v", "ON_ERROR_STOP=1", "-U", "shortener", "-d", "shortener", "-c", seed); err != nil {
		t.Fatal(err)
	}
	first := decode("", 1, 20, 24, 20)
	second := decode("?page=2", 2, 20, 24, 4)
	all := append(first.Data.Items, second.Data.Items...)
	for index, item := range all {
		n := 24 - index
		if item.ID <= 0 || item.Code != fmt.Sprintf("link-%d", n) || item.Destination != fmt.Sprintf("https://destination.example/%d", n) || item.ShortURL != fmt.Sprintf("https://short.example/base/link-%d", n) || item.Active != (n != 24) || (item.ExpiresAt != nil) != (n == 23) || item.CreatedAt.IsZero() || item.UpdatedAt.IsZero() {
			t.Errorf("item %d: %+v", index, item)
		}
		if index > 0 && all[index-1].CreatedAt.Equal(item.CreatedAt) && all[index-1].ID <= item.ID {
			t.Errorf("tied timestamps not ordered by descending ID: %+v, %+v", all[index-1], item)
		}
	}
	decode("?page=100&page_size=1", 100, 1, 24, 0)
	decode("?page=1&page_size=1", 1, 1, 24, 1)
	decode("?page=1&page_size=100", 1, 100, 24, 24)
	for _, query := range []string{
		"?page=0", "?page=-1", "?page=abc", "?page=9223372036854775808", "?page=%ZZ", "?page=2;page_size=10",
		"?page=9223372036854775807&page_size=100", "?page_size=0", "?page_size=-1",
		"?page_size=101", "?page_size=abc", "?page_size=9223372036854775808",
	} {
		status, body := get(query, key)
		if status != http.StatusBadRequest {
			t.Errorf("GET %q: want 400, got %d: %s", query, status, body)
		}
	}
	// Force a PostgreSQL query error without stopping the production process.
	if _, err := deps.Exec("postgres", "psql", "-v", "ON_ERROR_STOP=1", "-U", "shortener", "-d", "shortener", "-c", "ALTER TABLE links RENAME TO links_unavailable"); err != nil {
		t.Fatal(err)
	}
	status, body := get("", key)
	if status != http.StatusInternalServerError || strings.Contains(string(body), "links_unavailable") {
		t.Errorf("database failure: want sanitized 500, got %d: %s", status, body)
	}
}
