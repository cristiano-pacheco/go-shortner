//go:build integration

package bootstrap_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/cristiano-pacheco/go-shortner/test/integration/fixture"
)

func TestServerCommand(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate repository")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
	binary := filepath.Join(t.TempDir(), "shortener")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build application: %v\n%s", err, output)
	}
	deps := fixture.StartDependencies(t)
	pgHost, pgPort, err := net.SplitHostPort(deps.PostgresAddress)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	httpAddress := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	baseEnv := append(os.Environ(),
		"APP_CONFIG_DIR="+filepath.Join(root, "config"),
		"DATABASE_HOST="+pgHost,
		"DATABASE_PORT="+pgPort,
		"DATABASE_NAME=shortener",
		"DATABASE_USER=shortener",
		"DATABASE_PASSWORD=integration-only",
		"DATABASE_SSLMODE=disable",
		"REDIS_URL=redis://"+deps.RedisAddress+"/0",
		"API_KEY=integration-only-key",
		"PUBLIC_BASE_URL=http://127.0.0.1:8080",
		"HTTP_ADDR="+httpAddress,
	)

	t.Run("serves and closes dependency connections on SIGTERM", func(t *testing.T) {
		pgBefore := postgresConnections(t, deps)
		redisBefore := redisConnections(t, deps)
		outputPath := filepath.Join(t.TempDir(), "server.log")
		output, err := os.Create(outputPath)
		if err != nil {
			t.Fatal(err)
		}
		defer output.Close()
		cmd := exec.Command(binary, "server")
		cmd.Env = baseEnv
		cmd.Stdout, cmd.Stderr = output, output
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- cmd.Wait() }()
		stopped := false
		t.Cleanup(func() {
			if !stopped {
				_ = cmd.Process.Kill()
				<-done
			}
		})
		client := &http.Client{Timeout: time.Second}
		deadline := time.Now().Add(15 * time.Second)
		for {
			resp, err := client.Get("http://" + httpAddress + "/health")
			if err == nil {
				resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					t.Fatalf("GET /health: status %d", resp.StatusCode)
				}
				break
			}
			select {
			case err := <-done:
				stopped = true
				logs, _ := os.ReadFile(outputPath)
				t.Fatalf("server exited before HTTP startup: %v\n%s", err, logs)
			default:
			}
			if time.Now().After(deadline) {
				t.Fatal("server did not become ready")
			}
			time.Sleep(50 * time.Millisecond)
		}
		if got := postgresConnections(t, deps); got <= pgBefore {
			t.Fatalf("expected a PostgreSQL connection: before=%d running=%d", pgBefore, got)
		}
		if got := redisConnections(t, deps); got <= redisBefore {
			t.Fatalf("expected a Redis connection: before=%d running=%d", redisBefore, got)
		}
		if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			stopped = true
			if err != nil {
				logs, _ := os.ReadFile(outputPath)
				t.Fatalf("server shutdown: %v\n%s", err, logs)
			}
		case <-time.After(15 * time.Second):
			t.Fatal("server did not stop after SIGTERM")
		}
		if got := postgresConnections(t, deps); got != pgBefore {
			t.Fatalf("PostgreSQL connections after shutdown: want %d, got %d", pgBefore, got)
		}
		if got := redisConnections(t, deps); got != redisBefore {
			t.Fatalf("Redis connections after shutdown: want %d, got %d", redisBefore, got)
		}
	})

	for _, tc := range []struct {
		name    string
		env     string
		message string
	}{
		{"empty API key", "API_KEY=", "API key is required"},
		{"invalid public base URL", "PUBLIC_BASE_URL=not-a-url", "public base URL"},
		{"unavailable PostgreSQL", "DATABASE_PORT=1", "cannot connect to PostgreSQL"},
		{"unavailable Redis", "REDIS_URL=redis://127.0.0.1:1/0", "cannot connect to Redis"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, binary, "server")
			cmd.Env = append(append([]string(nil), baseEnv...), tc.env)
			output, err := cmd.CombinedOutput()
			if ctx.Err() != nil {
				t.Fatalf("server did not fail promptly: %v", ctx.Err())
			}
			if err == nil {
				t.Fatalf("server accepted invalid configuration: %s", output)
			}
			if !strings.Contains(string(output), tc.message) {
				t.Fatalf("expected failure mentioning %q, got: %s", tc.message, output)
			}
			if strings.Contains(string(output), "integration-only") {
				t.Fatal("server output exposed credentials")
			}
			if got := postgresConnections(t, deps); got != 0 {
				t.Fatalf("PostgreSQL connections after failed startup: got %d", got)
			}
		})
	}
}

func postgresConnections(t *testing.T, deps *fixture.Dependencies) int {
	t.Helper()
	result, err := deps.Exec("postgres", "psql", "-U", "shortener", "-d", "shortener", "-Atc", "SELECT count(*) FROM pg_stat_activity WHERE datname = current_database() AND pid <> pg_backend_pid()")
	if err != nil {
		t.Fatal(err)
	}
	count, err := strconv.Atoi(strings.TrimSpace(result))
	if err != nil {
		t.Fatalf("parse PostgreSQL connection count %q: %v", result, err)
	}
	return count
}

func redisConnections(t *testing.T, deps *fixture.Dependencies) int {
	t.Helper()
	result, err := deps.Exec("redis", "redis-cli", "CLIENT", "LIST")
	if err != nil {
		t.Fatal(err)
	}
	if result == "" {
		t.Fatal(fmt.Errorf("Redis returned an empty client list"))
	}
	return len(strings.Split(result, "\n"))
}
