package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CommentTemplate struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	CommentText string    `gorm:"type:text" json:"comment_text"`

	WorkflowTransitionID uuid.UUID           `gorm:"type:uuid;index;not null" json:"workflow_transition_id"`
	WorkflowTransition   *WorkflowTransition `gorm:"foreignKey:WorkflowTransitionID" json:"workflow_transition,omitempty"`

	// Who made the change
	UpdatedBy string `gorm:"type:string;not null" json:"updated_by"`

	IsActive  bool           `gorm:"default:true" json:"is_active"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (c *CommentTemplate) BeforeCreate(tx *gorm.DB) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return nil
}

// CommentTemplateCreateRequest is the payload for creating a new comment template.
type CommentTemplateCreateRequest struct {
	CommentText          string    `json:"comment_text" validate:"required,min=1,max=1000"`
	WorkflowTransitionID uuid.UUID `json:"workflow_transition_id" validate:"required"`
}

// CommentTemplateUpdateRequest is the payload for updating a comment template.
type CommentTemplateUpdateRequest struct {
	CommentText *string `json:"comment_text" validate:"omitempty,min=1,max=1000"`
	IsActive    *bool   `json:"is_active"`
}

// CommentTemplateResponse is the API response shape for a comment template.
type CommentTemplateResponse struct {
	ID                   uuid.UUID           `json:"id"`
	CommentText          string              `json:"comment_text"`
	WorkflowTransitionID uuid.UUID           `json:"workflow_transition_id"`
	WorkflowTransition   *WorkflowTransition `json:"workflow_transition,omitempty"`
	UpdatedBy            string              `json:"updated_by"`
	PerformedBy          *User               `json:"performed_by,omitempty"`
	IsActive             bool                `json:"is_active"`
	CreatedAt            time.Time           `json:"created_at"`
	UpdatedAt            time.Time           `json:"updated_at"`
}

// ToCommentTemplateResponse converts a CommentTemplate model to its response DTO.
func ToCommentTemplateResponse(c *CommentTemplate) CommentTemplateResponse {
	return CommentTemplateResponse{
		ID:                   c.ID,
		CommentText:          c.CommentText,
		WorkflowTransitionID: c.WorkflowTransitionID,
		WorkflowTransition:   c.WorkflowTransition,
		UpdatedBy:            c.UpdatedBy,
		IsActive:             c.IsActive,
		CreatedAt:            c.CreatedAt,
		UpdatedAt:            c.UpdatedAt,
	}
}
