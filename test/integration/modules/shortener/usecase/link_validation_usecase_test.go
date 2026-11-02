//go:build integration

package usecase_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/cristiano-pacheco/bricks/pkg/itestkit"
	bricksvalidator "github.com/cristiano-pacheco/bricks/pkg/validator"
	playgroundvalidator "github.com/go-playground/validator/v10"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/suite"
	"go.uber.org/fx"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/usecase"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/database"
	sharedredis "github.com/cristiano-pacheco/go-shortner/internal/shared/redis"
)

type LinkValidationUseCaseSuite struct {
	suite.Suite
	kit                *itestkit.ITestKit
	linkCreateUseCase  *usecase.LinkCreateUseCase
	linkGetUseCase     *usecase.LinkGetUseCase
	linkUpdateUseCase  *usecase.LinkUpdateUseCase
	linkListUseCase    *usecase.LinkListUseCase
	linkResolveUseCase *usecase.LinkResolveUseCase
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
	app := fx.New(
		fx.NopLogger,
		shortener.Module,
		bricksvalidator.Module,
		fx.Supply(&database.Database{DB: s.kit.DB()}, &sharedredis.Client{Client: redisClient}, config.Config{PublicBaseURL: "https://short.example"}),
		fx.Populate(&s.linkCreateUseCase, &s.linkGetUseCase, &s.linkUpdateUseCase, &s.linkListUseCase, &s.linkResolveUseCase),
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
	created, err := s.linkCreateUseCase.Execute(ctx, usecase.LinkCreateInput{Destination: "https://example.com", ExpiresAt: &expiration})
	s.Require().NoError(err)
	s.Require().NotNil(created.ExpiresAt)
	s.True(expiration.Truncate(time.Microsecond).Equal(*created.ExpiresAt))

	ignoredExpiration := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	active := false
	updated, err := s.linkUpdateUseCase.Execute(ctx, usecase.LinkUpdateInput{ID: created.ID, Active: &active, ExpiresAt: &ignoredExpiration})
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

func (s *LinkValidationUseCaseSuite) TestInvalidUpdateDoesNotChangePersistedLink() {
	ctx := context.Background()
	created, err := s.linkCreateUseCase.Execute(ctx, usecase.LinkCreateInput{Destination: "https://example.com"})
	s.Require().NoError(err)
	invalid := ""
	active := false
	_, err = s.linkUpdateUseCase.Execute(ctx, usecase.LinkUpdateInput{ID: created.ID, Destination: &invalid, Active: &active})
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
