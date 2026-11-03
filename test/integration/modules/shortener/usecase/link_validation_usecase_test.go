//go:build integration

package usecase_test

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/cristiano-pacheco/bricks/pkg/itestkit"
	"github.com/cristiano-pacheco/bricks/pkg/validator"
	playgroundvalidator "github.com/go-playground/validator/v10"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/suite"
	"go.uber.org/fx"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/usecase"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/database"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/metrics"
	sharedredis "github.com/cristiano-pacheco/go-shortner/internal/shared/redis"
)

type LinkValidationUseCaseSuite struct {
	suite.Suite

	kit                  *itestkit.ITestKit
	registry             *prometheus.Registry
	applicationMetrics   *metrics.Metrics
	validator            validator.Validator
	repository           port.LinkRepository
	destinationValidator port.DestinationValidator
	linkDeleteUseCase    *usecase.LinkDeleteUseCase
	linkCreateUseCase    *usecase.LinkCreateUseCase
	linkGetUseCase       *usecase.LinkGetUseCase
	linkUpdateUseCase    *usecase.LinkUpdateUseCase
	linkListUseCase      *usecase.LinkListUseCase
	linkResolveUseCase   *usecase.LinkResolveUseCase
}

func TestLinkValidationUseCaseSuite(t *testing.T) {
	suite.Run(t, new(LinkValidationUseCaseSuite))
}

func (s *LinkValidationUseCaseSuite) SetupSuite() {
	cfg := itestkit.DefaultConfig()
	cfg.PostgresImage = "postgres:17.5-alpine@sha256:6567bca8d7bc8c82c5922425a0baee57be8402df92bae5eacad5f01ae9544daa"
	cfg.RedisImage = "redis:7-alpine@sha256:02f2cc4882f8bf87c79a220ac958f58c700bdec0dfb9b9ea61b62fb0e8f1bfcf"
	cfg.MigrationsPath = "file://internal/modules/shortener/migration"
	s.kit = itestkit.New(cfg)
	s.T().Cleanup(s.kit.Cleanup)
	s.Require().NoError(s.kit.StartPostgres())
	s.Require().NoError(s.kit.StartRedis())
	s.Require().NoError(s.kit.RunMigrations())
	s.kit.DB().TranslateError = true
}

func (s *LinkValidationUseCaseSuite) SetupTest() {
	s.kit.TruncateTables(s.T())
	s.Require().NoError(s.kit.Redis().FlushDB(context.Background()).Err())
	redisClient, ok := s.kit.Redis().(*redis.Client)
	s.Require().True(ok)
	s.registry = prometheus.NewRegistry()
	app := fx.New(
		fx.NopLogger,
		shortener.Module,
		validator.Module,
		fx.Provide(metrics.New),
		fx.Supply(fx.Annotate(s.registry, fx.As(new(prometheus.Registerer)))),
		fx.Supply(
			&database.Database{DB: s.kit.DB()},
			&sharedredis.Client{Client: redisClient},
			config.Config{PublicBaseURL: "https://short.example"},
		),
		fx.Populate(
			&s.applicationMetrics,
			&s.validator,
			&s.repository,
			&s.destinationValidator,
			&s.linkCreateUseCase,
			&s.linkGetUseCase,
			&s.linkUpdateUseCase,
			&s.linkListUseCase,
			&s.linkResolveUseCase,
			&s.linkDeleteUseCase,
		),
	)
	s.Require().NoError(app.Err())
	s.Require().NoError(app.Start(context.Background()))
	s.T().Cleanup(func() {
		s.NoError(app.Stop(context.Background()))
	})
}

