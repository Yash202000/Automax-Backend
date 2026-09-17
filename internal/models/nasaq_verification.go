package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// NasaqVerificationStatus mirrors the Nasaq Integration Microservice's
// verificationStatus values, plus ERROR for when this Automax API couldn't
// complete the call to that service at all (network/timeout/5xx).
type NasaqVerificationStatus string

const (
	NasaqVerificationMatched        NasaqVerificationStatus = "MATCHED"
	NasaqVerificationNoMatch        NasaqVerificationStatus = "NO_MATCH"
	NasaqVerificationReviewRequired NasaqVerificationStatus = "REVIEW_REQUIRED"
	NasaqVerificationError          NasaqVerificationStatus = "ERROR"
)

// NasaqVerification is an immutable snapshot of one manual "Verify Nasaq"
// attempt on an Incident. Every attempt inserts a new row - rows are never
// updated - so the full verification history for an incident is preserved;
// the most recent row (by CreatedAt) is the current result.
type NasaqVerification struct {
	ID               uuid.UUID               `gorm:"type:uuid;primary_key" json:"id"`
	IncidentID       uuid.UUID               `gorm:"type:uuid;not null;index" json:"incident_id"`
	ClassificationID uuid.UUID               `gorm:"type:uuid;not null;index" json:"classification_id"`
	Status           NasaqVerificationStatus `gorm:"size:20;not null" json:"status"`
	PermitNumber     *int64                  `json:"permit_number,omitempty"`
	Distance         *float64                `json:"distance,omitempty"`
	// ResponseID is Nasaq's own responseId/correlation reference, for
	// traceability back to the upstream call.
	ResponseID string `gorm:"size:100" json:"response_id,omitempty"`
	// RawResponse stores the full nasaqData/candidates/message payload
	// returned by the Nasaq Integration Microservice, for auditing.
	RawResponse   datatypes.JSON `gorm:"type:jsonb" json:"raw_response,omitempty"`
	ErrorMessage  string         `gorm:"size:1000" json:"error_message,omitempty"`
	RequestedByID *uuid.UUID     `gorm:"type:uuid;index" json:"requested_by_id,omitempty"`
	RequestedBy   *User          `gorm:"foreignKey:RequestedByID" json:"requested_by,omitempty"`
	CreatedAt     time.Time      `gorm:"index" json:"created_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

func (n *NasaqVerification) BeforeCreate(tx *gorm.DB) error {
	if n.ID == uuid.Nil {
		n.ID = uuid.New()
	}
	return nil
}
