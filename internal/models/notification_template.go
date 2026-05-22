package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Channel constants
const (
	TemplateChannelEmail = "email"
	TemplateChannelSMS   = "sms"
)

// Module type constants — which product area the template belongs to
const (
	TemplateModuleIncident  = "incident"
	TemplateModuleComplaint = "complaint"
	TemplateModuleRequest   = "request"
	TemplateModuleQuery     = "query"
	TemplateModuleGlobal    = "global"
)

// Action type constants — what event triggers this template
const (
	TemplateActionEscalation   = "escalation"
	TemplateActionAssignment   = "assignment"
	TemplateActionClosure      = "closure"
	TemplateActionStatusChange = "status_change"
	TemplateActionReadyToClose = "ready_to_close"
	TemplateActionNewIncident  = "new_incident"
	TemplateActionCustom       = "custom"
)

// AvailableVariablesByActionType documents which template variables are injected at send-time
// for each action type. Use {{variable_name}} or {{.variable_name}} in template bodies.
var AvailableVariablesByActionType = map[string][]string{
	TemplateActionStatusChange: {
		"incident_number", "incident_title", "incident_id",
		"description", "record_type", "source", "channel",
		"from_state", "to_state", "transition_name",
		"performed_by", "first_name", "last_name",
		"assignee", "assignee_email", "assignee_phone",
		"current_state",
		"reporter_name", "reporter_email", "reporter_phone",
		"classification_name", "workflow_name",
		"sla_breached", "sla_deadline", "due_date",
		"created_at", "created_by_name",
		"address", "city", "country",
	},
	TemplateActionEscalation: {
		"incident_number", "incident_title", "incident_id",
		"description", "record_type", "source", "channel",
		"from_state", "to_state", "transition_name",
		"first_name", "last_name",
		"assignee", "assignee_email", "assignee_phone",
		"current_state", "state_name", "hours_in_state", "sla_hours",
		"reporter_name", "reporter_email", "reporter_phone",
		"classification_name", "workflow_name",
		"sla_breached", "sla_deadline", "due_date",
		"created_at", "created_by_name",
		"address", "city", "country",
		// Group batch notification variables
		"incident_count", "sla_page_url", "incidents_summary", "report_date",
		// Policy step per-incident variables
		"incident_url", "policy_name", "step_order", "hours_in_breach",
	},
	TemplateActionNewIncident: {
		"incident_number", "incident_title", "incident_id",
		"description", "record_type", "source", "channel",
		"assignee", "assignee_email", "assignee_phone",
		"first_name", "last_name", "reporter",
		"reporter_name", "reporter_email", "reporter_phone",
		"classification_name", "workflow_name",
		"sla_breached", "sla_deadline", "due_date",
		"created_at", "created_by_name",
		"address", "city", "country",
	},
	TemplateActionReadyToClose: {
		"incident_number", "incident_title", "incident_id",
		"description", "record_type", "source", "channel",
		"expires_at", "remaining_time",
		"first_name", "last_name",
		"assignee", "assignee_email", "assignee_phone",
		"reporter_name", "reporter_email", "reporter_phone",
		"classification_name", "workflow_name",
		"sla_breached", "sla_deadline", "due_date",
		"created_at", "created_by_name",
		"address", "city", "country",
	},
	TemplateActionAssignment: {
		"incident_number", "incident_title", "incident_id",
		"description", "record_type", "source", "channel",
		"assignee", "assignee_email", "assignee_phone",
		"first_name", "last_name", "performed_by",
		"reporter_name", "reporter_email", "reporter_phone",
		"classification_name", "workflow_name",
		"sla_breached", "sla_deadline", "due_date",
		"created_at", "created_by_name",
		"address", "city", "country",
	},
	TemplateActionClosure: {
		"incident_number", "incident_title", "incident_id",
		"description", "record_type", "source", "channel",
		"assignee", "assignee_email", "assignee_phone",
		"first_name", "last_name", "performed_by",
		"reporter_name", "reporter_email", "reporter_phone",
		"classification_name", "workflow_name",
		"sla_breached", "sla_deadline", "due_date",
		"created_at", "created_by_name",
		"address", "city", "country",
	},
	TemplateActionCustom: {
		"incident_number", "incident_title", "incident_id",
		"description", "record_type", "source", "channel",
		"from_state", "to_state", "transition_name",
		"performed_by", "first_name", "last_name",
		"assignee", "assignee_email", "assignee_phone",
		"current_state",
		"reporter_name", "reporter_email", "reporter_phone",
		"classification_name", "workflow_name",
		"sla_breached", "sla_deadline", "due_date",
		"created_at", "created_by_name",
		"address", "city", "country",
	},
}

