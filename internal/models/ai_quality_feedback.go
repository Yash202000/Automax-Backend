package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AIQualityFeedback stores the result of an AI quality check for an incident.
// Created by the AIQualityMonitor when it processes an incident that has
// is_ai_verified=true and its current workflow state has is_ai_qa=true.
type AIQualityFeedback struct {
	ID         uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	IncidentID uuid.UUID `gorm:"type:uuid;uniqueIndex;not null" json:"incident_id"`
	Incident   *Incident `gorm:"foreignKey:IncidentID" json:"incident,omitempty"`

	// ChangedSummary holds a textual summary of what the AI determined changed in this incident.
	ChangedSummary string `gorm:"type:text" json:"changed_summary"`

	// ResolutionStatus is the AI-assessed resolution status of the incident.
	ResolutionStatus string `gorm:"size:100" json:"resolution_status"`

	// DistanceMeters is the geographic distance (in metres) reported by the AI analysis.
	DistanceMeters float64 `gorm:"type:decimal(12,4)" json:"distance_meters"`

	// RawResponse stores the full JSON response from the AI quality API for auditing.
	RawResponse json.RawMessage `gorm:"type:jsonb" json:"raw_response,omitempty"`

	// IsReopened is set to true when a QA agent clicks "Reopen" from the Quality Audit page.
	// Reset to false automatically when the incident returns to a QA state (closed/ready_to_close).
	IsReopened bool `gorm:"column:is_reopened;default:false" json:"is_reopened"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (f *AIQualityFeedback) BeforeCreate(tx *gorm.DB) error {
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	return nil
}
