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
	TemplateActionCustom       = "custom"
)

// Language constants
const (
	TemplateLangEN = "en"
	TemplateLangAR = "ar"
)

type NotificationTemplate struct {
	ID           uuid.UUID           `gorm:"type:uuid;primaryKey"          json:"id"`
	Name         string              `gorm:"size:200"                      json:"name"`
	Code         string              `gorm:"size:100;not null;index"       json:"code"`
	Channel      string              `gorm:"size:20;not null;index"        json:"channel"`
	Language     string              `gorm:"size:10;not null;index"        json:"language"`
	ModuleType   string              `gorm:"size:50;index"                 json:"module_type"`
	ActionType   string              `gorm:"size:50;index"                 json:"action_type"`
	Subject      string              `gorm:"type:text"                     json:"subject,omitempty"`
	Body         string              `gorm:"type:text;not null"            json:"body"`
	Description  string              `gorm:"type:text"                     json:"description,omitempty"`
	Variables    string              `gorm:"type:text"                     json:"variables,omitempty"`
	TransitionID *uuid.UUID          `gorm:"type:uuid;index"               json:"transition_id,omitempty"`
	Transition   *WorkflowTransition `gorm:"foreignKey:TransitionID"       json:"transition,omitempty"`
	IsActive     bool                `gorm:"default:true"                  json:"is_active"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    time.Time           `json:"updated_at"`
	DeletedAt    gorm.DeletedAt      `gorm:"index"                         json:"-"`
}

func (t *NotificationTemplate) BeforeCreate(tx *gorm.DB) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	return nil
}

// NotificationTemplateCreateRequest is used for creating a single-language template.
type NotificationTemplateCreateRequest struct {
	Name         string     `json:"name"          validate:"required"`
	Code         string     `json:"code"          validate:"required"`
	Channel      string     `json:"channel"       validate:"required,oneof=email sms"`
	Language     string     `json:"language"      validate:"required,oneof=en ar"`
	ModuleType   string     `json:"module_type"   validate:"omitempty,oneof=incident complaint request query global"`
	ActionType   string     `json:"action_type"   validate:"omitempty,oneof=escalation assignment closure status_change ready_to_close custom"`
	Subject      string     `json:"subject"`
	Body         string     `json:"body"          validate:"required"`
	Description  string     `json:"description"`
	Variables    string     `json:"variables"`
	TransitionID *uuid.UUID `json:"transition_id"`
	IsActive     bool       `json:"is_active"`
}

// NotificationTemplateBilingualRequest creates both EN and AR records in one call.
type NotificationTemplateBilingualRequest struct {
	Name         string     `json:"name"          validate:"required"`
	Code         string     `json:"code"          validate:"required"`
	Channel      string     `json:"channel"       validate:"required,oneof=email sms"`
	ModuleType   string     `json:"module_type"   validate:"omitempty,oneof=incident complaint request query global"`
	ActionType   string     `json:"action_type"   validate:"omitempty,oneof=escalation assignment closure status_change ready_to_close custom"`
	Description  string     `json:"description"`
	Variables    string     `json:"variables"`
	TransitionID *uuid.UUID `json:"transition_id"`
	SubjectEN    string     `json:"subject_en"`
	BodyEN       string     `json:"body_en"       validate:"required"`
	SubjectAR    string     `json:"subject_ar"`
	BodyAR       string     `json:"body_ar"`
	IsActive     bool       `json:"is_active"`
}

// NotificationTemplateUpdateRequest for partial updates.
type NotificationTemplateUpdateRequest struct {
	Name         *string    `json:"name"`
	Subject      *string    `json:"subject"`
	Body         *string    `json:"body"`
	ModuleType   *string    `json:"module_type"`
	ActionType   *string    `json:"action_type"`
	Description  *string    `json:"description"`
	Variables    *string    `json:"variables"`
	TransitionID *uuid.UUID `json:"transition_id"`
	IsActive     *bool      `json:"is_active"`
}

// NotificationTemplateFilter for list queries.
type NotificationTemplateFilter struct {
	Channel      string
	Language     string
	ModuleType   string
	ActionType   string
	IsActive     *bool
	Code         string
	Search       string     // ILIKE on name and description
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
	Language     string     `json:"language"`
	ModuleType   string     `json:"module_type"`
	ActionType   string     `json:"action_type"`
	Subject      string     `json:"subject,omitempty"`
	Body         string     `json:"body"`
	Description  string     `json:"description,omitempty"`
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
		Language:     t.Language,
		ModuleType:   t.ModuleType,
		ActionType:   t.ActionType,
		Subject:      t.Subject,
		Body:         t.Body,
		Description:  t.Description,
		Variables:    t.Variables,
		TransitionID: t.TransitionID,
		IsActive:     t.IsActive,
		CreatedAt:    t.CreatedAt,
		UpdatedAt:    t.UpdatedAt,
	}
}
