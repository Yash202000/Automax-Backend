package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// IncidentRejectionLog is a dedicated, reporting-friendly table that captures a full
// snapshot of every rejection event at the moment it occurs.
// Designed for Power BI consumption and system operational reports.
type IncidentRejectionLog struct {
	ID         uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	IncidentID uuid.UUID `gorm:"type:uuid;not null;index" json:"incident_id"`
	Incident   *Incident `gorm:"foreignKey:IncidentID" json:"incident,omitempty"`

	// Rejection sequence tracking
	RejectionSequence   int `gorm:"not null" json:"rejection_sequence"`    // 1 = first, 2 = second, etc.
	TotalRejectionCount int `gorm:"not null" json:"total_rejection_count"` // count at time of this rejection

	// Timeline
	ReceivedAt          time.Time `gorm:"not null" json:"received_at"`           // when incident entered the from-state
	RejectedAt          time.Time `gorm:"not null;index" json:"rejected_at"`     // when rejection transition was executed
	ReactionTimeMinutes int64     `gorm:"not null" json:"reaction_time_minutes"` // (rejected_at - received_at) in minutes

	// Rejection context
	TransitionID    uuid.UUID           `gorm:"type:uuid;not null" json:"transition_id"`
	Transition      *WorkflowTransition `gorm:"foreignKey:TransitionID" json:"transition,omitempty"`
	FromStateID     uuid.UUID           `gorm:"type:uuid;not null" json:"from_state_id"`
	FromState       *WorkflowState      `gorm:"foreignKey:FromStateID" json:"from_state,omitempty"`
	ToStateID       uuid.UUID           `gorm:"type:uuid;not null" json:"to_state_id"`
	ToState         *WorkflowState      `gorm:"foreignKey:ToStateID" json:"to_state,omitempty"`
	RejectionReason string              `gorm:"type:text" json:"rejection_reason"` // comment at time of rejection

	// Who rejected (denormalized snapshot for historical accuracy)
	RejectedByID            uuid.UUID `gorm:"type:uuid;not null;index" json:"rejected_by_id"`
	RejectedBy              *User     `gorm:"foreignKey:RejectedByID" json:"rejected_by,omitempty"`
	RejectedByUsername      string    `gorm:"size:100" json:"rejected_by_username"`       // snapshot
	RejectedByRolesSnapshot string    `gorm:"type:text" json:"rejected_by_roles_snapshot"` // JSON: []string of role names at rejection time

	// SLA snapshot (captured at moment of rejection)
	SLAThresholdHours      *int    `gorm:"" json:"sla_threshold_hours"`       // SLAHours from FromState (nil = no SLA defined)
	SLAThresholdMinutes    *int64  `gorm:"" json:"sla_threshold_minutes"`     // threshold * 60, precomputed for BI queries
	SLABreachedAtRejection bool    `gorm:"default:false" json:"sla_breached_at_rejection"` // incident.sla_breached flag at time of rejection
	SLAStatus              string  `gorm:"size:20;not null" json:"sla_status"` // "within_sla" or "breached"

	// Incident snapshot (denormalized for historical accuracy and BI performance)
	IncidentNumber   string     `gorm:"size:50;not null" json:"incident_number"`
	IncidentTitle    string     `gorm:"size:200;not null" json:"incident_title"`
	RecordType       string     `gorm:"size:20;not null" json:"record_type"` // incident/request/complaint/query
	DepartmentID     *uuid.UUID `gorm:"type:uuid" json:"department_id"`
	ClassificationID *uuid.UUID `gorm:"type:uuid" json:"classification_id"`

	// Back-reference to the transition history record
	TransitionHistoryID uuid.UUID `gorm:"type:uuid;not null" json:"transition_history_id"`

	CreatedAt time.Time      `gorm:"index" json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (r *IncidentRejectionLog) BeforeCreate(tx *gorm.DB) error {
	if r.ID == uuid.Nil {
		r.ID = uuid.New()
	}
	return nil
}

// IncidentRejectionLogFilter for querying rejection logs
type IncidentRejectionLogFilter struct {
	IncidentID       *uuid.UUID `query:"incident_id" json:"incident_id"`
	RejectedByID     *uuid.UUID `query:"rejected_by_id" json:"rejected_by_id"`
	RecordType       *string    `query:"record_type" json:"record_type"`
	DepartmentID     *uuid.UUID `query:"department_id" json:"department_id"`
	ClassificationID *uuid.UUID `query:"classification_id" json:"classification_id"`
	SLAStatus        *string    `query:"sla_status" json:"sla_status"` // "within_sla" or "breached"
	StartDate        *time.Time `query:"start_date" json:"start_date"`
	EndDate          *time.Time `query:"end_date" json:"end_date"`
	Page             int        `query:"page" json:"page"`
	Limit            int        `query:"limit" json:"limit"`
}

// IncidentRejectionLogResponse is the API response for a rejection log entry
type IncidentRejectionLogResponse struct {
	ID                      uuid.UUID              `json:"id"`
	IncidentID              uuid.UUID              `json:"incident_id"`
	IncidentNumber          string                 `json:"incident_number"`
	IncidentTitle           string                 `json:"incident_title"`
	RecordType              string                 `json:"record_type"`
	RejectionSequence       int                    `json:"rejection_sequence"`
	TotalRejectionCount     int                    `json:"total_rejection_count"`
	ReceivedAt              time.Time              `json:"received_at"`
	RejectedAt              time.Time              `json:"rejected_at"`
	ReactionTimeMinutes     int64                  `json:"reaction_time_minutes"`
	RejectionReason         string                 `json:"rejection_reason"`
	FromState               *WorkflowStateResponse `json:"from_state,omitempty"`
	ToState                 *WorkflowStateResponse `json:"to_state,omitempty"`
	RejectedBy              *UserResponse          `json:"rejected_by,omitempty"`
	RejectedByUsername      string                 `json:"rejected_by_username"`
	RejectedByRolesSnapshot []string               `json:"rejected_by_roles_snapshot"`
	SLAThresholdHours       *int                   `json:"sla_threshold_hours"`
	SLAThresholdMinutes     *int64                 `json:"sla_threshold_minutes"`
	SLABreachedAtRejection  bool                   `json:"sla_breached_at_rejection"`
	SLAStatus               string                 `json:"sla_status"`
	DepartmentID            *uuid.UUID             `json:"department_id,omitempty"`
	ClassificationID        *uuid.UUID             `json:"classification_id,omitempty"`
	TransitionHistoryID     uuid.UUID              `json:"transition_history_id"`
	CreatedAt               time.Time              `json:"created_at"`
}
