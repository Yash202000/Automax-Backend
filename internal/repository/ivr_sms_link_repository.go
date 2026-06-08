package repository

import (
	"context"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type IvrSmsLinkRepository interface {
	Create(ctx context.Context, link *models.IvrSmsLink) error
	// FindByTokenHash returns the link matching the sha256 hex of the signed token for this incident.
	FindByTokenHash(ctx context.Context, tokenHash string, incidentID uuid.UUID) (*models.IvrSmsLink, error)
	// DeactivateAllForIncident marks every active link for an incident as inactive before sending a new one.
	DeactivateAllForIncident(ctx context.Context, incidentID uuid.UUID) error
	// FindLatestActiveByIncidentID returns the most-recently created active link for an incident.
	FindLatestActiveByIncidentID(ctx context.Context, incidentID uuid.UUID) (*models.IvrSmsLink, error)
	// MarkSubmitted sets SubmittedAt and SubmittedByPhone on a link record.
	MarkSubmitted(ctx context.Context, id uuid.UUID, phone string) error
	// FindSubmittedByIncidentIDs returns a map of incidentID → submitted IvrSmsLink for the given IDs.
	// Only incidents that have a submitted link (submitted_at IS NOT NULL) are included.
	FindSubmittedByIncidentIDs(ctx context.Context, incidentIDs []uuid.UUID) (map[uuid.UUID]*models.IvrSmsLink, error)
}

type ivrSmsLinkRepository struct {
	db *gorm.DB
}

func NewIvrSmsLinkRepository(db *gorm.DB) IvrSmsLinkRepository {
	return &ivrSmsLinkRepository{db: db}
}

func (r *ivrSmsLinkRepository) Create(ctx context.Context, link *models.IvrSmsLink) error {
	return r.db.WithContext(ctx).Create(link).Error
}

func (r *ivrSmsLinkRepository) FindByTokenHash(ctx context.Context, tokenHash string, incidentID uuid.UUID) (*models.IvrSmsLink, error) {
	var link models.IvrSmsLink
	err := r.db.WithContext(ctx).
		Where("token_hash = ? AND incident_id = ?", tokenHash, incidentID).
		First(&link).Error
	if err != nil {
		return nil, err
	}
	return &link, nil
}

func (r *ivrSmsLinkRepository) DeactivateAllForIncident(ctx context.Context, incidentID uuid.UUID) error {
	return r.db.WithContext(ctx).
		Model(&models.IvrSmsLink{}).
		Where("incident_id = ? AND is_active = true", incidentID).
		Update("is_active", false).Error
}

func (r *ivrSmsLinkRepository) FindLatestActiveByIncidentID(ctx context.Context, incidentID uuid.UUID) (*models.IvrSmsLink, error) {
	var link models.IvrSmsLink
	err := r.db.WithContext(ctx).
		Where("incident_id = ? AND is_active = true", incidentID).
		Order("created_at DESC").
		First(&link).Error
	if err != nil {
		return nil, err
	}
	return &link, nil
}

func (r *ivrSmsLinkRepository) MarkSubmitted(ctx context.Context, id uuid.UUID, phone string) error {
	now := time.Now()
	return r.db.WithContext(ctx).
		Model(&models.IvrSmsLink{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"submitted_at":       now,
			"submitted_by_phone": phone,
		}).Error
}

func (r *ivrSmsLinkRepository) FindSubmittedByIncidentIDs(ctx context.Context, incidentIDs []uuid.UUID) (map[uuid.UUID]*models.IvrSmsLink, error) {
	if len(incidentIDs) == 0 {
		return map[uuid.UUID]*models.IvrSmsLink{}, nil
	}

	var links []models.IvrSmsLink
	err := r.db.WithContext(ctx).
		Where("incident_id IN ? AND submitted_at IS NOT NULL", incidentIDs).
		Order("incident_id, submitted_at DESC").
		Find(&links).Error
	if err != nil {
		return nil, err
	}

	// Keep the most-recent submitted link per incident.
	result := make(map[uuid.UUID]*models.IvrSmsLink, len(links))
	for i := range links {
		if _, exists := result[links[i].IncidentID]; !exists {
			result[links[i].IncidentID] = &links[i]
		}
	}
	return result, nil
}
