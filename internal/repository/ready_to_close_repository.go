package repository

import (
	"context"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ReadyToCloseRepository handles persistence for IncidentReadyToCloseEntry records.
type ReadyToCloseRepository interface {
	// CreateEntry persists a new Ready-to-Close entry.
	CreateEntry(ctx context.Context, entry *models.IncidentReadyToCloseEntry) error

	// GetActiveEntry returns the currently active entry for an incident, or nil if none.
	GetActiveEntry(ctx context.Context, incidentID uuid.UUID) (*models.IncidentReadyToCloseEntry, error)

	// GetExpiredActiveEntries returns all active entries whose ExpiresAt is in the past.
	GetExpiredActiveEntries(ctx context.Context) ([]models.IncidentReadyToCloseEntry, error)

	// GetPreExpiryEntries returns active entries whose ExpiresAt is within [now, now+hours]
	// and for which no expiry notification has been sent yet.
	GetPreExpiryEntries(ctx context.Context, hours int) ([]models.IncidentReadyToCloseEntry, error)

	// DeactivateEntry marks an entry as inactive (incident left Ready-to-Close state).
	DeactivateEntry(ctx context.Context, entryID uuid.UUID) error

	// DeactivateForIncident deactivates all active entries for an incident.
	DeactivateForIncident(ctx context.Context, incidentID uuid.UUID) error

	// MarkExpiryNotified records the time the pre-expiry notification was sent.
	MarkExpiryNotified(ctx context.Context, entryID uuid.UUID) error
}

type readyToCloseRepository struct {
	db *gorm.DB
}

func NewReadyToCloseRepository(db *gorm.DB) ReadyToCloseRepository {
	return &readyToCloseRepository{db: db}
}

func (r *readyToCloseRepository) CreateEntry(ctx context.Context, entry *models.IncidentReadyToCloseEntry) error {
	return r.db.WithContext(ctx).Create(entry).Error
}

func (r *readyToCloseRepository) GetActiveEntry(ctx context.Context, incidentID uuid.UUID) (*models.IncidentReadyToCloseEntry, error) {
	var entry models.IncidentReadyToCloseEntry
	err := r.db.WithContext(ctx).
		Where("incident_id = ? AND is_active = true", incidentID).
		Order("created_at DESC").
		First(&entry).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &entry, nil
}

func (r *readyToCloseRepository) GetExpiredActiveEntries(ctx context.Context) ([]models.IncidentReadyToCloseEntry, error) {
	var entries []models.IncidentReadyToCloseEntry
	err := r.db.WithContext(ctx).
		Where("is_active = true AND expires_at <= ?", time.Now()).
		Find(&entries).Error
	return entries, err
}

func (r *readyToCloseRepository) GetPreExpiryEntries(ctx context.Context, hours int) ([]models.IncidentReadyToCloseEntry, error) {
	var entries []models.IncidentReadyToCloseEntry
	threshold := time.Now().Add(time.Duration(hours) * time.Hour)
	err := r.db.WithContext(ctx).
		Preload("EnteredBy").
		Where("is_active = true AND expires_at > ? AND expires_at <= ? AND expiry_notification_sent_at IS NULL",
			time.Now(), threshold).
		Find(&entries).Error
	return entries, err
}

func (r *readyToCloseRepository) DeactivateEntry(ctx context.Context, entryID uuid.UUID) error {
	return r.db.WithContext(ctx).
		Model(&models.IncidentReadyToCloseEntry{}).
		Where("id = ?", entryID).
		Update("is_active", false).Error
}

func (r *readyToCloseRepository) DeactivateForIncident(ctx context.Context, incidentID uuid.UUID) error {
	return r.db.WithContext(ctx).
		Model(&models.IncidentReadyToCloseEntry{}).
		Where("incident_id = ? AND is_active = true", incidentID).
		Update("is_active", false).Error
}

func (r *readyToCloseRepository) MarkExpiryNotified(ctx context.Context, entryID uuid.UUID) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&models.IncidentReadyToCloseEntry{}).
		Where("id = ?", entryID).
		Update("expiry_notification_sent_at", now).Error
}
