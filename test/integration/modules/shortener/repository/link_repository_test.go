//go:build integration

package repository_test

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/model"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/infrastructure"
	"github.com/cristiano-pacheco/go-shortner/test/integration/fixture"
	"go.uber.org/fx"
)

func TestLinkRepository(t *testing.T) {
	deps := fixture.StartDependencies(t)
	host, pgPort, err := net.SplitHostPort(deps.PostgresAddress)
	if err != nil {
		t.Fatal(err)
	}
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate project")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../../../.."))
	env := append(os.Environ(), "APP_CONFIG_DIR="+filepath.Join(root, "config"), "APP_ENV=local", "DATABASE_HOST="+host, "DATABASE_PORT="+pgPort, "DATABASE_NAME=shortener", "DATABASE_USER=shortener", "DATABASE_PASSWORD=integration-only", "DATABASE_SSLMODE=disable", "REDIS_URL=redis://"+deps.RedisAddress+"/0", "API_KEY=integration-only", "PUBLIC_BASE_URL=http://localhost:8080", "HTTP_ADDR=127.0.0.1:0")
	binary := filepath.Join(t.TempDir(), "shortener")
	build := exec.Command("go", "build", "-o", binary, ".")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	migrate := exec.Command(binary, "migrate")
	migrate.Env = env
	if output, err := migrate.CombinedOutput(); err != nil {
		t.Fatalf("migrate: %v\n%s", err, output)
	}
	cfg := config.Config{DatabaseHost: host, DatabasePort: pgPort, DatabaseName: "shortener", DatabaseUser: "shortener", DatabasePassword: "integration-only", DatabaseSSLMode: "disable", RedisURL: "redis://" + deps.RedisAddress + "/0"}
	ctx := context.Background()
	start := func() (*fx.App, port.LinkRepository, *infrastructure.Resources) {
		t.Helper()
		var repo port.LinkRepository
		var resources *infrastructure.Resources
		app := fx.New(fx.NopLogger, fx.Supply(cfg), fx.Provide(infrastructure.NewResources), shortener.Module, fx.Populate(&repo, &resources))
		if err := app.Start(ctx); err != nil {
			t.Fatal(err)
		}
		return app, repo, resources
	}
	app, repo, _ := start()
	expires := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
	first, err := repo.Create(ctx, model.LinkModel{Code: "first", Destination: "https://example.com", Active: true, ExpiresAt: &expires})
	if err != nil || first.ID <= 0 {
		t.Fatalf("create: %+v %v", first, err)
	}
	second, err := repo.Create(ctx, model.LinkModel{Code: "second", Destination: "https://example.com", Active: true})
	if err != nil || second.ID <= 0 || second.ID == first.ID {
		t.Fatalf("duplicate destination: %+v %v", second, err)
	}
	got, err := repo.FindByID(ctx, first.ID)
	if err != nil || got.Code != first.Code || got.ExpiresAt == nil || !got.ExpiresAt.Equal(expires) {
		t.Fatalf("lookup: %+v %v", got, err)
	}
	if err := app.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	app, repo, resources := start()
	defer func() {
		if err := app.Stop(ctx); err != nil {
			t.Error(err)
		}
	}()
	got, err = repo.FindByCode(ctx, first.Code)
	if err != nil || got.ID != first.ID {
		t.Fatalf("after restart: %+v %v", got, err)
	}
	// Equal timestamps exercise the identifier tie-breaker independently of timing.
	if err := resources.DB.WithContext(ctx).Model(&model.LinkModel{}).Where("id IN ?", []int64{first.ID, second.ID}).Update("created_at", first.CreatedAt).Error; err != nil {
		t.Fatal(err)
	}
	higher, lower := first.ID, second.ID
	if higher < lower {
		higher, lower = lower, higher
	}
	links, total, err := repo.List(ctx, 1, 0)
	if err != nil || total != 2 || len(links) != 1 || links[0].ID != higher {
		t.Fatalf("first page: %+v %d %v", links, total, err)
	}
	links, total, err = repo.List(ctx, 1, 1)
	if err != nil || total != 2 || len(links) != 1 || links[0].ID != lower {
		t.Fatalf("second page: %+v %d %v", links, total, err)
	}
	inactive := false
	dest := "https://changed.example"
	got, err = repo.Update(ctx, first.ID, model.LinkChanges{Destination: &dest, Active: &inactive, SetExpiresAt: true})
	if err != nil || got.Code != first.Code || got.Destination != dest || got.Active || got.ExpiresAt != nil {
		t.Fatalf("update: %+v %v", got, err)
	}
	deleted, err := repo.Delete(ctx, first.ID)
	if err != nil || deleted.Code != first.Code || deleted.DeletedAt == nil {
		t.Fatalf("delete: %+v %v", deleted, err)
	}
	if _, err := repo.FindByID(ctx, first.ID); !errors.Is(err, errs.ErrLinkNotFound) {
		t.Fatalf("deleted lookup: %v", err)
	}
	if _, err := repo.FindByCode(ctx, first.Code); !errors.Is(err, errs.ErrLinkNotFound) {
		t.Fatalf("deleted code lookup: %v", err)
	}
	if _, err := repo.Update(ctx, first.ID, model.LinkChanges{Active: &inactive}); !errors.Is(err, errs.ErrLinkNotFound) {
		t.Fatalf("update deleted: %v", err)
	}
	if _, err := repo.Delete(ctx, first.ID); !errors.Is(err, errs.ErrLinkNotFound) {
		t.Fatalf("delete twice: %v", err)
	}
	if err := app.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	app, repo, resources = start()
	if _, err := repo.Create(ctx, model.LinkModel{Code: first.Code, Destination: dest, Active: true}); !errors.Is(err, errs.ErrCodeConflict) {
		t.Fatalf("reuse deleted code: %v", err)
	}
	links, total, err = repo.List(ctx, 10, 0)
	if err != nil || total != 1 || len(links) != 1 || links[0].ID != second.ID {
		t.Fatalf("list after deletion: %+v %d %v", links, total, err)
	}
	const n = 12
	var wg sync.WaitGroup
	outcomes := make(chan error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := repo.Create(ctx, model.LinkModel{Code: "collision", Destination: dest, Active: true})
			outcomes <- err
		}()
	}
	wg.Wait()
	close(outcomes)
	success := 0
	for err := range outcomes {
		if err == nil {
			success++
		} else if !errors.Is(err, errs.ErrCodeConflict) {
			t.Fatalf("concurrent create: %v", err)
		}
	}
	if success != 1 {
		t.Fatalf("concurrent successes: %d", success)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := repo.FindByID(canceled, second.ID); err == nil || errors.Is(err, errs.ErrLinkNotFound) {
		t.Fatalf("canceled query: %v", err)
	}
	db, err := resources.DB.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.FindByID(ctx, second.ID); err == nil || errors.Is(err, errs.ErrLinkNotFound) {
		t.Fatalf("database failure mistaken for absence: %v", err)
	}
}
