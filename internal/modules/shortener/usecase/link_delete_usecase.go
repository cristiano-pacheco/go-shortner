package usecase

import (
	"context"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
)

type LinkDeleteInput struct {
	ID int64
}

type LinkDeleteOutput struct{}

type LinkDeleteUseCase struct {
	linkRepository port.LinkRepository
	redirectCache  port.RedirectCache
}

func NewLinkDeleteUseCase(linkRepository port.LinkRepository, redirectCache port.RedirectCache) *LinkDeleteUseCase {
	return &LinkDeleteUseCase{linkRepository: linkRepository, redirectCache: redirectCache}
}

func (uc *LinkDeleteUseCase) Execute(ctx context.Context, input LinkDeleteInput) (LinkDeleteOutput, error) {
	if input.ID <= 0 {
		return LinkDeleteOutput{}, errs.ErrInvalidInput
	}

	link, err := uc.linkRepository.Delete(ctx, input.ID)
	if err != nil {
		return LinkDeleteOutput{}, err
	}

	_ = uc.redirectCache.Delete(ctx, link.Code)

	return LinkDeleteOutput{}, nil
}
