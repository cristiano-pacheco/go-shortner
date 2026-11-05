package usecase

import (
	"context"
	"time"

	"github.com/cristiano-pacheco/bricks/pkg/otel/trace"
	"github.com/cristiano-pacheco/bricks/pkg/validator"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/metrics"
)

type LinkDeleteInput struct {
	ID int64 `validate:"gt=0"`
}

type LinkDeleteOutput struct{}

type LinkDeleteUseCase struct {
	applicationMetrics *metrics.Metrics
	validator          validator.Validator
	linkRepository     port.LinkRepository
	redirectCache      port.RedirectCache
}

func NewLinkDeleteUseCase(
	linkRepository port.LinkRepository,
	redirectCache port.RedirectCache,
	validator validator.Validator,
	applicationMetrics *metrics.Metrics,
) *LinkDeleteUseCase {
	return &LinkDeleteUseCase{
		linkRepository:     linkRepository,
		redirectCache:      redirectCache,
		validator:          validator,
		applicationMetrics: applicationMetrics,
	}
}

func (uc *LinkDeleteUseCase) Execute(ctx context.Context, input LinkDeleteInput) (LinkDeleteOutput, error) {
	ctx, span := trace.Span(ctx, "LinkDeleteUseCase.Execute")
	defer span.End()

	defer uc.applicationMetrics.ObserveDeleteDuration(time.Now())

	if err := uc.validator.Validate(input); err != nil {
		uc.applicationMetrics.IncrementDeleteError(errs.MetricCategory(err))
		return LinkDeleteOutput{}, errs.ValidationError(err)
	}

	link, err := uc.linkRepository.Delete(ctx, input.ID)
	if err != nil {
		uc.applicationMetrics.IncrementDeleteError(errs.MetricCategory(err))
		return LinkDeleteOutput{}, err
	}

	if cacheErr := uc.redirectCache.Delete(ctx, link.Code); cacheErr != nil {
		uc.applicationMetrics.IncrementCacheFailure("invalidation")
	}

	uc.applicationMetrics.IncrementDeleteSuccess()

	return LinkDeleteOutput{}, nil
}
