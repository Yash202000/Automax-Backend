package migrations

import (
	"log"

	"gorm.io/gorm"
)

// MigrateKpiDictionaryCodeUniqueIndex replaces the plain (non-partial)
// unique index GORM's `uniqueIndex` tag creates on strategic_kpis.code,
// operational_kpis.code, and award_kpis.code with one scoped to non-deleted
// rows.
//
// Postgres doesn't know about GORM's soft-delete convention unless the index
// says so: without WHERE deleted_at IS NULL, soft-deleting a KPI (the normal
// Delete* handler behavior) permanently reserves its code — trying to create
// a new KPI with that same code afterward fails with a 23505 duplicate-key
// error on idx_<table>_code even though the old row is "deleted" from the
// user's point of view.
//
// Idempotent: safe to run on every startup.
func MigrateKpiDictionaryCodeUniqueIndex(db *gorm.DB) error {
	specs := []struct {
		table  string
		oldIdx string
		newIdx string
	}{
		{"strategic_kpis", "idx_strategic_kpis_code", "idx_strategic_kpis_code_v2"},
		{"operational_kpis", "idx_operational_kpis_code", "idx_operational_kpis_code_v2"},
		{"award_kpis", "idx_award_kpis_code", "idx_award_kpis_code_v2"},
	}

	for _, sp := range specs {
		if !hasTable(db, sp.table) {
			continue
		}

		db.Exec("DROP INDEX IF EXISTS " + sp.oldIdx)

		if err := db.Exec(`
			CREATE UNIQUE INDEX IF NOT EXISTS ` + sp.newIdx + `
			ON ` + sp.table + ` (code)
			WHERE deleted_at IS NULL
		`).Error; err != nil {
			// Don't block startup — a pre-existing genuine duplicate among
			// currently-live rows (not just soft-deleted ones) would need
			// manual cleanup, not a silent auto-rename/delete of someone's data.
			log.Printf("Warning: failed to create partial unique index on %s.code: %v", sp.table, err)
		}
	}

	return nil
}
