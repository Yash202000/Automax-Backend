package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// IncidentPublicFeedback is a two-phase feedback record: created by staff (Phase 1),
// submitted by the public via a signed URL (Phase 2).
type IncidentPublicFeedback struct {
	ID         uuid.UUID `gorm:"type:uuid;primary_key"    json:"id"`
	IncidentID uuid.UUID `gorm:"type:uuid;index;not null" json:"incident_id"`

	MobileNo  string  `gorm:"size:50"   json:"mobile_no"`
	Satisfied *bool   `gorm:"type:bool" json:"satisfied"` // nil until Phase 2
	Comment   *string `gorm:"type:text" json:"comment"`   // nil until Phase 2

	Source string `gorm:"size:50"   json:"source"` // sms, email, web
	Meta   string `gorm:"type:text" json:"meta"`   // optional JSON blob

	ReporterID *uuid.UUID `gorm:"type:uuid;index"          json:"reporter_id"`
	CreatedBy  uuid.UUID  `gorm:"type:uuid;index;not null" json:"created_by"`

	CreatedAt   time.Time      `json:"created_at"`
	SubmittedAt *time.Time     `json:"submitted_at"` // nil until Phase 2
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (f *IncidentPublicFeedback) BeforeCreate(tx *gorm.DB) error {
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	return nil
}

// ── DTOs ─────────────────────────────────────────────────────────────────────

type IncidentPublicFeedbackCreateRequest struct {
	MobileNo   string     `json:"mobile_no"   validate:"required,max=50"`
	Source     string     `json:"source"      validate:"required,oneof=sms email web"`
	Meta       string     `json:"meta"`
	ReporterID *uuid.UUID `json:"reporter_id"`
	// Message is the SMS body to send. If empty a default message with the feedback link is used.
	Message string `json:"message"`
}

type IncidentPublicFeedbackSubmitRequest struct {
	Satisfied bool   `json:"satisfied"`
	Comment   string `json:"comment"`
}

type IncidentPublicFeedbackResponse struct {
	ID          uuid.UUID  `json:"id"`
	IncidentID  uuid.UUID  `json:"incident_id"`
	MobileNo    string     `json:"mobile_no"`
	Satisfied   *bool      `json:"satisfied"`
	Comment     *string    `json:"comment"`
	Source      string     `json:"source"`
	Meta        string     `json:"meta"`
	ReporterID  *uuid.UUID `json:"reporter_id"`
	CreatedBy   uuid.UUID  `json:"created_by"`
	CreatedAt   time.Time  `json:"created_at"`
	SubmittedAt *time.Time `json:"submitted_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	// SignedToken is only populated in the Phase 1 create response.
	SignedToken string `json:"signed_token,omitempty"`
}

func ToIncidentPublicFeedbackResponse(f *IncidentPublicFeedback) IncidentPublicFeedbackResponse {
	return IncidentPublicFeedbackResponse{
		ID:          f.ID,
		IncidentID:  f.IncidentID,
		MobileNo:    f.MobileNo,
		Satisfied:   f.Satisfied,
		Comment:     f.Comment,
		Source:      f.Source,
		Meta:        f.Meta,
		ReporterID:  f.ReporterID,
		CreatedBy:   f.CreatedBy,
		CreatedAt:   f.CreatedAt,
		SubmittedAt: f.SubmittedAt,
		UpdatedAt:   f.UpdatedAt,
	}
}

func ToIncidentPublicFeedbackResponseList(items []IncidentPublicFeedback) []IncidentPublicFeedbackResponse {
	result := make([]IncidentPublicFeedbackResponse, len(items))
	for i := range items {
		result[i] = ToIncidentPublicFeedbackResponse(&items[i])
	}
	return result
}
