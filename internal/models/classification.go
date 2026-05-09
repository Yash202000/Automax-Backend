package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ClassificationType represents a single type entry for a classification (many-to-many via join table)
type ClassificationType struct {
	ID               uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	ClassificationID uuid.UUID `gorm:"type:uuid;index;not null" json:"classification_id"`
	Type             string    `gorm:"size:20;not null;index" json:"type"` // 'incident', 'request', 'complaint', 'query', 'mobile', 'ivr'
}

func (ct *ClassificationType) BeforeCreate(tx *gorm.DB) error {
	if ct.ID == uuid.Nil {
		ct.ID = uuid.New()
	}
	return nil
}

// Classification represents a hierarchical classification (e.g., Incident > Hardware > Laptop)
type Classification struct {
	ID            uuid.UUID                   `gorm:"type:uuid;primary_key" json:"id"`
	Name          string                      `gorm:"not null;size:100" json:"name"`
	NameAr        string                      `gorm:"size:100" json:"name_ar"`
	Description   string                      `gorm:"size:500" json:"description"`
	DescriptionAr string                      `gorm:"size:500" json:"description_ar"`
	Types         []ClassificationType        `gorm:"foreignKey:ClassificationID" json:"types,omitempty"`
	ParentID      *uuid.UUID                  `gorm:"type:uuid;index" json:"parent_id"`
	Parent        *Classification             `gorm:"foreignKey:ParentID" json:"parent,omitempty"`
	Children      []Classification            `gorm:"foreignKey:ParentID" json:"children,omitempty"`
	Level         int                         `gorm:"default:0" json:"level"`
	Path          string                      `gorm:"size:1000" json:"path"` // Materialized path for efficient queries
	IsActive      bool                        `gorm:"default:true" json:"is_active"`
	SortOrder     int                         `gorm:"default:0" json:"sort_order"`
	Criticalities []ClassificationCriticality `gorm:"foreignKey:ClassificationID" json:"criticalities,omitempty"`
	CreatedAt     time.Time                   `json:"created_at"`
	UpdatedAt     time.Time                   `json:"updated_at"`
	DeletedAt     gorm.DeletedAt              `gorm:"index" json:"-"`
}

