package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/model"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
)

type LinkUpdateInput struct {
	ID           int64
	Destination  *string
	Active       *bool
	SetExpiresAt bool
	ExpiresAt    *time.Time
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
	linkRepository       port.LinkRepository
	redirectCache        port.RedirectCache
	destinationValidator port.DestinationValidator
	baseURL              string
}

func NewLinkUpdateUseCase(
	linkRepository port.LinkRepository,
	redirectCache port.RedirectCache,
	destinationValidator port.DestinationValidator,
	cfg config.Config,
) *LinkUpdateUseCase {
	return &LinkUpdateUseCase{
		linkRepository:       linkRepository,
		redirectCache:        redirectCache,
		destinationValidator: destinationValidator,
		baseURL:              strings.TrimRight(cfg.PublicBaseURL, "/"),
	}
}

func (uc *LinkUpdateUseCase) Execute(ctx context.Context, input LinkUpdateInput) (LinkUpdateOutput, error) {
	if input.ID <= 0 {
		return LinkUpdateOutput{}, errs.ErrInvalidInput
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
