package usecase

import (
	"context"
	"math"
	"strings"
	"time"

	bricksvalidator "github.com/cristiano-pacheco/bricks/pkg/validator"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/config"
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
	validator      bricksvalidator.Validator
	linkRepository port.LinkRepository
	baseURL        string
}

func NewLinkListUseCase(
	linkRepository port.LinkRepository,
	cfg config.Config,
	validator bricksvalidator.Validator,
) *LinkListUseCase {
	return &LinkListUseCase{
		linkRepository: linkRepository,
		baseURL:        strings.TrimRight(cfg.PublicBaseURL, "/"),
		validator:      validator,
	}
}

func (uc *LinkListUseCase) Execute(ctx context.Context, input LinkListInput) (LinkListOutput, error) {
	if err := uc.validator.Validate(input); err != nil {
		return LinkListOutput{}, err
	}

	if input.Page-1 > math.MaxInt/input.PageSize {
		return LinkListOutput{}, errs.ErrInvalidInput
	}

	links, total, err := uc.linkRepository.List(ctx, input.PageSize, (input.Page-1)*input.PageSize)
	if err != nil {
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

	return LinkListOutput{Items: items, Page: input.Page, PageSize: input.PageSize, Total: total}, nil
}
