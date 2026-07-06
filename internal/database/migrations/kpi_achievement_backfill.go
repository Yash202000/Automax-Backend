package migrations

import (
	"log"

	"gorm.io/gorm"
)

// MigrateKpiAchievementBackfill recomputes achievement_pct on existing kpi_performances
// rows using each KPI's own polarity (ascending/descending), replacing the old
// always-ascending actual/target*100 calculation. Safe to run repeatedly — it always
// recomputes from actual/target/polarity, so re-running yields the same result.
func MigrateKpiAchievementBackfill(db *gorm.DB) error {
	var hasTable bool
	if err := db.Raw(
		`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'kpi_performances')`,
	).Scan(&hasTable).Error; err != nil || !hasTable {
		return nil
	}

	polarityByCode := map[string]string{}
	for _, table := range []string{"strategic_kpis", "operational_kpis", "award_kpis"} {
		var rows []struct {
			Code     string
			Polarity string
		}
		if err := db.Raw("SELECT code, polarity FROM " + table + " WHERE deleted_at IS NULL").Scan(&rows).Error; err != nil {
			log.Printf("kpi achievement backfill: could not read %s: %v", table, err)
			continue
		}
		for _, r := range rows {
			polarityByCode[r.Code] = r.Polarity
		}
	}

	var perfs []struct {
		ID     string
		Code   string `gorm:"column:kpi_code"`
		Target float64
		Actual float64
	}
	if err := db.Raw("SELECT id, kpi_code, target, actual FROM kpi_performances WHERE deleted_at IS NULL").Scan(&perfs).Error; err != nil {
		return err
	}
	if len(perfs) == 0 {
		return nil
	}

	updated := 0
	for _, p := range perfs {
		polarity := polarityByCode[p.Code]
		var pct float64
		if polarity == "descending" {
			if p.Actual != 0 {
				pct = (p.Target / p.Actual) * 100
			}
		} else if p.Target != 0 {
			pct = (p.Actual / p.Target) * 100
		}
		if err := db.Exec("UPDATE kpi_performances SET achievement_pct = ? WHERE id = ?", pct, p.ID).Error; err != nil {
			log.Printf("kpi achievement backfill: failed to update %s: %v", p.ID, err)
			continue
		}
		updated++
	}
	log.Printf("kpi achievement backfill: recomputed achievement_pct for %d/%d performance rows", updated, len(perfs))
	return nil
}
