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

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/suite"
)

const apiKey = "e2e-test-key"
const publicBase = "https://short.example"

// Management suites run sequentially and reset PostgreSQL and Redis before each test.
//
//nolint:gochecknoglobals // TestMain owns the shared stack and its cleanup.
var managementStack httpSuite

//nolint:gochecknoglobals // TestMain cleans up even when startup fails.
var managementCleanups []func() error

//nolint:gochecknoglobals // All stacks share the build cache for one test run.
var e2eBuildID = func() string {
	if id := os.Getenv("SHORTNER_E2E_BUILD_ID"); id != "" {
		return id
	}

	return fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
}()

//nolint:gochecknoglobals // TestMain only cleans up images after a build was attempted.
var e2eBuildAttempted bool

func TestMain(m *testing.M) {
	code := m.Run()

	for _, cleanup := range managementCleanups {
		if err := cleanup(); err != nil {
			fmt.Fprintln(os.Stderr, err)

			code = 1
		}
	}

	if e2eBuildAttempted {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		out, err := exec.CommandContext(ctx, "docker", "image", "prune", "-f", "--filter", "label=io.go-shortner.e2e-build="+e2eBuildID).CombinedOutput()
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "remove E2E build images: %v\n%s", err, out)
			code = 1
		}
	}

	os.Exit(code)
}

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
	base        string
	metricsBase string
	client      *http.Client
	run         func(...string) ([]byte, error)
}

func start(t *testing.T) httpSuite {
	t.Helper()
	return startStack(t, false)
}

func startStack(t *testing.T, shared bool) httpSuite {
	t.Helper()

	return startConfiguredStack(t, shared, "")
}

func startConfiguredStack(t *testing.T, shared bool, composeOverride string) httpSuite {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate repository")
	}

	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../../.."))
	project := fmt.Sprintf("shortner-e2e-%d", time.Now().UnixNano())
	if dir := os.Getenv("SHORTNER_E2E_CLEANUP_DIR"); dir != "" {
		if err := os.WriteFile(filepath.Join(dir, "project-"+project), nil, 0o600); err != nil {
			t.Fatalf("register E2E cleanup: %v", err)
		}
	}

	env := append(
		os.Environ(),
		"POSTGRES_DB=shortener",
		"POSTGRES_USER=shortener",
		"POSTGRES_PASSWORD=e2e-only",
		"API_KEY="+apiKey,
		"PUBLIC_BASE_URL="+publicBase,
		"APP_PORT=0",
		"METRICS_PORT=0",
		"POSTGRES_PORT=0",
		"REDIS_PORT=0",
	)

	args := []string{
		"compose",
		"-p",
		project,
		"-f",
		filepath.Join(root, "docker/docker-compose.yaml"),
		"-f",
		filepath.Join(root, "docker/docker-compose.app.yaml"),
	}
	if composeOverride != "" {
		args = append(args, "-f", composeOverride)
	}

	run := func(extra ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		command := "docker"
		commandArgs := append(append([]string{}, args...), extra...)
		if dir := os.Getenv("SHORTNER_E2E_CLEANUP_DIR"); dir != "" {
			command = "bash"
			commandArgs = append([]string{filepath.Join(root, "scripts/e2e-compose.sh"), dir, project}, commandArgs...)
		}
		cmd := exec.CommandContext(ctx, command, commandArgs...)
		cmd.Dir = root
		cmd.Env = env

		return cmd.CombinedOutput()
	}

	cleanup := func() error {
		out, err := run("down", "--volumes", "--remove-orphans", "--rmi", "local")
		if err != nil {
			return fmt.Errorf("compose cleanup: %w\n%s", err, out)
		}

		return nil
	}
	if shared {
		managementCleanups = append(managementCleanups, cleanup)
	} else {
		t.Cleanup(func() {
			if err := cleanup(); err != nil {
				t.Error(err)
			}
		})
	}

	for _, step := range [][]string{{"up", "-d", "--wait", "postgres", "redis"}, {"build", "--build-arg", "E2E_BUILD_ID=" + e2eBuildID, "app"}, {"run", "--rm", "--no-deps", "app", "migrate"}, {"up", "-d", "--no-deps", "app"}} {
		if step[0] == "build" {
			e2eBuildAttempted = true
		}

		if out, err := run(step...); err != nil {
			t.Fatalf("compose %v: %v\n%s", step, err, out)
		}
	}

	s := httpSuite{
		client: &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		run: run,
	}
	s.base = s.composeAddress(t, "8080")
	s.metricsBase = s.composeAddress(t, "9090")

	s.waitReady(t)

	return s
}

