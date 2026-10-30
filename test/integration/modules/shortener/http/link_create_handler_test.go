//go:build integration

package http_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener"
	shortenerport "github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/httpserver"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/infrastructure"
	"github.com/cristiano-pacheco/go-shortner/test/integration/fixture"
	"go.uber.org/fx"
)

type linkResponse struct {
	ID          int64      `json:"id"`
	Code        string     `json:"code"`
	Destination string     `json:"destination"`
	ShortURL    string     `json:"short_url"`
	Active      bool       `json:"active"`
	ExpiresAt   *time.Time `json:"expires_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func TestCreateLinkHTTP(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate repository")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../../../.."))
	binary := filepath.Join(t.TempDir(), "shortener")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
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
	listener.Close()
	env := append(os.Environ(), "APP_CONFIG_DIR="+filepath.Join(root, "config"), "APP_ENV=local", "DATABASE_HOST="+host, "DATABASE_PORT="+port, "DATABASE_NAME=shortener", "DATABASE_USER=shortener", "DATABASE_PASSWORD=integration-only", "DATABASE_SSLMODE=disable", "REDIS_URL=redis://"+deps.RedisAddress+"/0", "API_KEY=integration-only-key", "PUBLIC_BASE_URL=https://short.example/base", "HTTP_ADDR="+address)
	migrate := exec.Command(binary, "migrate")
	migrate.Env = env
	if out, err := migrate.CombinedOutput(); err != nil {
		t.Fatalf("migrate: %v\n%s", err, out)
	}
	fixture.StartApplication(t, binary, env, "http://"+address+"/health")
	client := &http.Client{Timeout: 5 * time.Second}
	post := func(body any, key string) (int, []byte) {
		t.Helper()
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest(http.MethodPost, "http://"+address+"/api/v1/links", bytes.NewReader(data))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		if key != "" {
			req.Header.Set("X-API-Key", key)
		}
		req.Host = "untrusted.example"
		resp, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		result, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatal(err)
		}
		return resp.StatusCode, result
	}
	key := "integration-only-key"
	for _, auth := range []string{"", "wrong"} {
		if status, _ := post(map[string]any{"destination": "https://example.com"}, auth); status != 401 {
			t.Errorf("authentication %q: got %d", auth, status)
		}
	}
	status, body := post(map[string]any{"destination": "https://example.com/path"}, key)
	if status != 201 {
		t.Fatalf("create default: %d %s", status, body)
	}
	var firstEnvelope struct {
		Data linkResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &firstEnvelope); err != nil {
		t.Fatal(err)
	}
	first := firstEnvelope.Data
	if first.ID <= 0 || !regexp.MustCompile(`^[A-Za-z0-9]{8}$`).MatchString(first.Code) || first.Destination != "https://example.com/path" || first.ShortURL != "https://short.example/base/"+first.Code || !first.Active || first.ExpiresAt != nil || first.CreatedAt.IsZero() || first.UpdatedAt.IsZero() {
		t.Errorf("invalid default representation: %+v", first)
	}
	expiry := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	status, body = post(map[string]any{"destination": "https://example.com/path", "alias": "Case_123", "expires_at": expiry}, key)
	if status != 201 {
		t.Fatalf("create alias: %d %s", status, body)
	}
	var secondEnvelope struct {
		Data linkResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &secondEnvelope); err != nil {
		t.Fatal(err)
	}
	second := secondEnvelope.Data
	if second.ID <= 0 || second.ID == first.ID || second.Code != "Case_123" || second.ExpiresAt == nil || !second.ExpiresAt.Equal(expiry) || second.ShortURL != "https://short.example/base/Case_123" {
		t.Errorf("invalid alias representation: %+v", second)
	}
	for _, tc := range []struct {
		name string
		body map[string]any
		want int
	}{
		{"case sensitive", map[string]any{"destination": "http://localhost", "alias": "case_123"}, 201},
		{"private IP", map[string]any{"destination": "http://192.168.1.10/a", "alias": "private"}, 201},
		{"alias minimum", map[string]any{"destination": "https://example.com", "alias": "ab-"}, 201},
		{"alias maximum", map[string]any{"destination": "https://example.com", "alias": strings.Repeat("z", 32)}, 201},
		{"URL maximum", map[string]any{"destination": "https://example.com/" + strings.Repeat("a", 4096-len("https://example.com/")), "alias": "long-url"}, 201},
		{"URL too long", map[string]any{"destination": "https://example.com/" + strings.Repeat("a", 4097-len("https://example.com/"))}, 400},
		{"relative URL", map[string]any{"destination": "/relative"}, 400},
		{"scheme", map[string]any{"destination": "ftp://example.com"}, 400},
		{"missing hostname", map[string]any{"destination": "https:///path"}, 400},
		{"credentials", map[string]any{"destination": "https://user:pass@example.com"}, 400},
		{"missing destination", map[string]any{}, 400},
		{"short alias", map[string]any{"destination": "https://example.com", "alias": "ab"}, 400},
		{"long alias", map[string]any{"destination": "https://example.com", "alias": strings.Repeat("a", 33)}, 400},
		{"non-ASCII alias", map[string]any{"destination": "https://example.com", "alias": "cafés"}, 400},
		{"invalid alias", map[string]any{"destination": "https://example.com", "alias": "abc.def"}, 400},
		{"reserved api", map[string]any{"destination": "https://example.com", "alias": "api"}, 400},
		{"reserved health", map[string]any{"destination": "https://example.com", "alias": "health"}, 400},
		{"expired", map[string]any{"destination": "https://example.com", "expires_at": time.Now().Add(-time.Minute)}, 400},
		{"duplicate", map[string]any{"destination": "https://elsewhere.example", "alias": "Case_123"}, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, b := post(tc.body, key)
			if got != tc.want {
				t.Errorf("got %d want %d: %s", got, tc.want, b)
			}
		})
	}
	_, err = deps.Exec("postgres", "psql", "-v", "ON_ERROR_STOP=1", "-U", "shortener", "-d", "shortener", "-c", "INSERT INTO links (code, destination, deleted_at) VALUES ('deleted-alias', 'https://example.com', NOW())")
	if err != nil {
		t.Fatal(err)
	}
	if status, body := post(map[string]any{"destination": "https://example.com", "alias": "deleted-alias"}, key); status != 409 {
		t.Errorf("deleted alias: %d %s", status, body)
	}

	// Run the production Fx graph with only the randomness source replaced.
	// Both HTTP and the PostgreSQL uniqueness constraint remain real.
	listener2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address2 := listener2.Addr().String()
	listener2.Close()
	_, err = deps.Exec("postgres", "psql", "-v", "ON_ERROR_STOP=1", "-U", "shortener", "-d", "shortener", "-c", "INSERT INTO links (code, destination) VALUES ('COLLIDE1', 'https://example.com')")
	if err != nil {
		t.Fatal(err)
	}
	generator := &controlledGenerator{}
	cfg := config.Config{DatabaseHost: host, DatabasePort: port, DatabaseName: "shortener", DatabaseUser: "shortener", DatabasePassword: "integration-only", DatabaseSSLMode: "disable", RedisURL: "redis://" + deps.RedisAddress + "/0", APIKey: key, PublicBaseURL: "https://short.example/base", HTTPAddr: address2}
	app := fx.New(fx.NopLogger, fx.Supply(cfg), fx.Provide(infrastructure.NewResources), shortener.Module, httpserver.Module,
		fx.Decorate(func(shortenerport.CodeGenerator) shortenerport.CodeGenerator { return generator }))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := app.Start(ctx); err != nil {
		t.Fatalf("start collision application: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := app.Stop(ctx); err != nil {
			t.Errorf("stop collision application: %v", err)
		}
	})
	collisionPost := func() (int, []byte) {
		req, err := http.NewRequest(http.MethodPost, "http://"+address2+"/api/v1/links", strings.NewReader(`{"destination":"https://example.com"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-API-Key", key)
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		b, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, b
	}
	status, body = collisionPost()
	if status != 201 {
		t.Fatalf("retry after collision: %d %s", status, body)
	}
	var retriedEnvelope struct {
		Data linkResponse `json:"data"`
	}
	if err := json.Unmarshal(body, &retriedEnvelope); err != nil {
		t.Fatal(err)
	}
	retried := retriedEnvelope.Data
	if retried.Code != "FRESH001" {
		t.Errorf("collision retry returned code %q", retried.Code)
	}
	generator.mu.Lock()
	generator.exhaust = true
	generator.mu.Unlock()
	status, body = collisionPost()
	if status != 500 {
		t.Errorf("exhausted collisions: %d %s", status, body)
	}

	const n = 12
	results := make(chan int, n)
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			status, _ := post(map[string]any{"destination": "https://example.com", "alias": "race-alias"}, key)
			results <- status
		}()
	}
	wg.Wait()
	close(results)
	counts := map[int]int{}
	for status := range results {
		counts[status]++
	}
	if counts[201] != 1 || counts[409] != n-1 {
		t.Error(fmt.Sprintf("concurrent alias results: %v", counts))
	}
}

// controlledGenerator returns a reserved code first, then either a fresh code or
// the reserved code indefinitely. PostgreSQL remains responsible for collisions.
type controlledGenerator struct {
	mu      sync.Mutex
	calls   int
	exhaust bool
}

func (g *controlledGenerator) Generate() (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	if g.exhaust || g.calls == 1 {
		return "COLLIDE1", nil
	}
	return "FRESH001", nil
}
