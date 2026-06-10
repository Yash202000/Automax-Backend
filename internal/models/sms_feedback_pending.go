package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SmsFeedbackPending tracks SMS feedback that should be sent after a delay
// if no WhatsApp chatbot response is received within the configured window.
// Created when a final-close transition fires; processed by the SLA monitor.
type SmsFeedbackPending struct {
	ID         uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	IncidentID uuid.UUID `gorm:"type:uuid;index;not null" json:"incident_id"`
	// FeedbackID is the IncidentPublicFeedback record pre-created at final close.
	FeedbackID  uuid.UUID `gorm:"type:uuid;not null" json:"feedback_id"`
	MobileNo    string    `gorm:"size:50;not null" json:"mobile_no"`
	ClosedAt    time.Time `gorm:"not null" json:"closed_at"`    // when the final-close happened
	ScheduledAt time.Time `gorm:"not null" json:"scheduled_at"` // ClosedAt + delay hours

	Sent        bool       `gorm:"default:false" json:"sent"`
	Skipped     bool       `gorm:"default:false" json:"skipped"` // true when WhatsApp responded
	RetryCount  int        `gorm:"default:0" json:"retry_count"`
	ProcessedAt *time.Time `json:"processed_at"`
	Log         string     `gorm:"type:text" json:"log"` // reason: sent / skipped / error

	CreatedAt time.Time      `json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (s *SmsFeedbackPending) BeforeCreate(tx *gorm.DB) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}
