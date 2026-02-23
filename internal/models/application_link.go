package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ApplicationLink represents an external application shortcut displayed on the dashboard
type ApplicationLink struct {
	ID             uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	Name           string         `gorm:"not null;size:100" json:"name"`
	Description    string         `gorm:"size:500" json:"description"`
	URL            string         `gorm:"not null;size:500" json:"url"`
	Icon           string         `gorm:"size:50;default:'ExternalLink'" json:"icon"` // Lucide icon name (fallback if no image)
	ImageURL       string         `gorm:"size:500" json:"image_url"`                  // Uploaded logo image URL
	Color          string         `gorm:"size:50;default:'blue'" json:"color"`        // Color scheme: blue, violet, emerald, amber, rose, orange
	SortOrder      int            `gorm:"default:0" json:"sort_order"`
	IsActive       bool           `gorm:"default:true" json:"is_active"`
	SSOEnabled     bool           `gorm:"default:false" json:"sso_enabled"`
	SSOCallbackURL string         `gorm:"size:500" json:"sso_callback_url"` // e.g. https://target.app/sso/callback
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (a *ApplicationLink) BeforeCreate(tx *gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}

// Request/Response DTOs

type ApplicationLinkCreateRequest struct {
	Name           string `json:"name" validate:"required,min=2,max=100"`
	Description    string `json:"description" validate:"max=500"`
	URL            string `json:"url" validate:"required,url,max=500"`
	Icon           string `json:"icon" validate:"max=50"`
	ImageURL       string `json:"image_url" validate:"max=500"`
	Color          string `json:"color" validate:"max=50"`
	SortOrder      int    `json:"sort_order"`
	IsActive       bool   `json:"is_active"`
	SSOEnabled     bool   `json:"sso_enabled"`
	SSOCallbackURL string `json:"sso_callback_url" validate:"omitempty,url,max=500"`
}

type ApplicationLinkUpdateRequest struct {
	Name           string  `json:"name" validate:"omitempty,min=2,max=100"`
	Description    string  `json:"description" validate:"max=500"`
	URL            string  `json:"url" validate:"omitempty,url,max=500"`
	Icon           string  `json:"icon" validate:"max=50"`
	ImageURL       string  `json:"image_url" validate:"max=500"`
	Color          string  `json:"color" validate:"max=50"`
	SortOrder      *int    `json:"sort_order"`
	IsActive       *bool   `json:"is_active"`
	SSOEnabled     *bool   `json:"sso_enabled"`
	SSOCallbackURL *string `json:"sso_callback_url" validate:"omitempty,url,max=500"`
}

type ApplicationLinkResponse struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	URL            string    `json:"url"`
	Icon           string    `json:"icon"`
	ImageURL       string    `json:"image_url"`
	Color          string    `json:"color"`
	SortOrder      int       `json:"sort_order"`
	IsActive       bool      `json:"is_active"`
	SSOEnabled     bool      `json:"sso_enabled"`
	SSOCallbackURL string    `json:"sso_callback_url"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Converter function
func ToApplicationLinkResponse(link *ApplicationLink) ApplicationLinkResponse {
	return ApplicationLinkResponse{
		ID:             link.ID.String(),
		Name:           link.Name,
		Description:    link.Description,
		URL:            link.URL,
		Icon:           link.Icon,
		ImageURL:       link.ImageURL,
		Color:          link.Color,
		SortOrder:      link.SortOrder,
		IsActive:       link.IsActive,
		SSOEnabled:     link.SSOEnabled,
		SSOCallbackURL: link.SSOCallbackURL,
		CreatedAt:      link.CreatedAt,
		UpdatedAt:      link.UpdatedAt,
	}
}
