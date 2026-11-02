//go:build integration

package shortener_test

import (
	"context"
	"fmt"
	"net"
	"net/url"
	"strings"
	"testing"
	"time"

	bricksvalidator "github.com/cristiano-pacheco/bricks/pkg/validator"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/cache"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/repository"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/usecase"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/validator"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/database"
	sharedredis "github.com/cristiano-pacheco/go-shortner/internal/shared/redis"
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

//nolint:paralleltest // This test changes service authentication and checks connection counts.
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

	const (
		postgresPassword = "p@ss:/?# word"
		redisPassword    = "r@ss:/?#word"
	)

	run(
		"exec",
		"-T",
		"postgres",
		"psql",
		"-X",
		"-v",
		"ON_ERROR_STOP=1",
		"-U",
		"shortener",
		"-d",
		"shortener",
		"-c",
		"ALTER ROLE shortener PASSWORD '"+postgresPassword+"'",
	)
	run("exec", "-T", "redis", "redis-cli", "CONFIG", "SET", "requirepass", redisPassword)
	redisURL := &url.URL{
		Scheme: "redis",
		User:   url.UserPassword("default", redisPassword),
		Host:   redisAddress,
		Path:   "/3",
	}
	cfg := config.Config{
		DatabaseHost:     postgresHost,
		DatabasePort:     postgresPort,
		DatabaseName:     "shortener",
		DatabaseUser:     "shortener",
		DatabasePassword: postgresPassword,
		DatabaseSSLMode:  "disable",
		RedisURL:         redisURL.String(),
		PublicBaseURL:    publicBase,
	}
	connectionCount := func() string {
		return run(
			"exec",
			"-T",
			"postgres",
			"psql",
			"-X",
			"-A",
			"-t",
			"-U",
			"shortener",
			"-d",
			"shortener",
			"-c",
			"SELECT count(*) FROM pg_stat_activity WHERE datname = 'shortener' AND client_addr IS NOT NULL",
		)
	}
	initialConnections := connectionCount()
	invalidConfig := cfg
	invalidConfig.RedisURL = ":invalid"
	invalidApp := fx.New(fx.NopLogger, fx.Supply(invalidConfig), database.Module, sharedredis.Module,
		fx.Invoke(func(_ *database.Database, _ *sharedredis.Client) {
		}))
	require.Error(t, invalidApp.Err())
	require.Equal(
		t,
		initialConnections,
		connectionCount(),
		"invalid Redis configuration must not acquire PostgreSQL connections",
	)

	unavailableConfig := cfg
	unavailableConfig.RedisURL = "redis://127.0.0.1:1/3"
	unavailableApp := fx.New(fx.NopLogger, fx.Supply(unavailableConfig), database.Module, sharedredis.Module,
		fx.Invoke(func(_ *database.Database, _ *sharedredis.Client) {
		}))
	require.NoError(t, unavailableApp.Err())
	require.Error(t, unavailableApp.Start(ctx))
	require.Eventually(t, func() bool {
		return connectionCount() == initialConnections
	}, 5*time.Second, 100*time.Millisecond,
		"failed Redis startup must close the PostgreSQL connection")

	generator := &collisionGenerator{
		codes: []string{"occupied", "available", "occupied", "occupied", "occupied", "occupied", "occupied"},
	}

	var (
		create  *usecase.LinkCreateUseCase
		resolve *usecase.LinkResolveUseCase
	)

	app := fx.New(fx.NopLogger, fx.Supply(cfg), database.Module, sharedredis.Module, bricksvalidator.Module,
		fx.Provide(
			fx.Annotate(repository.NewLinkRepository, fx.As(new(port.LinkRepository))),
			fx.Annotate(cache.NewRedirectCache, fx.As(new(port.RedirectCache))),
			fx.Annotate(validator.NewDestinationValidator, fx.As(new(port.DestinationValidator))),
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
			"--no-auth-warning",
			"--user",
			"default",
			"--pass",
			redisPassword,
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
			"--no-auth-warning",
			"--user",
			"default",
			"--pass",
			redisPassword,
			"-n",
			"0",
			"EXISTS",
			key,
		),
	)
}
