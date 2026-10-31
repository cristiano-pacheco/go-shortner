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

// TestUpdateLinkAPI exercises the running application with real dependencies.
func TestUpdateLinkAPI(t *testing.T) {
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
		"API_KEY=integration-only-key", "PUBLIC_BASE_URL=https://short.example/base", "HTTP_ADDR="+address)
	migrate := exec.Command(binary, "migrate")

	migrate.Env = env
	if output, err := migrate.CombinedOutput(); err != nil {
		t.Fatalf("migrate: %v\n%s", err, output)
	}

	sql := func(query string) string {
		t.Helper()

		result, err := deps.Exec(
			"postgres",
			"psql",
			"-X",
			"-A",
			"-t",
			"-v",
			"ON_ERROR_STOP=1",
			"-U",
			"shortener",
			"-d",
			"shortener",
			"-c",
			query,
		)
		if err != nil {
			t.Fatal(err)
		}

		return result
	}
	sql(`INSERT INTO links (id, code, destination, active, expires_at, created_at, updated_at, deleted_at) VALUES
 (101, 'update-code', 'https://old.example/path', true, '2099-01-01T00:00:00Z', now(), now(), NULL),
 (102, 'deleted-code', 'https://deleted.example', true, NULL, now(), now(), now()),
 (103, 'expired-code', 'https://expired.example', false, '2020-01-01T00:00:00Z', now(), now(), NULL)`)
	fixture.StartApplication(t, binary, env, "http://"+address+"/health")

	client := &http.Client{Timeout: 5 * time.Second}
	patch := func(id, body, key string) (int, []byte) {
		t.Helper()

		request, err := http.NewRequest(
			http.MethodPatch,
			"http://"+address+"/api/v1/links/"+id,
			strings.NewReader(body),
		)
		if err != nil {
			t.Fatal(err)
		}

		request.Header.Set("Content-Type", "application/json")

		request.Host = "untrusted.example"
		if key != "" {
			request.Header.Set("X-Api-Key", key)
		}

		response, err := client.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()

		result, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}

		return response.StatusCode, result
	}
	snapshot := func() string {
		t.Helper()
		return sql("SELECT row_to_json(links) FROM links WHERE id = 101")
	}
	key := "integration-only-key"
	initial := snapshot()

	for _, invalidKey := range []string{"", "wrong"} {
		status, body := patch("101", `{"active":false}`, invalidKey)
		if status != http.StatusUnauthorized {
			t.Errorf("key %q: want 401, got %d: %s", invalidKey, status, body)
		}
	}

	if got := snapshot(); got != initial {
		t.Fatalf("unauthorized request changed row: %s", got)
	}

	expectUpdate := func(body string, destination string, active bool, expiration *time.Time) {
		t.Helper()

		status, raw := patch("101", body, key)
		if status != http.StatusOK {
			t.Fatalf("PATCH %s: want 200, got %d: %s", body, status, raw)
		}

		var response struct {
			Data listLink `json:"data"`
		}
		if err := json.Unmarshal(raw, &response); err != nil {
			t.Fatalf("decode %s: %v", raw, err)
		}

		link := response.Data
		if link.ID != 101 || link.Code != "update-code" || link.ShortURL != "https://short.example/base/update-code" ||
			link.Destination != destination ||
			link.Active != active ||
			link.CreatedAt.IsZero() ||
			link.UpdatedAt.IsZero() {
			t.Fatalf("unexpected representation: %s", raw)
		}

		if (link.ExpiresAt == nil) != (expiration == nil) || (expiration != nil && !link.ExpiresAt.Equal(*expiration)) {
			t.Fatalf("unexpected expiration: %s", raw)
		}

		var persisted struct {
			ID          int64      `json:"id"`
			Code        string     `json:"code"`
			Destination string     `json:"destination"`
			Active      bool       `json:"active"`
			ExpiresAt   *time.Time `json:"expires_at"`
		}
		if err := json.Unmarshal([]byte(snapshot()), &persisted); err != nil {
			t.Fatal(err)
		}

		if persisted.ID != link.ID || persisted.Code != link.Code || persisted.Destination != link.Destination ||
			persisted.Active != link.Active ||
			(persisted.ExpiresAt == nil) != (link.ExpiresAt == nil) ||
			(link.ExpiresAt != nil && !persisted.ExpiresAt.Equal(*link.ExpiresAt)) {
			t.Fatalf("response differs from persisted row: %+v vs %+v", link, persisted)
		}
	}
	originalExpiry := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)
	expectUpdate(
		`{"destination":"https://new.example/path","active":false}`,
		"https://new.example/path",
		false,
		&originalExpiry,
	)
	expectUpdate(`{"active":true}`, "https://new.example/path", true, &originalExpiry)
	expectUpdate(`{"expires_at":null}`, "https://new.example/path", true, nil)

	newExpiry := time.Date(2098, 1, 1, 0, 0, 0, 0, time.UTC)
	expectUpdate(`{"expires_at":"2098-01-01T00:00:00Z","active":false}`, "https://new.example/path", false, &newExpiry)
	expectUpdate(`{"active":true}`, "https://new.example/path", true, &newExpiry)

	status, raw := patch("103", `{"active":true}`, key)
	if status != http.StatusOK {
		t.Fatalf("reactivate expired link: want 200, got %d: %s", status, raw)
	}

	var expiredResponse struct {
		Data listLink `json:"data"`
	}
	if err := json.Unmarshal(raw, &expiredResponse); err != nil {
		t.Fatal(err)
	}

	expiredAt := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	if !expiredResponse.Data.Active || expiredResponse.Data.ExpiresAt == nil ||
		!expiredResponse.Data.ExpiresAt.Equal(expiredAt) {
		t.Fatalf("reactivation changed expiration: %s", raw)
	}

	var expiredRow struct {
		Active    bool      `json:"active"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(
		[]byte(sql("SELECT row_to_json(links) FROM links WHERE id = 103")),
		&expiredRow,
	); err != nil {
		t.Fatal(err)
	}

	if !expiredRow.Active || !expiredRow.ExpiresAt.Equal(expiredAt) {
		t.Fatalf("persisted reactivation changed expiration: %+v", expiredRow)
	}

	before := snapshot()

	for _, body := range []string{
		`{"code":"other-code"}`, `{"destination":"ftp://example.com","active":false}`,
		`{"destination":"https://user:pass@example.com","active":false}`, `{"destination":"https://","active":false}`,
		fmt.Sprintf(`{"destination":"https://example.com/%s","active":false}`, strings.Repeat("a", 4096)),
		`{"expires_at":"2020-01-01T00:00:00Z","active":false}`, `{"expires_at":"not-a-date"}`,
		`{"active":"false"}`, `{"active":null}`, `{"destination":null}`, `{"destination":123}`,
		`{"active":false`,
	} {
		status, raw := patch("101", body, key)
		if status != http.StatusBadRequest {
			t.Errorf("invalid PATCH %s: want 400, got %d: %s", body, status, raw)
		}

		if got := snapshot(); got != before {
			t.Fatalf("rejected PATCH %s changed persisted state: before %s after %s", body, before, got)
		}
	}

	for _, id := range []string{"abc", "0", "-1", "9223372036854775808"} {
		status, raw := patch(id, `{"active":false}`, key)
		if status != http.StatusBadRequest {
			t.Errorf("ID %s: want 400, got %d: %s", id, status, raw)
		}
	}

	for _, id := range []string{"102", "999999"} {
		status, raw := patch(id, `{"active":false}`, key)
		if status != http.StatusNotFound {
			t.Errorf("ID %s: want 404, got %d: %s", id, status, raw)
		}
	}
}
