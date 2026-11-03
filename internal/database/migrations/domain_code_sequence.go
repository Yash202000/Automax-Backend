package migrations

import "gorm.io/gorm"

// MigrateDomainCodeSequence makes Postgres assign domains.code (KPI-DOM-01,
// -02, …) from a sequence, so codes are generated DB-side, are race-free, and
// are never reused after a soft-delete. Existing rows (including soft-deleted
// ones) are backfilled in creation order, then the column is made NOT NULL.
//
// Idempotent: safe to run on every startup.
func MigrateDomainCodeSequence(db *gorm.DB) error {
	if !hasTable(db, "domains") {
		return nil
	}
	if err := db.Exec(`CREATE SEQUENCE IF NOT EXISTS domain_code_seq`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		UPDATE domains d
		SET code = 'KPI-DOM-' || lpad(nextval('domain_code_seq')::text, 2, '0')
		FROM (
			SELECT id FROM domains WHERE code IS NULL OR code = ''
			ORDER BY created_at, id
		) o
		WHERE d.id = o.id
	`).Error; err != nil {
		return err
	}
	if err := db.Exec(`
		ALTER TABLE domains
		ALTER COLUMN code SET DEFAULT 'KPI-DOM-' || lpad(nextval('domain_code_seq')::text, 2, '0')
	`).Error; err != nil {
		return err
	}

	return db.Exec(`ALTER TABLE domains ALTER COLUMN code SET NOT NULL`).Error
}
