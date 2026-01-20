package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// LookupCategory represents a category of lookup values (e.g., Priority, Severity, Nationality)
type LookupCategory struct {
	ID                uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	Code              string         `gorm:"size:50;uniqueIndex;not null" json:"code"`
	Name              string         `gorm:"size:100;not null" json:"name"`
	NameAr            string         `gorm:"size:100" json:"name_ar"`
	Description       string         `gorm:"size:500" json:"description"`
	IsSystem          bool           `gorm:"default:false" json:"is_system"`
	IsActive          bool           `gorm:"default:true" json:"is_active"`
	AddToIncidentForm bool           `gorm:"default:false" json:"add_to_incident_form"` // New field
	Values            []LookupValue  `gorm:"foreignKey:CategoryID" json:"values,omitempty"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"-"`
}

func (l *LookupCategory) BeforeCreate(tx *gorm.DB) error {
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	return nil
}

// LookupValue represents a single value in a lookup category
type LookupValue struct {
	ID          uuid.UUID       `gorm:"type:uuid;primary_key" json:"id"`
	CategoryID  uuid.UUID       `gorm:"type:uuid;index;not null" json:"category_id"`
	Category    *LookupCategory `gorm:"foreignKey:CategoryID" json:"category,omitempty"`
	Code        string          `gorm:"size:50;not null" json:"code"`
	Name        string          `gorm:"size:100;not null" json:"name"`
	NameAr      string          `gorm:"size:100" json:"name_ar"`
	Description string          `gorm:"size:500" json:"description"`
	SortOrder   int             `gorm:"default:0" json:"sort_order"`
	Color       string          `gorm:"size:50" json:"color"`
	IsDefault   bool            `gorm:"default:false" json:"is_default"`
	IsActive    bool            `gorm:"default:true" json:"is_active"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	DeletedAt   gorm.DeletedAt  `gorm:"index" json:"-"`
}

func (l *LookupValue) BeforeCreate(tx *gorm.DB) error {
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	return nil
}

// Request types

// LookupCategoryCreateRequest for creating a new lookup category
type LookupCategoryCreateRequest struct {
	Code              string `json:"code" validate:"required,min=1,max=50"`
	Name              string `json:"name" validate:"required,min=1,max=100"`
	NameAr            string `json:"name_ar" validate:"max=100"`
	Description       string `json:"description" validate:"max=500"`
	IsActive          *bool  `json:"is_active"`
	AddToIncidentForm *bool  `json:"add_to_incident_form"`
}

// LookupCategoryUpdateRequest for updating a lookup category
type LookupCategoryUpdateRequest struct {
	Code              string `json:"code" validate:"max=50"`
	Name              string `json:"name" validate:"max=100"`
	NameAr            string `json:"name_ar" validate:"max=100"`
	Description       string `json:"description" validate:"max=500"`
	IsActive          *bool  `json:"is_active"`
	AddToIncidentForm *bool  `json:"add_to_incident_form"`
}

// LookupValueCreateRequest for creating a new lookup value
type LookupValueCreateRequest struct {
	Code        string `json:"code" validate:"required,min=1,max=50"`
	Name        string `json:"name" validate:"required,min=1,max=100"`
	NameAr      string `json:"name_ar" validate:"max=100"`
	Description string `json:"description" validate:"max=500"`
	SortOrder   int    `json:"sort_order"`
	Color       string `json:"color" validate:"max=50"`
	IsDefault   bool   `json:"is_default"`
	IsActive    *bool  `json:"is_active"`
}

// LookupValueUpdateRequest for updating a lookup value
type LookupValueUpdateRequest struct {
	Code        string `json:"code" validate:"max=50"`
	Name        string `json:"name" validate:"max=100"`
	NameAr      string `json:"name_ar" validate:"max=100"`
	Description string `json:"description" validate:"max=500"`
	SortOrder   *int   `json:"sort_order"`
	Color       string `json:"color" validate:"max=50"`
	IsDefault   *bool  `json:"is_default"`
	IsActive    *bool  `json:"is_active"`
}

// Response types

// LookupCategoryResponse for API responses
type LookupCategoryResponse struct {
	ID                uuid.UUID             `json:"id"`
	Code              string                `json:"code"`
	Name              string                `json:"name"`
	NameAr            string                `json:"name_ar"`
	Description       string                `json:"description"`
	IsSystem          bool                  `json:"is_system"`
	IsActive          bool                  `json:"is_active"`
	AddToIncidentForm bool                  `json:"add_to_incident_form"`
	ValuesCount       int                   `json:"values_count"`
	Values            []LookupValueResponse `json:"values,omitempty"`
	CreatedAt         time.Time             `json:"created_at"`
	UpdatedAt         time.Time             `json:"updated_at"`
}

// LookupValueResponse for API responses
type LookupValueResponse struct {
	ID          uuid.UUID                `json:"id"`
	CategoryID  uuid.UUID                `json:"category_id"`
	Category    *LookupCategoryResponse  `json:"category,omitempty"`
	Code        string                   `json:"code"`
	Name        string                   `json:"name"`
	NameAr      string                   `json:"name_ar"`
	Description string                   `json:"description"`
	SortOrder   int                      `json:"sort_order"`
	Color       string                   `json:"color"`
	IsDefault   bool                     `json:"is_default"`
	IsActive    bool                     `json:"is_active"`
	CreatedAt   time.Time                `json:"created_at"`
	UpdatedAt   time.Time                `json:"updated_at"`
}

// ToLookupCategoryResponse converts a LookupCategory to LookupCategoryResponse
func ToLookupCategoryResponse(c *LookupCategory) LookupCategoryResponse {
	resp := LookupCategoryResponse{
		ID:                c.ID,
		Code:              c.Code,
		Name:              c.Name,
		NameAr:            c.NameAr,
		Description:       c.Description,
		IsSystem:          c.IsSystem,
		IsActive:          c.IsActive,
		AddToIncidentForm: c.AddToIncidentForm,
		ValuesCount:       len(c.Values),
		CreatedAt:         c.CreatedAt,
		UpdatedAt:         c.UpdatedAt,
	}

	if len(c.Values) > 0 {
		resp.Values = make([]LookupValueResponse, len(c.Values))
		for i, v := range c.Values {
			resp.Values[i] = ToLookupValueResponse(&v)
		}
	}

	return resp
}

// ToLookupValueResponse converts a LookupValue to LookupValueResponse
func ToLookupValueResponse(v *LookupValue) LookupValueResponse {
	resp := LookupValueResponse{
		ID:          v.ID,
		CategoryID:  v.CategoryID,
		Code:        v.Code,
		Name:        v.Name,
		NameAr:      v.NameAr,
		Description: v.Description,
		SortOrder:   v.SortOrder,
		Color:       v.Color,
		IsDefault:   v.IsDefault,
		IsActive:    v.IsActive,
		CreatedAt:   v.CreatedAt,
		UpdatedAt:   v.UpdatedAt,
	}
	if v.Category != nil {
		catResp := ToLookupCategoryResponse(v.Category)
		resp.Category = &catResp
	}
	return resp
}
