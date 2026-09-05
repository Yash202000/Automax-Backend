package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	KpiWFEntityPerformance = "kpi_performance"
	KpiWFEntityTarget      = "kpi_target"
	KpiWFEntityInitiative  = "initiative"
	KpiWFEntityGoal        = "strategic_goal"
	KpiWFEntityEntry       = "kpi_entry"
	KpiWFEntityDictionary  = "kpi_dictionary"
)

const (
	KpiWFStatusActive    = "active"
	KpiWFStatusCompleted = "completed"
	KpiWFStatusCancelled = "cancelled"
)

// ──────────────────────────────────────────────────────────
// KpiWorkflowInstance
// Attaches an existing Workflow run to a Goal Management entity.
// entity_type + entity_id identify what is being approved.
// ──────────────────────────────────────────────────────────

type KpiWorkflowInstance struct {
	ID              uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	WorkflowID      uuid.UUID      `gorm:"type:uuid;not null;index" json:"workflow_id"`
	Workflow        *Workflow      `gorm:"foreignKey:WorkflowID" json:"workflow,omitempty"`
	EntityType      string         `gorm:"size:50;not null" json:"entity_type"`
	EntityID        string         `gorm:"size:100;not null;index" json:"entity_id"`
	CurrentStateID  uuid.UUID      `gorm:"type:uuid;not null;index" json:"current_state_id"`
	CurrentState    *WorkflowState `gorm:"foreignKey:CurrentStateID" json:"current_state,omitempty"`
	InitiatedByID   uuid.UUID      `gorm:"type:uuid;not null;index" json:"initiated_by_id"`
	InitiatedBy     *User          `gorm:"foreignKey:InitiatedByID" json:"initiated_by,omitempty"`
	Status          string         `gorm:"size:30;not null;default:'active'" json:"status"`
	CreatedAt       time.Time      `json:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at"`
	DeletedAt       gorm.DeletedAt `gorm:"index" json:"-"`
}

func (k *KpiWorkflowInstance) BeforeCreate(tx *gorm.DB) error {
	if k.ID == uuid.Nil {
		k.ID = uuid.New()
	}
	return nil
}

type KpiWorkflowInstanceRequest struct {
	WorkflowID     uuid.UUID `json:"workflow_id" validate:"required"`
	EntityType     string    `json:"entity_type" validate:"required,oneof=kpi_performance kpi_target initiative strategic_goal kpi_entry"`
	EntityID       string    `json:"entity_id" validate:"required,max=100"`
	CurrentStateID uuid.UUID `json:"current_state_id" validate:"required"`
	InitiatedByID  uuid.UUID `json:"initiated_by_id" validate:"required"`
}

