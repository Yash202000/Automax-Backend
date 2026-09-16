package repository

import (
	"context"
	"errors"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type NasaqVerificationRepository interface {
	Create(ctx context.Context, v *models.NasaqVerification) error
	// GetLatestByIncident returns the most recent verification attempt for an
	// incident, or (nil, nil) when none exists yet.
	GetLatestByIncident(ctx context.Context, incidentID uuid.UUID) (*models.NasaqVerification, error)
	GetHistoryByIncident(ctx context.Context, incidentID uuid.UUID) ([]models.NasaqVerification, error)
}

type nasaqVerificationRepository struct {
	db *gorm.DB
}

func NewNasaqVerificationRepository(db *gorm.DB) NasaqVerificationRepository {
	return &nasaqVerificationRepository{db: db}
}

func (r *nasaqVerificationRepository) Create(ctx context.Context, v *models.NasaqVerification) error {
	return r.db.WithContext(ctx).Create(v).Error
}

func (r *nasaqVerificationRepository) GetLatestByIncident(ctx context.Context, incidentID uuid.UUID) (*models.NasaqVerification, error) {
	var v models.NasaqVerification
	err := r.db.WithContext(ctx).
		Where("incident_id = ?", incidentID).
		Order("created_at DESC").
		First(&v).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *nasaqVerificationRepository) GetHistoryByIncident(ctx context.Context, incidentID uuid.UUID) ([]models.NasaqVerification, error) {
	var list []models.NasaqVerification
	err := r.db.WithContext(ctx).
		Where("incident_id = ?", incidentID).
		Order("created_at DESC").
		Find(&list).Error
	return list, err
}
