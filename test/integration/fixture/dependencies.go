//go:build integration

package fixture

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Dependencies owns a Compose project and its disposable database volume.
type Dependencies struct {
	root            string
	project         string
	env             []string
	PostgresAddress string
	RedisAddress    string
}

// StartDependencies starts real services. Missing Docker or unhealthy services fail the test.
func StartDependencies(t *testing.T) *Dependencies {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration fixture")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
	d := &Dependencies{
		root:    root,
		project: fmt.Sprintf("shortener-it-%d", time.Now().UnixNano()),
		env:     append(os.Environ(), "POSTGRES_DB=shortener", "POSTGRES_USER=shortener", "POSTGRES_PASSWORD=integration-only", "POSTGRES_PORT=0", "REDIS_PORT=0"),
	}
	t.Cleanup(func() {
		if _, err := d.compose("down", "--volumes", "--remove-orphans"); err != nil {
			t.Errorf("remove integration dependencies: %v", err)
		}
	})
	if _, err := d.compose("up", "--detach", "--wait", "--wait-timeout", "60"); err != nil {
		t.Fatalf("start integration dependencies: %v", err)
	}
	var err error
	d.PostgresAddress, err = d.compose("port", "postgres", "5432")
	if err != nil {
		t.Fatal(err)
	}
	d.RedisAddress, err = d.compose("port", "redis", "6379")
	if err != nil {
		t.Fatal(err)
	}
	for _, address := range []string{d.PostgresAddress, d.RedisAddress} {
		if _, _, err := net.SplitHostPort(address); err != nil {
			t.Fatalf("invalid published address %q: %v", address, err)
		}
	}
	return d
}

func (d *Dependencies) compose(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	arguments := append([]string{"compose", "-f", filepath.Join(d.root, "docker-compose.yaml"), "-p", d.project}, args...)
	cmd := exec.CommandContext(ctx, "docker", arguments...)
	cmd.Env = d.env
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("docker compose %s: %w: %s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output)), nil
}

// Exec executes a command inside a running dependency container.
func (d *Dependencies) Exec(service string, args ...string) (string, error) {
	return d.compose(append([]string{"exec", "-T", service}, args...)...)
}
