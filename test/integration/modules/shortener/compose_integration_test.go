//go:build integration

package shortener_test

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

const publicBase = "https://short.example"

type integrationStack struct {
	run func(...string) ([]byte, error)
}

func start(t *testing.T) integrationStack {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate repository")
	}

	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../../.."))
	project := fmt.Sprintf("shortner-integration-%d", time.Now().UnixNano())

	env := append(os.Environ(),
		"POSTGRES_DB=shortener",
		"POSTGRES_USER=shortener",
		"POSTGRES_PASSWORD=e2e-only",
		"API_KEY=integration-test-key",
		"PUBLIC_BASE_URL="+publicBase,
		"APP_PORT=0",
		"METRICS_PORT=0",
		"POSTGRES_PORT=0",
		"REDIS_PORT=0",
	)
	args := []string{
		"compose", "-p", project,
		"-f", filepath.Join(root, "docker/docker-compose.yaml"),
		"-f", filepath.Join(root, "docker/docker-compose.app.yaml"),
	}
	run := func(extra ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()

		command := exec.CommandContext(ctx, "docker", append(append([]string{}, args...), extra...)...)
		command.Dir = root
		command.Env = env

		return command.CombinedOutput()
	}

	t.Cleanup(func() {
		if out, err := run("down", "--volumes", "--remove-orphans"); err != nil {
			t.Errorf("compose cleanup: %v\n%s", err, out)
		}
	})

	for _, step := range [][]string{
		{"up", "-d", "--wait", "postgres", "redis"},
		{"build", "app"},
		{"run", "--rm", "--no-deps", "app", "migrate"},
	} {
		if out, err := run(step...); err != nil {
			t.Fatalf("compose %v: %v\n%s", step, err, out)
		}
	}

	return integrationStack{run: run}
}
