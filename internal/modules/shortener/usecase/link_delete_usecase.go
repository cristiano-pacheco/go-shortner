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
}

func NewLinkDeleteUseCase(linkRepository port.LinkRepository) *LinkDeleteUseCase {
	return &LinkDeleteUseCase{linkRepository: linkRepository}
}

func (uc *LinkDeleteUseCase) Execute(ctx context.Context, input LinkDeleteInput) (LinkDeleteOutput, error) {
	if input.ID <= 0 {
		return LinkDeleteOutput{}, errs.ErrInvalidInput
	}

	if _, err := uc.linkRepository.Delete(ctx, input.ID); err != nil {
		return LinkDeleteOutput{}, err
	}

	return LinkDeleteOutput{}, nil
}
