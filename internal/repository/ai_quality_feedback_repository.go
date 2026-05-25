package repository

import (
	"context"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AIQualityFeedbackRepository interface {
	// Create persists a new AIQualityFeedback record.
	Create(ctx context.Context, feedback *models.AIQualityFeedback) error

	// ExistsForIncident returns true when a feedback record already exists for the given incident.
	ExistsForIncident(ctx context.Context, incidentID uuid.UUID) (bool, error)

	// FindByIncidentID retrieves the AIQualityFeedback record for the given incident, if any.
	FindByIncidentID(ctx context.Context, incidentID uuid.UUID) (*models.AIQualityFeedback, error)

	// ListAllWithIncident returns all AI quality feedback records ordered newest-first,
	// with incident, current state, classification, and department preloaded.
	ListAllWithIncident(ctx context.Context) ([]models.AIQualityFeedback, error)

	// FindPendingIncidents returns incidents that are AI-verified and whose current workflow state
	// is marked as AI QA required, but do not yet have an AIQualityFeedback record.
	FindPendingIncidents(ctx context.Context) ([]models.Incident, error)

	// SetReopened updates the is_reopened flag on the feedback record for the given incident.
	SetReopened(ctx context.Context, incidentID uuid.UUID, value bool) error
}

type aiQualityFeedbackRepository struct {
	db *gorm.DB
}

func NewAIQualityFeedbackRepository(db *gorm.DB) AIQualityFeedbackRepository {
	return &aiQualityFeedbackRepository{db: db}
}

func (r *aiQualityFeedbackRepository) Create(ctx context.Context, feedback *models.AIQualityFeedback) error {
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "incident_id"}},
			DoUpdates: clause.Assignments(map[string]interface{}{
				"changed_summary":   feedback.ChangedSummary,
				"resolution_status": feedback.ResolutionStatus,
				"distance_meters":   feedback.DistanceMeters,
				"raw_response":      feedback.RawResponse,
				"is_reopened":       false,
				"deleted_at":        nil,
				"updated_at":        time.Now(),
			}),
		}).
		Create(feedback).Error
}

func (r *aiQualityFeedbackRepository) ListAllWithIncident(ctx context.Context) ([]models.AIQualityFeedback, error) {
	var feedbacks []models.AIQualityFeedback
	err := r.db.WithContext(ctx).
		Preload("Incident.CurrentState").
		Preload("Incident.Classification").
		Preload("Incident.Department").
		Preload("Incident.Assignees").
		Order("ai_quality_feedbacks.created_at DESC").
		Find(&feedbacks).Error
	return feedbacks, err
}

func (r *aiQualityFeedbackRepository) FindByIncidentID(ctx context.Context, incidentID uuid.UUID) (*models.AIQualityFeedback, error) {
	var feedback models.AIQualityFeedback
	err := r.db.WithContext(ctx).
		Where("incident_id = ?", incidentID).
		First(&feedback).Error
	if err != nil {
		return nil, err
	}
	return &feedback, nil
}

func (r *aiQualityFeedbackRepository) ExistsForIncident(ctx context.Context, incidentID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.AIQualityFeedback{}).
		Where("incident_id = ?", incidentID).
		Count(&count).Error
	return count > 0, err
}

func (r *aiQualityFeedbackRepository) SetReopened(ctx context.Context, incidentID uuid.UUID, value bool) error {
	return r.db.WithContext(ctx).
		Model(&models.AIQualityFeedback{}).
		Where("incident_id = ?", incidentID).
		Update("is_reopened", value).Error
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
		// Where("NOT EXISTS (SELECT 1 FROM ai_quality_feedbacks WHERE ai_quality_feedbacks.incident_id = incidents.id AND ai_quality_feedbacks.deleted_at IS NULL)").
		Find(&incidents).Error
	return incidents, err
}
