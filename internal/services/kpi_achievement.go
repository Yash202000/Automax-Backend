package services

import (
	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// CalculateAchievement computes KPI achievement percentage given an actual value,
// a target value and the KPI's polarity, using the same formula as every
// other achievement in the module (models.AchievementPercent): ascending KPIs
// (higher is better) use actual/target; descending KPIs (lower is better)
// use target/actual, where an actual of 0 is the best result (100). A value
// that can't be calculated (e.g. a zero target on an ascending KPI) returns 0.
func CalculateAchievement(actual, target float64, polarity string) float64 {
	pct, ok := models.AchievementPercent(actual, target, models.NormalizeDirection(polarity))
	if !ok {
		return 0
	}
	return pct
}

// UsableTargetStatuses are the target statuses that count as "the approved
// target" wherever a target is read for measuring entries: approved, and
// locked (an approved target frozen against revision — still the target in
// force for its period).
var UsableTargetStatuses = []string{"approved", "locked"}

// FindUsableTarget returns the approved/locked KpiAnnualTarget for one
// metric+year+period, scoped to organizationID (nil = the unscoped/global
// target only), highest version first. Returns gorm.ErrRecordNotFound when
// there is none.
func FindUsableTarget(db *gorm.DB, kpiCode, kpiType string, metricID uuid.UUID, year int, periodCode string, organizationID *uuid.UUID) (*models.KpiAnnualTarget, error) {
	q := db.Where(
		"kpi_code = ? AND kpi_type = ? AND metric_id = ? AND target_year = ? AND period_code = ? AND target_status IN ?",
		kpiCode, kpiType, metricID, year, periodCode, UsableTargetStatuses,
	)
	if organizationID != nil {
		q = q.Where("organization_id = ?", *organizationID)
	} else {
		q = q.Where("organization_id IS NULL")
	}
	var target models.KpiAnnualTarget
	if err := q.Order("version DESC").First(&target).Error; err != nil {
		return nil, err
	}
	return &target, nil
}

// EffectiveBand resolves the Performance Band that applies to a KPI code: its
// own band if one exists, else the global default row, else the hardcoded
// models.DefaultKpiPerformanceBand. GET /kpi/performance-bands/effective and
// every status calculation use this same resolution.
func EffectiveBand(db *gorm.DB, kpiCode string) models.KpiPerformanceBandResponse {
	if kpiCode != "" {
		var override models.KpiPerformanceBand
		if err := db.Where("kpi_code = ?", kpiCode).First(&override).Error; err == nil {
			return override.ToResponse()
		}
	}
	var global models.KpiPerformanceBand
	if err := db.Where("kpi_code IS NULL").Order("created_at ASC").First(&global).Error; err == nil {
		return global.ToResponse()
	}
	return models.DefaultKpiPerformanceBand
}

// PerformanceRulesFor builds the rules a metric's Actual is judged by: the
// metric direction, the KPI's effective band, and — when target is non-nil —
// the target value and its own thresholds/range.
func PerformanceRulesFor(db *gorm.DB, kpiCode, direction string, target *models.KpiAnnualTarget) models.PerformanceRules {
	band := EffectiveBand(db, kpiCode)
	green, amber := band.GreenMin, band.AmberMin
	rules := models.PerformanceRules{Direction: direction, GreenMin: &green, AmberMin: &amber}
	return rules.WithTarget(target)
}

// EvaluateMetricActual judges an aggregated Actual against a target with the
// KPI's band — the shared path for rollups, composite scores and dashboards.
func EvaluateMetricActual(db *gorm.DB, kpiCode, direction string, actual float64, target *models.KpiAnnualTarget, capPercent float64) models.PerformanceResult {
	return models.EvaluatePerformance(actual, PerformanceRulesFor(db, kpiCode, direction, target), capPercent)
}

// ComputeEntryAchievement recomputes an entry's achievement/status with its
// snapshotted target's thresholds (loaded by TargetID) and the KPI's band.
func ComputeEntryAchievement(db *gorm.DB, kpiCode string, e *models.KpiEntry) {
	var target *models.KpiAnnualTarget
	if e.TargetID != nil {
		var t models.KpiAnnualTarget
		if err := db.Unscoped().First(&t, *e.TargetID).Error; err == nil {
			target = &t
		}
	}
	e.ComputeAchievementWith(PerformanceRulesFor(db, kpiCode, e.DirectionSnapshot, target))
}

// GetKPIPolarity looks up the polarity configured on a KPI's dictionary record.
// Falls back to ascending if the KPI can't be found, matching the model default.
func GetKPIPolarity(db *gorm.DB, kpiCode, kpiType string) string {
	switch kpiType {
	case models.KPITypeOperational:
		var k models.OperationalKPI
		if err := db.Select("polarity").Where("code = ?", kpiCode).First(&k).Error; err == nil {
			return k.Polarity
		}
	case models.KPITypeAward:
		var k models.AwardKPI
		if err := db.Select("polarity").Where("code = ?", kpiCode).First(&k).Error; err == nil {
			return k.Polarity
		}
	default:
		var k models.StrategicKPI
		if err := db.Select("polarity").Where("code = ?", kpiCode).First(&k).Error; err == nil {
			return k.Polarity
		}
	}
	return models.KPIPolarityAscending
}
