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
	TemplateActionEscalation       = "escalation"
	TemplateActionAssignment       = "assignment"
	TemplateActionClosure          = "closure"
	TemplateActionStatusChange     = "status_change"
	TemplateActionReadyToClose     = "ready_to_close"
	TemplateActionNewIncident      = "new_incident"
	TemplateActionConvertToRequest = "convert_to_request"
	TemplateActionCustom           = "custom"
)

// AvailableVariablesByActionType documents which template variables are injected at send-time
// for each action type. Use {{variable_name}} or {{.variable_name}} in template bodies.
// AvailableVariablesByActionType lists every {{variable}} that will be substituted
// when a template of a given action_type is rendered.
//
// The canonical implementation is BuildIncidentVariables() in template_variables.go.
// Add a new variable there first, then add it here so the frontend shows it.
var AvailableVariablesByActionType = map[string][]string{
	// ── Transition-triggered (workflow action executor) ───────────────────────
	TemplateActionStatusChange: {
		// Core incident
		"incident_number", "incident_title", "incident_id",
		"description", "record_type", "source", "channel",
		// Transition context
		"from_state", "to_state", "transition_name",
		"performed_by", "first_name", "last_name",
		// Assignee
		"assignee", "assignee_email", "assignee_phone",
		// State
		"current_state", "state_name", "sla_hours",
		// Reporter
		"reporter_name", "reporter_email", "reporter_phone",
		// Classification / workflow / location
		"classification_name", "workflow_name", "location_name",
		// SLA / dates
		"sla_breached", "sla_deadline", "due_date", "created_at", "created_by_name",
		// Geography
		"address", "city", "country",
		// Extra
		"priority", "comments", "transition_comment",
		// URLs
		"incident_url", "sla_page_url",
	},
	// ── SLA escalation (per-incident and policy-step) ────────────────────────
	TemplateActionEscalation: {
		// Core incident
		"incident_number", "incident_title", "incident_id",
		"description", "record_type", "source", "channel",
		// Notified user
		"first_name", "last_name",
		// Assignee
		"assignee", "assignee_email", "assignee_phone",
		// State / SLA
		"current_state", "state_name", "hours_in_state", "sla_hours", "hours_in_breach",
		// Reporter
		"reporter_name", "reporter_email", "reporter_phone",
		// Classification / workflow / location
		"classification_name", "workflow_name", "location_name",
		// SLA / dates
		"sla_breached", "sla_deadline", "due_date", "created_at", "created_by_name",
		// Geography
		"address", "city", "country",
		// Geolocation / map
		"latitude", "longitude", "map_url", "location_url",
		// Comments
		"comment", "comments", "transition_comment",
		// Extra
		"priority",
		// URLs
		"incident_url", "sla_page_url",
		// Policy-step specific
		"policy_name", "step_order",
		// Group batch specific
		"incident_count", "incidents_summary", "report_date",
	},
	// ── New incident created ─────────────────────────────────────────────────
	TemplateActionNewIncident: {
		// Core incident
		"incident_number", "incident_title", "incident_id",
		"description", "record_type", "source", "channel",
		// Assignee (first_name/last_name = assignee for new incidents)
		"assignee", "assignee_email", "assignee_phone",
		"first_name", "last_name",
		// Reporter
		"reporter", "reporter_name", "reporter_email", "reporter_phone",
		// Classification / workflow / location
		"classification_name", "workflow_name", "location_name",
		// SLA / dates
		"sla_breached", "sla_deadline", "due_date", "created_at", "created_by_name",
		// Geography
		"address", "city", "country",
		// Extra
		"priority",
		// URLs
		"incident_url", "sla_page_url",
	},
	// ── Partial Close (Ready-to-Close) expiry warning ────────────────────────
	TemplateActionReadyToClose: {
		// Core incident
		"incident_number", "incident_title", "incident_id",
		"description", "record_type", "source", "channel",
		// Expiry-specific
		"expires_at", "remaining_time",
		// Assignee (first_name/last_name = assignee)
		"first_name", "last_name",
		"assignee", "assignee_email", "assignee_phone",
		// Reporter
		"reporter_name", "reporter_email", "reporter_phone",
		// Classification / workflow / location
		"classification_name", "workflow_name", "location_name",
		// SLA / dates
		"sla_breached", "sla_deadline", "due_date", "created_at", "created_by_name",
		// Geography
		"address", "city", "country",
		// Extra
		"priority",
		// URLs
		"incident_url", "sla_page_url",
	},
	// ── Assignee changed ─────────────────────────────────────────────────────
	TemplateActionAssignment: {
		// Core incident
		"incident_number", "incident_title", "incident_id",
		"description", "record_type", "source", "channel",
		// Assignee (first_name/last_name = new assignee via performedBy)
		"assignee", "assignee_email", "assignee_phone",
		"first_name", "last_name", "performed_by",
		// Transition context
		"from_state", "to_state", "transition_name",
		// State
		"current_state", "state_name", "sla_hours",
		// Reporter
		"reporter_name", "reporter_email", "reporter_phone",
		// Classification / workflow / location
		"classification_name", "workflow_name", "location_name",
		// SLA / dates
		"sla_breached", "sla_deadline", "due_date", "created_at", "created_by_name",
		// Geography
		"address", "city", "country",
		// Extra
		"priority", "comments", "transition_comment",
		// URLs
		"incident_url", "sla_page_url",
	},
	// ── Incident closed ──────────────────────────────────────────────────────
	TemplateActionClosure: {
		// Core incident
		"incident_number", "incident_title", "incident_id",
		"description", "record_type", "source", "channel",
		// Performer
		"first_name", "last_name", "performed_by",
		// Transition context
		"from_state", "to_state", "transition_name",
		// State
		"current_state", "state_name", "sla_hours",
		// Assignee
		"assignee", "assignee_email", "assignee_phone",
		// Reporter
		"reporter_name", "reporter_email", "reporter_phone",
		// Classification / workflow / location
		"classification_name", "workflow_name", "location_name",
		// SLA / dates
		"sla_breached", "sla_deadline", "due_date", "created_at", "created_by_name",
		// Geography
		"address", "city", "country",
		// Extra
		"priority", "comments", "transition_comment",
		// Not-belong closure: the external department the incident was referred to
		"department_name",
		// URLs
		"incident_url", "sla_page_url",
	},
	// ── Incident converted to request ────────────────────────────────────────
	TemplateActionConvertToRequest: {
		// Core incident (source)
		"incident_number", "incident_title", "incident_id",
		"description", "record_type", "source", "channel",
		// The new request number
		"request_number",
		// Reporter / citizen
		"reporter_name", "reporter_email", "reporter_phone",
		// Classification / workflow / location
		"classification_name", "workflow_name", "location_name",
		// Dates
		"created_at", "created_by_name",
		// Geography
		"address", "city", "country",
		// URLs
		"incident_url", "sla_page_url",
	},
	// ── Custom / catch-all ───────────────────────────────────────────────────
	TemplateActionCustom: {
		// Core incident
		"incident_number", "incident_title", "incident_id",
		"description", "record_type", "source", "channel",
		// Transition context
		"from_state", "to_state", "transition_name",
		"performed_by", "first_name", "last_name",
		// Assignee
		"assignee", "assignee_email", "assignee_phone",
		// State
		"current_state", "state_name", "sla_hours",
		// Reporter
		"reporter_name", "reporter_email", "reporter_phone",
		// Classification / workflow / location
		"classification_name", "workflow_name", "location_name",
		// SLA / dates
		"sla_breached", "sla_deadline", "due_date", "created_at", "created_by_name",
		// Geography
		"address", "city", "country",
		// Extra
		"priority", "comments", "transition_comment",
		// URLs
		"incident_url", "sla_page_url",
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
	ActionType   string     `json:"action_type"   validate:"omitempty,oneof=escalation assignment closure status_change ready_to_close new_incident convert_to_request custom"`
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
