package usecase

import (
	"context"
	"errors"
	"net/url"
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
	repository port.LinkRepository
	generator  port.CodeGenerator
	baseURL    string
}

func NewLinkCreateUseCase(
	repository port.LinkRepository,
	generator port.CodeGenerator,
	cfg config.Config,
) *LinkCreateUseCase {
	return &LinkCreateUseCase{
		repository: repository,
		generator:  generator,
		baseURL:    strings.TrimRight(cfg.PublicBaseURL, "/"),
	}
}

func (uc *LinkCreateUseCase) Execute(ctx context.Context, input LinkCreateInput) (LinkCreateOutput, error) {
	if !uc.validDestination(input.Destination) {
		return LinkCreateOutput{}, errs.ErrInvalidInput
	}
	if input.ExpiresAt != nil {
		expiration := input.ExpiresAt.UTC().Truncate(time.Microsecond)
		if !expiration.After(time.Now()) {
			return LinkCreateOutput{}, errs.ErrInvalidInput
		}
		input.ExpiresAt = &expiration
	}
	if input.Alias != nil && !uc.validAlias(*input.Alias) {
		return LinkCreateOutput{}, errs.ErrInvalidInput
	}
	for range maxCodeAttempts {
		var code string
		if input.Alias != nil {
			code = *input.Alias
		} else {
			var generateErr error
			code, generateErr = uc.generator.Generate()
			if generateErr != nil {
				return LinkCreateOutput{}, generateErr
			}
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

func (uc *LinkCreateUseCase) validDestination(destination string) bool {
	u, err := url.Parse(destination)
	return len(destination) <= 4096 && err == nil && u != nil && (u.Scheme == "http" || u.Scheme == "https") &&
		u.Hostname() != "" && u.User == nil
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
