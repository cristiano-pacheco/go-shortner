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

func NewLinkCreateUseCase(repository port.LinkRepository, generator port.CodeGenerator, cfg config.Config) *LinkCreateUseCase {
	return &LinkCreateUseCase{repository: repository, generator: generator, baseURL: strings.TrimRight(cfg.PublicBaseURL, "/")}
}

func (uc *LinkCreateUseCase) Execute(ctx context.Context, input LinkCreateInput) (LinkCreateOutput, error) {
	u, err := url.Parse(input.Destination)
	if len(input.Destination) > 4096 || err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil {
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
			code, err = uc.generator.Generate()
			if err != nil {
				return LinkCreateOutput{}, err
			}
		}
		link, err := uc.repository.Create(ctx, model.LinkModel{Code: code, Destination: input.Destination, Active: true, ExpiresAt: input.ExpiresAt})
		if err != nil {
			if errors.Is(err, errs.ErrCodeConflict) && input.Alias == nil {
				continue
			}
			return LinkCreateOutput{}, err
		}
		return LinkCreateOutput{ID: link.ID, Code: link.Code, Destination: link.Destination, ShortURL: uc.baseURL + "/" + link.Code, Active: link.Active, ExpiresAt: link.ExpiresAt, CreatedAt: link.CreatedAt, UpdatedAt: link.UpdatedAt}, nil
	}
	return LinkCreateOutput{}, errors.New("automatic code generation exhausted retries")
}

func (uc *LinkCreateUseCase) validAlias(alias string) bool {
	if len(alias) < 3 || len(alias) > 32 || alias == "api" || alias == "health" {
		return false
	}
	for _, c := range alias {
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_') {
			return false
		}
	}
	return true
}
