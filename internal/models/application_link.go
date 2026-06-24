package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ApplicationLink represents an external application shortcut displayed on the dashboard.
// A link with no ParentID is a root card. If it has Children it acts as a group — clicking
// it opens a sub-links modal instead of navigating directly.
type ApplicationLink struct {
	ID              uuid.UUID         `gorm:"type:uuid;primary_key" json:"id"`
	ParentID        *uuid.UUID        `gorm:"type:uuid;index" json:"parent_id"`
	Name            string            `gorm:"not null;size:100" json:"name"`
	NameAr          string            `gorm:"size:100" json:"name_ar"`
	Description     string            `gorm:"size:500" json:"description"`
	DescriptionAr   string            `gorm:"size:500" json:"description_ar"`
	URL             string            `gorm:"size:500" json:"url"`
	Icon            string            `gorm:"size:50;default:'ExternalLink'" json:"icon"`
	ImageURL        string            `gorm:"size:500" json:"image_url"`
	Color           string            `gorm:"size:50;default:'blue'" json:"color"`
	SortOrder       int               `gorm:"default:0" json:"sort_order"`
	IsActive        bool              `gorm:"default:true" json:"is_active"`
	SSOEnabled      bool              `gorm:"default:false" json:"sso_enabled"`
	SSOCallbackURL  string            `gorm:"size:500" json:"sso_callback_url"`
	SSORedirectPath string            `gorm:"size:500" json:"sso_redirect_path"`
	Children        []ApplicationLink `gorm:"foreignKey:ParentID" json:"children,omitempty"`
	CreatedAt       time.Time         `json:"created_at"`
	UpdatedAt       time.Time         `json:"updated_at"`
	DeletedAt       gorm.DeletedAt    `gorm:"index" json:"-"`
}

func (a *ApplicationLink) BeforeCreate(tx *gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}

// Request/Response DTOs

type ApplicationLinkCreateRequest struct {
	ParentID        *string `json:"parent_id" validate:"omitempty,uuid"`
	Name            string  `json:"name" validate:"required,min=2,max=100"`
	NameAr          string  `json:"name_ar" validate:"max=100"`
	Description     string  `json:"description" validate:"max=500"`
	DescriptionAr   string  `json:"description_ar" validate:"max=500"`
	URL             string  `json:"url" validate:"omitempty,url,max=500"`
	Icon            string  `json:"icon" validate:"max=50"`
	ImageURL        string  `json:"image_url" validate:"max=500"`
	Color           string  `json:"color" validate:"max=50"`
	SortOrder       int     `json:"sort_order"`
	IsActive        bool    `json:"is_active"`
	SSOEnabled      bool    `json:"sso_enabled"`
	SSOCallbackURL  string  `json:"sso_callback_url" validate:"omitempty,url,max=500"`
	SSORedirectPath string  `json:"sso_redirect_path" validate:"max=500"`
}

type ApplicationLinkUpdateRequest struct {
	ParentID        *string `json:"parent_id" validate:"omitempty,uuid"`
	Name            string  `json:"name" validate:"omitempty,min=2,max=100"`
	NameAr          string  `json:"name_ar" validate:"max=100"`
	Description     string  `json:"description" validate:"max=500"`
	DescriptionAr   string  `json:"description_ar" validate:"max=500"`
	URL             string  `json:"url" validate:"omitempty,url,max=500"`
	Icon            string  `json:"icon" validate:"max=50"`
	ImageURL        string  `json:"image_url" validate:"max=500"`
	Color           string  `json:"color" validate:"max=50"`
	SortOrder       *int    `json:"sort_order"`
	IsActive        *bool   `json:"is_active"`
	SSOEnabled      *bool   `json:"sso_enabled"`
	SSOCallbackURL  *string `json:"sso_callback_url" validate:"omitempty,url,max=500"`
	SSORedirectPath *string `json:"sso_redirect_path" validate:"omitempty,max=500"`
	ClearParent     *bool   `json:"clear_parent"` // set true to remove parent (make root)
}

type ApplicationLinkResponse struct {
	ID              string                     `json:"id"`
	ParentID        *string                    `json:"parent_id,omitempty"`
	Name            string                     `json:"name"`
	NameAr          string                     `json:"name_ar,omitempty"`
	Description     string                     `json:"description"`
	DescriptionAr   string                     `json:"description_ar,omitempty"`
	URL             string                     `json:"url"`
	Icon            string                     `json:"icon"`
	ImageURL        string                     `json:"image_url"`
	Color           string                     `json:"color"`
	SortOrder       int                        `json:"sort_order"`
	IsActive        bool                       `json:"is_active"`
	SSOEnabled      bool                       `json:"sso_enabled"`
	SSOCallbackURL  string                     `json:"sso_callback_url"`
	SSORedirectPath string                     `json:"sso_redirect_path"`
	Children        []ApplicationLinkResponse  `json:"children,omitempty"`
	CreatedAt       time.Time                  `json:"created_at"`
	UpdatedAt       time.Time                  `json:"updated_at"`
}

// ToApplicationLinkResponse converts a model to its response DTO.
// Children are converted recursively (1 level deep for dashboard use).
func ToApplicationLinkResponse(link *ApplicationLink) ApplicationLinkResponse {
	resp := ApplicationLinkResponse{
		ID:              link.ID.String(),
		Name:            link.Name,
		NameAr:          link.NameAr,
		Description:     link.Description,
		DescriptionAr:   link.DescriptionAr,
		URL:             link.URL,
		Icon:            link.Icon,
		ImageURL:        link.ImageURL,
		Color:           link.Color,
		SortOrder:       link.SortOrder,
		IsActive:        link.IsActive,
		SSOEnabled:      link.SSOEnabled,
		SSOCallbackURL:  link.SSOCallbackURL,
		SSORedirectPath: link.SSORedirectPath,
		CreatedAt:       link.CreatedAt,
		UpdatedAt:       link.UpdatedAt,
	}
	if link.ParentID != nil {
		s := link.ParentID.String()
		resp.ParentID = &s
	}
	if len(link.Children) > 0 {
		resp.Children = make([]ApplicationLinkResponse, len(link.Children))
		for i := range link.Children {
			resp.Children[i] = ToApplicationLinkResponse(&link.Children[i])
		}
	}
	return resp
}
