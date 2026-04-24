package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type FeedbackTemplate struct {
	ID           uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	FeedbackText string    `gorm:"type:text" json:"feedback_text"`

	WorkflowTransitionID uuid.UUID           `gorm:"type:uuid;index;not null" json:"workflow_transition_id"`
	WorkflowTransition   *WorkflowTransition `gorm:"foreignKey:WorkflowTransitionID" json:"workflow_transition,omitempty"`

	// Who made the change
	UpdatedBy string `gorm:"type:string;not null" json:"updated_by"`

	IsActive  bool           `gorm:"default:true" json:"is_active"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (f *FeedbackTemplate) BeforeCreate(tx *gorm.DB) error {
	if f.ID == uuid.Nil {
		f.ID = uuid.New()
	}
	return nil
}

// FeedbackTemplateCreateRequest is the payload for creating a new feedback template.
type FeedbackTemplateCreateRequest struct {
	FeedbackText         string    `json:"feedback_text" validate:"required,min=1,max=1000"`
	WorkflowTransitionID uuid.UUID `json:"workflow_transition_id" validate:"required"`
}

// FeedbackTemplateUpdateRequest is the payload for updating a feedback template.
type FeedbackTemplateUpdateRequest struct {
	FeedbackText *string `json:"feedback_text" validate:"omitempty,min=1,max=1000"`
	IsActive     *bool   `json:"is_active"`
}

// FeedbackTemplateResponse is the API response shape for a feedback template.
type FeedbackTemplateResponse struct {
	ID                   uuid.UUID           `json:"id"`
	FeedbackText         string              `json:"feedback_text"`
	WorkflowTransitionID uuid.UUID           `json:"workflow_transition_id"`
	WorkflowTransition   *WorkflowTransition `json:"workflow_transition,omitempty"`
	UpdatedBy            string              `json:"updated_by"`
	IsActive             bool                `json:"is_active"`
	CreatedAt            time.Time           `json:"created_at"`
	UpdatedAt            time.Time           `json:"updated_at"`
}

// ToFeedbackTemplateResponse converts a FeedbackTemplate model to its response DTO.
func ToFeedbackTemplateResponse(f *FeedbackTemplate) FeedbackTemplateResponse {
	return FeedbackTemplateResponse{
		ID:                   f.ID,
		FeedbackText:         f.FeedbackText,
		WorkflowTransitionID: f.WorkflowTransitionID,
		WorkflowTransition:   f.WorkflowTransition,
		UpdatedBy:            f.UpdatedBy,
		IsActive:             f.IsActive,
		CreatedAt:            f.CreatedAt,
		UpdatedAt:            f.UpdatedAt,
	}
}

func ToFeedbackTemplateResponseList(templates []FeedbackTemplate) []FeedbackTemplateResponse {
	result := make([]FeedbackTemplateResponse, len(templates))
	for i := range templates {
		result[i] = ToFeedbackTemplateResponse(&templates[i])
	}
	return result
}
