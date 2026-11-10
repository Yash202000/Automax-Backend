package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EscalationSLA struct {
	ID uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`

	// Incident that triggered the SLA breach
	IncidentID *uuid.UUID `gorm:"type:uuid;index" json:"incident_id,omitempty"`
	Incident   *Incident  `gorm:"foreignKey:IncidentID" json:"incident,omitempty"`

	// The state in which the SLA was exceeded
	StateID *uuid.UUID     `gorm:"type:uuid;index" json:"state_id,omitempty"`
	State   *WorkflowState `gorm:"foreignKey:StateID" json:"state,omitempty"`

	// The outgoing transition the user is responsible for (nullable – there may be more than one)
	TransitionID *uuid.UUID          `gorm:"type:uuid;index" json:"transition_id,omitempty"`
	Transition   *WorkflowTransition `gorm:"foreignKey:TransitionID" json:"transition,omitempty"`

	// The user who was notified
	NotifiedUserID *uuid.UUID `gorm:"type:uuid;index" json:"notified_user_id,omitempty"`
	NotifiedUser   *User      `gorm:"foreignKey:NotifiedUserID" json:"notified_user,omitempty"`

	// Policy step that triggered this notification (nil = legacy hardcoded logic)
	EscalationPolicyID     *uuid.UUID             `gorm:"type:uuid;index" json:"escalation_policy_id,omitempty"`
	EscalationPolicyStepID *uuid.UUID             `gorm:"type:uuid;index" json:"escalation_policy_step_id,omitempty"`
	EscalationPolicyStep   *EscalationPolicyStep  `gorm:"foreignKey:EscalationPolicyStepID" json:"escalation_policy_step,omitempty"`

	// EscalationType classifies the trigger: "state_sla" | "global_sla"
	EscalationType string `gorm:"size:20;default:'state_sla'" json:"escalation_type"`

	// Contact details used at notification time (snapshot)
	Email string `gorm:"size:255" json:"email"`
	Phone string `gorm:"size:50"  json:"phone"`

	// Actions taken during the escalation
	Actions TextArray `gorm:"type:text[]" json:"actions"`

	// SLA context
	SLAHoursAllowed int     `gorm:"default:0" json:"sla_hours_allowed"` // configured SLA hours for the state
	HoursInState    float64 `gorm:"default:0" json:"hours_in_state"`    // actual hours incident spent in the state

	NotifiedAt *time.Time `gorm:"index" json:"notified_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

func (e *EscalationSLA) BeforeCreate(_ *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}

// EscalationSLAResponse is the API response for a breach notification record.
type EscalationSLAResponse struct {
	ID                     uuid.UUID                      `json:"id"`
	IncidentID             *uuid.UUID                     `json:"incident_id,omitempty"`
	Incident               *IncidentResponse              `json:"incident,omitempty"`
	StateID                *uuid.UUID                     `json:"state_id,omitempty"`
	State                  *WorkflowStateResponse         `json:"state,omitempty"`
	TransitionID           *uuid.UUID                     `json:"transition_id,omitempty"`
	NotifiedUserID         *uuid.UUID                     `json:"notified_user_id,omitempty"`
	NotifiedUser           *UserResponse                  `json:"notified_user,omitempty"`
	Email                  string                         `json:"email"`
	Phone                  string                         `json:"phone"`
	Actions                []string                       `json:"actions"`
	SLAHoursAllowed        int                            `json:"sla_hours_allowed"`
	HoursInState           float64                        `json:"hours_in_state"`
	EscalationPolicyID     *uuid.UUID                     `json:"escalation_policy_id,omitempty"`
	EscalationPolicyStepID *uuid.UUID                     `json:"escalation_policy_step_id,omitempty"`
	EscalationType         string                         `json:"escalation_type"`
	NotifiedAt             *time.Time                     `json:"notified_at,omitempty"`
	CreatedAt              time.Time                      `json:"created_at"`
}

func ToEscalationSLAResponse(e *EscalationSLA) EscalationSLAResponse {
	resp := EscalationSLAResponse{
		ID:                     e.ID,
		IncidentID:             e.IncidentID,
		StateID:                e.StateID,
		TransitionID:           e.TransitionID,
		NotifiedUserID:         e.NotifiedUserID,
		Email:                  e.Email,
		Phone:                  e.Phone,
		Actions:                []string(e.Actions),
		SLAHoursAllowed:        e.SLAHoursAllowed,
		HoursInState:           e.HoursInState,
		EscalationPolicyID:     e.EscalationPolicyID,
		EscalationPolicyStepID: e.EscalationPolicyStepID,
		EscalationType:         e.EscalationType,
		NotifiedAt:             e.NotifiedAt,
		CreatedAt:              e.CreatedAt,
	}

	if e.Incident != nil {
		r := ToIncidentResponse(e.Incident)
		resp.Incident = &r
	}
	if e.State != nil {
		r := ToWorkflowStateResponse(e.State)
		resp.State = &r
	}
	if e.NotifiedUser != nil {
		r := ToUserResponse(e.NotifiedUser)
		resp.NotifiedUser = &r
	}

	return resp
}

// EscalationLogFilter contains escalation-log search and pagination options.
type EscalationLogFilter struct {
	IncidentID       *uuid.UUID `query:"incident_id" json:"incident_id,omitempty"`
	IncidentNumber   string     `query:"incident_number" json:"incident_number,omitempty"`
	Level            *int       `query:"level" json:"level,omitempty"`
	ClassificationID *uuid.UUID `query:"classification_id" json:"classification_id,omitempty"`
	DateFrom         *time.Time `query:"date_from" json:"date_from,omitempty"`
	DateTo           *time.Time `query:"date_to" json:"date_to,omitempty"`
	Person           string     `query:"person" json:"person,omitempty"`
	EscalationType   string     `query:"escalation_type" json:"escalation_type,omitempty"`
	Page             int        `query:"page" json:"page,omitempty"`
	Limit            int        `query:"limit" json:"limit,omitempty"`
}

// EscalationNoticeResultResponse records one recipient's delivery result.
type EscalationNoticeResultResponse struct {
	Channel           string     `json:"channel"`
	Recipient         string     `json:"recipient"`
	Status            string     `json:"status"` // sent | failed | skipped | unknown
	FailureCode       string     `json:"failure_code,omitempty"`
	Error             string     `json:"error,omitempty"`
	NotificationLogID *uuid.UUID `json:"notification_log_id,omitempty"`
	SentAt            *time.Time `json:"sent_at,omitempty"`
}

// EscalationRecipientResponse describes one escalation recipient.
type EscalationRecipientResponse struct {
	UserID  *uuid.UUID                       `json:"user_id,omitempty"`
	Name    string                           `json:"name"`
	Email   string                           `json:"email"`
	Phone   string                           `json:"phone"`
	Status  string                           `json:"status"`
	Notices []EscalationNoticeResultResponse `json:"notices"`
}

// EscalationLogEntryResponse represents one recipient's escalation record.
type EscalationLogEntryResponse struct {
	ID                     uuid.UUID                   `json:"id"`
	IncidentID             *uuid.UUID                  `json:"incident_id,omitempty"`
	IncidentNumber         string                      `json:"incident_number"`
	ClassificationID       *uuid.UUID                  `json:"classification_id,omitempty"`
	ClassificationName     string                      `json:"classification_name,omitempty"`
	StateID                *uuid.UUID                  `json:"state_id,omitempty"`
	StateName              string                      `json:"state_name,omitempty"`
	EscalationType         string                      `json:"escalation_type"`
	EscalatedAt            time.Time                   `json:"escalated_at"`
	Reason                 string                      `json:"reason"`
	FromLevel              *int                        `json:"from_level,omitempty"`
	ToLevel                *int                        `json:"to_level,omitempty"`
	EscalationPolicyID     *uuid.UUID                  `json:"escalation_policy_id,omitempty"`
	EscalationPolicyStepID *uuid.UUID                  `json:"escalation_policy_step_id,omitempty"`
	Status                 string                      `json:"status"`
	Recipient              EscalationRecipientResponse `json:"recipient"`
	Actions                []string                    `json:"actions,omitempty"`
	SLAHoursAllowed        int                         `json:"sla_hours_allowed"`
	HoursInState           float64                     `json:"hours_in_state"`
	CreatedAt              time.Time                   `json:"created_at"`
}

// EscalationLogListResponse contains a page of escalation entries.
type EscalationLogListResponse struct {
	Entries []EscalationLogEntryResponse `json:"entries"`
	Total   int                          `json:"total"`
	Page    int                          `json:"page"`
	Limit   int                          `json:"limit"`
}
