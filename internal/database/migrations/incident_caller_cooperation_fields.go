package migrations

import "gorm.io/gorm"

// MigrateIncidentCallerCooperationFields adds caller_identity_text (text, 200)
// and cooperation_status (varchar 50, no DB constraint; app-validated). Idempotent.
func MigrateIncidentCallerCooperationFields(db *gorm.DB) error {
	migrationSQL := `
	ALTER TABLE incidents ADD COLUMN IF NOT EXISTS caller_identity_text VARCHAR(200) NOT NULL DEFAULT '';
	ALTER TABLE incidents ADD COLUMN IF NOT EXISTS cooperation_status VARCHAR(50) NOT NULL DEFAULT '';
	`

	return db.Exec(migrationSQL).Error
}