func (s *LinkValidationUseCaseSuite) TestExpirationPrecisionAndOptionalUpdate() {
	ctx := context.Background()
	expiration := time.Date(2099, 1, 1, 0, 0, 0, 123456789, time.UTC)
	created, err := s.linkCreateUseCase.Execute(
		ctx,
		usecase.LinkCreateInput{Destination: "https://example.com", ExpiresAt: &expiration},
	)
	s.Require().NoError(err)
	s.Require().NotNil(created.ExpiresAt)
	s.True(expiration.Truncate(time.Microsecond).Equal(*created.ExpiresAt))

	ignoredExpiration := time.Date(2098, 1, 1, 0, 0, 0, 0, time.UTC)
	active := false
	updated, err := s.linkUpdateUseCase.Execute(
		ctx,
		usecase.LinkUpdateInput{ID: created.ID, Active: &active, ExpiresAt: &ignoredExpiration},
	)
	s.Require().NoError(err)
	s.False(updated.Active)
	s.Require().NotNil(updated.ExpiresAt)
	s.True(created.ExpiresAt.Equal(*updated.ExpiresAt))

	cleared, err := s.linkUpdateUseCase.Execute(ctx, usecase.LinkUpdateInput{ID: created.ID, SetExpiresAt: true})
	s.Require().NoError(err)
	s.Nil(cleared.ExpiresAt)
	stored, err := s.linkGetUseCase.Execute(ctx, usecase.LinkGetInput{ID: created.ID})
	s.Require().NoError(err)
	s.False(stored.Active)
	s.Nil(stored.ExpiresAt)
}

func (s *LinkValidationUseCaseSuite) TestResolveRetriesAfterFreshnessWindow() {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	created, err := s.linkCreateUseCase.Execute(ctx, usecase.LinkCreateInput{Destination: "https://example.com/retry"})
	s.Require().NoError(err)

	before := s.freshnessRetries()
	transaction := s.kit.DB().Begin()

	s.Require().NoError(transaction.Error)
	defer transaction.Rollback()

	s.Require().NoError(transaction.Exec("LOCK TABLE links IN ACCESS EXCLUSIVE MODE").Error)

	type resolveResult struct {
		output usecase.LinkResolveOutput
		err    error
	}

	results := make(chan resolveResult, 1)

	go func() {
		output, resolveErr := s.linkResolveUseCase.Execute(ctx, usecase.LinkResolveInput{Code: created.Code})
		results <- resolveResult{output: output, err: resolveErr}
	}()

	var blocked bool

	s.Require().Eventually(func() bool {
		err = s.kit.DB().Raw(`SELECT EXISTS (
			SELECT 1 FROM pg_locks
			WHERE relation = 'links'::regclass AND mode = 'AccessShareLock' AND NOT granted
		)`).Scan(&blocked).Error

		return err == nil && blocked
	}, 10*time.Second, 100*time.Millisecond, "resolve read did not wait for the table lock")

	// The read has entered PostgreSQL, so the freshness window has already started.
	time.Sleep(61 * time.Second)
	s.Require().NoError(transaction.Commit().Error)

	select {
	case result := <-results:
		s.Require().NoError(result.err)
		s.Equal(created.Destination, result.output.Destination)
	case <-ctx.Done():
		s.FailNow("resolve did not finish", ctx.Err().Error())
	}

	s.InDelta(before+1, s.freshnessRetries(), 0.001)
}

func (s *LinkValidationUseCaseSuite) freshnessRetries() float64 {
	s.T().Helper()
	families, err := s.registry.Gather()
	s.Require().NoError(err)

	for _, family := range families {
		if family.GetName() == "shortener_resolution_freshness_retries_total" {
			s.Require().Len(family.GetMetric(), 1)
			return family.GetMetric()[0].GetCounter().GetValue()
		}
	}

	s.FailNow("freshness retry counter not registered")

	return 0
}

