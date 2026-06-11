package repository

import (
	"context"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type IncidentPublicFeedbackRepository interface {
	Create(ctx context.Context, f *models.IncidentPublicFeedback) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.IncidentPublicFeedback, error)
	FindByIncidentID(ctx context.Context, incidentID uuid.UUID) ([]models.IncidentPublicFeedback, error)
	// FindLatestPendingByIncidentID returns the most-recently created feedback record
	// for the incident that has not yet been submitted (submitted_at IS NULL).
	FindLatestPendingByIncidentID(ctx context.Context, incidentID uuid.UUID) (*models.IncidentPublicFeedback, error)
	// FindLatestByIncidentID returns the most-recently created feedback record
	// regardless of submitted status.
	FindLatestByIncidentID(ctx context.Context, incidentID uuid.UUID) (*models.IncidentPublicFeedback, error)
	// HasSubmittedFeedback returns true if a public (SMS/WhatsApp) feedback record
	// has already been submitted (submitted_at IS NOT NULL) for this incident.
	HasSubmittedFeedback(ctx context.Context, incidentID uuid.UUID) (bool, error)
	List(ctx context.Context) ([]models.IncidentPublicFeedback, error)
	Update(ctx context.Context, f *models.IncidentPublicFeedback) error
}

type incidentPublicFeedbackRepository struct {
	db *gorm.DB
}

func NewIncidentPublicFeedbackRepository(db *gorm.DB) IncidentPublicFeedbackRepository {
	return &incidentPublicFeedbackRepository{db: db}
}

func (r *incidentPublicFeedbackRepository) Create(ctx context.Context, f *models.IncidentPublicFeedback) error {
	return r.db.WithContext(ctx).Create(f).Error
}

func (r *incidentPublicFeedbackRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.IncidentPublicFeedback, error) {
	var f models.IncidentPublicFeedback
	if err := r.db.WithContext(ctx).First(&f, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *incidentPublicFeedbackRepository) FindByIncidentID(ctx context.Context, incidentID uuid.UUID) ([]models.IncidentPublicFeedback, error) {
	var items []models.IncidentPublicFeedback
	err := r.db.WithContext(ctx).
		Where("incident_id = ?", incidentID).
		Order("created_at DESC").
		Find(&items).Error
	return items, err
}

func (r *incidentPublicFeedbackRepository) FindLatestPendingByIncidentID(ctx context.Context, incidentID uuid.UUID) (*models.IncidentPublicFeedback, error) {
	var f models.IncidentPublicFeedback
	err := r.db.WithContext(ctx).
		Where("incident_id = ? AND submitted_at IS NULL", incidentID).
		Order("created_at DESC").
		First(&f).Error
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *incidentPublicFeedbackRepository) FindLatestByIncidentID(ctx context.Context, incidentID uuid.UUID) (*models.IncidentPublicFeedback, error) {
	var f models.IncidentPublicFeedback
	err := r.db.WithContext(ctx).
		Where("incident_id = ?", incidentID).
		Order("created_at DESC").
		First(&f).Error
	if err != nil {
		return nil, err
	}
	return &f, nil
}

func (r *incidentPublicFeedbackRepository) List(ctx context.Context) ([]models.IncidentPublicFeedback, error) {
	var items []models.IncidentPublicFeedback
	err := r.db.WithContext(ctx).
		Order("created_at DESC").
		Find(&items).Error
	return items, err
}

func (r *incidentPublicFeedbackRepository) HasSubmittedFeedback(ctx context.Context, incidentID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.IncidentPublicFeedback{}).
		Where("incident_id = ? AND submitted_at IS NOT NULL", incidentID).
		Count(&count).Error
	return count > 0, err
}

func (r *incidentPublicFeedbackRepository) Update(ctx context.Context, f *models.IncidentPublicFeedback) error {
	return r.db.WithContext(ctx).Save(f).Error
}
