//go:build integration

package shortener_test

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cristiano-pacheco/bricks/pkg/database"
	"github.com/cristiano-pacheco/bricks/pkg/redis"
	"github.com/cristiano-pacheco/bricks/pkg/validator"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/cache"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/repository"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/usecase"
	destinationvalidator "github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/validator"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/metrics"
)

type collisionGenerator struct {
	codes []string
	calls int
}

func (g *collisionGenerator) Generate() (string, error) {
	if g.calls >= len(g.codes) {
		return "", fmt.Errorf("unexpected generator call %d", g.calls+1)
	}

	code := g.codes[g.calls]
	g.calls++

	return code, nil
}

func TestResourceConfigurationAndCodeCollisions(t *testing.T) {
	stack := start(t)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	run := func(args ...string) string {
		t.Helper()

		out, err := stack.run(args...)
		require.NoError(t, err, "%s", out)

		return strings.TrimSpace(string(out))
	}
	postgresAddress := run("port", "postgres", "5432")
	redisAddress := run("port", "redis", "6379")
	postgresHost, postgresPort, err := net.SplitHostPort(postgresAddress)
	require.NoError(t, err)

	configDir := t.TempDir()
	t.Setenv("APP_CONFIG_DIR", configDir)
	t.Setenv("APP_ENV", "local")

	contents := fmt.Sprintf(`app:
  database:
    host: %q
    port: %s
    name: shortener
    user: shortener
    password: e2e-only
    sslmode: false
  redis:
    type: single_node
    url: "redis://%s"
    db: 3
`, postgresHost, postgresPort, redisAddress)
	require.NoError(t, os.WriteFile(filepath.Join(configDir, "base.yaml"), []byte(contents), 0600))

	cfg := config.Config{PublicBaseURL: publicBase}

	generator := &collisionGenerator{
		codes: []string{"occupied", "available", "occupied", "occupied", "occupied", "occupied", "occupied"},
	}

	var (
		create  *usecase.LinkCreateUseCase
		resolve *usecase.LinkResolveUseCase
	)

	app := fx.New(fx.NopLogger, fx.Supply(cfg), database.Module, redis.ClientModule, validator.Module,
		fx.Provide(
			func() prometheus.Registerer {
				return prometheus.NewRegistry()
			},
			metrics.New,
			fx.Annotate(repository.NewLinkRepository, fx.As(new(port.LinkRepository))),
			fx.Annotate(cache.NewRedirectCache, fx.As(new(port.RedirectCache))),
			fx.Annotate(destinationvalidator.NewDestinationValidator, fx.As(new(port.DestinationValidator))),
			func() port.CodeGenerator {
				return generator
			},
			usecase.NewLinkCreateUseCase, usecase.NewLinkResolveUseCase,
		), fx.Populate(&create, &resolve))
	require.NoError(t, app.Err())
	require.NoError(t, app.Start(ctx))
	t.Cleanup(func() {
		stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer stopCancel()

		require.NoError(t, app.Stop(stopCtx))
	})

	occupied := "occupied"
	_, err = create.Execute(ctx, usecase.LinkCreateInput{Destination: "https://example.com/occupied", Alias: &occupied})
	require.NoError(t, err)
	created, err := create.Execute(ctx, usecase.LinkCreateInput{Destination: "https://example.com/available"})
	require.NoError(t, err)
	require.Equal(t, "available", created.Code)
	require.Equal(t, 2, generator.calls)

	_, err = create.Execute(ctx, usecase.LinkCreateInput{Destination: "https://example.com/exhausted"})
	require.ErrorContains(t, err, "automatic code generation exhausted retries")
	require.Equal(t, 7, generator.calls)

	resolved, err := resolve.Execute(ctx, usecase.LinkResolveInput{Code: created.Code})
	require.NoError(t, err)
	require.Equal(t, created.Destination, resolved.Destination)
	key := "shortener:redirect:v1:" + created.Code
	require.Equal(
		t,
		"1",
		run(
			"exec",
			"-T",
			"redis",
			"redis-cli",
			"-n",
			"3",
			"EXISTS",
			key,
		),
	)
	require.Equal(
		t,
		"0",
		run(
			"exec",
			"-T",
			"redis",
			"redis-cli",
			"-n",
			"0",
			"EXISTS",
			key,
		),
	)
}
