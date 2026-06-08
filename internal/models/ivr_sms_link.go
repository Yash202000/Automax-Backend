package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// IvrSmsLink tracks every IVR SMS link sent for an incident.
// Only the most recently sent link is active (IsActive = true).
// SubmittedAt is set when the citizen completes the IVR form.
type IvrSmsLink struct {
	ID         uuid.UUID `gorm:"type:uuid;primary_key"    json:"id"`
	IncidentID uuid.UUID `gorm:"type:uuid;index;not null" json:"incident_id"`

	// sha256 hex of the raw signed token — allows exact lookup without storing the full token.
	TokenHash string `gorm:"size:64;index;not null" json:"token_hash"`

	SentBy    *uuid.UUID `gorm:"type:uuid;index" json:"sent_by"`
	SentAt    time.Time  `gorm:"not null"        json:"sent_at"`
	ExpiresAt time.Time  `gorm:"not null"        json:"expires_at"`

	// IsActive is false when a newer link has been sent for the same incident.
	IsActive bool `gorm:"default:true;index" json:"is_active"`

	// Set when the citizen successfully submits additional information.
	SubmittedAt      *time.Time `json:"submitted_at"`
	SubmittedByPhone string     `gorm:"size:50" json:"submitted_by_phone"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (l *IvrSmsLink) BeforeCreate(tx *gorm.DB) error {
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	return nil
}
