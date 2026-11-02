package usecase

import (
	"context"
	"strings"
	"time"

	bricksvalidator "github.com/cristiano-pacheco/bricks/pkg/validator"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/metrics"
)

type LinkGetInput struct {
	ID int64 `validate:"gt=0"`
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
	applicationMetrics *metrics.Metrics
	validator          bricksvalidator.Validator
	linkRepository     port.LinkRepository
	baseURL            string
}

func NewLinkGetUseCase(
	linkRepository port.LinkRepository,
	cfg config.Config,
	validator bricksvalidator.Validator,
	applicationMetrics *metrics.Metrics,
) *LinkGetUseCase {
	return &LinkGetUseCase{
		applicationMetrics: applicationMetrics,
		linkRepository:     linkRepository,
		baseURL:            strings.TrimRight(cfg.PublicBaseURL, "/"),
		validator:          validator,
	}
}

func (uc *LinkGetUseCase) Execute(ctx context.Context, input LinkGetInput) (LinkGetOutput, error) {
	defer uc.applicationMetrics.ObserveGetDuration(time.Now())

	if err := uc.validator.Validate(input); err != nil {
		uc.applicationMetrics.IncrementGetError(errs.MetricCategory(err))
		return LinkGetOutput{}, err
	}

	link, err := uc.linkRepository.FindByID(ctx, input.ID)
	if err != nil {
		uc.applicationMetrics.IncrementGetError(errs.MetricCategory(err))
		return LinkGetOutput{}, err
	}

	uc.applicationMetrics.IncrementGetSuccess()

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
