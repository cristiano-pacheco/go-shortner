//go:build integration

package migration_test

import (
	"context"
	sqlpkg "database/sql"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cristiano-pacheco/go-shortner/test/integration/fixture"
	_ "github.com/jackc/pgx/v5/stdlib"
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
	// A competing transaction blocks schema_migrations reads until the migration
	// connection's statement timeout stops the command.
	dsn := url.URL{Scheme: "postgres", User: url.UserPassword("shortener", "integration-only"), Host: deps.PostgresAddress, Path: "/shortener", RawQuery: "sslmode=disable"}
	db, err := sqlpkg.Open("pgx", dsn.String())
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec("LOCK TABLE schema_migrations IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	blocked := exec.CommandContext(ctx, binary, "migrate")
	blocked.Env = env
	output, err := blocked.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("migration did not stop while blocked: %v", ctx.Err())
	}
	if err == nil || !strings.Contains(string(output), "database migration failed") {
		t.Fatalf("blocked migration should fail: %v, %s", err, output)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
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
