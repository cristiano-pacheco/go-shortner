package port

import (
	"context"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/model"
)

// LinkRepository excludes deleted links from reads and mutations. Missing links
// return errs.ErrLinkNotFound; duplicate codes return errs.ErrCodeConflict.
// List orders by creation time and ID descending. Count and page are separate
// reads and need not reflect a single snapshot under concurrent writes.
type LinkRepository interface {
	Create(context.Context, model.LinkModel) (model.LinkModel, error)
	FindByID(context.Context, int64) (model.LinkModel, error)
	FindByCode(context.Context, string) (model.LinkModel, error)
	List(context.Context, int, int) ([]model.LinkModel, int64, error)
	Update(context.Context, int64, model.LinkChanges) (model.LinkModel, error)
	Delete(context.Context, int64) (model.LinkModel, error)
}
