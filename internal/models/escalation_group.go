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

	// ClassificationID is kept for backward compatibility (nullable).
	// Prefer Classifications (many2many) for new groups.
	ClassificationID *uuid.UUID      `gorm:"type:uuid;index" json:"classification_id,omitempty"`
	Classification   *Classification `gorm:"foreignKey:ClassificationID" json:"classification,omitempty"`

	// Classifications supports multiple classifications per group.
	Classifications []Classification `gorm:"many2many:escalation_group_classifications;" json:"classifications,omitempty"`

	// How often to send: "daily" or "weekly" (weekly = every Monday)
	Frequency string `gorm:"size:20;not null;default:'daily'" json:"frequency"`

	// Which channels to use: "sms", "email", or "both"
	Channel string `gorm:"size:20;not null;default:'both'" json:"channel"`

	IsActive bool `gorm:"default:true" json:"is_active"`

	// Tracks when the last notification batch was sent (used for schedule enforcement)
	LastNotifiedAt *time.Time `json:"last_notified_at,omitempty"`

	// Targets defines who receives notifications (dept+role → resolved users minus exclusions).
	// If Targets is empty, falls back to legacy Users many2many.
	Targets []EscalationGroupTarget `gorm:"foreignKey:EscalationGroupID;constraint:OnDelete:CASCADE" json:"targets,omitempty"`

	// Users is retained for backward compatibility. Prefer Targets for new groups.
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
	Name              string                         `json:"name" validate:"required,min=1,max=200"`
	// ClassificationIDs supports selecting multiple classifications.
	ClassificationIDs []string                       `json:"classification_ids" validate:"omitempty,dive,uuid"`
	// ClassificationID is kept for backward compatibility (single).
	ClassificationID  string                         `json:"classification_id" validate:"omitempty,uuid"`
	Frequency         string                         `json:"frequency" validate:"required,oneof=daily weekly"`
	Channel           string                         `json:"channel" validate:"required,oneof=sms email both"`
	IsActive          *bool                          `json:"is_active"`
	Targets           []EscalationGroupTargetRequest `json:"targets"`
	// Deprecated: use Targets instead
	UserIDs []string `json:"user_ids" validate:"omitempty,dive,uuid"`
}

type UpdateEscalationGroupRequest struct {
	Name              *string                        `json:"name" validate:"omitempty,min=1,max=200"`
	// ClassificationIDs replaces all classifications when non-nil.
	ClassificationIDs []string                       `json:"classification_ids" validate:"omitempty,dive,uuid"`
	// ClassificationID is kept for backward compatibility (single).
	ClassificationID  *string                        `json:"classification_id" validate:"omitempty,uuid"`
	Frequency         *string                        `json:"frequency" validate:"omitempty,oneof=daily weekly"`
	Channel           *string                        `json:"channel" validate:"omitempty,oneof=sms email both"`
	IsActive          *bool                          `json:"is_active"`
	Targets           []EscalationGroupTargetRequest `json:"targets"` // nil = no change; non-nil = full replacement
	// Deprecated: use Targets instead
	UserIDs []string `json:"user_ids" validate:"omitempty,dive,uuid"`
}

// Response type ---

type EscalationGroupResponse struct {
	ID               uuid.UUID                       `json:"id"`
	Name             string                          `json:"name"`
	ClassificationID *uuid.UUID                      `json:"classification_id,omitempty"`
	Classification   *ClassificationResponse         `json:"classification,omitempty"`
	Classifications  []ClassificationResponse        `json:"classifications"`
	Frequency        string                          `json:"frequency"`
	Channel          string                          `json:"channel"`
	IsActive         bool                            `json:"is_active"`
	LastNotifiedAt   *time.Time                      `json:"last_notified_at,omitempty"`
	Targets          []EscalationGroupTargetResponse `json:"targets"`
	Users            []UserResponse                  `json:"users"` // legacy
	CreatedAt        time.Time                       `json:"created_at"`
	UpdatedAt        time.Time                       `json:"updated_at"`
}

func ToEscalationGroupResponse(g *EscalationGroup) EscalationGroupResponse {
	resp := EscalationGroupResponse{
		ID:              g.ID,
		Name:            g.Name,
		ClassificationID: g.ClassificationID,
		Frequency:       g.Frequency,
		Channel:         g.Channel,
		IsActive:        g.IsActive,
		LastNotifiedAt:  g.LastNotifiedAt,
		Classifications: []ClassificationResponse{},
		Targets:         []EscalationGroupTargetResponse{},
		Users:           []UserResponse{},
		CreatedAt:       g.CreatedAt,
		UpdatedAt:       g.UpdatedAt,
	}
	if g.Classification != nil {
		r := ToClassificationResponse(g.Classification)
		resp.Classification = &r
	}
	for _, c := range g.Classifications {
		resp.Classifications = append(resp.Classifications, ToClassificationResponse(&c))
	}
	for _, t := range g.Targets {
		tr := EscalationGroupTargetResponse{
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
		resp.Targets = append(resp.Targets, tr)
	}
	for _, u := range g.Users {
		resp.Users = append(resp.Users, ToUserResponse(&u))
	}
	return resp
}
