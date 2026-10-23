package repository

import (
	"context"
	"errors"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ConfigRepository interface {
	Create(ctx context.Context, cfg *models.Config) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.Config, error)
	FindByEnvModuleName(ctx context.Context, env, module, name string) (*models.Config, error)
	// Exists reports whether (environment, module, name) is taken, without logging a "record not found" warning.
	Exists(ctx context.Context, env, module, name string) (bool, error)
	List(ctx context.Context, filter *models.ConfigListFilter) ([]models.Config, int64, error)
	// Update saves cfg only if the stored version equals expectedVersion, bumping the version.
	Update(ctx context.Context, cfg *models.Config, expectedVersion int) error
	Delete(ctx context.Context, id uuid.UUID) error
	// CreateIfMissing inserts rows whose (environment, module, name) does not exist yet and leaves existing rows untouched.
	CreateIfMissing(ctx context.Context, cfgs []models.Config) error
}

type configRepository struct {
	db *gorm.DB
}

func NewConfigRepository(db *gorm.DB) ConfigRepository {
	return &configRepository{db: db}
}

func (r *configRepository) Create(ctx context.Context, cfg *models.Config) error {
	return r.db.WithContext(ctx).Create(cfg).Error
}

func (r *configRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.Config, error) {
	var cfg models.Config
	if err := r.db.WithContext(ctx).First(&cfg, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, models.ErrConfigNotFound
		}
		return nil, err
	}
	return &cfg, nil
}

func (r *configRepository) FindByEnvModuleName(ctx context.Context, env, module, name string) (*models.Config, error) {
	var cfg models.Config
	if err := r.db.WithContext(ctx).Where("environment = ? AND module = ? AND name = ?", env, module, name).First(&cfg).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, models.ErrConfigNotFound
		}
		return nil, err
	}
	return &cfg, nil
}

func (r *configRepository) Exists(ctx context.Context, env, module, name string) (bool, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&models.Config{}).
		Where("environment = ? AND module = ? AND name = ?", env, module, name).
		Limit(1).Count(&n).Error
	return n > 0, err
}

func (r *configRepository) List(ctx context.Context, filter *models.ConfigListFilter) ([]models.Config, int64, error) {
	q := r.db.WithContext(ctx).Model(&models.Config{})
	if filter.Environment != "" {
		q = q.Where("environment = ?", filter.Environment)
	}
	if filter.Module != "" {
		q = q.Where("module = ?", filter.Module)
	}
	if filter.Search != "" {
		like := "%" + filter.Search + "%"
		q = q.Where("name ILIKE ? OR module ILIKE ?", like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var cfgs []models.Config
	q = q.Order("environment ASC, module ASC, name ASC")
	if filter.Limit > 0 {
		q = q.Limit(filter.Limit).Offset((filter.Page - 1) * filter.Limit)
	}
	if err := q.Find(&cfgs).Error; err != nil {
		return nil, 0, err
	}
	return cfgs, total, nil
}

func (r *configRepository) Update(ctx context.Context, cfg *models.Config, expectedVersion int) error {
	res := r.db.WithContext(ctx).Model(&models.Config{}).
		Where("id = ? AND version = ?", cfg.ID, expectedVersion).
		Updates(map[string]any{
			"value":       cfg.Value,
			"value_type":  cfg.ValueType,
			"description": cfg.Description,
			"is_secret":   cfg.IsSecret,
			"version":     gorm.Expr("version + 1"),
			"updated_at":  gorm.Expr("now()"),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return models.ErrConfigVersionConflict
	}
	cfg.Version = expectedVersion + 1
	return nil
}

func (r *configRepository) Delete(ctx context.Context, id uuid.UUID) error {
	res := r.db.WithContext(ctx).Delete(&models.Config{}, "id = ?", id)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return models.ErrConfigNotFound
	}
	return nil
}

func (r *configRepository) CreateIfMissing(ctx context.Context, cfgs []models.Config) error {
	if len(cfgs) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "environment"}, {Name: "module"}, {Name: "name"}},
			DoNothing: true,
		}).
		Create(&cfgs).Error
}
