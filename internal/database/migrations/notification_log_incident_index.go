package migrations

import (
	"gorm.io/gorm"
)

// MigrateNotificationLogIncidentIndex adds a composite index on notification_logs(incident_id, created_at).
// The incident communication history query filters by incident_id and sorts by created_at ASC;
// without this index that query falls back to a sequential scan as the table grows.
func MigrateNotificationLogIncidentIndex(db *gorm.DB) error {
	migrationSQL := `
	CREATE INDEX IF NOT EXISTS idx_notification_logs_incident_created ON notification_logs(incident_id, created_at);
	`

	return db.Exec(migrationSQL).Error
}
