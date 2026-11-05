package usecase

import (
	"context"
	"math"
	"strings"
	"time"

	"github.com/cristiano-pacheco/bricks/pkg/otel/trace"
	"github.com/cristiano-pacheco/bricks/pkg/validator"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/metrics"
)

type LinkListInput struct {
	Page     int `validate:"gte=1"`
	PageSize int `validate:"gte=1,lte=100"`
}

type LinkListItem struct {
	ID          int64
	Code        string
	Destination string
	ShortURL    string
	Active      bool
	ExpiresAt   *time.Time
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

type LinkListOutput struct {
	Items    []LinkListItem
	Page     int
	PageSize int
	Total    int64
}

type LinkListUseCase struct {
	applicationMetrics *metrics.Metrics
	validator          validator.Validator
	linkRepository     port.LinkRepository
	baseURL            string
}

func NewLinkListUseCase(
	linkRepository port.LinkRepository,
	cfg config.Config,
	validator validator.Validator,
	applicationMetrics *metrics.Metrics,
) *LinkListUseCase {
	return &LinkListUseCase{
		applicationMetrics: applicationMetrics,
		linkRepository:     linkRepository,
		baseURL:            strings.TrimRight(cfg.PublicBaseURL, "/"),
		validator:          validator,
	}
}

func (uc *LinkListUseCase) Execute(ctx context.Context, input LinkListInput) (LinkListOutput, error) {
	ctx, span := trace.Span(ctx, "LinkListUseCase.Execute")
	defer span.End()

	defer uc.applicationMetrics.ObserveListDuration(time.Now())

	if err := uc.validator.Validate(input); err != nil {
		uc.applicationMetrics.IncrementListError(errs.MetricCategory(err))
		return LinkListOutput{}, errs.ValidationError(err)
	}

	if input.Page-1 > math.MaxInt/input.PageSize {
		uc.applicationMetrics.IncrementListError(errs.MetricCategory(errs.ErrPaginationOverflow))
		return LinkListOutput{}, errs.ErrPaginationOverflow
	}

	links, total, err := uc.linkRepository.List(ctx, input.PageSize, (input.Page-1)*input.PageSize)
	if err != nil {
		uc.applicationMetrics.IncrementListError(errs.MetricCategory(err))
		return LinkListOutput{}, err
	}

	items := make([]LinkListItem, 0, len(links))
	for _, link := range links {
		items = append(items, LinkListItem{
			ID: link.ID, Code: link.Code, Destination: link.Destination,
			ShortURL: uc.baseURL + "/" + link.Code, Active: link.Active,
			ExpiresAt: link.ExpiresAt, CreatedAt: link.CreatedAt, UpdatedAt: link.UpdatedAt,
		})
	}

	uc.applicationMetrics.IncrementListSuccess()

	return LinkListOutput{Items: items, Page: input.Page, PageSize: input.PageSize, Total: total}, nil
}