func (s *LinkValidationUseCaseSuite) TestPastExpirationReturnsValidationError() {
	ctx := context.Background()
	created, err := s.linkCreateUseCase.Execute(ctx, usecase.LinkCreateInput{Destination: "https://example.com"})
	s.Require().NoError(err)

	for _, expiration := range []time.Time{{}, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)} {
		_, err = s.linkCreateUseCase.Execute(
			ctx,
			usecase.LinkCreateInput{Destination: "https://example.com", ExpiresAt: &expiration},
		)

		var validationErrors playgroundvalidator.ValidationErrors
		s.Require().ErrorAs(err, &validationErrors)
		s.Require().Len(validationErrors, 1)
		s.Equal("ExpiresAt", validationErrors[0].Field())
		s.Equal("gt", validationErrors[0].Tag())

		for _, setExpiresAt := range []bool{false, true} {
			_, err = s.linkUpdateUseCase.Execute(
				ctx,
				usecase.LinkUpdateInput{ID: created.ID, SetExpiresAt: setExpiresAt, ExpiresAt: &expiration},
			)
			s.Require().ErrorAs(err, &validationErrors)
			s.Require().Len(validationErrors, 1)
			s.Equal("ExpiresAt", validationErrors[0].Field())
			s.Equal("gt", validationErrors[0].Tag())
		}
	}
}

func (s *LinkValidationUseCaseSuite) TestInvalidUpdateDoesNotChangePersistedLink() {
	ctx := context.Background()
	created, err := s.linkCreateUseCase.Execute(ctx, usecase.LinkCreateInput{Destination: "https://example.com"})
	s.Require().NoError(err)

	invalid := ""
	active := false
	_, err = s.linkUpdateUseCase.Execute(
		ctx,
		usecase.LinkUpdateInput{ID: created.ID, Destination: &invalid, Active: &active},
	)
	s.ErrorIs(err, errs.ErrInvalidInput)
	stored, err := s.linkGetUseCase.Execute(ctx, usecase.LinkGetInput{ID: created.ID})
	s.Require().NoError(err)
	s.Equal(created.Destination, stored.Destination)
	s.Equal(created.Active, stored.Active)
	s.True(created.UpdatedAt.Equal(stored.UpdatedAt))
}

func (s *LinkValidationUseCaseSuite) TestDirectInputsRejectInvalidValues() {
	ctx := context.Background()
	_, err := s.linkListUseCase.Execute(ctx, usecase.LinkListInput{Page: math.MaxInt, PageSize: 100})
	s.ErrorIs(err, errs.ErrInvalidInput)
	_, err = s.linkGetUseCase.Execute(ctx, usecase.LinkGetInput{ID: 0})

	var validationErrors playgroundvalidator.ValidationErrors
	s.Require().ErrorAs(err, &validationErrors)
	s.Require().Len(validationErrors, 1)
	s.Equal("ID", validationErrors[0].Field())
	s.Equal("gt", validationErrors[0].Tag())
	_, err = s.linkResolveUseCase.Execute(ctx, usecase.LinkResolveInput{})
	s.Require().ErrorAs(err, &validationErrors)
	s.Require().Len(validationErrors, 1)
	s.Equal("Code", validationErrors[0].Field())
	s.Equal("required", validationErrors[0].Tag())

	for _, code := range []string{"api", "health"} {
		_, err = s.linkResolveUseCase.Execute(ctx, usecase.LinkResolveInput{Code: code})
		s.ErrorIs(err, errs.ErrLinkNotFound)
	}
}

