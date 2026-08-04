package migrations

import (
	"log"

	"gorm.io/gorm"
)

// MigrateKpiEntryMultiOrgScope implements the "Calculate a Composite KPI"
// requirements doc's data-model changes:
//
//  1. Multiple approved Entries are now allowed for the same metric+period —
//     the system aggregates them (sum-numerator/sum-denominator for ratio
//     metrics, AggregationMethod-driven for others) rather than treating one
//     Entry as the period's final value. The old idx_kpi_entry_unique
//     (kpi_id, kpi_type, metric_id, reporting_year, period_code) must be
//     dropped — it's exactly the constraint that made multi-entry capture
//     impossible.
//  2. organization_id + segmentation_values + version columns are added to
//     both kpi_entries and kpi_annual_targets, so a Target's uniqueness key
//     can be Metric+Year+Period+Organization+Segment+Version (per BR-01) and
//     an Entry can be scoped to match the Target it's measured against
//     (BR-12 "scope consistency").
//  3. The target unique index is rebuilt (v3) to include the new scope
//     columns. NULL organization_id/segmentation_values are coalesced to a
//     sentinel so "no organization / no segment" is itself treated as one
//     consistent scope for uniqueness purposes — plain SQL NULLs are never
//     equal to each other and would otherwise let unlimited duplicate
//     unscoped targets through.
//
// Idempotent: safe to run on every startup.
func MigrateKpiEntryMultiOrgScope(db *gorm.DB) error {
	if hasTable(db, "kpi_entries") {
		db.Exec(`DROP INDEX IF EXISTS idx_kpi_entry_unique`)

		if err := db.Exec(`ALTER TABLE kpi_entries ADD COLUMN IF NOT EXISTS organization_id uuid`).Error; err != nil {
			log.Printf("Warning: failed to add kpi_entries.organization_id: %v", err)
		}
		if err := db.Exec(`ALTER TABLE kpi_entries ADD COLUMN IF NOT EXISTS segmentation_values jsonb`).Error; err != nil {
			log.Printf("Warning: failed to add kpi_entries.segmentation_values: %v", err)
		}
	}

	if hasTable(db, "kpi_annual_targets") {
		if err := db.Exec(`ALTER TABLE kpi_annual_targets ADD COLUMN IF NOT EXISTS organization_id uuid`).Error; err != nil {
			log.Printf("Warning: failed to add kpi_annual_targets.organization_id: %v", err)
		}
		if err := db.Exec(`ALTER TABLE kpi_annual_targets ADD COLUMN IF NOT EXISTS version integer NOT NULL DEFAULT 1`).Error; err != nil {
			log.Printf("Warning: failed to add kpi_annual_targets.version: %v", err)
		}

		db.Exec(`DROP INDEX IF EXISTS idx_kpi_target_unique_v2`)
		if err := db.Exec(`
			CREATE UNIQUE INDEX IF NOT EXISTS idx_kpi_target_unique_v3
			ON kpi_annual_targets (
				kpi_code, kpi_type, metric_id, target_year, period_code,
				COALESCE(organization_id, '00000000-0000-0000-0000-000000000000'::uuid),
				COALESCE(segmentation_values::text, ''),
				version
			)
			WHERE deleted_at IS NULL
		`).Error; err != nil {
			log.Printf("Warning: failed to create kpi_annual_targets v3 unique index (existing duplicate rows may need manual cleanup): %v", err)
		}
	}

	return nil
}
