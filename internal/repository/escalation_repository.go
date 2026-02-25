package repository

import (
	"context"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EscalationRepository interface {
	// Create stores a new SLA breach notification record
	Create(ctx context.Context, e *models.EscalationSLA) error

	// GetAll returns all breach notification records (most recent first)
	GetAll(ctx context.Context) ([]models.EscalationSLA, error)

	// GetByIncidentID returns all breach notifications for a specific incident
	GetByIncidentID(ctx context.Context, incidentID uuid.UUID) ([]models.EscalationSLA, error)

	// HasBeenNotified checks whether a user was already notified for the given incident+state
	// combination (prevents duplicate notifications within the same SLA breach period)
	HasBeenNotified(ctx context.Context, incidentID, stateID, userID uuid.UUID) (bool, error)

	// HasGlobalBreachNotification checks whether a global SLA deadline breach notification
	// (state_id IS NULL) has already been sent to the given user for this incident.
	HasGlobalBreachNotification(ctx context.Context, incidentID, userID uuid.UUID) (bool, error)

	// HasBatchNotificationToday checks whether a batch SLA grouped summary notification
	// (incident_id IS NULL AND state_id IS NULL) was already sent to this user today.
	// Used to prevent duplicate daily summary notifications.
	HasBatchNotificationToday(ctx context.Context, userID uuid.UUID) (bool, error)
}

type escalationRepository struct {
	db *gorm.DB
}

func NewEscalationRepository(db *gorm.DB) EscalationRepository {
	return &escalationRepository{db: db}
}

func (r *escalationRepository) Create(ctx context.Context, e *models.EscalationSLA) error {
	return r.db.WithContext(ctx).Create(e).Error
}

func (r *escalationRepository) GetAll(ctx context.Context) ([]models.EscalationSLA, error) {
	var records []models.EscalationSLA
	err := r.db.WithContext(ctx).
		Preload("Incident").
		Preload("State").
		Preload("Transition").
		Preload("NotifiedUser").
		Order("notified_at DESC").
		Find(&records).Error
	return records, err
}

func (r *escalationRepository) GetByIncidentID(ctx context.Context, incidentID uuid.UUID) ([]models.EscalationSLA, error) {
	var records []models.EscalationSLA
	err := r.db.WithContext(ctx).
		Preload("Incident").
		Preload("State").
		Preload("Transition").
		Preload("NotifiedUser").
		Where("incident_id = ?", incidentID).
		Order("notified_at DESC NULLS LAST").
		Find(&records).Error
	return records, err
}

func (r *escalationRepository) HasBeenNotified(ctx context.Context, incidentID, stateID, userID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.EscalationSLA{}).
		Where("incident_id = ? AND state_id = ? AND notified_user_id = ?",
			incidentID, stateID, userID).
		Count(&count).Error
	return count > 0, err
}

func (r *escalationRepository) HasGlobalBreachNotification(ctx context.Context, incidentID, userID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.EscalationSLA{}).
		Where("incident_id = ? AND state_id IS NULL AND notified_user_id = ?", incidentID, userID).
		Count(&count).Error
	return count > 0, err
}

// HasBatchNotificationToday returns true when a batch grouped summary
// (incident_id IS NULL AND state_id IS NULL) was already sent to this user
// today (on or after the current calendar day's midnight).
func (r *escalationRepository) HasBatchNotificationToday(ctx context.Context, userID uuid.UUID) (bool, error) {
	var count int64
	err := r.db.WithContext(ctx).
		Model(&models.EscalationSLA{}).
		Where("incident_id IS NULL AND state_id IS NULL AND notified_user_id = ? AND notified_at >= CURRENT_DATE", userID).
		Count(&count).Error
	return count > 0, err
}
