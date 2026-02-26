package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// IncidentReadyToCloseEntry tracks each time an incident enters a "Ready to Close" state.
// It records the selected duration, expiry time, and notification status for audit and automation.
type IncidentReadyToCloseEntry struct {
	ID         uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	IncidentID uuid.UUID `gorm:"type:uuid;index;not null" json:"incident_id"`
	Incident   *Incident `gorm:"foreignKey:IncidentID" json:"incident,omitempty"`

	// EnteredByID is the user who triggered the transition to Ready to Close.
	EnteredByID uuid.UUID `gorm:"type:uuid;index;not null" json:"entered_by_id"`
	EnteredBy   *User     `gorm:"foreignKey:EnteredByID" json:"entered_by,omitempty"`

	// EnteredAt is when the incident transitioned into the Ready to Close state.
	EnteredAt time.Time `gorm:"not null;index" json:"entered_at"`

	// ExpiresAt is when the incident will automatically revert if not closed.
	ExpiresAt time.Time `gorm:"not null;index" json:"expires_at"`

	// Duration is the human-readable label chosen by the user (e.g. "1 Week").
	Duration string `gorm:"size:100;not null" json:"duration"`

	// Comment is the optional note provided by the user on transition.
	Comment string `gorm:"type:text" json:"comment"`

	// IsActive indicates this entry is still in effect (incident still in Ready to Close).
	// Set to false when the incident is closed, manually reverted, or auto-reverted.
	IsActive bool `gorm:"default:true;index" json:"is_active"`

	// ExpiryNotificationSentAt records when the pre-expiry warning notification was sent.
	// Null means the notification has not been sent yet.
	ExpiryNotificationSentAt *time.Time `json:"expiry_notification_sent_at"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (e *IncidentReadyToCloseEntry) BeforeCreate(tx *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}
