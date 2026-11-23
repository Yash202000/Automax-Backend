package services

import (
	"math"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MetricScoreBreakdown is one row of the "Composite KPI Calculation" table
// (doc section 8): Actual, Target, Achievement and the metric's contribution
// to the overall weighted score for one period.
type MetricScoreBreakdown struct {
	MetricID       uuid.UUID `json:"metric_id"`
	MetricName     string    `json:"metric_name"`
	Baseline       float64   `json:"baseline"`
	Actual         *float64  `json:"actual"`
	Target         *float64  `json:"target"`
	Weight         float64   `json:"weight"`
	HasData        bool      `json:"has_data"`
	EntryCount     int       `json:"entry_count"`
	AchievementRaw *float64  `json:"achievement_raw"`
	AchievementCap *float64  `json:"achievement_capped"`
	WeightedResult *float64  `json:"weighted_result"`
	// PerformanceStatus is the metric's status for the period (target
	// thresholds / KPI band, see models.EvaluatePerformance).
	PerformanceStatus string `json:"performance_status,omitempty"`
	// Informational metrics are listed but never scored or weighted.
	Informational bool `json:"informational,omitempty"`
	// PeriodCode/Year/IsFallbackPeriod are only populated by
	// ComputeCompositeScoreLatest, where each metric contributes its OWN
	// most-recent-with-data period rather than one shared period — so the
	// breakdown list stays legible about which period each row is actually
	// showing when they don't all line up.
	PeriodCode       string `json:"period_code,omitempty"`
	Year             int    `json:"year,omitempty"`
	IsFallbackPeriod bool   `json:"is_fallback_period,omitempty"`
}

// CompositeScoreResult is the overall weighted KPI score for one period
// (doc section 8: "Raw Overall KPI Score" + an optional capped display
// score, BR-11). HasData is false — and both scores nil — if ANY active
// metric lacks approved data for the period: a weighted sum silently
// computed from a subset of metrics would understate the score exactly the
// way BR-09 says a missing period must not be treated as zero.
type CompositeScoreResult struct {
	HasData     bool     `json:"has_data"`
	RawScore    *float64 `json:"raw_score"`
	CappedScore *float64 `json:"capped_score"`
	// PerformanceStatus buckets RawScore with the KPI's band.
	PerformanceStatus string                 `json:"performance_status,omitempty"`
	Metrics           []MetricScoreBreakdown `json:"metrics"`
}

// scoreMetric fills row's target/achievement/status for one metric's period
// Actual and reports whether it produced a weighted result. Informational
// metrics are marked and never scored.
func scoreMetric(db *gorm.DB, row *MetricScoreBreakdown, metric *models.KpiMetric, kpiCode string, actual float64, target *models.KpiAnnualTarget, capPercent float64) bool {
	row.Target = &target.TargetValue
	res := EvaluateMetricActual(db, kpiCode, metric.Direction, actual, target, capPercent)
	row.PerformanceStatus = res.Status
	if !res.Calculable {
		return false
	}
	raw, capped := res.Raw, res.Capped
	row.AchievementRaw = &raw
	row.AchievementCap = &capped
	// Weighted results and the overall raw score are built from each
	// metric's RAW (uncapped) achievement, matching the doc's own worked
	// table (section 8) exactly — e.g. 101.09% x 30% = 30.33%, not a
	// pre-capped 100% x 30%. The cap (BR-11/AC-06) is applied once, to
	// the FINAL composite score only, not to each metric before summing.
	weightedRaw := math.Round(raw*(metric.Weight/100)*100) / 100
	row.WeightedResult = &weightedRaw
	return true
}

// compositeStatus buckets a composite raw score (already an achievement %)
// with the KPI's band: judged as an actual against a target of 100.
func compositeStatus(db *gorm.DB, kpiCode string, rawScore float64) string {
	rules := PerformanceRulesFor(db, kpiCode, models.DirectionHigherIsBetter, nil)
	hundred := 100.0
	rules.Target = &hundred
	return models.EvaluatePerformance(rawScore, rules, 0).Status
}

// ComputeCompositeScore combines every Active metric's Achievement (weighted)
// into one overall KPI score for a period. kpiID scopes Entries, kpiCode
// scopes Targets (KpiAnnualTarget is keyed by code, KpiEntry by id) —
// callers resolve kpiCode once via their existing KPI-code lookup.
func ComputeCompositeScore(db *gorm.DB, kpiID uuid.UUID, kpiCode, kpiType string, year int, periodCode string, organizationID *uuid.UUID, capPercent float64) (*CompositeScoreResult, error) {
	var metrics []models.KpiMetric
	if err := db.Where("kpi_id = ? AND kpi_type = ? AND metric_status = ?", kpiID, kpiType, "Active").
		Order("display_order ASC").Find(&metrics).Error; err != nil {
		return nil, err
	}

	result := &CompositeScoreResult{HasData: true}
	var rawSum, cappedSum float64
	scored := 0

	for _, metric := range metrics {
		row := MetricScoreBreakdown{
			MetricID:   metric.ID,
			MetricName: metric.Name,
			Baseline:   metric.BaselineValue,
			Weight:     metric.Weight,
		}

		if models.NormalizeDirection(metric.Direction) == models.DirectionInformational {
			row.Informational = true
			result.Metrics = append(result.Metrics, row)
			continue
		}
		scored++

		rollup := AggregateMetricPeriod(db, &metric, kpiID, kpiType, year, periodCode, organizationID)
		row.HasData = rollup.HasData
		row.EntryCount = rollup.EntryCount
		row.Actual = rollup.Actual

		if !rollup.HasData {
			result.HasData = false
			result.Metrics = append(result.Metrics, row)
			continue
		}

		target, err := FindUsableTarget(db, kpiCode, kpiType, metric.ID, year, periodCode, organizationID)
		if err != nil {
			result.HasData = false
			result.Metrics = append(result.Metrics, row)
			continue
		}
		if !scoreMetric(db, &row, &metric, kpiCode, *rollup.Actual, target, capPercent) {
			result.HasData = false
			result.Metrics = append(result.Metrics, row)
			continue
		}
		rawSum += *row.WeightedResult

		result.Metrics = append(result.Metrics, row)
	}

	if result.HasData && scored > 0 {
		rawSum = math.Round(rawSum*100) / 100
		cappedSum = rawSum
		if capPercent > 0 && cappedSum > capPercent {
			cappedSum = capPercent
		}
		result.RawScore = &rawSum
		result.CappedScore = &cappedSum
		result.PerformanceStatus = compositeStatus(db, kpiCode, rawSum)
	} else if scored == 0 {
		result.HasData = false
	}

	return result, nil
}

// ComputeCompositeScoreLatest is the "display" counterpart to
// ComputeCompositeScore: instead of requiring every active metric to have
// data for one exact shared period (and reporting "No Data" the instant any
// one of them doesn't), each metric contributes its OWN most-recent period
// with data — resolved the same way the Metric Card resolves its single
// metric (ResolveDisplayPeriod). Two metrics on different real-world
// cadences (one just updated in August, another in September) both show up
// instead of the whole composite going dark over a period mismatch. Because
// the contributing periods can differ per row, this is not "the score for
// period X" the way the strict version is — callers should label it as
// showing each metric's latest available data, not name one period.
func ComputeCompositeScoreLatest(db *gorm.DB, kpiID uuid.UUID, kpiCode, kpiType string, capPercent float64) (*CompositeScoreResult, error) {
	var metrics []models.KpiMetric
	if err := db.Where("kpi_id = ? AND kpi_type = ? AND metric_status = ?", kpiID, kpiType, "Active").
		Order("display_order ASC").Find(&metrics).Error; err != nil {
		return nil, err
	}

	result := &CompositeScoreResult{}
	var rawSum float64
	anyData := false

	for _, metric := range metrics {
		year, periodCode, isFallback := ResolveDisplayPeriod(db, &metric, kpiID, kpiCode, kpiType)

		row := MetricScoreBreakdown{
			MetricID:         metric.ID,
			MetricName:       metric.Name,
			Baseline:         metric.BaselineValue,
			Weight:           metric.Weight,
			PeriodCode:       periodCode,
			Year:             year,
			IsFallbackPeriod: isFallback,
		}

		if models.NormalizeDirection(metric.Direction) == models.DirectionInformational {
			row.Informational = true
			result.Metrics = append(result.Metrics, row)
			continue
		}

		rollup := AggregateMetricPeriod(db, &metric, kpiID, kpiType, year, periodCode, nil)
		row.HasData = rollup.HasData
		row.EntryCount = rollup.EntryCount
		row.Actual = rollup.Actual

		if !rollup.HasData {
			result.Metrics = append(result.Metrics, row)
			continue
		}

		target, err := FindUsableTarget(db, kpiCode, kpiType, metric.ID, year, periodCode, nil)
		if err != nil || !scoreMetric(db, &row, &metric, kpiCode, *rollup.Actual, target, capPercent) {
			result.Metrics = append(result.Metrics, row)
			continue
		}
		rawSum += *row.WeightedResult
		anyData = true

		result.Metrics = append(result.Metrics, row)
	}

	if anyData {
		rawSum = math.Round(rawSum*100) / 100
		cappedSum := rawSum
		if capPercent > 0 && cappedSum > capPercent {
			cappedSum = capPercent
		}
		result.RawScore = &rawSum
		result.CappedScore = &cappedSum
		result.PerformanceStatus = compositeStatus(db, kpiCode, rawSum)
	}
	result.HasData = anyData

	return result, nil
}

// ActiveMetricWeightSum returns how many Active metrics a KPI has and the sum
// of their weights (rounded to 2 decimals), for the BR-07 activation gate:
// a KPI needs at least one Active metric and weights summing to 100%.
// Inactive metrics are excluded because they never contribute to the score.
func ActiveMetricWeightSum(db *gorm.DB, kpiID uuid.UUID, kpiType string) (int, float64, error) {
	var metrics []models.KpiMetric
	if err := db.Where("kpi_id = ? AND kpi_type = ? AND metric_status = ?", kpiID, kpiType, "Active").Find(&metrics).Error; err != nil {
		return 0, 0, err
	}
	var sum float64
	for _, m := range metrics {
		sum += m.Weight
	}
	return len(metrics), math.Round(sum*100) / 100, nil
}
