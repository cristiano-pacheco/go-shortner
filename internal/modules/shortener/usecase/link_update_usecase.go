package usecase

import (
	"context"
	"strings"
	"time"

	bricksvalidator "github.com/cristiano-pacheco/bricks/pkg/validator"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/model"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
)

type LinkUpdateInput struct {
	ID           int64   `validate:"gt=0"`
	Destination  *string `validate:"omitempty,required"`
	Active       *bool
	SetExpiresAt bool
	ExpiresAt    *time.Time `validate:"omitempty,gt"`
}

type LinkUpdateOutput struct {
	ID          int64
	Code        string
	Destination string
	ShortURL    string
	Active      bool
	ExpiresAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type LinkUpdateUseCase struct {
	validator            bricksvalidator.Validator
	linkRepository       port.LinkRepository
	redirectCache        port.RedirectCache
	destinationValidator port.DestinationValidator
	baseURL              string
}

func NewLinkUpdateUseCase(
	linkRepository port.LinkRepository,
	redirectCache port.RedirectCache,
	destinationValidator port.DestinationValidator,
	validator bricksvalidator.Validator,
	cfg config.Config,
) *LinkUpdateUseCase {
	return &LinkUpdateUseCase{
		validator:            validator,
		linkRepository:       linkRepository,
		redirectCache:        redirectCache,
		destinationValidator: destinationValidator,
		baseURL:              strings.TrimRight(cfg.PublicBaseURL, "/"),
	}
}

func (uc *LinkUpdateUseCase) Execute(ctx context.Context, input LinkUpdateInput) (LinkUpdateOutput, error) {
	if err := uc.validator.Validate(input); err != nil {
		return LinkUpdateOutput{}, err
	}

	if input.Destination != nil {
		if err := uc.destinationValidator.Validate(*input.Destination); err != nil {
			return LinkUpdateOutput{}, err
		}
	}

	if input.SetExpiresAt && input.ExpiresAt != nil {
		expiration := input.ExpiresAt.UTC().Truncate(time.Microsecond)
		if !expiration.After(time.Now()) {
			return LinkUpdateOutput{}, errs.ErrInvalidInput
		}

		input.ExpiresAt = &expiration
	}

	link, err := uc.linkRepository.Update(
		ctx,
		input.ID,
		model.LinkChanges{
			Destination:  input.Destination,
			Active:       input.Active,
			SetExpiresAt: input.SetExpiresAt,
			ExpiresAt:    input.ExpiresAt,
		},
	)
	if err != nil {
		return LinkUpdateOutput{}, err
	}

	_ = uc.redirectCache.Delete(ctx, link.Code)

	return LinkUpdateOutput{
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
