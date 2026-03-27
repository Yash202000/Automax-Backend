package migrations

import (
	"gorm.io/gorm"
)

// MigrateUserMobileVerified adds the mobile_verified column to the users table
func MigrateUserMobileVerified(db *gorm.DB) error {
	// Check if column exists before adding
	if !db.Migrator().HasColumn(&User{}, "mobile_verified") {
		migrationSQL := `
			-- Add mobile_verified column to users table
			ALTER TABLE users ADD COLUMN IF NOT EXISTS mobile_verified BOOLEAN DEFAULT false;
		`
		return db.Exec(migrationSQL).Error
	}
	return nil
}

// User is a minimal struct for migration purposes
type User struct {
	ID string `gorm:"type:uuid;primary_key"`
}