// NotificationTemplate stores a bilingual template (EN + AR) in a single row.
type NotificationTemplate struct {
	ID           uuid.UUID           `gorm:"type:uuid;primaryKey"                    json:"id"`
	Name         string              `gorm:"size:200"                                json:"name"`
	Code         string              `gorm:"size:100;not null;index"                 json:"code"`
	Channel      string              `gorm:"size:20;not null;index"                  json:"channel"`
	ModuleType   string              `gorm:"size:50;index"                           json:"module_type"`
	ActionType   string              `gorm:"size:50;index"                           json:"action_type"`
	SubjectEN    string              `gorm:"column:subject_en;type:text"             json:"subject_en,omitempty"`
	BodyEN       string              `gorm:"column:body_en;type:text"                json:"body_en"`
	SubjectAR    string              `gorm:"column:subject_ar;type:text"             json:"subject_ar,omitempty"`
	BodyAR       string              `gorm:"column:body_ar;type:text"                json:"body_ar,omitempty"`
	Variables    string              `gorm:"type:text"                               json:"variables,omitempty"`
	TransitionID *uuid.UUID          `gorm:"type:uuid;index"                         json:"transition_id,omitempty"`
	Transition   *WorkflowTransition `gorm:"foreignKey:TransitionID"                 json:"transition,omitempty"`
	IsActive     bool                `gorm:"default:true"                            json:"is_active"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
	DeletedAt    gorm.DeletedAt      `gorm:"index"                                   json:"-"`
}

func (t *NotificationTemplate) BeforeCreate(tx *gorm.DB) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	return nil
}

// NotificationTemplateCreateRequest creates a bilingual template in a single DB row.
type NotificationTemplateCreateRequest struct {
	Name         string     `json:"name"          validate:"required"`
	Code         string     `json:"code"          validate:"required"`
	Channel      string     `json:"channel"       validate:"required,oneof=email sms"`
	ModuleType   string     `json:"module_type"   validate:"omitempty,oneof=incident complaint request query global"`
	ActionType   string     `json:"action_type"   validate:"omitempty,oneof=escalation assignment closure status_change ready_to_close new_incident custom"`
	Variables    string     `json:"variables"`
	TransitionID *uuid.UUID `json:"transition_id"`
	SubjectEN    string     `json:"subject_en"`
	BodyEN       string     `json:"body_en"`
	SubjectAR    string     `json:"subject_ar"`
	BodyAR       string     `json:"body_ar"`
	IsActive     bool       `json:"is_active"`
}

// NotificationTemplateUpdateRequest for partial updates.
type NotificationTemplateUpdateRequest struct {
	Name         *string    `json:"name"`
	SubjectEN    *string    `json:"subject_en"`
	Channel      string     `json:"channel"       validate:"required,oneof=email sms"`
	BodyEN       *string    `json:"body_en"`
	SubjectAR    *string    `json:"subject_ar"`
	BodyAR       *string    `json:"body_ar"`
	ModuleType   *string    `json:"module_type"`
	ActionType   *string    `json:"action_type"`
	Variables    *string    `json:"variables"`
	TransitionID *uuid.UUID `json:"transition_id"`
	IsActive     *bool      `json:"is_active"`
}

// NotificationTemplateFilter for list queries.
type NotificationTemplateFilter struct {
	Channel      string
	ModuleType   string
	ActionType   string
	IsActive     *bool
	Code         string
	Search       string     // ILIKE on name
	TransitionID *uuid.UUID // filter by linked transition
	Page         int
	Limit        int
}

// NotificationTemplateResponse is the API response shape.
type NotificationTemplateResponse struct {
	ID           uuid.UUID  `json:"id"`
	Name         string     `json:"name"`
	Code         string     `json:"code"`
	Channel      string     `json:"channel"`
	ModuleType   string     `json:"module_type"`
	ActionType   string     `json:"action_type"`
	SubjectEN    string     `json:"subject_en,omitempty"`
	BodyEN       string     `json:"body_en"`
	SubjectAR    string     `json:"subject_ar,omitempty"`
	BodyAR       string     `json:"body_ar,omitempty"`
	Variables    string     `json:"variables,omitempty"`
	TransitionID *uuid.UUID `json:"transition_id,omitempty"`
	IsActive     bool       `json:"is_active"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func ToNotificationTemplateResponse(t *NotificationTemplate) NotificationTemplateResponse {
	return NotificationTemplateResponse{
		ID:           t.ID,
		Name:         t.Name,
		Code:         t.Code,
		Channel:      t.Channel,
		ModuleType:   t.ModuleType,
		ActionType:   t.ActionType,
		SubjectEN:    t.SubjectEN,
		BodyEN:       t.BodyEN,
		SubjectAR:    t.SubjectAR,
		BodyAR:       t.BodyAR,
		Variables:    t.Variables,
		TransitionID: t.TransitionID,
		IsActive:     t.IsActive,
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
	}
}
