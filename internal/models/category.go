package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Category represents a hierarchical, admin-managed taxonomy used by goals
// (and potentially other modules). Tree depth is bounded to 5 levels at the
// service layer. Mirrors the Classification pattern but without types/criticalities.
type Category struct {
	ID            uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	Name          string         `gorm:"size:255;not null" json:"name"`
	NameAr        string         `gorm:"size:255" json:"name_ar"`
	Code          string         `gorm:"size:100;uniqueIndex;not null" json:"code"`
	Description   string         `gorm:"type:text" json:"description"`
	DescriptionAr string         `gorm:"type:text" json:"description_ar"`
	ParentID    *uuid.UUID     `gorm:"type:uuid;index" json:"parent_id"`
	Parent      *Category      `gorm:"foreignKey:ParentID" json:"parent,omitempty"`
	Children    []Category     `gorm:"foreignKey:ParentID" json:"children,omitempty"`
	Level       int            `gorm:"default:0" json:"level"`
	Path        string         `gorm:"size:500;index" json:"path"` // materialized path e.g. "/root/child"
	IsActive    bool           `gorm:"default:true" json:"is_active"`
	SortOrder   int            `gorm:"default:0" json:"sort_order"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (c *Category) BeforeCreate(tx *gorm.DB) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return nil
}

// CategoryCreateRequest is the payload for creating a new category.
type CategoryCreateRequest struct {
	Name          string     `json:"name" validate:"required,min=1,max=255"`
	NameAr        string     `json:"name_ar"`
	Code          string     `json:"code" validate:"required,min=1,max=100"`
	Description   string     `json:"description"`
	DescriptionAr string     `json:"description_ar"`
	ParentID      *uuid.UUID `json:"parent_id"`
	SortOrder     int        `json:"sort_order"`
}

// CategoryUpdateRequest is the payload for updating a category.
// ParentID changes are not permitted — would require rewriting descendant paths.
type CategoryUpdateRequest struct {
	Name          *string `json:"name" validate:"omitempty,min=1,max=255"`
	NameAr        *string `json:"name_ar"`
	Description   *string `json:"description"`
	DescriptionAr *string `json:"description_ar"`
	IsActive      *bool   `json:"is_active"`
	SortOrder     *int    `json:"sort_order"`
}

// CategoryResponse is the API response shape for a category node.
// Children are populated recursively when the tree endpoint is used.
type CategoryResponse struct {
	ID            uuid.UUID          `json:"id"`
	Name          string             `json:"name"`
	NameAr        string             `json:"name_ar"`
	Code          string             `json:"code"`
	Description   string             `json:"description"`
	DescriptionAr string             `json:"description_ar"`
	ParentID      *uuid.UUID         `json:"parent_id"`
	Level         int                `json:"level"`
	Path          string             `json:"path"`
	IsActive      bool               `json:"is_active"`
	SortOrder     int                `json:"sort_order"`
	Children      []CategoryResponse `json:"children,omitempty"`
	CreatedAt     time.Time          `json:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at"`
}

// ToCategoryResponse converts a Category model to its response DTO.
// Children are recursively mapped but Parent is intentionally dropped to avoid loops.
func ToCategoryResponse(c *Category) CategoryResponse {
	resp := CategoryResponse{
		ID:            c.ID,
		Name:          c.Name,
		NameAr:        c.NameAr,
		Code:          c.Code,
		Description:   c.Description,
		DescriptionAr: c.DescriptionAr,
		ParentID:      c.ParentID,
		Level:         c.Level,
		Path:          c.Path,
		IsActive:      c.IsActive,
		SortOrder:     c.SortOrder,
		CreatedAt:     c.CreatedAt,
		UpdatedAt:     c.UpdatedAt,
	}

	if len(c.Children) > 0 {
		resp.Children = make([]CategoryResponse, len(c.Children))
		for i, child := range c.Children {
			resp.Children[i] = ToCategoryResponse(&child)
		}
	}

	return resp
}
