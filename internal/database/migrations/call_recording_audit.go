package migrations

import (
	"gorm.io/gorm"
)

// MigrateCallRecordingAudit makes call_recording_audits append-only at the
// database level: any UPDATE, DELETE or TRUNCATE raises an exception, so no one
// (administrators and raw SQL from the application user included) can edit or
// remove an entry. The table itself is created by AutoMigrate.
//
// Idempotent: CREATE OR REPLACE for the function, DROP ... IF EXISTS for triggers.
func MigrateCallRecordingAudit(db *gorm.DB) error {
	return db.Exec(`
		CREATE OR REPLACE FUNCTION call_recording_audits_immutable() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'call_recording_audits entries are immutable (% blocked)', TG_OP
				USING ERRCODE = 'insufficient_privilege';
		END;
		$$ LANGUAGE plpgsql;

		DROP TRIGGER IF EXISTS trg_call_recording_audits_immutable ON call_recording_audits;
		CREATE TRIGGER trg_call_recording_audits_immutable
			BEFORE UPDATE OR DELETE ON call_recording_audits
			FOR EACH ROW EXECUTE FUNCTION call_recording_audits_immutable();

		DROP TRIGGER IF EXISTS trg_call_recording_audits_no_truncate ON call_recording_audits;
		CREATE TRIGGER trg_call_recording_audits_no_truncate
			BEFORE TRUNCATE ON call_recording_audits
			FOR EACH STATEMENT EXECUTE FUNCTION call_recording_audits_immutable();
	`).Error
}
