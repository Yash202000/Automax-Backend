package migrations

import (
	"gorm.io/gorm"
)

// MigrateUserCallStatusDefault changes the call_status column default from 'offline' to 'online'
func MigrateUserCallStatusDefault(db *gorm.DB) error {
	return db.Exec(`ALTER TABLE users ALTER COLUMN call_status SET DEFAULT 'online'`).Error
}
