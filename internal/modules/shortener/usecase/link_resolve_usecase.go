package usecase

import (
	"context"
	"time"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
)

type LinkResolveInput struct {
	Code string
}

type LinkResolveOutput struct {
	Destination string
}

type LinkResolveUseCase struct {
	linkRepository port.LinkRepository
}

func NewLinkResolveUseCase(linkRepository port.LinkRepository) *LinkResolveUseCase {
	return &LinkResolveUseCase{linkRepository: linkRepository}
}

func (uc *LinkResolveUseCase) Execute(ctx context.Context, input LinkResolveInput) (LinkResolveOutput, error) {
	if input.Code == "" || input.Code == "api" || input.Code == "health" {
		return LinkResolveOutput{}, errs.ErrLinkNotFound
	}

	link, err := uc.linkRepository.FindByCode(ctx, input.Code)
	if err != nil {
		return LinkResolveOutput{}, err
	}

	if !link.Active || (link.ExpiresAt != nil && !link.ExpiresAt.After(time.Now().UTC())) {
		return LinkResolveOutput{}, errs.ErrLinkNotFound
	}

	return LinkResolveOutput{Destination: link.Destination}, nil
}
