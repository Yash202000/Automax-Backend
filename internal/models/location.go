package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Location represents a hierarchical location (e.g., Country > State > City > Building > Floor)
type Location struct {
	ID            uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	Name          string         `gorm:"not null;size:100" json:"name"`
	NameAr        string         `gorm:"size:100" json:"name_ar"`
	Code          string         `gorm:"size:50;uniqueIndex" json:"code"` // Short code like "US", "NY", "NYC-HQ"
	Description   string         `gorm:"size:500" json:"description"`
	DescriptionAr string         `gorm:"size:500" json:"description_ar"`
	Type          string         `gorm:"size:50" json:"type"` // country, state, city, building, floor, room
	ParentID      *uuid.UUID     `gorm:"type:uuid;index" json:"parent_id"`
	Parent        *Location      `gorm:"foreignKey:ParentID" json:"parent,omitempty"`
	Children      []Location     `gorm:"foreignKey:ParentID" json:"children,omitempty"`
	Level         int            `gorm:"default:0" json:"level"`
	Path          string         `gorm:"size:1000" json:"path"`
	Address       string         `gorm:"size:500" json:"address"`
	Latitude      *float64       `gorm:"type:decimal(10,8)" json:"latitude"`
	Longitude     *float64       `gorm:"type:decimal(11,8)" json:"longitude"`
	IsActive      bool           `gorm:"default:true" json:"is_active"`
	SortOrder     int            `gorm:"default:0" json:"sort_order"`
	Source        string         `gorm:"size:20;default:'master'" json:"source"` // "master" or "map"
	ExternalID    string         `gorm:"size:100" json:"external_id"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

func (l *Location) BeforeCreate(tx *gorm.DB) error {
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	return nil
}

// LocationCreateRequest for creating a new location
type LocationCreateRequest struct {
	Name          string     `json:"name" validate:"required,notblank,name,min=1,max=100"`
	NameAr        string     `json:"name_ar" validate:"omitempty,notblank,name,max=100"`
	Code          string     `json:"code" validate:"max=50"`
	Description   string     `json:"description" validate:"max=500"`
	DescriptionAr string     `json:"description_ar" validate:"max=500"`
	Type          string     `json:"type" validate:"max=50"`
	ParentID      *uuid.UUID `json:"parent_id"`
	Address       string     `json:"address" validate:"max=500"`
	Latitude      *float64   `json:"latitude" validate:"omitempty,min=-90,max=90"`
	Longitude     *float64   `json:"longitude" validate:"omitempty,min=-180,max=180"`
	SortOrder     int        `json:"sort_order"`
	Source        string     `json:"source"` // "master" or "map" (reverse geocoding)
	// ExternalID is a free-form reference to an external system's ID for this location
	// (e.g. MOMRA's MunicipalityID/SubMunicipalityID) — same pattern as Classification
	// and Department's ExternalID fields used elsewhere in the MOMRA integration.
	ExternalID string `json:"external_id" validate:"max=100"`
}

// LocationUpdateRequest for updating a location
type LocationUpdateRequest struct {
	Name          string   `json:"name" validate:"omitempty,notblank,name,min=1,max=100"`
	NameAr        string   `json:"name_ar" validate:"omitempty,notblank,name,max=100"`
	Code          string   `json:"code" validate:"max=50"`
	Description   string   `json:"description" validate:"max=500"`
	DescriptionAr string   `json:"description_ar" validate:"max=500"`
	Type          string   `json:"type" validate:"max=50"`
	Address       string   `json:"address" validate:"max=500"`
	Latitude      *float64 `json:"latitude" validate:"omitempty,min=-90,max=90"`
	Longitude     *float64 `json:"longitude" validate:"omitempty,min=-180,max=180"`
	IsActive      *bool    `json:"is_active"`
	SortOrder     *int     `json:"sort_order"`
	ExternalID    *string  `json:"external_id" validate:"omitempty,max=100"`
}

// LocationResponse for API responses
type LocationResponse struct {
	ID            uuid.UUID          `json:"id"`
	Name          string             `json:"name"`
	NameAr        string             `json:"name_ar"`
	Code          string             `json:"code"`
	Description   string             `json:"description"`
	DescriptionAr string             `json:"description_ar"`
	Type          string             `json:"type"`
	ParentID      *uuid.UUID         `json:"parent_id"`
	Level         int                `json:"level"`
	Path          string             `json:"path"`
	Address       string             `json:"address"`
	Latitude      *float64           `json:"latitude,omitempty"`
	Longitude     *float64           `json:"longitude,omitempty"`
	IsActive      bool               `json:"is_active"`
	SortOrder     int                `json:"sort_order"`
	Source        string             `json:"source"`
	ExternalID    string             `json:"external_id"`
	Children      []LocationResponse `json:"children,omitempty"`
	CreatedAt     time.Time          `json:"created_at"`
}

func ToLocationResponse(l *Location) LocationResponse {
	resp := LocationResponse{
		ID:            l.ID,
		Name:          l.Name,
		NameAr:        l.NameAr,
		Code:          l.Code,
		Description:   l.Description,
		DescriptionAr: l.DescriptionAr,
		Type:          l.Type,
		ParentID:      l.ParentID,
		Level:         l.Level,
		Path:          l.Path,
		Address:       l.Address,
		Latitude:      l.Latitude,
		Longitude:     l.Longitude,
		IsActive:      l.IsActive,
		SortOrder:     l.SortOrder,
		Source:        l.Source,
		ExternalID:    l.ExternalID,
		CreatedAt:     l.CreatedAt,
	}

	if len(l.Children) > 0 {
		resp.Children = make([]LocationResponse, len(l.Children))
		for i, child := range l.Children {
			resp.Children[i] = ToLocationResponse(&child)
		}
	}

	return resp
}

// LocationWithStats includes location data with incident count
type LocationWithStats struct {
	ID          uuid.UUID           `json:"id"`
	Name        string              `json:"name"`
	Code        string              `json:"code"`
	Description string              `json:"description"`
	Type        string              `json:"type"`
	ParentID    *uuid.UUID          `json:"parent_id"`
	Level       int                 `json:"level"`
	Path        string              `json:"path"`
	Address     string              `json:"address"`
	Latitude    *float64            `json:"latitude,omitempty"`
	Longitude   *float64            `json:"longitude,omitempty"`
	IsActive    bool                `json:"is_active"`
	SortOrder   int                 `json:"sort_order"`
	Count       int64               `json:"count"`
	Children    []LocationWithStats `json:"children,omitempty"`
	CreatedAt   time.Time           `json:"created_at"`
}
