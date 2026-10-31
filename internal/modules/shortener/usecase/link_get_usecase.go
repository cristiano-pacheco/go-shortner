package usecase

import (
	"context"
	"strings"
	"time"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
)

type LinkGetInput struct {
	ID int64
}

type LinkGetOutput struct {
	ID          int64
	Code        string
	Destination string
	ShortURL    string
	Active      bool
	ExpiresAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type LinkGetUseCase struct {
	linkRepository port.LinkRepository
	baseURL        string
}

func NewLinkGetUseCase(linkRepository port.LinkRepository, cfg config.Config) *LinkGetUseCase {
	return &LinkGetUseCase{linkRepository: linkRepository, baseURL: strings.TrimRight(cfg.PublicBaseURL, "/")}
}

func (uc *LinkGetUseCase) Execute(ctx context.Context, input LinkGetInput) (LinkGetOutput, error) {
	if input.ID <= 0 {
		return LinkGetOutput{}, errs.ErrInvalidInput
	}

	link, err := uc.linkRepository.FindByID(ctx, input.ID)
	if err != nil {
		return LinkGetOutput{}, err
	}

	return LinkGetOutput{
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
