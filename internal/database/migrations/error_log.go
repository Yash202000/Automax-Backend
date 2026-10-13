package migrations

import "gorm.io/gorm"

// MigrateErrorLogSequence creates the sequence used to generate ERR-000123 style error codes.
func MigrateErrorLogSequence(db *gorm.DB) error {
	return db.Exec(`CREATE SEQUENCE IF NOT EXISTS error_log_seq START 1`).Error
}