func (s httpSuite) waitReady(t *testing.T) {
	t.Helper()

	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := s.client.Get(s.base + "/health")
		if err == nil {
			resp.Body.Close()

			if resp.StatusCode == http.StatusOK {
				return
			}
		}

		time.Sleep(200 * time.Millisecond)
	}

	logs, _ := s.run("logs", "app")
	t.Fatalf("application did not become healthy:\n%s", logs)
}

func (s httpSuite) composeAddress(t *testing.T, port string) string {
	t.Helper()

	out, err := s.run("port", "app", port)
	if err != nil {
		t.Fatalf("compose port %s: %v\n%s", port, err, out)
	}

	return "http://" + strings.TrimSpace(string(out))
}

func (s httpSuite) request(t *testing.T, method, path, body, key, contentType string) (int, http.Header, []byte) {
	t.Helper()

	req, err := http.NewRequest(method, s.base+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}

	if key != "" {
		req.Header.Set("X-Api-Key", key)
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

	http         httpSuite
	postgresPool *pgxpool.Pool
	redisClient  *redis.Client
}

func (s *endpointSuite) SetupSuite() {
	if s.http.base == "" {
		s.startStack()
	}

	t := s.T()
	t.Cleanup(func() {
		if s.postgresPool != nil {
			s.postgresPool.Close()
		}

		if s.redisClient != nil {
			if err := s.redisClient.Close(); err != nil {
				t.Errorf("close Redis fixture client: %v", err)
			}
		}
	})
	s.connectFixtures()
}

func (s *endpointSuite) startStack() {
	switch s.T().Name() {
	case "TestCreateLinkSuite", "TestDeleteLinkSuite", "TestGetLinkSuite", "TestListLinkSuite", "TestUpdateLinkSuite":
		if managementStack.base == "" {
			managementStack = startStack(s.T(), true)
		}

		s.http = managementStack
	default:
		s.http = start(s.T())
	}
}

func (s *endpointSuite) connectFixtures() {
	out, err := s.http.run("port", "postgres", "5432")
	s.Require().NoError(err, "%s", out)
	postgresAddress := strings.TrimSpace(string(out))
	out, err = s.http.run("port", "redis", "6379")
	s.Require().NoError(err, "%s", out)
	redisAddress := strings.TrimSpace(string(out))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if s.postgresPool != nil {
		s.postgresPool.Close()
	}

	if s.redisClient != nil {
		s.Require().NoError(s.redisClient.Close())
		s.redisClient = nil
	}

	s.postgresPool, err = pgxpool.New(
		ctx,
		"postgres://shortener:e2e-only@"+postgresAddress+"/shortener?sslmode=disable",
	)
	s.Require().NoError(err)
	s.Require().NoError(s.postgresPool.Ping(ctx))
	s.redisClient = redis.NewClient(&redis.Options{Addr: redisAddress})
	s.Require().NoError(s.redisClient.Ping(ctx).Err())
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := s.postgresPool.Exec(ctx, query)
	s.Require().NoError(err, "fixture SQL")
}

func (s *endpointSuite) SetupTest() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := s.postgresPool.Exec(ctx, "TRUNCATE TABLE links RESTART IDENTITY")
	if err != nil {
		s.connectFixtures()
		_, err = s.postgresPool.Exec(ctx, "TRUNCATE TABLE links RESTART IDENTITY")
	}

	s.Require().NoError(err)

	err = s.redisClient.FlushDB(ctx).Err()
	if err != nil {
		s.connectFixtures()
		err = s.redisClient.FlushDB(ctx).Err()
	}

	s.Require().NoError(err)
}
