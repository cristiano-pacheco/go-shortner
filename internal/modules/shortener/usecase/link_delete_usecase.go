package usecase

import (
	"context"

	bricksvalidator "github.com/cristiano-pacheco/bricks/pkg/validator"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
)

type LinkDeleteInput struct {
	ID int64 `validate:"gt=0"`
}

type LinkDeleteOutput struct{}

type LinkDeleteUseCase struct {
	validator      bricksvalidator.Validator
	linkRepository port.LinkRepository
	redirectCache  port.RedirectCache
}

func NewLinkDeleteUseCase(
	linkRepository port.LinkRepository,
	redirectCache port.RedirectCache,
	validator bricksvalidator.Validator,
) *LinkDeleteUseCase {
	return &LinkDeleteUseCase{linkRepository: linkRepository, redirectCache: redirectCache, validator: validator}
}

func (uc *LinkDeleteUseCase) Execute(ctx context.Context, input LinkDeleteInput) (LinkDeleteOutput, error) {
	if err := uc.validator.Validate(input); err != nil {
		return LinkDeleteOutput{}, err
	}

	link, err := uc.linkRepository.Delete(ctx, input.ID)
	if err != nil {
		return LinkDeleteOutput{}, err
	}

	_ = uc.redirectCache.Delete(ctx, link.Code)

	return LinkDeleteOutput{}, nil
}
