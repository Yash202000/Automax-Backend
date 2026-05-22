package migrations

import (
	"gorm.io/gorm"
)

// MigrateCallLogRecordingURL drops the recording_url column from call_logs.
// Recording URLs are now generated on-the-fly from call_log_attachments.
func MigrateCallLogRecordingURL(db *gorm.DB) error {
	return db.Exec(`ALTER TABLE call_logs DROP COLUMN IF EXISTS recording_url`).Error
}

// MigrateCallParticipantPhone drops the user_id column from call_participants.
// Participants are now identified solely by phone_number; user resolution happens at read time.
func MigrateCallParticipantPhone(db *gorm.DB) error {
	return db.Exec(`
		ALTER TABLE call_participants DROP COLUMN IF EXISTS user_id;
		ALTER TABLE call_participants ALTER COLUMN phone_number SET NOT NULL;
	`).Error
}
