package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// EscalationGroup defines a custom escalation notification group.

type EscalationGroup struct {
	ID   uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey" json:"id"`
	Name string    `gorm:"size:200;not null" json:"name"`

	// Classification that triggers this escalation group
	ClassificationID uuid.UUID       `gorm:"type:uuid;index;not null" json:"classification_id"`
	Classification   *Classification `gorm:"foreignKey:ClassificationID" json:"classification,omitempty"`

	// How often to send: "daily" or "weekly" (weekly = every Monday)
	Frequency string `gorm:"size:20;not null;default:'daily'" json:"frequency"`

	// Which channels to use: "sms", "email", or "both"
	Channel string `gorm:"size:20;not null;default:'both'" json:"channel"`

	IsActive bool `gorm:"default:true" json:"is_active"`

	// Tracks when the last notification batch was sent (used for schedule enforcement)
	LastNotifiedAt *time.Time `json:"last_notified_at,omitempty"`

	// Users belonging to this escalation group (many-to-many)
	Users []User `gorm:"many2many:escalation_group_users;" json:"users,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (e *EscalationGroup) BeforeCreate(_ *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}

//Request types ---

type CreateEscalationGroupRequest struct {
	Name             string   `json:"name" validate:"required,min=1,max=200"`
	ClassificationID string   `json:"classification_id" validate:"required,uuid"`
	Frequency        string   `json:"frequency" validate:"required,oneof=daily weekly"`
	Channel          string   `json:"channel" validate:"required,oneof=sms email both"`
	IsActive         *bool    `json:"is_active"`
	UserIDs          []string `json:"user_ids" validate:"omitempty,dive,uuid"`
}

type UpdateEscalationGroupRequest struct {
	Name             *string  `json:"name" validate:"omitempty,min=1,max=200"`
	ClassificationID *string  `json:"classification_id" validate:"omitempty,uuid"`
	Frequency        *string  `json:"frequency" validate:"omitempty,oneof=daily weekly"`
	Channel          *string  `json:"channel" validate:"omitempty,oneof=sms email both"`
	IsActive         *bool    `json:"is_active"`
	UserIDs          []string `json:"user_ids" validate:"omitempty,dive,uuid"`
}

// Response type ---

type EscalationGroupResponse struct {
	ID               uuid.UUID               `json:"id"`
	Name             string                  `json:"name"`
	ClassificationID uuid.UUID               `json:"classification_id"`
	Classification   *ClassificationResponse `json:"classification,omitempty"`
	Frequency        string                  `json:"frequency"`
	Channel          string                  `json:"channel"`
	IsActive         bool                    `json:"is_active"`
	LastNotifiedAt   *time.Time              `json:"last_notified_at,omitempty"`
	Users            []UserResponse          `json:"users"`
	CreatedAt        time.Time               `json:"created_at"`
	UpdatedAt        time.Time               `json:"updated_at"`
}

func ToEscalationGroupResponse(g *EscalationGroup) EscalationGroupResponse {
	resp := EscalationGroupResponse{
		ID:               g.ID,
		Name:             g.Name,
		ClassificationID: g.ClassificationID,
		Frequency:        g.Frequency,
		Channel:          g.Channel,
		IsActive:         g.IsActive,
		LastNotifiedAt:   g.LastNotifiedAt,
		Users:            []UserResponse{},
		CreatedAt:        g.CreatedAt,
		UpdatedAt:        g.UpdatedAt,
	}
	if g.Classification != nil {
		r := ToClassificationResponse(g.Classification)
		resp.Classification = &r
	}
	for _, u := range g.Users {
		resp.Users = append(resp.Users, ToUserResponse(&u))
	}
	return resp
}
