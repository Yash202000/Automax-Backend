package migrations

import (
	"log"

	"gorm.io/gorm"
)

// MigrateKpiDictionaryUnifyTaxonomy relaxes award_kpis.award_sub_criterion_id
// from NOT NULL to nullable. All three KPI dictionary types (Strategic,
// Operational, Award) now share the same create form: Objective is the one
// universal required taxonomy field, and Award Criteria/Sub-Criteria becomes
// an optional supplementary tag on every type — including Award KPIs, which
// previously required it. AutoMigrate adds the new nullable columns needed
// for the rest of this change on its own; it does not relax an existing NOT
// NULL constraint, so that one change needs this explicit migration.
func MigrateKpiDictionaryUnifyTaxonomy(db *gorm.DB) error {
	if !hasTable(db, "award_kpis") {
		return nil
	}
	if err := db.Exec(
		`ALTER TABLE award_kpis ALTER COLUMN award_sub_criterion_id DROP NOT NULL;`,
	).Error; err != nil {
		log.Printf("Warning: failed to relax award_kpis.award_sub_criterion_id NOT NULL constraint: %v", err)
		return err
	}
	return nil
}
