package migrations

import "gorm.io/gorm"

// MigrateAwardKpiMeasureCodeSequence makes Postgres assign award_kpi_measures.code
// (KPI-AWRD-MSR-01, -02, …) from a sequence, so codes are generated DB-side,
// are race-free, and are never reused after a soft-delete.
//
// A generated column driven by the model alone is not viable: GORM's AutoMigrate
// re-issues ALTER COLUMN ... GENERATED on every startup, which Postgres rejects.
//
// Idempotent: safe to run on every startup.
func MigrateAwardKpiMeasureCodeSequence(db *gorm.DB) error {
	if !hasTable(db, "award_kpi_measures") {
		return nil
	}
	if err := db.Exec(`CREATE SEQUENCE IF NOT EXISTS award_kpi_measure_code_seq`).Error; err != nil {
		return err
	}
	return db.Exec(`
		ALTER TABLE award_kpi_measures
		ALTER COLUMN code SET DEFAULT 'KPI-AWRD-MSR-' || lpad(nextval('award_kpi_measure_code_seq')::text, 2, '0')
	`).Error
}
