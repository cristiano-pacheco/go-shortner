package repository

import (
	"context"
	"errors"
	"time"

	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/errs"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/model"
	"github.com/cristiano-pacheco/go-shortner/internal/modules/shortener/port"
	"github.com/cristiano-pacheco/go-shortner/internal/shared/infrastructure"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type LinkRepository struct{ resources *infrastructure.Resources }

var _ port.LinkRepository = (*LinkRepository)(nil)

func NewLinkRepository(resources *infrastructure.Resources) *LinkRepository {
	return &LinkRepository{resources: resources}
}

func (r *LinkRepository) Create(ctx context.Context, link model.Link) (model.Link, error) {
	now := time.Now().UTC()
	link.ID, link.CreatedAt, link.UpdatedAt, link.DeletedAt = 0, now, now, nil
	err := gorm.G[model.Link](r.resources.DB).Create(ctx, &link)
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return model.Link{}, errs.ErrCodeConflict
	}
	if err != nil {
		return model.Link{}, err
	}
	return link, nil
}

func (r *LinkRepository) FindByID(ctx context.Context, id int64) (model.Link, error) {
	link, err := gorm.G[model.Link](r.resources.DB).Where("id = ? AND deleted_at IS NULL", id).Limit(1).First(ctx)
	return found(link, err)
}

func (r *LinkRepository) FindByCode(ctx context.Context, code string) (model.Link, error) {
	link, err := gorm.G[model.Link](r.resources.DB).Where("code = ? AND deleted_at IS NULL", code).Limit(1).First(ctx)
	return found(link, err)
}

func (r *LinkRepository) List(ctx context.Context, limit, offset int) ([]model.Link, int64, error) {
	db := r.resources.DB.WithContext(ctx).Model(&model.Link{}).Where("deleted_at IS NULL")
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	links := make([]model.Link, 0)
	if err := db.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&links).Error; err != nil {
		return nil, 0, err
	}
	return links, total, nil
}

func (r *LinkRepository) Update(ctx context.Context, id int64, changes model.LinkChanges) (model.Link, error) {
	values := map[string]any{"updated_at": time.Now().UTC()}
	if changes.Destination != nil {
		values["destination"] = *changes.Destination
	}
	if changes.Active != nil {
		values["active"] = *changes.Active
	}
	if changes.SetExpiresAt {
		values["expires_at"] = changes.ExpiresAt
	}
	return r.mutate(ctx, id, values)
}

func (r *LinkRepository) Delete(ctx context.Context, id int64) (model.Link, error) {
	now := time.Now().UTC()
	return r.mutate(ctx, id, map[string]any{"deleted_at": now, "updated_at": now})
}

// PostgreSQL RETURNING obtains the written row in the same atomic statement.
func (r *LinkRepository) mutate(ctx context.Context, id int64, values map[string]any) (model.Link, error) {
	var link model.Link
	result := r.resources.DB.WithContext(ctx).Model(&link).Clauses(clause.Returning{}).
		Where("id = ? AND deleted_at IS NULL", id).Updates(values)
	if result.Error != nil {
		return model.Link{}, result.Error
	}
	if result.RowsAffected == 0 {
		return model.Link{}, errs.ErrLinkNotFound
	}
	return link, nil
}

func found(link model.Link, err error) (model.Link, error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Link{}, errs.ErrLinkNotFound
	}
	if err != nil {
		return model.Link{}, err
	}
	return link, nil
}
