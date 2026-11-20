package migrations

import (
	"log"

	"gorm.io/gorm"
)

// MigrateKpiTargetUniqueIndexV4 closes two gaps in idx_kpi_target_unique_v3
// (MigrateKpiEntryMultiOrgScope), which must run first:
//
//  1. v3 keys on the raw metric_id, and Postgres never treats two NULLs as
//     equal, so KPI-level targets (no metric) had no uniqueness at all —
//     concurrent creates produced duplicates. v4 coalesces metric_id to a
//     sentinel like v3 already does for organization_id, and keys on
//     lower(period_code) so "Q3" and "q3" can never coexist.
//  2. Nothing stopped two approved targets for the same KPI + metric + year
//     + period + organization + segment under different versions.
//     idx_kpi_target_one_approved allows at most one approved row per key
//     (a revision's approval supersedes the original first, in the same
//     transaction, so it never trips this).
//
// v3 is dropped only once v4 exists. When existing rows violate an index it
// is not created and a warning is logged — the duplicates need manual
// cleanup, they are never deleted automatically.
//
// Idempotent: safe to run on every startup.
func MigrateKpiTargetUniqueIndexV4(db *gorm.DB) error {
	if !hasTable(db, "kpi_annual_targets") {
		return nil
	}

	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_kpi_target_unique_v4
		ON kpi_annual_targets (
			kpi_code, kpi_type,
			COALESCE(metric_id, '00000000-0000-0000-0000-000000000000'::uuid),
			target_year, lower(period_code),
			COALESCE(organization_id, '00000000-0000-0000-0000-000000000000'::uuid),
			COALESCE(segmentation_values::text, ''),
			version
		)
		WHERE deleted_at IS NULL
	`).Error; err != nil {
		log.Printf("Warning: failed to create kpi_annual_targets v4 unique index (existing duplicate rows need manual cleanup; v3 kept): %v", err)
	} else {
		db.Exec(`DROP INDEX IF EXISTS idx_kpi_target_unique_v3`)
	}

	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_kpi_target_one_approved
		ON kpi_annual_targets (
			kpi_code, kpi_type,
			COALESCE(metric_id, '00000000-0000-0000-0000-000000000000'::uuid),
			target_year, lower(period_code),
			COALESCE(organization_id, '00000000-0000-0000-0000-000000000000'::uuid),
			COALESCE(segmentation_values::text, '')
		)
		WHERE deleted_at IS NULL AND target_status = 'approved'
	`).Error; err != nil {
		log.Printf("Warning: failed to create kpi_annual_targets one-approved index (several approved targets share a key and need manual cleanup): %v", err)
	}

	return nil
}
