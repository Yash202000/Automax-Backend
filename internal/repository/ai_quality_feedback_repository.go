package repository

import (
	"context"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type AIQualityFeedbackRepository interface {
	// Create persists a new AIQualityFeedback record.
	Create(ctx context.Context, feedback *models.AIQualityFeedback) error

	// ExistsForIncident returns true when a feedback record already exists for the given incident.
	ExistsForIncident(ctx context.Context, incidentID uuid.UUID) (bool, error)

	// FindPendingIncidents returns incidents that are AI-verified and whose current workflow state
	// is marked as AI QA required, but do not yet have an AIQualityFeedback record.
	FindPendingIncidents(ctx context.Context) ([]models.Incident, error)
}

type aiQualityFeedbackRepository struct {
	db *gorm.DB
}

func NewAIQualityFeedbackRepository(db *gorm.DB) AIQualityFeedbackRepository {
	return &aiQualityFeedbackRepository{db: db}
}

func (r *aiQualityFeedbackRepository) Create(ctx context.Context, feedback *models.AIQualityFeedback) error {
	return r.db.WithContext(ctx).Create(feedback).Error
}

func (r *aiQualityFeedbackRepository) ExistsForIncident(ctx context.Context, incidentID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.AIQualityFeedback{}).
		Where("incident_id = ?", incidentID).
		Count(&count).Error
	return count > 0, err
}

// FindPendingIncidents fetches incidents that satisfy both conditions:
//   - is_ai_verified = true on the incident
//   - is_ai_qa = true on the incident's current workflow state
//
// and excludes incidents that already have an AIQualityFeedback record.
// Attachments are preloaded ordered oldest-first so index 0 = before image, last = after image.
// Comments are preloaded ordered newest-first so index 0 = the latest resolver comment.
func (r *aiQualityFeedbackRepository) FindPendingIncidents(ctx context.Context) ([]models.Incident, error) {
	var incidents []models.Incident
	err := r.db.WithContext(ctx).Debug().
		Preload("CurrentState").
		Preload("Attachments", func(db *gorm.DB) *gorm.DB {
			return db.Order("created_at ASC")
		}).
		Preload("Comments", func(db *gorm.DB) *gorm.DB {
			return db.Order("created_at DESC")
		}).
		Joins("JOIN workflow_states ON workflow_states.id = incidents.current_state_id").
		Where("incidents.is_ai_verified = ? AND workflow_states.is_ai_qa = ?", false, true).
		Where("incidents.deleted_at IS NULL AND workflow_states.deleted_at IS NULL").
		Where("NOT EXISTS (SELECT 1 FROM ai_quality_feedbacks WHERE ai_quality_feedbacks.incident_id = incidents.id AND ai_quality_feedbacks.deleted_at IS NULL)").
		Find(&incidents).Error
	return incidents, err
}