// TestDirectInvalidInputsAreObserved covers values that HTTP decoding rejects before a use case runs.
func (s *LinkValidationUseCaseSuite) TestDirectInvalidInputsAreObserved() {
	ctx := context.Background()

	cases := []struct {
		operation string
		execute   func() error
	}{
		{"create", func() error {
			_, err := s.linkCreateUseCase.Execute(ctx, usecase.LinkCreateInput{})
			return err
		}},
		{"get", func() error {
			_, err := s.linkGetUseCase.Execute(ctx, usecase.LinkGetInput{})
			return err
		}},
		{"list", func() error {
			_, err := s.linkListUseCase.Execute(ctx, usecase.LinkListInput{Page: math.MaxInt, PageSize: 100})
			return err
		}},
		{"update", func() error {
			_, err := s.linkUpdateUseCase.Execute(ctx, usecase.LinkUpdateInput{})
			return err
		}},
		{"delete", func() error {
			_, err := s.linkDeleteUseCase.Execute(ctx, usecase.LinkDeleteInput{})
			return err
		}},
		{"resolve", func() error {
			_, err := s.linkResolveUseCase.Execute(ctx, usecase.LinkResolveInput{})
			return err
		}},
	}
	for _, tc := range cases {
		s.Run(tc.operation, func() {
			beforeInvalid, beforeOutcomes, beforeDuration := s.metricCounts(tc.operation)
			s.Require().Error(tc.execute())
			afterInvalid, afterOutcomes, afterDuration := s.metricCounts(tc.operation)
			s.Equal(beforeInvalid+1, afterInvalid)
			s.Equal(beforeOutcomes+1, afterOutcomes)
			s.Equal(beforeDuration+1, afterDuration)
		})
	}
}

func (s *LinkValidationUseCaseSuite) metricCounts(operation string) (float64, float64, uint64) {
	s.T().Helper()
	families, err := s.registry.Gather()
	s.Require().NoError(err)

	var (
		invalid, outcomes float64
		duration          uint64
	)

	for _, family := range families {
		name := family.GetName()
		for _, metric := range family.GetMetric() {
			if strings.HasPrefix(name, "shortener_usecase_") &&
				(strings.HasSuffix(name, "_success_total") || strings.HasSuffix(name, "_errors_total")) {
				outcomes += metric.GetCounter().GetValue()
			}

			switch name {
			case "shortener_usecase_" + operation + "_errors_total":
				for _, label := range metric.GetLabel() {
					if label.GetName() == "category" && label.GetValue() == "invalid_argument" {
						invalid += metric.GetCounter().GetValue()
					}
				}
			case "shortener_usecase_" + operation + "_duration_seconds":
				duration += metric.GetHistogram().GetSampleCount()
			}
		}
	}

	return invalid, outcomes, duration
}

type sequenceGenerator struct {
	codes []string
	calls int
	err   error
}

func (g *sequenceGenerator) Generate() (string, error) {
	g.calls++
	if g.err != nil {
		return "", g.err
	}

	return g.codes[(g.calls-1)%len(g.codes)], nil
}

func (s *LinkValidationUseCaseSuite) TestGeneratedCodeCollisionsAndExhaustion() {
	ctx := context.Background()
	alias := "occupied"
	original, err := s.linkCreateUseCase.Execute(ctx, usecase.LinkCreateInput{Destination: "https://example.com/original", Alias: &alias})
	s.Require().NoError(err)
	_, err = s.linkDeleteUseCase.Execute(ctx, usecase.LinkDeleteInput{ID: original.ID})
	s.Require().NoError(err)

	generator := &sequenceGenerator{codes: []string{alias, "freecode"}}
	create := usecase.NewLinkCreateUseCase(s.repository, generator, s.destinationValidator, s.validator, s.applicationMetrics, config.Config{PublicBaseURL: "https://short.example"})
	attempts := s.generationMetric("shortener_code_generation_attempts_total", "result", "success")
	duration := s.generationMetric("shortener_code_generation_duration_seconds", "result", "success")
	collisions := s.generationMetric("shortener_code_creation_events_total", "event", "automatic_collision")
	exhausted := s.generationMetric("shortener_code_creation_events_total", "event", "retries_exhausted")
	created, err := create.Execute(ctx, usecase.LinkCreateInput{Destination: "https://example.com/new"})
	s.Require().NoError(err)
	s.Equal("freecode", created.Code)
	s.Equal(2, generator.calls)
	s.Equal(attempts+2, s.generationMetric("shortener_code_generation_attempts_total", "result", "success"))
	s.Equal(duration+2, s.generationMetric("shortener_code_generation_duration_seconds", "result", "success"))
	s.Equal(collisions+1, s.generationMetric("shortener_code_creation_events_total", "event", "automatic_collision"))
	stored, err := s.linkGetUseCase.Execute(ctx, usecase.LinkGetInput{ID: created.ID})
	s.Require().NoError(err)
	s.Equal(created.Code, stored.Code)

	generator = &sequenceGenerator{codes: []string{alias}}
	create = usecase.NewLinkCreateUseCase(s.repository, generator, s.destinationValidator, s.validator, s.applicationMetrics, config.Config{PublicBaseURL: "https://short.example"})
	_, err = create.Execute(ctx, usecase.LinkCreateInput{Destination: "https://example.com/exhausted"})
	s.Require().Error(err)
	s.Equal(5, generator.calls)
	s.Equal(attempts+7, s.generationMetric("shortener_code_generation_attempts_total", "result", "success"))
	s.Equal(duration+7, s.generationMetric("shortener_code_generation_duration_seconds", "result", "success"))
	s.Equal(collisions+6, s.generationMetric("shortener_code_creation_events_total", "event", "automatic_collision"))
	s.Equal(exhausted+1, s.generationMetric("shortener_code_creation_events_total", "event", "retries_exhausted"))
	list, err := s.linkListUseCase.Execute(ctx, usecase.LinkListInput{Page: 1, PageSize: 10})
	s.Require().NoError(err)
	s.EqualValues(1, list.Total)
}

