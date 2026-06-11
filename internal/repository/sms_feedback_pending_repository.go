package repository

import (
	"context"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SmsFeedbackPendingRepository interface {
	Create(ctx context.Context, record *models.SmsFeedbackPending) error
	FindDue(ctx context.Context) ([]models.SmsFeedbackPending, error)
	Update(ctx context.Context, record *models.SmsFeedbackPending) error
	SetTemplateCode(ctx context.Context, incidentID uuid.UUID, templateCode, language string) error
	// HasWhatsAppFeedback returns true if the incident received feedback via the
	// WhatsApp chatbot (transition_history_id IS NULL) after the given time.
	HasWhatsAppFeedback(ctx context.Context, incidentID uuid.UUID, since time.Time) (bool, error)
}

type smsFeedbackPendingRepository struct {
	db *gorm.DB
}

func NewSmsFeedbackPendingRepository(db *gorm.DB) SmsFeedbackPendingRepository {
	return &smsFeedbackPendingRepository{db: db}
}

func (r *smsFeedbackPendingRepository) Create(ctx context.Context, record *models.SmsFeedbackPending) error {
	return r.db.WithContext(ctx).Create(record).Error
}

// FindDue returns all pending records whose scheduled time has passed and have not yet been
// sent, skipped, or exceeded the max retry threshold.
func (r *smsFeedbackPendingRepository) FindDue(ctx context.Context) ([]models.SmsFeedbackPending, error) {
	var records []models.SmsFeedbackPending
	err := r.db.WithContext(ctx).
		Where("scheduled_at <= ? AND sent = false AND skipped = false AND retry_count < 3", time.Now()).
		Find(&records).Error
	return records, err
}

func (r *smsFeedbackPendingRepository) Update(ctx context.Context, record *models.SmsFeedbackPending) error {
	return r.db.WithContext(ctx).Save(record).Error
}

func (r *smsFeedbackPendingRepository) SetTemplateCode(ctx context.Context, incidentID uuid.UUID, templateCode, language string) error {
	return r.db.WithContext(ctx).Model(&models.SmsFeedbackPending{}).
		Where("incident_id = ? AND sent = false AND skipped = false", incidentID).
		Updates(map[string]interface{}{"template_code": templateCode, "language": language}).Error
}

// HasWhatsAppFeedback checks the incident_feedbacks table for a row submitted by the
// WhatsApp chatbot. Chatbot submissions have no linked transition history (transition_history_id IS NULL).
func (r *smsFeedbackPendingRepository) HasWhatsAppFeedback(ctx context.Context, incidentID uuid.UUID, since time.Time) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Table("incident_feedbacks").
		Where("incident_id = ? AND transition_history_id IS NULL AND created_at >= ?", incidentID, since).
		Count(&count).Error
	return count > 0, err
}
