package usecase

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/model"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
)

const maxCodeAttempts = 5

type LinkCreateInput struct {
	Destination string
	Alias       *string
	ExpiresAt   *time.Time
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
	repository           port.LinkRepository
	generator            port.CodeGenerator
	baseURL              string
	destinationValidator port.DestinationValidator
}

func NewLinkCreateUseCase(
	repository port.LinkRepository,
	generator port.CodeGenerator,
	destinationValidator port.DestinationValidator,
	cfg config.Config,
) *LinkCreateUseCase {
	return &LinkCreateUseCase{
		repository:           repository,
		generator:            generator,
		destinationValidator: destinationValidator,
		baseURL:              strings.TrimRight(cfg.PublicBaseURL, "/"),
	}
}

func (uc *LinkCreateUseCase) Execute(ctx context.Context, input LinkCreateInput) (LinkCreateOutput, error) {
	if err := uc.destinationValidator.Validate(input.Destination); err != nil {
		return LinkCreateOutput{}, err
	}

	if err := uc.normalizeExpiration(&input); err != nil {
		return LinkCreateOutput{}, err
	}

	if input.Alias != nil && !uc.validAlias(*input.Alias) {
		return LinkCreateOutput{}, errs.ErrInvalidInput
	}

	for range maxCodeAttempts {
		code, generateErr := uc.code(input.Alias)
		if generateErr != nil {
			return LinkCreateOutput{}, generateErr
		}

		link, createErr := uc.repository.Create(
			ctx,
			model.LinkModel{Code: code, Destination: input.Destination, Active: true, ExpiresAt: input.ExpiresAt},
		)
		if errors.Is(createErr, errs.ErrCodeConflict) && input.Alias == nil {
			continue
		}

		if createErr != nil {
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

	return uc.generator.Generate()
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
