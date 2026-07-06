package services

import (
	"github.com/automax/backend/internal/models"
	"gorm.io/gorm"
)

// CalculateAchievement computes KPI achievement percentage given an actual value,
// a target value and the KPI's polarity. Ascending KPIs (higher is better) use
// actual/target; descending KPIs (lower is better) use target/actual. A zero
// denominator returns 0 explicitly rather than leaving a stale value.
func CalculateAchievement(actual, target float64, polarity string) float64 {
	if polarity == models.KPIPolarityDescending {
		if actual == 0 {
			return 0
		}
		return (target / actual) * 100
	}
	if target == 0 {
		return 0
	}
	return (actual / target) * 100
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
