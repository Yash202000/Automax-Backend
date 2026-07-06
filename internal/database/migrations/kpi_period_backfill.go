package migrations

import (
	"log"

	"gorm.io/gorm"
)

// MigrateKpiPeriodBackfill backfills period_type/period_key on existing
// kpi_annual_targets and kpi_performances rows (annual/quarter respectively,
// derived from their existing year/quarter columns) and drops the old
// non-unique composite index on kpi_annual_targets that the new unique
// (kpi_code, kpi_type, period_type, period_key) index replaces. Idempotent —
// only touches rows where period_key is still empty.
func MigrateKpiPeriodBackfill(db *gorm.DB) error {
	db.Exec(`DROP INDEX IF EXISTS idx_kpi_target_unique`)

	if hasTable(db, "kpi_annual_targets") {
		res := db.Exec(`
			UPDATE kpi_annual_targets
			SET period_type = 'annual', period_key = year::text
			WHERE period_key = '' OR period_key IS NULL
		`)
		if res.Error != nil {
			log.Printf("kpi period backfill: failed to backfill kpi_annual_targets: %v", res.Error)
		} else if res.RowsAffected > 0 {
			log.Printf("kpi period backfill: backfilled period_key for %d annual target rows", res.RowsAffected)
		}
	}

	if hasTable(db, "kpi_performances") {
		res := db.Exec(`
			UPDATE kpi_performances
			SET period_type = 'quarter', period_key = year::text || '-Q' || quarter::text
			WHERE period_key = '' OR period_key IS NULL
		`)
		if res.Error != nil {
			log.Printf("kpi period backfill: failed to backfill kpi_performances: %v", res.Error)
		} else if res.RowsAffected > 0 {
			log.Printf("kpi period backfill: backfilled period_key for %d performance rows", res.RowsAffected)
		}
	}

	return nil
}

func hasTable(db *gorm.DB, table string) bool {
	var exists bool
	if err := db.Raw(
		`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = ?)`, table,
	).Scan(&exists).Error; err != nil {
		return false
	}
	return exists
}
