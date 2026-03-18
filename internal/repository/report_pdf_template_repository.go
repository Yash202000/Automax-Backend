package repository

import (
	"context"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ReportPdfTemplateRepository interface {
	Create(ctx context.Context, template *models.ReportPdfTemplate) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.ReportPdfTemplate, error)
	FindByIDWithRelations(ctx context.Context, id uuid.UUID) (*models.ReportPdfTemplate, error)
	List(ctx context.Context, filter *models.ReportPdfTemplateFilter) ([]models.ReportPdfTemplate, int64, error)
	Update(ctx context.Context, template *models.ReportPdfTemplate) error
	Delete(ctx context.Context, id uuid.UUID) error
	SetDefault(ctx context.Context, id uuid.UUID) error
	GetDefault(ctx context.Context) (*models.ReportPdfTemplate, error)
}

type reportPdfTemplateRepository struct {
	db *gorm.DB
}

func NewReportPdfTemplateRepository(db *gorm.DB) ReportPdfTemplateRepository {
	return &reportPdfTemplateRepository{db: db}
}

func (r *reportPdfTemplateRepository) Create(ctx context.Context, template *models.ReportPdfTemplate) error {
	return r.db.WithContext(ctx).Create(template).Error
}

func (r *reportPdfTemplateRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.ReportPdfTemplate, error) {
	var template models.ReportPdfTemplate
	err := r.db.WithContext(ctx).First(&template, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &template, nil
}

func (r *reportPdfTemplateRepository) FindByIDWithRelations(ctx context.Context, id uuid.UUID) (*models.ReportPdfTemplate, error) {
	var template models.ReportPdfTemplate
	err := r.db.WithContext(ctx).
		Preload("CreatedBy").
		First(&template, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &template, nil
}

func (r *reportPdfTemplateRepository) List(ctx context.Context, filter *models.ReportPdfTemplateFilter) ([]models.ReportPdfTemplate, int64, error) {
	var templates []models.ReportPdfTemplate
	var total int64

	query := r.db.WithContext(ctx).Model(&models.ReportPdfTemplate{})

	if filter.Search != "" {
		searchPattern := "%" + filter.Search + "%"
		query = query.Where("name ILIKE ? OR description ILIKE ?", searchPattern, searchPattern)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page := filter.Page
	if page < 1 {
		page = 1
	}
	limit := filter.Limit
	if limit < 1 || limit > 100 {
		limit = 20
	}
	offset := (page - 1) * limit

	err := query.
		Preload("CreatedBy").
		Offset(offset).
		Limit(limit).
		Order("created_at DESC").
		Find(&templates).Error
	if err != nil {
		return nil, 0, err
	}

	return templates, total, nil
}

func (r *reportPdfTemplateRepository) Update(ctx context.Context, template *models.ReportPdfTemplate) error {
	return r.db.WithContext(ctx).Save(template).Error
}

func (r *reportPdfTemplateRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.ReportPdfTemplate{}, "id = ?", id).Error
}

func (r *reportPdfTemplateRepository) SetDefault(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.ReportPdfTemplate{}).
			Where("is_default = ?", true).
			Update("is_default", false).Error; err != nil {
			return err
		}
		return tx.Model(&models.ReportPdfTemplate{}).
			Where("id = ?", id).
			Update("is_default", true).Error
	})
}

func (r *reportPdfTemplateRepository) GetDefault(ctx context.Context) (*models.ReportPdfTemplate, error) {
	var template models.ReportPdfTemplate
	err := r.db.WithContext(ctx).
		Preload("CreatedBy").
		First(&template, "is_default = ?", true).Error
	if err != nil {
		return nil, err
	}
	return &template, nil
}
