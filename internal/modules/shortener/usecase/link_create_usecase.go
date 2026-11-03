package usecase

import (
	"context"
	"errors"
	"strings"
	"time"

	bricksvalidator "github.com/cristiano-pacheco/bricks/pkg/validator"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/model"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/metrics"
)

const maxCodeAttempts = 5

type LinkCreateInput struct {
	Destination string     `validate:"required"`
	Alias       *string    `validate:"omitempty,min=3,max=32"`
	ExpiresAt   *time.Time `validate:"omitempty,gt"`
}

type LinkCreateOutput struct {
	ID          int64
	Code        string
	Destination string
	ShortURL    string
	Active      bool
	ExpiresAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type LinkCreateUseCase struct {
	applicationMetrics   *metrics.Metrics
	validator            bricksvalidator.Validator
	repository           port.LinkRepository
	generator            port.CodeGenerator
	baseURL              string
	destinationValidator port.DestinationValidator
}

func NewLinkCreateUseCase(
	repository port.LinkRepository,
	generator port.CodeGenerator,
	destinationValidator port.DestinationValidator,
	validator bricksvalidator.Validator,
	applicationMetrics *metrics.Metrics,
	cfg config.Config,
) *LinkCreateUseCase {
	return &LinkCreateUseCase{
		applicationMetrics:   applicationMetrics,
		validator:            validator,
		repository:           repository,
		generator:            generator,
		destinationValidator: destinationValidator,
		baseURL:              strings.TrimRight(cfg.PublicBaseURL, "/"),
	}
}

func (uc *LinkCreateUseCase) Execute(ctx context.Context, input LinkCreateInput) (LinkCreateOutput, error) {
	defer uc.applicationMetrics.ObserveCreateDuration(time.Now())

	if err := uc.validator.Validate(input); err != nil {
		uc.applicationMetrics.IncrementCreateError(errs.MetricCategory(err))
		return LinkCreateOutput{}, err
	}

	if err := uc.destinationValidator.Validate(input.Destination); err != nil {
		uc.applicationMetrics.IncrementCreateError(errs.MetricCategory(err))
		return LinkCreateOutput{}, err
	}

	if err := uc.normalizeExpiration(&input); err != nil {
		uc.applicationMetrics.IncrementCreateError(errs.MetricCategory(err))
		return LinkCreateOutput{}, err
	}

	if input.Alias != nil && !uc.validAlias(*input.Alias) {
		uc.applicationMetrics.IncrementCreateError(errs.MetricCategory(errs.ErrInvalidInput))
		return LinkCreateOutput{}, errs.ErrInvalidInput
	}

	output, err := uc.create(ctx, input)
	if err != nil {
		uc.applicationMetrics.IncrementCreateError(errs.MetricCategory(err))
		return output, err
	}

	uc.applicationMetrics.IncrementCreateSuccess()

	return output, nil
}

func (uc *LinkCreateUseCase) create(ctx context.Context, input LinkCreateInput) (LinkCreateOutput, error) {
	for range maxCodeAttempts {
		code, generateErr := uc.code(input.Alias)
		if generateErr != nil {
			return LinkCreateOutput{}, generateErr
		}

		link, createErr := uc.repository.Create(
			ctx,
			model.LinkModel{Code: code, Destination: input.Destination, Active: true, ExpiresAt: input.ExpiresAt},
		)
		if errors.Is(createErr, errs.ErrCodeConflict) {
			if input.Alias == nil {
				uc.applicationMetrics.IncrementAutomaticCollision()
				continue
			}

			uc.applicationMetrics.IncrementAliasConflict()

			return LinkCreateOutput{}, createErr
		}

		if createErr != nil {
			uc.applicationMetrics.IncrementCodePersistenceError()
			return LinkCreateOutput{}, createErr
		}

		return LinkCreateOutput{
			ID:          link.ID,
			Code:        link.Code,
			Destination: link.Destination,
			ShortURL:    uc.baseURL + "/" + link.Code,
			Active:      link.Active,
			ExpiresAt:   link.ExpiresAt,
			CreatedAt:   link.CreatedAt,
			UpdatedAt:   link.UpdatedAt,
		}, nil
	}

	uc.applicationMetrics.IncrementCodeRetriesExhausted()

	return LinkCreateOutput{}, errors.New("automatic code generation exhausted retries")
}

func (uc *LinkCreateUseCase) normalizeExpiration(input *LinkCreateInput) error {
	if input.ExpiresAt == nil {
		return nil
	}

	expiration := input.ExpiresAt.UTC().Truncate(time.Microsecond)
	if !expiration.After(time.Now()) {
		return errs.ErrInvalidInput
	}

	input.ExpiresAt = &expiration

	return nil
}

func (uc *LinkCreateUseCase) code(alias *string) (string, error) {
	if alias != nil {
		return *alias, nil
	}

	startedAt := time.Now()
	code, err := uc.generator.Generate()
	uc.applicationMetrics.ObserveCodeGeneration(startedAt, err)

	return code, err
}

func (uc *LinkCreateUseCase) validAlias(alias string) bool {
	if len(alias) < 3 || len(alias) > 32 || alias == "api" || alias == "health" {
		return false
	}

	for _, c := range alias {
		if !strings.ContainsRune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_", c) {
			return false
		}
	}

	return true
}
