//go:build integration

package migration_test

import (
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/cristiano-pacheco/go-shortner/test/integration/fixture"
)

func TestMigrationCommand(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate repository")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../.."))
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
	env := append(os.Environ(), "APP_CONFIG_DIR="+filepath.Join(root, "config"), "APP_ENV=local", "DATABASE_HOST="+host, "DATABASE_PORT="+port, "DATABASE_NAME=shortener", "DATABASE_USER=shortener", "DATABASE_PASSWORD=integration-only", "DATABASE_SSLMODE=disable", "REDIS_URL=redis://127.0.0.1:1/0", "API_KEY=integration-only-key", "PUBLIC_BASE_URL=http://localhost:8080", "HTTP_ADDR=127.0.0.1:1")
	sql := func(query string) string {
		t.Helper()
		out, err := deps.Exec("postgres", "psql", "-v", "ON_ERROR_STOP=1", "-U", "shortener", "-d", "shortener", "-Atc", query)
		if err != nil {
			t.Fatal(err)
		}
		return strings.TrimSpace(out)
	}
	if got := sql("SELECT to_regclass('public.links') IS NULL"); got != "t" {
		t.Fatalf("database was not empty: %s", got)
	}
	for i := 0; i < 2; i++ {
		command := exec.Command(binary, "migrate")
		command.Env = env
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("migrate: %v\n%s", err, output)
		}
	}
	if got := sql("SELECT to_regclass('public.links') IS NOT NULL"); got != "t" {
		t.Fatalf("links table missing: %s", got)
	}
	if got := sql("INSERT INTO links (code, destination) VALUES ('first', 'https://example.com') RETURNING id > 0"); !strings.HasPrefix(got, "t") {
		t.Fatalf("identifier is not positive: %s", got)
	}
	down, err := os.ReadFile(filepath.Join(root, "internal/modules/shortener/migration/20261030000100_create_links.down.sql"))
	if err != nil {
		t.Fatal(err)
	}
	sql(string(down))
	if got := sql("SELECT to_regclass('public.links') IS NULL"); got != "t" {
		t.Fatalf("down migration did not remove links: %s", got)
	}
}
