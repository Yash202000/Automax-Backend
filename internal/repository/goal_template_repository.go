package repository

import (
	"context"
	"fmt"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type GoalTemplateRepository interface {
	Create(ctx context.Context, template *models.GoalTemplate) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.GoalTemplate, error)
	List(ctx context.Context, filter *models.GoalTemplateFilter) ([]models.GoalTemplate, int64, error)
	ListActive(ctx context.Context) ([]models.GoalTemplate, error)
	Update(ctx context.Context, template *models.GoalTemplate) error
	Delete(ctx context.Context, id uuid.UUID) error
}

type goalTemplateRepository struct {
	db *gorm.DB
}

func NewGoalTemplateRepository(db *gorm.DB) GoalTemplateRepository {
	return &goalTemplateRepository{db: db}
}

func (r *goalTemplateRepository) Create(ctx context.Context, template *models.GoalTemplate) error {
	return r.db.WithContext(ctx).Create(template).Error
}

func (r *goalTemplateRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.GoalTemplate, error) {
	var template models.GoalTemplate
	err := r.db.WithContext(ctx).
		Preload("CreatedBy").
		First(&template, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &template, nil
}

func (r *goalTemplateRepository) List(ctx context.Context, filter *models.GoalTemplateFilter) ([]models.GoalTemplate, int64, error) {
	var templates []models.GoalTemplate
	var total int64

	query := r.db.WithContext(ctx).Model(&models.GoalTemplate{})

	if filter.Search != "" {
		searchPattern := "%" + filter.Search + "%"
		query = query.Where("name ILIKE ? OR description ILIKE ?", searchPattern, searchPattern)
	}
	if filter.IsActive != nil {
		query = query.Where("is_active = ?", *filter.IsActive)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := filter.Page
	limit := filter.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	err := query.
		Preload("CreatedBy").
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&templates).Error
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list goal templates: %w", err)
	}

	return templates, total, nil
}

func (r *goalTemplateRepository) ListActive(ctx context.Context) ([]models.GoalTemplate, error) {
	var templates []models.GoalTemplate
	err := r.db.WithContext(ctx).
		Where("is_active = ?", true).
		Order("name ASC").
		Find(&templates).Error
	return templates, err
}

func (r *goalTemplateRepository) Update(ctx context.Context, template *models.GoalTemplate) error {
	return r.db.WithContext(ctx).Save(template).Error
}

func (r *goalTemplateRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.GoalTemplate{}, "id = ?", id).Error
}