type KpiWorkflowInstanceResponse struct {
	ID              uuid.UUID          `json:"id"`
	WorkflowID      uuid.UUID          `json:"workflow_id"`
	EntityType      string             `json:"entity_type"`
	EntityID        string             `json:"entity_id"`
	CurrentStateID  uuid.UUID          `json:"current_state_id"`
	CurrentState    *WorkflowStateBrief `json:"current_state,omitempty"`
	InitiatedByID   uuid.UUID          `json:"initiated_by_id"`
	InitiatedBy     *UserBriefResponse `json:"initiated_by,omitempty"`
	Status          string             `json:"status"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

func (k *KpiWorkflowInstance) ToResponse() KpiWorkflowInstanceResponse {
	return KpiWorkflowInstanceResponse{
		ID:             k.ID,
		WorkflowID:     k.WorkflowID,
		EntityType:     k.EntityType,
		EntityID:       k.EntityID,
		CurrentStateID: k.CurrentStateID,
		CurrentState:   ToWorkflowStateBrief(k.CurrentState),
		InitiatedByID:  k.InitiatedByID,
		InitiatedBy:    ToUserBriefResponse(k.InitiatedBy),
		Status:         k.Status,
		CreatedAt:      k.CreatedAt,
		UpdatedAt:      k.UpdatedAt,
	}
}

// ──────────────────────────────────────────────────────────
// KpiWorkflowAction
// Records every transition performed on a KpiWorkflowInstance.
// ──────────────────────────────────────────────────────────

type KpiWorkflowAction struct {
	ID                 uuid.UUID           `gorm:"type:uuid;primary_key" json:"id"`
	WorkflowInstanceID uuid.UUID           `gorm:"type:uuid;not null;index" json:"workflow_instance_id"`
	WorkflowInstance   *KpiWorkflowInstance `gorm:"foreignKey:WorkflowInstanceID" json:"workflow_instance,omitempty"`
	TransitionID       uuid.UUID           `gorm:"type:uuid;not null;index" json:"transition_id"`
	Transition         *WorkflowTransition `gorm:"foreignKey:TransitionID" json:"transition,omitempty"`
	FromStateID        uuid.UUID           `gorm:"type:uuid;not null" json:"from_state_id"`
	FromState          *WorkflowState      `gorm:"foreignKey:FromStateID" json:"from_state,omitempty"`
	ToStateID          uuid.UUID           `gorm:"type:uuid;not null" json:"to_state_id"`
	ToState            *WorkflowState      `gorm:"foreignKey:ToStateID" json:"to_state,omitempty"`
	PerformedByID      uuid.UUID           `gorm:"type:uuid;not null;index" json:"performed_by_id"`
	PerformedBy        *User               `gorm:"foreignKey:PerformedByID" json:"performed_by,omitempty"`
	Comment            string              `gorm:"type:text" json:"comment"`
	PerformedAt        time.Time           `gorm:"index" json:"performed_at"`
	CreatedAt          time.Time           `json:"created_at"`
}

func (k *KpiWorkflowAction) BeforeCreate(tx *gorm.DB) error {
	if k.ID == uuid.Nil {
		k.ID = uuid.New()
	}
	if k.PerformedAt.IsZero() {
		k.PerformedAt = time.Now()
	}
	return nil
}

type KpiWorkflowActionRequest struct {
	WorkflowInstanceID uuid.UUID `json:"workflow_instance_id" validate:"required"`
	TransitionID       uuid.UUID `json:"transition_id" validate:"required"`
	FromStateID        uuid.UUID `json:"from_state_id" validate:"required"`
	ToStateID          uuid.UUID `json:"to_state_id" validate:"required"`
	PerformedByID      uuid.UUID `json:"performed_by_id" validate:"required"`
	Comment            string    `json:"comment"`
}

type KpiWorkflowActionResponse struct {
	ID                 uuid.UUID          `json:"id"`
	WorkflowInstanceID uuid.UUID          `json:"workflow_instance_id"`
	TransitionID       uuid.UUID          `json:"transition_id"`
	TransitionName     string             `json:"transition_name,omitempty"`
	FromStateName      string             `json:"from_state_name,omitempty"`
	ToStateName        string             `json:"to_state_name,omitempty"`
	PerformedByID      uuid.UUID          `json:"performed_by_id"`
	PerformedBy        *UserBriefResponse `json:"performed_by,omitempty"`
	Comment            string             `json:"comment"`
	PerformedAt        time.Time          `json:"performed_at"`
	CreatedAt          time.Time          `json:"created_at"`
}

func (k *KpiWorkflowAction) ToResponse() KpiWorkflowActionResponse {
	resp := KpiWorkflowActionResponse{
		ID:                 k.ID,
		WorkflowInstanceID: k.WorkflowInstanceID,
		TransitionID:       k.TransitionID,
		PerformedByID:      k.PerformedByID,
		PerformedBy:        ToUserBriefResponse(k.PerformedBy),
		Comment:            k.Comment,
		PerformedAt:        k.PerformedAt,
		CreatedAt:          k.CreatedAt,
	}
	if k.Transition != nil {
		resp.TransitionName = k.Transition.Name
	}
	if k.FromState != nil {
		resp.FromStateName = k.FromState.Name
	}
	if k.ToState != nil {
		resp.ToStateName = k.ToState.Name
	}
	return resp
}
