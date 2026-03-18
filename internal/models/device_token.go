package models

import (
	"time"

	"github.com/google/uuid"
)

type DeviceToken struct {
	ID          uuid.UUID `gorm:"primaryKey" json:"id"`
	UserID      string    `gorm:"type:uuid;index" json:"user_id"`
	DeviceToken string    `gorm:"size:2000;uniqueIndex;not null" json:"device_token"`
	DeviceType  string    `gorm:"size:20;not null" json:"device_type"`
	IsActive    bool      `gorm:"default:true" json:"is_active"`
	LastUsed    time.Time `json:"last_used"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type RegisterTokenRequest struct {
	UserID      string `json:"user_id" validate:"required"`
	DeviceToken string `json:"device_token" validate:"required"`
	DeviceType  string `json:"device_type"`
}
