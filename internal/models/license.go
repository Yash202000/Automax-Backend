package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// License represents the application license stored in the database.
// This is a single-row table — only one active license at a time.
type License struct {
	ID               uuid.UUID      `gorm:"type:uuid;primary_key;default:gen_random_uuid()" json:"id"`
	EncryptedKey     string         `gorm:"type:text;not null" json:"-"`         // AES-256-GCM encrypted JWT
	KeyNonce         string         `gorm:"type:text;not null" json:"-"`         // GCM nonce (base64)
	LicenseID        string         `gorm:"size:100" json:"license_id"`          // From JWT: license_id
	ClientName       string         `gorm:"size:255" json:"client_name"`
	ClientEmail      string         `gorm:"size:255" json:"client_email"`
	CompanyName      string         `gorm:"size:255" json:"company_name"`
	Product          string         `gorm:"size:100" json:"product"`
	LicenseType      string         `gorm:"size:50" json:"license_type"`         // trial/standard/professional/enterprise
	Features         datatypes.JSON `gorm:"type:jsonb" json:"features"`          // []string
	MaxUsers         int            `gorm:"default:1" json:"max_users"`
	ExpiresAt        *time.Time     `json:"expires_at"`
	ActivatedAt      *time.Time     `json:"activated_at"`
	ActivatedBy      *uuid.UUID     `gorm:"type:uuid" json:"activated_by"`
	ValidationStatus string         `gorm:"size:50;default:'pending'" json:"validation_status"` // pending/valid/expired/invalid
	PublicKey        string         `gorm:"type:text" json:"-"`                  // Cached PEM public key
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
}

// BeforeCreate sets UUID if not provided.
func (l *License) BeforeCreate(tx *gorm.DB) error {
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	return nil
}

// LicenseActivateRequest is the request body for activating a license.
type LicenseActivateRequest struct {
	LicenseKey string `json:"license_key" validate:"required"`
	PublicKey  string `json:"public_key" validate:"required"`
}

// LicenseStatusResponse is the full license status (admin only).
type LicenseStatusResponse struct {
	LicenseID        string   `json:"license_id"`
	ClientName       string   `json:"client_name"`
	ClientEmail      string   `json:"client_email"`
	CompanyName      string   `json:"company_name"`
	Product          string   `json:"product"`
	LicenseType      string   `json:"license_type"`
	Features         []string `json:"features"`
	MaxUsers         int      `json:"max_users"`
	ActiveUserCount  int64    `json:"active_user_count"`
	ExpiresAt        *string  `json:"expires_at"`
	DaysRemaining    *int     `json:"days_remaining"`
	IsGracePeriod    bool     `json:"is_grace_period"`
	ValidationStatus string   `json:"validation_status"`
	ActivatedAt      *string  `json:"activated_at"`
	ActivatedBy      *string  `json:"activated_by"`
}

// LicenseInfoResponse is the public license info (all authenticated users).
type LicenseInfoResponse struct {
	LicenseType      string   `json:"license_type"`
	Features         []string `json:"features"`
	MaxUsers         int      `json:"max_users"`
	ActiveUserCount  int64    `json:"active_user_count"`
	ExpiresAt        *string  `json:"expires_at"`
	DaysRemaining    *int     `json:"days_remaining"`
	IsGracePeriod    bool     `json:"is_grace_period"`
	ValidationStatus string   `json:"validation_status"`
}
