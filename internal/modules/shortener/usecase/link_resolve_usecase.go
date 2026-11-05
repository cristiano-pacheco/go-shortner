package usecase

import (
	"context"
	"errors"
	"time"

	"github.com/cristiano-pacheco/bricks/pkg/otel/trace"
	"github.com/cristiano-pacheco/bricks/pkg/validator"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/metrics"
)

const redirectSnapshotLifetime = 60 * time.Second

type LinkResolveInput struct {
	Code string `validate:"required"`
}

type LinkResolveOutput struct {
	Destination string
}

type LinkResolveUseCase struct {
	applicationMetrics *metrics.Metrics
	validator          validator.Validator
	linkRepository     port.LinkRepository
	redirectCache      port.RedirectCache
}

func NewLinkResolveUseCase(
	linkRepository port.LinkRepository,
	redirectCache port.RedirectCache,
	validator validator.Validator,
	applicationMetrics *metrics.Metrics,
) *LinkResolveUseCase {
	return &LinkResolveUseCase{
		linkRepository:     linkRepository,
		redirectCache:      redirectCache,
		validator:          validator,
		applicationMetrics: applicationMetrics,
	}
}

func (uc *LinkResolveUseCase) Execute(
	ctx context.Context,
	input LinkResolveInput,
) (LinkResolveOutput, error) {
	ctx, span := trace.Span(ctx, "LinkResolveUseCase.Execute")
	defer span.End()

	defer uc.applicationMetrics.ObserveResolveDuration(time.Now())

	if err := uc.validator.Validate(input); err != nil {
		uc.applicationMetrics.IncrementResolveError(errs.MetricCategory(err))
		return LinkResolveOutput{}, errs.ValidationError(err)
	}

	if input.Code == "api" || input.Code == "health" {
		uc.applicationMetrics.IncrementResolveError(errs.MetricCategory(errs.ErrLinkNotFound))
		return LinkResolveOutput{}, errs.ErrLinkNotFound
	}

	if snapshot, ok := uc.cachedSnapshot(ctx, input.Code); ok {
		uc.applicationMetrics.IncrementResolveSuccess()
		uc.applicationMetrics.IncrementResolutionSource("cache")

		return LinkResolveOutput{Destination: snapshot.Destination}, nil
	}

	for {
		output, retry, err := uc.resolveFromDatabase(ctx, input.Code)
		if !retry {
			if err != nil {
				uc.applicationMetrics.IncrementResolveError(errs.MetricCategory(err))
				return output, err
			}

			uc.applicationMetrics.IncrementResolveSuccess()
			uc.applicationMetrics.IncrementResolutionSource("database")

			return output, nil
		}

		uc.applicationMetrics.IncrementFreshnessRetry()
	}
}

func (uc *LinkResolveUseCase) cachedSnapshot(ctx context.Context, code string) (port.RedirectSnapshot, bool) {
	snapshot, hit, err := uc.redirectCache.Get(ctx, code)

	// Precedence is invalid encoding, error, miss, stale, inactive, invalid, expired, hit.
	decision := "hit"

	now := time.Now().UTC()
	switch {
	case errors.Is(err, errs.ErrInvalidSnapshot):
		decision = "invalid"
	case err != nil:
		decision = "error"
	case !hit:
		decision = "miss"
	case !snapshot.FreshUntil.After(now):
		decision = "stale"
	case !snapshot.Active:
		decision = "inactive"
	case snapshot.Destination == "":
		decision = "invalid"
	case snapshot.ExpiresAt != nil && !snapshot.ExpiresAt.After(now):
		decision = "expired"
	}

	uc.applicationMetrics.IncrementCacheDecision(decision)

	if decision != "hit" {
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

	err = uc.redirectCache.Set(ctx, code, port.RedirectSnapshot{
		Destination: link.Destination,
		Active:      link.Active,
		ExpiresAt:   link.ExpiresAt,
		FreshUntil:  freshUntil,
	})
	if err != nil {
		uc.applicationMetrics.IncrementCacheFailure("fill")
	}

	now := time.Now().UTC()
	if !freshUntil.After(now) {
		return LinkResolveOutput{}, ctx.Err() == nil, ctx.Err()
	}

	if link.ExpiresAt != nil && !link.ExpiresAt.After(now) {
		return LinkResolveOutput{}, false, errs.ErrLinkNotFound
	}

	return LinkResolveOutput{Destination: link.Destination}, false, nil
}
