package usecase

import (
	"context"
	"time"

	bricksvalidator "github.com/cristiano-pacheco/bricks/pkg/validator"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
)

const redirectSnapshotLifetime = 60 * time.Second

type LinkResolveInput struct {
	Code string `validate:"required"`
}

type LinkResolveOutput struct {
	Destination string
}

type LinkResolveUseCase struct {
	validator      bricksvalidator.Validator
	linkRepository port.LinkRepository
	redirectCache  port.RedirectCache
}

func NewLinkResolveUseCase(
	linkRepository port.LinkRepository,
	redirectCache port.RedirectCache,
	validator bricksvalidator.Validator,
) *LinkResolveUseCase {
	return &LinkResolveUseCase{linkRepository: linkRepository, redirectCache: redirectCache, validator: validator}
}

func (uc *LinkResolveUseCase) Execute(ctx context.Context, input LinkResolveInput) (LinkResolveOutput, error) {
	if err := uc.validator.Validate(input); err != nil {
		return LinkResolveOutput{}, err
	}

	if input.Code == "api" || input.Code == "health" {
		return LinkResolveOutput{}, errs.ErrLinkNotFound
	}

	if snapshot, ok := uc.cachedSnapshot(ctx, input.Code); ok {
		return LinkResolveOutput{Destination: snapshot.Destination}, nil
	}

	for {
		output, retry, err := uc.resolveFromDatabase(ctx, input.Code)
		if !retry {
			return output, err
		}
	}
}

func (uc *LinkResolveUseCase) cachedSnapshot(ctx context.Context, code string) (port.RedirectSnapshot, bool) {
	snapshot, hit, err := uc.redirectCache.Get(ctx, code)

	now := time.Now().UTC()
	if err != nil || !hit || !snapshot.FreshUntil.After(now) || !snapshot.Active ||
		snapshot.Destination == "" || (snapshot.ExpiresAt != nil && !snapshot.ExpiresAt.After(now)) {
		return port.RedirectSnapshot{}, false
	}

	return snapshot, true
}

func (uc *LinkResolveUseCase) resolveFromDatabase(ctx context.Context, code string) (LinkResolveOutput, bool, error) {
	// Start the lifetime before the database read, not after a potentially delayed fill.
	freshUntil := time.Now().UTC().Add(redirectSnapshotLifetime)

	link, err := uc.linkRepository.FindByCode(ctx, code)
	if err != nil {
		return LinkResolveOutput{}, false, err
	}

	if !freshUntil.After(time.Now().UTC()) {
		return LinkResolveOutput{}, ctx.Err() == nil, ctx.Err()
	}

	if !link.Active || (link.ExpiresAt != nil && !link.ExpiresAt.After(time.Now().UTC())) {
		return LinkResolveOutput{}, false, errs.ErrLinkNotFound
	}

	_ = uc.redirectCache.Set(ctx, code, port.RedirectSnapshot{
		Destination: link.Destination,
		Active:      link.Active,
		ExpiresAt:   link.ExpiresAt,
		FreshUntil:  freshUntil,
	})

	now := time.Now().UTC()
	if !freshUntil.After(now) {
		return LinkResolveOutput{}, ctx.Err() == nil, ctx.Err()
	}

	if link.ExpiresAt != nil && !link.ExpiresAt.After(now) {
		return LinkResolveOutput{}, false, errs.ErrLinkNotFound
	}

	return LinkResolveOutput{Destination: link.Destination}, false, nil
}
