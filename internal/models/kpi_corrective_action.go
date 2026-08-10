package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	CorrectiveActionStatusOpen       = "open"
	CorrectiveActionStatusInProgress = "in_progress"
	CorrectiveActionStatusClosed     = "closed"
	CorrectiveActionStatusEscalated  = "escalated"
)

// KpiCorrectiveAction tracks a remediation item raised against an
// underperforming KPI performance record — who owns it, when it's due, and
// what closed it. A performance record can have several of these.
type KpiCorrectiveAction struct {
	ID                 uuid.UUID       `gorm:"type:uuid;primary_key" json:"id"`
	KpiPerformanceID   uuid.UUID       `gorm:"type:uuid;not null;index" json:"kpi_performance_id"`
	KpiPerformance     *KpiPerformance `gorm:"foreignKey:KpiPerformanceID" json:"-"`
	Description        string          `gorm:"type:text;not null" json:"description"`
	OwnerID            uuid.UUID       `gorm:"type:uuid;not null;index" json:"owner_id"`
	Owner              *User           `gorm:"foreignKey:OwnerID" json:"owner,omitempty"`
	DueDate            *time.Time      `json:"due_date"`
	Status             string          `gorm:"size:20;not null;default:'open'" json:"status"`
	ClosureNote        string          `gorm:"type:text" json:"closure_note"`
	ClosureEvidenceURL string          `gorm:"size:500" json:"closure_evidence_url"`
	EscalatedAt        *time.Time      `json:"escalated_at"`
	CreatedByID        uuid.UUID       `gorm:"type:uuid;not null" json:"created_by_id"`
	CreatedBy          *User           `gorm:"foreignKey:CreatedByID" json:"created_by,omitempty"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
	DeletedAt          gorm.DeletedAt  `gorm:"index" json:"-"`
}

func (a *KpiCorrectiveAction) BeforeCreate(tx *gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}

type KpiCorrectiveActionRequest struct {
	KpiPerformanceID uuid.UUID  `json:"kpi_performance_id" validate:"required"`
	Description      string     `json:"description" validate:"required"`
	OwnerID          uuid.UUID  `json:"owner_id" validate:"required"`
	DueDate          *time.Time `json:"due_date"`
}

type KpiCorrectiveActionStatusRequest struct {
	Status             string `json:"status" validate:"required,oneof=open in_progress closed escalated"`
	ClosureNote        string `json:"closure_note"`
	ClosureEvidenceURL string `json:"closure_evidence_url"`
}

type KpiCorrectiveActionResponse struct {
	ID                 uuid.UUID          `json:"id"`
	KpiPerformanceID   uuid.UUID          `json:"kpi_performance_id"`
	Description        string             `json:"description"`
	OwnerID            uuid.UUID          `json:"owner_id"`
	Owner              *UserBriefResponse `json:"owner,omitempty"`
	DueDate            *time.Time         `json:"due_date"`
	Status             string             `json:"status"`
	ClosureNote        string             `json:"closure_note"`
	ClosureEvidenceURL string             `json:"closure_evidence_url"`
	EscalatedAt        *time.Time         `json:"escalated_at"`
	CreatedByID        uuid.UUID          `json:"created_by_id"`
	CreatedBy          *UserBriefResponse `json:"created_by,omitempty"`
	CreatedAt          time.Time          `json:"created_at"`
	UpdatedAt          time.Time          `json:"updated_at"`
}

func (a *KpiCorrectiveAction) ToResponse() KpiCorrectiveActionResponse {
	return KpiCorrectiveActionResponse{
		ID:                 a.ID,
		KpiPerformanceID:   a.KpiPerformanceID,
		Description:        a.Description,
		OwnerID:            a.OwnerID,
		Owner:              ToUserBriefResponse(a.Owner),
		DueDate:            a.DueDate,
		Status:             a.Status,
		ClosureNote:        a.ClosureNote,
		ClosureEvidenceURL: a.ClosureEvidenceURL,
		EscalatedAt:        a.EscalatedAt,
		CreatedByID:        a.CreatedByID,
		CreatedBy:          ToUserBriefResponse(a.CreatedBy),
		CreatedAt:          a.CreatedAt,
		UpdatedAt:          a.UpdatedAt,
	}
}
