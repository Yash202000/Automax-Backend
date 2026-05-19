package models

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TextArray is a []string that reads/writes PostgreSQL text[] columns.
type TextArray []string

func (a TextArray) Value() (driver.Value, error) {
	if len(a) == 0 {
		return "{}", nil
	}
	elems := make([]string, len(a))
	for i, s := range a {
		s = strings.ReplaceAll(s, `\`, `\\`)
		s = strings.ReplaceAll(s, `"`, `\"`)
		elems[i] = `"` + s + `"`
	}
	return "{" + strings.Join(elems, ",") + "}", nil
}

func (a *TextArray) Scan(value interface{}) error {
	if value == nil {
		*a = TextArray{}
		return nil
	}
	var str string
	switch v := value.(type) {
	case string:
		str = v
	case []byte:
		str = string(v)
	default:
		return fmt.Errorf("TextArray.Scan: unsupported type %T", value)
	}
	str = strings.TrimPrefix(strings.TrimSuffix(str, "}"), "{")
	if str == "" {
		*a = TextArray{}
		return nil
	}
	parts := strings.Split(str, ",")
	result := make(TextArray, len(parts))
	for i, p := range parts {
		result[i] = strings.Trim(p, `"`)
	}
	*a = result
	return nil
}

// EscalationPolicy is a reusable ordered set of escalation steps that can be
// attached to a WorkflowState or ClassificationCriticality. When the SLA for
// that entity is breached the steps fire in order, each after an optional delay.
type EscalationPolicy struct {
	ID          uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Name        string         `gorm:"size:200;not null;uniqueIndex" json:"name"`
	Description string         `gorm:"size:1000" json:"description"`
	IsActive    bool           `gorm:"default:true" json:"is_active"`
	Steps       []EscalationPolicyStep `gorm:"foreignKey:PolicyID;constraint:OnDelete:CASCADE" json:"steps,omitempty"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (e *EscalationPolicy) BeforeCreate(_ *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}

// EscalationPolicyStep is one action within a policy.
// Steps are executed in ascending StepOrder order.
// DelayHours = 0 means fire immediately when the SLA is first breached;
// DelayHours = N means fire N hours after the breach started.
type EscalationPolicyStep struct {
	ID                uuid.UUID                    `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	PolicyID          uuid.UUID                    `gorm:"type:uuid;index;not null" json:"policy_id"`
	StepOrder         int                          `gorm:"default:1;not null" json:"step_order"`
	DelayHours        int                          `gorm:"default:0;not null" json:"delay_hours"` // hours after breach to fire
	Channel           string                       `gorm:"size:20;not null;default:'both'" json:"channel"` // "email" | "sms" | "both"
	EmailTemplateCode string                       `gorm:"size:100" json:"email_template_code"` // optional — overrides SLA_POLICY_BREACH
	SMSTemplateCode   string                       `gorm:"size:100" json:"sms_template_code"`   // optional — overrides SLA_POLICY_BREACH_SMS
	Targets           []EscalationPolicyStepTarget `gorm:"foreignKey:StepID;constraint:OnDelete:CASCADE" json:"targets,omitempty"`
	CreatedAt         time.Time                    `json:"created_at"`
	UpdatedAt         time.Time                    `json:"updated_at"`
}

func (e *EscalationPolicyStep) BeforeCreate(_ *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}

// EscalationPolicyStepTarget defines who to notify for a step.
// Users are resolved at notification time by querying all active users
// that belong to DepartmentID AND hold RoleID, then removing ExcludedUserIDs.
type EscalationPolicyStepTarget struct {
	ID              uuid.UUID   `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	StepID          uuid.UUID   `gorm:"type:uuid;index;not null" json:"step_id"`
	DepartmentID    *uuid.UUID  `gorm:"type:uuid;index" json:"department_id,omitempty"`
	Department      *Department `gorm:"foreignKey:DepartmentID" json:"department,omitempty"`
	RoleID          *uuid.UUID  `gorm:"type:uuid;index" json:"role_id,omitempty"`
	Role            *Role       `gorm:"foreignKey:RoleID" json:"role,omitempty"`
	// ExcludedUserIDs stores UUID strings of users to remove from the resolved set
	ExcludedUserIDs TextArray   `gorm:"type:text[]" json:"excluded_user_ids"`
	CreatedAt       time.Time   `json:"created_at"`
}

func (e *EscalationPolicyStepTarget) BeforeCreate(_ *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}

// ─── Request types ───────────────────────────────────────────────────────────

type EscalationPolicyStepTargetRequest struct {
	DepartmentID    *string  `json:"department_id" validate:"omitempty,uuid"`
	RoleID          *string  `json:"role_id" validate:"omitempty,uuid"`
	ExcludedUserIDs []string `json:"excluded_user_ids" validate:"omitempty,dive,uuid"`
}

type EscalationPolicyStepRequest struct {
	StepOrder         int                                 `json:"step_order" validate:"min=1"`
	DelayHours        int                                 `json:"delay_hours" validate:"min=0"`
	Channel           string                              `json:"channel" validate:"required,oneof=email sms both"`
	EmailTemplateCode string                              `json:"email_template_code"`
	SMSTemplateCode   string                              `json:"sms_template_code"`
	Targets           []EscalationPolicyStepTargetRequest `json:"targets"`
}

type CreateEscalationPolicyRequest struct {
	Name        string                         `json:"name" validate:"required,min=1,max=200"`
	Description string                         `json:"description" validate:"max=1000"`
	IsActive    *bool                          `json:"is_active"`
	Steps       []EscalationPolicyStepRequest  `json:"steps"`
}

type UpdateEscalationPolicyRequest struct {
	Name        *string                        `json:"name" validate:"omitempty,min=1,max=200"`
	Description *string                        `json:"description" validate:"omitempty,max=1000"`
	IsActive    *bool                          `json:"is_active"`
	Steps       []EscalationPolicyStepRequest  `json:"steps"` // nil = no change; non-nil = full replacement
}

// ─── Response types ──────────────────────────────────────────────────────────

type EscalationPolicyStepTargetResponse struct {
	ID              uuid.UUID           `json:"id"`
	DepartmentID    *uuid.UUID          `json:"department_id,omitempty"`
	Department      *DepartmentResponse `json:"department,omitempty"`
	RoleID          *uuid.UUID          `json:"role_id,omitempty"`
	Role            *RoleResponse       `json:"role,omitempty"`
	ExcludedUserIDs []string            `json:"excluded_user_ids"`
}

type EscalationPolicyStepResponse struct {
	ID                uuid.UUID                            `json:"id"`
	StepOrder         int                                  `json:"step_order"`
	DelayHours        int                                  `json:"delay_hours"`
	Channel           string                               `json:"channel"`
	EmailTemplateCode string                               `json:"email_template_code"`
	SMSTemplateCode   string                               `json:"sms_template_code"`
	Targets           []EscalationPolicyStepTargetResponse `json:"targets"`
}

type EscalationPolicyResponse struct {
	ID          uuid.UUID                       `json:"id"`
	Name        string                          `json:"name"`
	Description string                          `json:"description"`
	IsActive    bool                            `json:"is_active"`
	Steps       []EscalationPolicyStepResponse  `json:"steps"`
	CreatedAt   time.Time                       `json:"created_at"`
	UpdatedAt   time.Time                       `json:"updated_at"`
}

func ToEscalationPolicyResponse(p *EscalationPolicy) EscalationPolicyResponse {
	resp := EscalationPolicyResponse{
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		IsActive:    p.IsActive,
		Steps:       []EscalationPolicyStepResponse{},
		CreatedAt:   p.CreatedAt,
		UpdatedAt:   p.UpdatedAt,
	}
	for _, step := range p.Steps {
		sr := EscalationPolicyStepResponse{
			ID:                step.ID,
			StepOrder:         step.StepOrder,
			DelayHours:        step.DelayHours,
			Channel:           step.Channel,
			EmailTemplateCode: step.EmailTemplateCode,
			SMSTemplateCode:   step.SMSTemplateCode,
			Targets:           []EscalationPolicyStepTargetResponse{},
		}
		for _, t := range step.Targets {
			tr := EscalationPolicyStepTargetResponse{
				ID:              t.ID,
				DepartmentID:    t.DepartmentID,
				RoleID:          t.RoleID,
				ExcludedUserIDs: t.ExcludedUserIDs,
			}
			if tr.ExcludedUserIDs == nil {
				tr.ExcludedUserIDs = []string{}
			}
			if t.Department != nil {
				d := ToDepartmentResponse(t.Department)
				tr.Department = &d
			}
			if t.Role != nil {
				r := ToRoleResponse(t.Role)
				tr.Role = &r
			}
			sr.Targets = append(sr.Targets, tr)
		}
		resp.Steps = append(resp.Steps, sr)
	}
	return resp
}

// ─── EscalationGroupTarget ───────────────────────────────────────────────────

// EscalationGroupTarget replaces the flat Users many2many on EscalationGroup.
// Users are resolved at send time: all active users with DepartmentID + RoleID,
// minus ExcludedUserIDs.
type EscalationGroupTarget struct {
	ID                uuid.UUID   `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	EscalationGroupID uuid.UUID   `gorm:"type:uuid;index;not null" json:"escalation_group_id"`
	DepartmentID      *uuid.UUID  `gorm:"type:uuid;index" json:"department_id,omitempty"`
	Department        *Department `gorm:"foreignKey:DepartmentID" json:"department,omitempty"`
	RoleID            *uuid.UUID  `gorm:"type:uuid;index" json:"role_id,omitempty"`
	Role              *Role       `gorm:"foreignKey:RoleID" json:"role,omitempty"`
	ExcludedUserIDs   TextArray   `gorm:"type:text[]" json:"excluded_user_ids"`
	CreatedAt         time.Time   `json:"created_at"`
}

func (e *EscalationGroupTarget) BeforeCreate(_ *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}

type EscalationGroupTargetRequest struct {
	DepartmentID    *string  `json:"department_id" validate:"omitempty,uuid"`
	RoleID          *string  `json:"role_id" validate:"omitempty,uuid"`
	ExcludedUserIDs []string `json:"excluded_user_ids" validate:"omitempty,dive,uuid"`
}

type EscalationGroupTargetResponse struct {
	ID              uuid.UUID           `json:"id"`
	DepartmentID    *uuid.UUID          `json:"department_id,omitempty"`
	Department      *DepartmentResponse `json:"department,omitempty"`
	RoleID          *uuid.UUID          `json:"role_id,omitempty"`
	Role            *RoleResponse       `json:"role,omitempty"`
	ExcludedUserIDs []string            `json:"excluded_user_ids"`
}
