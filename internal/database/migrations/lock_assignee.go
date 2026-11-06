package migrations

import (
	"gorm.io/gorm"
)

// MigrateLockAssignee adds the lock_assignee column to workflow_states.
// When true, an incident's assignee cannot be changed manually while it is in that state.
func MigrateLockAssignee(db *gorm.DB) error {
	migrationSQL := `
	ALTER TABLE workflow_states ADD COLUMN IF NOT EXISTS lock_assignee BOOLEAN NOT NULL DEFAULT FALSE;
	`

	return db.Exec(migrationSQL).Error
}
