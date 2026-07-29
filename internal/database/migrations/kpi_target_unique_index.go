package migrations

import (
	"log"

	"gorm.io/gorm"
)

// MigrateKpiTargetUniqueIndex replaces the original composite unique index on
// kpi_annual_targets — (kpi_code, kpi_type, period_type, period_key) — with a
// correct one on (kpi_code, kpi_type, metric_id, target_year, period_code),
// scoped to non-deleted rows only.
//
// The original index was broken two ways:
//  1. period_type is hardcoded to "annual" for every target regardless of its
//     actual reporting frequency (see KpiAnnualTargetRequest.ToModel), so it
//     contributed nothing to uniqueness — in effect the constraint was just
//     (kpi_code, kpi_type, period_key), meaning only ONE target could ever
//     exist per KPI+period across every metric that KPI has, and it ignored
//     target_year entirely (a "q1" target for 2026 collided with "q1" 2027).
//  2. It wasn't a partial index (no WHERE deleted_at IS NULL), so a
//     soft-deleted target's row permanently blocked recreating a target with
//     the same key — Postgres doesn't know about GORM's soft-delete
//     convention unless the index says so.
//
// Idempotent: safe to run on every startup.
func MigrateKpiTargetUniqueIndex(db *gorm.DB) error {
	if !hasTable(db, "kpi_annual_targets") {
		return nil
	}

	db.Exec(`DROP INDEX IF EXISTS idx_kpi_target_period_unique`)

	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_kpi_target_unique_v2
		ON kpi_annual_targets (kpi_code, kpi_type, metric_id, target_year, period_code)
		WHERE deleted_at IS NULL
	`).Error; err != nil {
		// Don't block startup — a pre-existing genuine duplicate (unlikely,
		// since the old index was if anything more restrictive) would need
		// manual cleanup, not a silent auto-delete of someone's data.
		log.Printf("Warning: failed to create kpi_annual_targets unique index (existing duplicate rows may need manual cleanup): %v", err)
	}

	return nil
}
