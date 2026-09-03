package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MOMRAStatusMapping links an Automax WorkflowState to a MOMRA CaseStatusID (3.14
// Update Status Version 2), per docs/MOMRA_Outbound_Integration_Spec_v1.0.md §3 Story A.
// Editable via admin CRUD rather than hardcoded, because MOMRA's full status code list
// is not yet confirmed (TFIS v1.0's OD-008 / this integration's OD-N1) — only four
// codes are known today (NEW/1, 002 Resolved, 003 Rejected, 004 Route-to-EE).
type MOMRAStatusMapping struct {
	ID         uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	WorkflowID uuid.UUID      `gorm:"type:uuid;index;not null" json:"workflow_id"`
	Workflow   *Workflow      `gorm:"foreignKey:WorkflowID" json:"workflow,omitempty"`
	StateID    uuid.UUID      `gorm:"type:uuid;index;not null" json:"state_id"`
	State      *WorkflowState `gorm:"foreignKey:StateID" json:"state,omitempty"`
	// CaseStatusID is MOMRA's status code, transported as a string to preserve leading
	// zeros (e.g. "002", "003", "004") — see TFIS v1.0 CT-005/CL-006.
	CaseStatusID string `gorm:"size:20;not null" json:"case_status_id"`
	// IsClosureStatus drives the outbound ClosureFlag ("Yes"/"No") on 3.14's payload.
	IsClosureStatus bool           `gorm:"default:false" json:"is_closure_status"`
	IsActive        bool           `gorm:"default:true" json:"is_active"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
}

func (m *MOMRAStatusMapping) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}

// MOMRAStatusMappingRequest is the shared create/update request shape for admin CRUD.
type MOMRAStatusMappingRequest struct {
	WorkflowID      uuid.UUID `json:"workflow_id" validate:"required"`
	StateID         uuid.UUID `json:"state_id" validate:"required"`
	CaseStatusID    string    `json:"case_status_id" validate:"required,max=20"`
	IsClosureStatus bool      `json:"is_closure_status"`
	IsActive        *bool     `json:"is_active"`
}
