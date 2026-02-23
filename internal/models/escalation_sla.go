package models

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type EscalationSLA struct {
	ID               uuid.UUID      `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	UserID           uuid.UUID      `gorm:"type:uuid;not null"`
	Actions          pq.StringArray `gorm:"type:text[]"` // ["SMS","EMAIL"]
	LocationID       uuid.UUID      `gorm:"type:uuid"`
	ClassificationID uuid.UUID      `gorm:"type:uuid"`
	ReportFrequency  string         `gorm:"type:varchar(20)"` // DAILY / WEEKLY
	CreatedAt        time.Time
}