// ClassificationCriticality represents the max closing time for a classification based on criticality
type ClassificationCriticality struct {
	ID                uuid.UUID       `gorm:"type:uuid;primary_key" json:"id"`
	ClassificationID  uuid.UUID       `gorm:"type:uuid;index;not null" json:"classification_id"`
	Classification    *Classification `gorm:"foreignKey:ClassificationID" json:"classification,omitempty"`
	CriticalityID     uuid.UUID       `gorm:"type:uuid;index;not null" json:"criticality_id"` // References LookupValue ID for criticality
	Criticality       *LookupValue    `gorm:"foreignKey:CriticalityID" json:"criticality,omitempty"`
	MaxClosingHours   int             `gorm:"not null;default:0" json:"max_closing_hours"`   // Hours component
	MaxClosingMinutes int             `gorm:"not null;default:0" json:"max_closing_minutes"` // Minutes component (0-59)
	IsActive          bool            `gorm:"default:true" json:"is_active"`
	// EscalationPolicyID links the escalation policy to fire when this classification+criticality SLA is breached.
	EscalationPolicyID *uuid.UUID        `gorm:"type:uuid;index" json:"escalation_policy_id,omitempty"`
	EscalationPolicy   *EscalationPolicy `gorm:"foreignKey:EscalationPolicyID" json:"escalation_policy,omitempty"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
	DeletedAt         gorm.DeletedAt  `gorm:"index" json:"-"`
}

func (c *Classification) BeforeCreate(tx *gorm.DB) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return nil
}

func (cc *ClassificationCriticality) BeforeCreate(tx *gorm.DB) error {
	if cc.ID == uuid.Nil {
		cc.ID = uuid.New()
	}
	return nil
}

// ClassificationCreateRequest for creating a new classification
type ClassificationCreateRequest struct {
	Name          string     `json:"name" validate:"required,min=1,max=100"`
	NameAr        string     `json:"name_ar" validate:"max=100"`
	Description   string     `json:"description" validate:"max=500"`
	DescriptionAr string     `json:"description_ar" validate:"max=500"`
	Types       []string   `json:"types" validate:"omitempty,dive,oneof=incident request complaint query mobile ivr"`
	ParentID    *uuid.UUID `json:"parent_id"`
	SortOrder   int        `json:"sort_order"`
}

// ClassificationUpdateRequest for updating a classification
type ClassificationUpdateRequest struct {
	Name          string   `json:"name" validate:"min=1,max=100"`
	NameAr        string   `json:"name_ar" validate:"max=100"`
	Description   string   `json:"description" validate:"max=500"`
	DescriptionAr string   `json:"description_ar" validate:"max=500"`
	Types       []string `json:"types" validate:"omitempty,dive,oneof=incident request complaint query mobile ivr"`
	IsActive    *bool    `json:"is_active"`
	SortOrder   *int     `json:"sort_order"`
}

// ClassificationResponse for API responses
type ClassificationResponse struct {
	ID            uuid.UUID                           `json:"id"`
	Name          string                              `json:"name"`
	NameAr        string                              `json:"name_ar"`
	Description   string                              `json:"description"`
	DescriptionAr string                              `json:"description_ar"`
	Types         []string                            `json:"types"`
	ParentID      *uuid.UUID                          `json:"parent_id"`
	Level         int                                 `json:"level"`
	Path          string                              `json:"path"`
	IsActive      bool                                `json:"is_active"`
	SortOrder     int                                 `json:"sort_order"`
	Criticalities []ClassificationCriticalityResponse `json:"criticalities,omitempty"`
	Children      []ClassificationResponse            `json:"children,omitempty"`
	CreatedAt     time.Time                           `json:"created_at"`
}

// extractTypeStrings converts []ClassificationType to a plain []string of type values
func extractTypeStrings(types []ClassificationType) []string {
	result := make([]string, len(types))
	for i, t := range types {
		result[i] = t.Type
	}
	return result
}

func ToClassificationResponse(c *Classification) ClassificationResponse {
	resp := ClassificationResponse{
		ID:            c.ID,
		Name:          c.Name,
		NameAr:        c.NameAr,
		Description:   c.Description,
		DescriptionAr: c.DescriptionAr,
		Types:         extractTypeStrings(c.Types),
		ParentID:      c.ParentID,
		Level:         c.Level,
		Path:          c.Path,
		IsActive:      c.IsActive,
		SortOrder:     c.SortOrder,
		CreatedAt:     c.CreatedAt,
	}

	if len(c.Criticalities) > 0 {
		resp.Criticalities = make([]ClassificationCriticalityResponse, len(c.Criticalities))
		for i, crit := range c.Criticalities {
			resp.Criticalities[i] = ToClassificationCriticalityResponse(&crit)
		}
	}

	if len(c.Children) > 0 {
		resp.Children = make([]ClassificationResponse, len(c.Children))
		for i, child := range c.Children {
			resp.Children[i] = ToClassificationResponse(&child)
		}
	}

	return resp
}

// ClassificationWithStats includes classification data with incident count
type ClassificationWithStats struct {
	ID            uuid.UUID                 `json:"id"`
	Name          string                    `json:"name"`
	NameAr        string                    `json:"name_ar"`
	Description   string                    `json:"description"`
	DescriptionAr string                    `json:"description_ar"`
	Types       []string                  `json:"types"`
	ParentID    *uuid.UUID                `json:"parent_id"`
	Level       int                       `json:"level"`
	Path        string                    `json:"path"`
	IsActive    bool                      `json:"is_active"`
	SortOrder   int                       `json:"sort_order"`
	Count       int64                     `json:"count"`
	Children    []ClassificationWithStats `json:"children,omitempty"`
	CreatedAt   time.Time                 `json:"created_at"`
}

// ClassificationCriticalityCreateRequest for creating classification criticality settings
// MaxClosingHours: 5 weeks = 840 hours maximum
type ClassificationCriticalityCreateRequest struct {
	CriticalityID        string  `json:"criticality_id" validate:"required,uuid"`
	MaxClosingHours      int     `json:"max_closing_hours" validate:"required,min=0,max=840"` // Max 35 days
	MaxClosingMinutes    int     `json:"max_closing_minutes" validate:"required,min=0,max=59"`
	EscalationPolicyID   *string `json:"escalation_policy_id" validate:"omitempty,uuid"`
}

// ClassificationCriticalityUpdateRequest for updating classification criticality settings
// MaxClosingHours: 5 weeks = 840 hours maximum
type ClassificationCriticalityUpdateRequest struct {
	MaxClosingHours      *int    `json:"max_closing_hours" validate:"omitempty,min=0,max=840"`
	MaxClosingMinutes    *int    `json:"max_closing_minutes" validate:"omitempty,min=0,max=59"`
	IsActive             *bool   `json:"is_active"`
	EscalationPolicyID   *string `json:"escalation_policy_id" validate:"omitempty,uuid"`
}

// ClassificationCriticalityResponse for API responses
type ClassificationCriticalityResponse struct {
	ID                   uuid.UUID                  `json:"id"`
	ClassificationID     uuid.UUID                  `json:"classification_id"`
	CriticalityID        uuid.UUID                  `json:"criticality_id"`
	Criticality          *LookupValueResponse       `json:"criticality,omitempty"`
	MaxClosingHours      int                        `json:"max_closing_hours"`
	MaxClosingMinutes    int                        `json:"max_closing_minutes"`
	IsActive             bool                       `json:"is_active"`
	EscalationPolicyID   *uuid.UUID                 `json:"escalation_policy_id,omitempty"`
	EscalationPolicy     *EscalationPolicyResponse  `json:"escalation_policy,omitempty"`
	CreatedAt            time.Time                  `json:"created_at"`
	UpdatedAt            time.Time                  `json:"updated_at"`
}

// ToClassificationCriticalityResponse converts ClassificationCriticality to response
func ToClassificationCriticalityResponse(cc *ClassificationCriticality) ClassificationCriticalityResponse {
	resp := ClassificationCriticalityResponse{
		ID:                   cc.ID,
		ClassificationID:     cc.ClassificationID,
		CriticalityID:        cc.CriticalityID,
		MaxClosingHours:      cc.MaxClosingHours,
		MaxClosingMinutes:    cc.MaxClosingMinutes,
		IsActive:             cc.IsActive,
		EscalationPolicyID:   cc.EscalationPolicyID,
		CreatedAt:            cc.CreatedAt,
		UpdatedAt:            cc.UpdatedAt,
	}
	if cc.EscalationPolicy != nil {
		r := ToEscalationPolicyResponse(cc.EscalationPolicy)
		resp.EscalationPolicy = &r
	}
	if cc.Criticality != nil {
		resp.Criticality = &LookupValueResponse{
			ID:          cc.Criticality.ID,
			CategoryID:  cc.Criticality.CategoryID,
			Code:        cc.Criticality.Code,
			Name:        cc.Criticality.Name,
			NameAr:      cc.Criticality.NameAr,
			Description: cc.Criticality.Description,
			SortOrder:   cc.Criticality.SortOrder,
			Color:       cc.Criticality.Color,
			IsDefault:   cc.Criticality.IsDefault,
			IsActive:    cc.Criticality.IsActive,
			CreatedAt:   cc.Criticality.CreatedAt,
			UpdatedAt:   cc.Criticality.UpdatedAt,
		}
	}
	return resp
}

// ClassificationCreateRequestWithCriticalities extends ClassificationCreateRequest with criticalities
type ClassificationCreateRequestWithCriticalities struct {
	Name          string                                   `json:"name" validate:"required,min=1,max=100"`
	NameAr        string                                   `json:"name_ar" validate:"max=100"`
	Description   string                                   `json:"description" validate:"max=500"`
	DescriptionAr string                                   `json:"description_ar" validate:"max=500"`
	Types         []string                                 `json:"types" validate:"omitempty,dive,oneof=incident request complaint query mobile ivr"`
	ParentID      *uuid.UUID                               `json:"parent_id"`
	SortOrder     int                                      `json:"sort_order"`
	Criticalities []ClassificationCriticalityCreateRequest `json:"criticalities,omitempty"`
}
