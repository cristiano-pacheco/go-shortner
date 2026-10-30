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

func (r *LinkRepository) Create(ctx context.Context, link model.LinkModel) (model.LinkModel, error) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	link.ID, link.CreatedAt, link.UpdatedAt, link.DeletedAt = 0, now, now, nil
	err := gorm.G[model.LinkModel](r.resources.DB).Create(ctx, &link)
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return model.LinkModel{}, errs.ErrCodeConflict
	}
	if err != nil {
		return model.LinkModel{}, err
	}
	return link, nil
}

func (r *LinkRepository) FindByID(ctx context.Context, id int64) (model.LinkModel, error) {
	link, err := gorm.G[model.LinkModel](r.resources.DB).Where("id = ? AND deleted_at IS NULL", id).Limit(1).First(ctx)
	return r.found(link, err)
}

func (r *LinkRepository) FindByCode(ctx context.Context, code string) (model.LinkModel, error) {
	link, err := gorm.G[model.LinkModel](r.resources.DB).Where("code = ? AND deleted_at IS NULL", code).Limit(1).First(ctx)
	return r.found(link, err)
}

func (r *LinkRepository) List(ctx context.Context, limit, offset int) ([]model.LinkModel, int64, error) {
	db := r.resources.DB.WithContext(ctx).Model(&model.LinkModel{}).Where("deleted_at IS NULL")
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	links := make([]model.LinkModel, 0)
	if err := db.Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&links).Error; err != nil {
		return nil, 0, err
	}
	return links, total, nil
}

func (r *LinkRepository) Update(ctx context.Context, id int64, changes model.LinkChanges) (model.LinkModel, error) {
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

func (r *LinkRepository) Delete(ctx context.Context, id int64) (model.LinkModel, error) {
	now := time.Now().UTC()
	return r.mutate(ctx, id, map[string]any{"deleted_at": now, "updated_at": now})
}

// PostgreSQL RETURNING obtains the written row in the same atomic statement.
func (r *LinkRepository) mutate(ctx context.Context, id int64, values map[string]any) (model.LinkModel, error) {
	var link model.LinkModel
	result := r.resources.DB.WithContext(ctx).Model(&link).Clauses(clause.Returning{}).
		Where("id = ? AND deleted_at IS NULL", id).Updates(values)
	if result.Error != nil {
		return model.LinkModel{}, result.Error
	}
	if result.RowsAffected == 0 {
		return model.LinkModel{}, errs.ErrLinkNotFound
	}
	return link, nil
}

func (r *LinkRepository) found(link model.LinkModel, err error) (model.LinkModel, error) {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.LinkModel{}, errs.ErrLinkNotFound
	}
	if err != nil {
		return model.LinkModel{}, err
	}
	return link, nil
}
