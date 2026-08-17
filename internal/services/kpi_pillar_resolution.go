package services

import (
	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ResolvePillarForKPI finds the Pillar a KPI's evidence should be filed
// under in Documenta, per the approved org hierarchy
// (Pillar → KPI → Supporting Folder → Documents/Evidence). Returns
// (nil, nil) — not an error — when no Pillar can be resolved; the KPI
// dictionary genuinely has no path to Pillar for that combination, and
// callers must block the upload rather than fall back to some other root,
// per explicit product decision (Award KPIs never resolve; Operational
// KPIs only resolve if their Objective or Process happens to have a
// PillarID set).
//
//   - StrategicKPI: direct PillarID field.
//   - OperationalKPI: no direct PillarID — checked via its Operational
//     Objective's PillarID first, then its Process's PillarID.
//   - AwardKPI: no path to Pillar exists anywhere in the data model
//     (chains only to AwardSubCriterion → AwardCriterion, neither of
//     which references Pillar or Enabler) — always returns (nil, nil).
func ResolvePillarForKPI(db *gorm.DB, kpiType string, kpiID uuid.UUID) (*models.Pillar, error) {
	switch kpiType {
	case models.KPITypeStrategic, "":
		var kpi models.StrategicKPI
		if err := db.Select("pillar_id").Where("id = ?", kpiID).First(&kpi).Error; err != nil {
			return nil, err
		}
		if kpi.PillarID == nil {
			return nil, nil
		}
		return loadPillar(db, *kpi.PillarID)

	case models.KPITypeOperational:
		var kpi models.OperationalKPI
		if err := db.Select("operational_objective_id, process_id").Where("id = ?", kpiID).First(&kpi).Error; err != nil {
			return nil, err
		}
		var objective models.OperationalObjective
		if err := db.Select("pillar_id").Where("id = ?", kpi.OperationalObjectiveID).First(&objective).Error; err == nil && objective.PillarID != nil {
			return loadPillar(db, *objective.PillarID)
		}
		var process models.Process
		if err := db.Select("pillar_id").Where("id = ?", kpi.ProcessID).First(&process).Error; err == nil && process.PillarID != nil {
			return loadPillar(db, *process.PillarID)
		}
		return nil, nil

	case models.KPITypeAward:
		return nil, nil

	default:
		return nil, nil
	}
}

func loadPillar(db *gorm.DB, id uuid.UUID) (*models.Pillar, error) {
	var pillar models.Pillar
	if err := db.Where("id = ?", id).First(&pillar).Error; err != nil {
		return nil, err
	}
	return &pillar, nil
}
