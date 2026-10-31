//go:build e2e

package shortner_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

const apiKey = "e2e-test-key"
const publicBase = "https://short.example"

type link struct {
	ID          int64      `json:"id"`
	Code        string     `json:"code"`
	Destination string     `json:"destination"`
	ShortURL    string     `json:"short_url"`
	Active      bool       `json:"active"`
	ExpiresAt   *time.Time `json:"expires_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

type httpSuite struct {
	base   string
	client *http.Client
	run    func(...string) ([]byte, error)
}

func start(t *testing.T) httpSuite {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate repository")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../../.."))
	project := fmt.Sprintf("shortner-e2e-%d", time.Now().UnixNano())
	env := append(os.Environ(), "POSTGRES_DB=shortener", "POSTGRES_USER=shortener", "POSTGRES_PASSWORD=e2e-only", "API_KEY="+apiKey, "PUBLIC_BASE_URL="+publicBase, "APP_PORT=0", "POSTGRES_PORT=0", "REDIS_PORT=0")
	args := []string{"compose", "-p", project, "-f", filepath.Join(root, "docker/docker-compose.yaml"), "-f", filepath.Join(root, "docker/docker-compose.app.yaml")}
	run := func(extra ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, "docker", append(append([]string{}, args...), extra...)...)
		cmd.Dir = root
		cmd.Env = env
		return cmd.CombinedOutput()
	}
	t.Cleanup(func() {
		if out, err := run("down", "--volumes", "--remove-orphans"); err != nil {
			t.Errorf("compose cleanup: %v\n%s", err, out)
		}
	})
	for _, step := range [][]string{{"up", "-d", "--wait", "postgres", "redis"}, {"build", "app"}, {"run", "--rm", "--no-deps", "app", "migrate"}, {"up", "-d", "--no-deps", "app"}} {
		if out, err := run(step...); err != nil {
			t.Fatalf("compose %v: %v\n%s", step, err, out)
		}
	}
	out, err := run("port", "app", "8080")
	if err != nil {
		t.Fatalf("compose port: %v\n%s", err, out)
	}
	base := "http://" + strings.TrimSpace(string(out))
	s := httpSuite{base, &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}, run}
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := s.client.Get(base + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return s
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	logs, _ := run("logs", "app")
	t.Fatalf("application did not become healthy:\n%s", logs)
	return httpSuite{}
}

func (s httpSuite) request(t *testing.T, method, path, body, key, contentType string) (int, http.Header, []byte) {
	t.Helper()
	req, err := http.NewRequest(method, s.base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, resp.Header, b
}
func (s httpSuite) expect(t *testing.T, method, path, body, key, contentType string, want int) (http.Header, []byte) {
	t.Helper()
	got, h, b := s.request(t, method, path, body, key, contentType)
	if got != want {
		t.Fatalf("%s %s: got %d, want %d: %s", method, path, got, want, b)
	}
	return h, b
}
func decodeLink(t *testing.T, b []byte) link {
	t.Helper()
	var envelope struct {
		Data link `json:"data"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		t.Fatal(err)
	}
	return envelope.Data
}

type endpointSuite struct {
	suite.Suite
	http httpSuite
}

func (s *endpointSuite) SetupSuite() {
	s.http = start(s.T())
}

func (s *endpointSuite) create(alias string) link {
	s.T().Helper()
	body := fmt.Sprintf(`{"destination":"https://example.com/path","alias":%q}`, alias)
	_, raw := s.http.expect(s.T(), "POST", "/api/v1/links", body, apiKey, "application/json", 201)
	item := decodeLink(s.T(), raw)
	s.Require().Equal("https://example.com/path", item.Destination)
	return item
}

// sql executes fixtures only inside the isolated Compose project's PostgreSQL.
func (s *endpointSuite) sql(query string) {
	s.T().Helper()
	out, err := s.http.run("exec", "-T", "postgres", "psql", "-X", "-v", "ON_ERROR_STOP=1", "-U", "shortener", "-d", "shortener", "-c", query)
	s.Require().NoError(err, "fixture SQL: %s", out)
}

func (s *endpointSuite) SetupTest() {
	s.sql("TRUNCATE TABLE links RESTART IDENTITY")
	out, err := s.http.run("exec", "-T", "redis", "redis-cli", "FLUSHDB")
	s.Require().NoError(err, "clear Redis: %s", out)
	s.Require().Equal("OK", strings.TrimSpace(string(out)))
}