func (s *LinkValidationUseCaseSuite) generationMetric(name, labelName, labelValue string) float64 {
	s.T().Helper()
	families, err := s.registry.Gather()
	s.Require().NoError(err)
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, metric := range family.GetMetric() {
			for _, label := range metric.GetLabel() {
				if label.GetName() == labelName && label.GetValue() == labelValue {
					if metric.GetCounter() != nil {
						return metric.GetCounter().GetValue()
					}
					if metric.GetHistogram() != nil {
						return float64(metric.GetHistogram().GetSampleCount())
					}
				}
			}
		}
	}
	return 0
}

func (s *LinkValidationUseCaseSuite) TestGenerationAndPersistenceFailuresAreDistinct() {
	expected := errors.New("controlled entropy failure")
	generator := &sequenceGenerator{err: expected}
	create := usecase.NewLinkCreateUseCase(s.repository, generator, s.destinationValidator, s.validator, s.applicationMetrics, config.Config{PublicBaseURL: "https://short.example"})
	failure := s.generationMetric("shortener_code_generation_attempts_total", "result", "error")
	failureDuration := s.generationMetric("shortener_code_generation_duration_seconds", "result", "error")
	persistence := s.generationMetric("shortener_code_creation_events_total", "event", "persistence_error")
	_, err := create.Execute(context.Background(), usecase.LinkCreateInput{Destination: "https://example.com"})
	s.ErrorIs(err, expected)
	s.Equal(1, generator.calls)
	s.Equal(failure+1, s.generationMetric("shortener_code_generation_attempts_total", "result", "error"))
	s.Equal(failureDuration+1, s.generationMetric("shortener_code_generation_duration_seconds", "result", "error"))
	s.Equal(persistence, s.generationMetric("shortener_code_creation_events_total", "event", "persistence_error"))

	generator = &sequenceGenerator{codes: []string{"freecode"}}
	create = usecase.NewLinkCreateUseCase(s.repository, generator, s.destinationValidator, s.validator, s.applicationMetrics, config.Config{PublicBaseURL: "https://short.example"})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = create.Execute(ctx, usecase.LinkCreateInput{Destination: "https://example.com"})
	s.Require().Error(err)
	s.Equal(1, generator.calls)
	s.Equal(failure+1, s.generationMetric("shortener_code_generation_attempts_total", "result", "error"))
	s.Equal(persistence+1, s.generationMetric("shortener_code_creation_events_total", "event", "persistence_error"))
	list, err := s.linkListUseCase.Execute(context.Background(), usecase.LinkListInput{Page: 1, PageSize: 10})
	s.Require().NoError(err)
	s.Zero(list.Total)
}
