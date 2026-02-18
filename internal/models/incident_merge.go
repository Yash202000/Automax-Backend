package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// IncidentMerge tracks the relationship between merged incidents and their master
type IncidentMerge struct {
	ID                  uuid.UUID  `gorm:"type:uuid;primary_key" json:"id"`
	MasterIncidentID    uuid.UUID  `gorm:"type:uuid;not null;index" json:"master_incident_id"`
	MasterIncident      *Incident  `gorm:"foreignKey:MasterIncidentID" json:"master_incident,omitempty"`
	RelatedIncidentID   uuid.UUID  `gorm:"type:uuid;not null;index" json:"related_incident_id"`
	RelatedIncident     *Incident  `gorm:"foreignKey:RelatedIncidentID" json:"related_incident,omitempty"`
	MergedByUserID      *uuid.UUID `gorm:"type:uuid;index" json:"merged_by_user_id,omitempty"`
	MergedBy            *User      `gorm:"foreignKey:MergedByUserID" json:"merged_by,omitempty"`
	MergedAt            *time.Time `gorm:"index" json:"merged_at"`
	UnmergedByUserID    *uuid.UUID `gorm:"type:uuid" json:"unmerged_by_user_id,omitempty"`
	UnmergedBy          *User      `gorm:"foreignKey:UnmergedByUserID" json:"unmerged_by,omitempty"`
	UnmergedAt          *time.Time `json:"unmerged_at,omitempty"`
	IsActive            bool       `gorm:"default:true;index" json:"is_active"`
	Notes               string     `gorm:"type:text" json:"notes,omitempty"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (m *IncidentMerge) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}

// IncidentMergeCreateRequest for creating merge with new master
type IncidentMergeCreateRequest struct {
	IncidentIDs []string `json:"incident_ids" validate:"required,min=2,dive,uuid"`
	Title       string   `json:"title" validate:"required,min=5,max=200"`
	Description string   `json:"description"`
	Comment     string   `json:"comment"`
}
