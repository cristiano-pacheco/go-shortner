//go:build integration

package usecase_test

import (
	"context"
	"errors"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.uber.org/fx"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/model"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/usecase"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/infrastructure"
	"github.com/cristiano-pacheco/go-shortner/test/integration/fixture"
)

func TestLinkUpdateUseCase(t *testing.T) {
	deps := fixture.StartDependencies(t)

	host, port, err := net.SplitHostPort(deps.PostgresAddress)
	if err != nil {
		t.Fatal(err)
	}

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate project")
	}

	root := filepath.Clean(filepath.Join(filepath.Dir(file), "../../../../.."))
	binary := filepath.Join(t.TempDir(), "shortener")
	build := exec.Command("go", "build", "-o", binary, ".")

	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}

	migrate := exec.Command(binary, "migrate")

	migrate.Env = append(os.Environ(), "APP_CONFIG_DIR="+filepath.Join(root, "config"), "APP_ENV=local",
		"DATABASE_HOST="+host, "DATABASE_PORT="+port, "DATABASE_NAME=shortener", "DATABASE_USER=shortener",
		"DATABASE_PASSWORD=integration-only", "DATABASE_SSLMODE=disable", "REDIS_URL=redis://"+deps.RedisAddress+"/0",
		"API_KEY=integration-only", "PUBLIC_BASE_URL=https://short.example", "HTTP_ADDR=127.0.0.1:0")
	if output, err := migrate.CombinedOutput(); err != nil {
		t.Fatalf("migrate: %v\n%s", err, output)
	}

	cfg := config.Config{
		DatabaseHost:     host,
		DatabasePort:     port,
		DatabaseName:     "shortener",
		DatabaseUser:     "shortener",
		DatabasePassword: "integration-only",
		DatabaseSSLMode:  "disable",
		RedisURL:         "redis://" + deps.RedisAddress + "/0",
		PublicBaseURL:    "https://short.example",
	}

	var (
		operation *usecase.LinkUpdateUseCase
		resources *infrastructure.Resources
	)

	app := fx.New(
		fx.NopLogger,
		fx.Supply(cfg),
		fx.Provide(infrastructure.NewResources),
		shortener.Module,
		fx.Populate(&operation, &resources),
	)

	ctx := context.Background()
	if err := app.Start(ctx); err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if err := app.Stop(ctx); err != nil {
			t.Error(err)
		}
	})

	originalExpiration := time.Date(2099, 1, 1, 0, 0, 0, 0, time.UTC)

	seed := model.LinkModel{
		Code:        "business-case",
		Destination: "https://old.example",
		Active:      true,
		ExpiresAt:   &originalExpiration,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}
	if err := resources.DB.Create(&seed).Error; err != nil {
		t.Fatal(err)
	}

	read := func() model.LinkModel {
		t.Helper()

		var link model.LinkModel
		if err := resources.DB.First(&link, seed.ID).Error; err != nil {
			t.Fatal(err)
		}

		return link
	}
	newDestination := "https://new.example/path"
	inactive := false

	updated, err := operation.Execute(
		ctx,
		usecase.LinkUpdateInput{ID: seed.ID, Destination: &newDestination, Active: &inactive},
	)
	if err != nil || updated.ID != seed.ID || updated.Code != seed.Code ||
		updated.ShortURL != "https://short.example/business-case" ||
		updated.Destination != newDestination ||
		updated.Active ||
		updated.ExpiresAt == nil ||
		!updated.ExpiresAt.Equal(originalExpiration) {
		t.Fatalf("partial update: %+v %v", updated, err)
	}

	if got := read(); got.Destination != newDestination || got.Active || got.ExpiresAt == nil ||
		!got.ExpiresAt.Equal(originalExpiration) {
		t.Fatalf("persisted partial update: %+v", got)
	}

	active := true

	updated, err = operation.Execute(ctx, usecase.LinkUpdateInput{ID: seed.ID, Active: &active, SetExpiresAt: true})
	if err != nil || !updated.Active || updated.ExpiresAt != nil || updated.Destination != newDestination {
		t.Fatalf("reactivate and clear expiration: %+v %v", updated, err)
	}

	future := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)

	updated, err = operation.Execute(ctx, usecase.LinkUpdateInput{ID: seed.ID, SetExpiresAt: true, ExpiresAt: &future})
	if err != nil || updated.ExpiresAt == nil || !updated.ExpiresAt.Equal(future) {
		t.Fatalf("assign expiration: %+v %v", updated, err)
	}

	before := read()

	past := time.Now().UTC().Add(-time.Hour)
	for _, invalid := range []usecase.LinkUpdateInput{
		{ID: 0, Active: &inactive},
		{ID: seed.ID, Destination: new(string), Active: &inactive},
		{ID: seed.ID, Destination: &[]string{"ftp://example.com"}[0], Active: &inactive},
		{ID: seed.ID, Destination: &[]string{"https://user:pass@example.com"}[0], Active: &inactive},
		{ID: seed.ID, Destination: &[]string{"https://example.com/" + strings.Repeat("x", 4096)}[0], Active: &inactive},
		{ID: seed.ID, SetExpiresAt: true, ExpiresAt: &past, Active: &inactive},
	} {
		if _, err := operation.Execute(ctx, invalid); !errors.Is(err, errs.ErrInvalidInput) {
			t.Errorf("invalid input: got %v", err)
		}

		after := read()
		if after.Destination != before.Destination || after.Active != before.Active || after.ExpiresAt == nil ||
			!after.ExpiresAt.Equal(*before.ExpiresAt) ||
			!after.UpdatedAt.Equal(before.UpdatedAt) {
			t.Fatalf("rejected update changed persisted link: before %+v after %+v", before, after)
		}
	}

	if _, err := operation.Execute(
		ctx,
		usecase.LinkUpdateInput{ID: seed.ID + 1000, Active: &inactive},
	); !errors.Is(
		err,
		errs.ErrLinkNotFound,
	) {
		t.Fatalf("unknown link: %v", err)
	}

	if err := resources.DB.Model(&model.LinkModel{}).
		Where("id = ?", seed.ID).
		Update("deleted_at", time.Now().UTC()).
		Error; err != nil {
		t.Fatal(err)
	}

	if _, err := operation.Execute(
		ctx,
		usecase.LinkUpdateInput{ID: seed.ID, Active: &inactive},
	); !errors.Is(
		err,
		errs.ErrLinkNotFound,
	) {
		t.Fatalf("deleted link: %v", err)
	}
}
