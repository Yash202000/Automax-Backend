package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ──────────────────────────────────────────────────
// Goal Template
// ──────────────────────────────────────────────────

type GoalTemplate struct {
	ID                   uuid.UUID                    `gorm:"type:uuid;primary_key" json:"id"`
	Name                 string                       `gorm:"size:255;not null;uniqueIndex" json:"name"`
	Description          string                       `gorm:"type:text" json:"description"`
	Category             string                       `gorm:"size:100" json:"category"`
	Priority             string                       `gorm:"size:20;default:'Medium'" json:"priority"`
	DefaultMetrics       TemplateMetricArray           `gorm:"type:jsonb;default:'[]'" json:"default_metrics"`
	DefaultCollaborators TemplateCollaboratorRoleArray `gorm:"type:jsonb;default:'[]'" json:"default_collaborators"`
	WorkflowID           *uuid.UUID                   `gorm:"type:uuid" json:"workflow_id"`
	IsActive             bool                         `gorm:"default:true" json:"is_active"`
	CreatedByID          uuid.UUID                    `gorm:"type:uuid;index" json:"created_by_id"`
	CreatedBy            *User                        `gorm:"foreignKey:CreatedByID" json:"created_by,omitempty"`
	CreatedAt            time.Time                    `json:"created_at"`
	UpdatedAt            time.Time                    `json:"updated_at"`
	DeletedAt            gorm.DeletedAt               `gorm:"index" json:"-"`
}

func (t *GoalTemplate) BeforeCreate(tx *gorm.DB) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	return nil
}

// ──────────────────────────────────────────────────
// JSONB value types
// ──────────────────────────────────────────────────

type TemplateMetric struct {
	Name          string  `json:"name"`
	MetricType    string  `json:"metric_type"`
	Unit          string  `json:"unit"`
	BaselineValue float64 `json:"baseline_value"`
	TargetValue   float64 `json:"target_value"`
	Weight        float64 `json:"weight"`
}

type TemplateCollaboratorRole struct {
	Role string `json:"role"`
}

type TemplateMetricArray []TemplateMetric

func (a TemplateMetricArray) Value() (driver.Value, error) {
	return json.Marshal(a)
}

func (a *TemplateMetricArray) Scan(value interface{}) error {
	if value == nil {
		*a = TemplateMetricArray{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal TemplateMetricArray: %v", value)
	}
	return json.Unmarshal(bytes, a)
}

type TemplateCollaboratorRoleArray []TemplateCollaboratorRole

func (a TemplateCollaboratorRoleArray) Value() (driver.Value, error) {
	return json.Marshal(a)
}

func (a *TemplateCollaboratorRoleArray) Scan(value interface{}) error {
	if value == nil {
		*a = TemplateCollaboratorRoleArray{}
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("failed to unmarshal TemplateCollaboratorRoleArray: %v", value)
	}
	return json.Unmarshal(bytes, a)
}

// ──────────────────────────────────────────────────
// Request Types
// ──────────────────────────────────────────────────

type GoalTemplateCreateRequest struct {
	Name                 string                       `json:"name" validate:"required,max=255"`
	Description          string                       `json:"description"`
	Category             string                       `json:"category" validate:"max=100"`
	Priority             string                       `json:"priority" validate:"oneof=Critical High Medium Low"`
	DefaultMetrics       TemplateMetricArray           `json:"default_metrics"`
	DefaultCollaborators TemplateCollaboratorRoleArray `json:"default_collaborators"`
	WorkflowID           *uuid.UUID                   `json:"workflow_id"`
	IsActive             bool                         `json:"is_active"`
}

type GoalTemplateUpdateRequest struct {
	Name                 *string                       `json:"name" validate:"omitempty,max=255"`
	Description          *string                       `json:"description"`
	Category             *string                       `json:"category" validate:"omitempty,max=100"`
	Priority             *string                       `json:"priority" validate:"omitempty,oneof=Critical High Medium Low"`
	DefaultMetrics       *TemplateMetricArray           `json:"default_metrics"`
	DefaultCollaborators *TemplateCollaboratorRoleArray `json:"default_collaborators"`
	WorkflowID           *uuid.UUID                    `json:"workflow_id"`
	IsActive             *bool                         `json:"is_active"`
}

type GoalTemplateFilter struct {
	Page     int    `query:"page"`
	Limit    int    `query:"limit"`
	Search   string `query:"search"`
	IsActive *bool  `query:"is_active"`
}

// ──────────────────────────────────────────────────
// Response Types
// ──────────────────────────────────────────────────

type GoalTemplateResponse struct {
	ID                   uuid.UUID                    `json:"id"`
	Name                 string                       `json:"name"`
	Description          string                       `json:"description"`
	Category             string                       `json:"category"`
	Priority             string                       `json:"priority"`
	DefaultMetrics       TemplateMetricArray           `json:"default_metrics"`
	DefaultCollaborators TemplateCollaboratorRoleArray `json:"default_collaborators"`
	WorkflowID           *uuid.UUID                   `json:"workflow_id"`
	IsActive             bool                         `json:"is_active"`
	CreatedByID          uuid.UUID                    `json:"created_by_id"`
	CreatedBy            *UserBriefResponse           `json:"created_by,omitempty"`
	CreatedAt            time.Time                    `json:"created_at"`
	UpdatedAt            time.Time                    `json:"updated_at"`
}

func (t *GoalTemplate) ToResponse() GoalTemplateResponse {
	return GoalTemplateResponse{
		ID:                   t.ID,
		Name:                 t.Name,
		Description:          t.Description,
		Category:             t.Category,
		Priority:             t.Priority,
		DefaultMetrics:       t.DefaultMetrics,
		DefaultCollaborators: t.DefaultCollaborators,
		WorkflowID:           t.WorkflowID,
		IsActive:             t.IsActive,
		CreatedByID:          t.CreatedByID,
		CreatedBy:            ToUserBriefResponse(t.CreatedBy),
		CreatedAt:            t.CreatedAt,
		UpdatedAt:            t.UpdatedAt,
	}
}
