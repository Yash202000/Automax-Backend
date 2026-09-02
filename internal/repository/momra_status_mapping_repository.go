package repository

import (
	"context"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type MOMRAStatusMappingRepository interface {
	Create(ctx context.Context, m *models.MOMRAStatusMapping) error
	Update(ctx context.Context, m *models.MOMRAStatusMapping) error
	Delete(ctx context.Context, id uuid.UUID) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.MOMRAStatusMapping, error)
	// FindActiveByState returns the active mapping for a given workflow state, or
	// gorm.ErrRecordNotFound if none is configured — callers must treat a missing
	// mapping as a loggable gap, not a silent no-op (Story A acceptance criterion).
	FindActiveByState(ctx context.Context, stateID uuid.UUID) (*models.MOMRAStatusMapping, error)
	ListByWorkflow(ctx context.Context, workflowID uuid.UUID) ([]models.MOMRAStatusMapping, error)
}

type momraStatusMappingRepository struct {
	db *gorm.DB
}

func NewMOMRAStatusMappingRepository(db *gorm.DB) MOMRAStatusMappingRepository {
	return &momraStatusMappingRepository{db: db}
}

func (r *momraStatusMappingRepository) Create(ctx context.Context, m *models.MOMRAStatusMapping) error {
	return r.db.WithContext(ctx).Create(m).Error
}

func (r *momraStatusMappingRepository) Update(ctx context.Context, m *models.MOMRAStatusMapping) error {
	return r.db.WithContext(ctx).Save(m).Error
}

func (r *momraStatusMappingRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.MOMRAStatusMapping{}, "id = ?", id).Error
}

func (r *momraStatusMappingRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.MOMRAStatusMapping, error) {
	var m models.MOMRAStatusMapping
	if err := r.db.WithContext(ctx).First(&m, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *momraStatusMappingRepository) FindActiveByState(ctx context.Context, stateID uuid.UUID) (*models.MOMRAStatusMapping, error) {
	var m models.MOMRAStatusMapping
	if err := r.db.WithContext(ctx).First(&m, "state_id = ? AND is_active = ?", stateID, true).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

func (r *momraStatusMappingRepository) ListByWorkflow(ctx context.Context, workflowID uuid.UUID) ([]models.MOMRAStatusMapping, error) {
	var list []models.MOMRAStatusMapping
	err := r.db.WithContext(ctx).Preload("State").Where("workflow_id = ?", workflowID).Order("created_at").Find(&list).Error
	return list, err
}
