package services

import (
	"math"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MetricPeriodRollup is the result of aggregating every Approved KpiEntry for
// one metric+period into a single period Actual — the core calculation the
// "Complete KPI Calculation Example" doc requires (section 4.3/8, BR-04,
// BR-09, BR-10): multiple raw Entries within a period are combined, never
// averaged as pre-computed percentages, and a period with zero Approved
// entries is explicit "No Data", not a silent zero.
type MetricPeriodRollup struct {
	HasData        bool
	EntryCount     int
	Actual         *float64
	SumNumerator   *float64
	SumDenominator *float64
}

// AggregateMetricPeriod pulls every Approved KpiEntry for the given
// metric+kpi+year+period(+organization) and combines them into one Actual:
//   - Ratio/Percentage types: sum(numerator) / sum(denominator) recombined
//     into one ratio (BR-10) — never an average of each entry's own percentage.
//   - Everything else: combined per the metric's configured AggregationMethod
//     (Sum/Average/Minimum/Maximum/Latest Approved Value; "Weighted Average"
//     and "No Aggregation" fall back to Average and Latest respectively, since
//     KpiEntry carries no per-entry weight to drive a true weighted average).
func AggregateMetricPeriod(db *gorm.DB, metric *models.KpiMetric, kpiID uuid.UUID, kpiType string, year int, periodCode string, organizationID *uuid.UUID) MetricPeriodRollup {
	q := db.Where(
		"kpi_id = ? AND kpi_type = ? AND metric_id = ? AND reporting_year = ? AND period_code = ? AND status = ?",
		kpiID, kpiType, metric.ID, year, periodCode, models.KpiEntryStatusApproved,
	)
	if organizationID != nil {
		q = q.Where("organization_id = ?", *organizationID)
	} else {
		q = q.Where("organization_id IS NULL")
	}

	var entries []models.KpiEntry
	q.Order("created_at ASC").Find(&entries)

	result := MetricPeriodRollup{EntryCount: len(entries)}
	if len(entries) == 0 {
		return result
	}
	result.HasData = true

	switch metric.CalculationType {
	case "Percentage - Ratio", "Ratio":
		var sumNum, sumDenom float64
		for _, e := range entries {
			if e.NumeratorValue != nil {
				sumNum += *e.NumeratorValue
			}
			if e.DenominatorValue != nil {
				sumDenom += *e.DenominatorValue
			}
		}
		result.SumNumerator = &sumNum
		result.SumDenominator = &sumDenom
		if sumDenom == 0 {
			result.HasData = false
			result.Actual = nil
			return result
		}
		ratio := sumNum / sumDenom
		if metric.CalculationType == "Percentage - Ratio" {
			ratio *= 100
		}
		result.Actual = &ratio

	default:
		values := make([]float64, len(entries))
		for i, e := range entries {
			values[i] = e.ActualValue
		}
		var actual float64
		switch metric.AggregationMethod {
		case "Sum":
			for _, v := range values {
				actual += v
			}
		case "Minimum":
			actual = values[0]
			for _, v := range values {
				if v < actual {
					actual = v
				}
			}
		case "Maximum":
			actual = values[0]
			for _, v := range values {
				if v > actual {
					actual = v
				}
			}
		case "Latest Approved Value", "No Aggregation":
			actual = values[len(values)-1]
		case "Average", "Weighted Average":
			var sum float64
			for _, v := range values {
				sum += v
			}
			actual = sum / float64(len(values))
		default:
			var sum float64
			for _, v := range values {
				sum += v
			}
			actual = sum / float64(len(values))
		}
		result.Actual = &actual
	}

	return result
}

// AchievementResult carries both the raw (uncapped) achievement percentage —
// retained for audit/analysis per BR-11 — and a capped display value.
type AchievementResult struct {
	Raw    float64
	Capped float64
}

// ComputeAchievementRawCapped applies the Higher/Lower-is-Better formula to an
// aggregated period Actual against a Target, per the doc's section on
// achievement calculation. capPercent is the display cap (e.g. 100); pass 0
// or a negative value to mean "no cap" (capped == raw).
func ComputeAchievementRawCapped(actual, target float64, direction string, capPercent float64) *AchievementResult {
	if target == 0 {
		return nil
	}
	var raw float64
	if direction == "Lower is Better" {
		raw = (target / actual) * 100
	} else {
		raw = (actual / target) * 100
	}
	raw = math.Round(raw*100) / 100

	capped := raw
	if capPercent > 0 && capped > capPercent {
		capped = capPercent
	}
	if capped < 0 {
		capped = 0
	}
	return &AchievementResult{Raw: raw, Capped: capped}
}

// PerformanceStatusFor buckets a (capped) achievement percentage the same way
// KpiEntry.ComputeAchievement already does, so the rollup/composite views and
// individual entries always agree on status labeling.
func PerformanceStatusFor(cappedAchievement float64) string {
	switch {
	case cappedAchievement >= 100:
		return "Exceeded"
	case cappedAchievement >= 80:
		return "Achieved"
	case cappedAchievement >= 50:
		return "Warning"
	default:
		return "Below Target"
	}
}

// MetricPeriodPoint is one point on the Baseline/Target/Actual trend chart
// (doc Figure 1) — a single period's rollup plus its Target and Achievement,
// carrying the period's own label so the frontend doesn't need to re-derive
// month/quarter names.
type MetricPeriodPoint struct {
	PeriodCode     string   `json:"period_code"`
	PeriodLabel    string   `json:"period_label"`
	Baseline       float64  `json:"baseline"`
	HasData        bool     `json:"has_data"`
	EntryCount     int      `json:"entry_count"`
	Actual         *float64 `json:"actual"`
	Target         *float64 `json:"target"`
	AchievementRaw *float64 `json:"achievement_raw"`
	AchievementCap *float64 `json:"achievement_capped"`
}

// BuildMetricPeriodSeries computes one MetricPeriodPoint per period of the
// year (every month/quarter/half per the metric's reporting frequency),
// letting the dashboard render the whole year's Baseline/Target/Actual trend
// in a single request instead of one round trip per period.
func BuildMetricPeriodSeries(db *gorm.DB, metric *models.KpiMetric, kpiID uuid.UUID, kpiCode, kpiType string, year int, organizationID *uuid.UUID, capPercent float64) []MetricPeriodPoint {
	frequency := metric.ReportingFrequency
	if frequency == "" {
		frequency = GetKPIReportingFrequency(db, kpiCode, kpiType)
	}
	periodCodes := PeriodCodesForFrequency(frequency)

	points := make([]MetricPeriodPoint, 0, len(periodCodes))
	for _, periodCode := range periodCodes {
		point := MetricPeriodPoint{
			PeriodCode:  periodCode,
			PeriodLabel: FormatPeriodLabel(periodCode, year),
			Baseline:    metric.BaselineValue,
		}

		rollup := AggregateMetricPeriod(db, metric, kpiID, kpiType, year, periodCode, organizationID)
		point.HasData = rollup.HasData
		point.EntryCount = rollup.EntryCount
		point.Actual = rollup.Actual

		targetQuery := db.Where(
			"kpi_code = ? AND kpi_type = ? AND metric_id = ? AND target_year = ? AND period_code = ? AND target_status = 'approved'",
			kpiCode, kpiType, metric.ID, year, periodCode,
		)
		if organizationID != nil {
			targetQuery = targetQuery.Where("organization_id = ?", *organizationID)
		} else {
			targetQuery = targetQuery.Where("organization_id IS NULL")
		}
		var target models.KpiAnnualTarget
		if err := targetQuery.Order("version DESC").First(&target).Error; err == nil {
			point.Target = &target.TargetValue
			if rollup.HasData {
				if achievement := ComputeAchievementRawCapped(*rollup.Actual, target.TargetValue, metric.Direction, capPercent); achievement != nil {
					point.AchievementRaw = &achievement.Raw
					point.AchievementCap = &achievement.Capped
				}
			}
		}

		points = append(points, point)
	}

	return points
}

// ResolveDisplayPeriod picks which period the Metric Card should show: the
// actual current calendar period if it has approved data (entries or a
// target), otherwise the chronologically latest period with an approved
// entry, or failing that the chronologically latest period with an approved
// target — so a metric someone just populated for a different period (e.g.
// testing next month, or backfilling a past one) doesn't silently read as
// "No Data" just because it isn't literally "now". The fallback ranks
// candidates by their own (year, period) position rather than by when the row
// was created/approved — a metric can have several approved Targets and
// several approved Entries across periods, and approving/backfilling them out
// of chronological order (e.g. an earlier month entered after a later one is
// already approved) must not make an older period look "latest" just because
// it was the most recently touched row. isFallback tells the caller whether
// the returned period differs from the real current one, so the UI can label it.
func ResolveDisplayPeriod(db *gorm.DB, metric *models.KpiMetric, kpiID uuid.UUID, kpiCode, kpiType string) (year int, periodCode string, isFallback bool) {
	frequency := metric.ReportingFrequency
	if frequency == "" {
		frequency = GetKPIReportingFrequency(db, kpiCode, kpiType)
	}
	now := time.Now()
	currentYear := now.Year()
	currentPeriod := CurrentPeriodCode(frequency, now)

	rollup := AggregateMetricPeriod(db, metric, kpiID, kpiType, currentYear, currentPeriod, nil)
	if rollup.HasData {
		return currentYear, currentPeriod, false
	}
	var currentTarget models.KpiAnnualTarget
	if db.Where(
		"kpi_code = ? AND kpi_type = ? AND metric_id = ? AND target_year = ? AND period_code = ? AND target_status = 'approved' AND organization_id IS NULL",
		kpiCode, kpiType, metric.ID, currentYear, currentPeriod,
	).First(&currentTarget).Error == nil {
		return currentYear, currentPeriod, false
	}

	var approvedEntries []models.KpiEntry
	db.Select("reporting_year", "period_code").
		Where("kpi_id = ? AND kpi_type = ? AND metric_id = ? AND status = ?", kpiID, kpiType, metric.ID, models.KpiEntryStatusApproved).
		Find(&approvedEntries)
	if len(approvedEntries) > 0 {
		latest := approvedEntries[0]
		for _, e := range approvedEntries[1:] {
			if PeriodSortKey(e.ReportingYear, e.PeriodCode) > PeriodSortKey(latest.ReportingYear, latest.PeriodCode) {
				latest = e
			}
		}
		return latest.ReportingYear, latest.PeriodCode, true
	}

	var approvedTargets []models.KpiAnnualTarget
	db.Select("target_year", "period_code").
		Where("kpi_code = ? AND kpi_type = ? AND metric_id = ? AND target_status = 'approved'", kpiCode, kpiType, metric.ID).
		Find(&approvedTargets)
	if len(approvedTargets) > 0 {
		latest := approvedTargets[0]
		for _, t := range approvedTargets[1:] {
			if PeriodSortKey(t.TargetYear, t.PeriodCode) > PeriodSortKey(latest.TargetYear, latest.PeriodCode) {
				latest = t
			}
		}
		return latest.TargetYear, latest.PeriodCode, true
	}

	return currentYear, currentPeriod, false
}
