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
	Create(context.Context, model.Link) (model.Link, error)
	FindByID(context.Context, int64) (model.Link, error)
	FindByCode(context.Context, string) (model.Link, error)
	List(context.Context, int, int) ([]model.Link, int64, error)
	Update(context.Context, int64, model.LinkChanges) (model.Link, error)
	Delete(context.Context, int64) (model.Link, error)
}
