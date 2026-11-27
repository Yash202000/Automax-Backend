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
// metric+kpi+year+period(+organization) and combines them into one Actual
// (see AggregateEntries).
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
	q.Order(entryChronologicalOrder).Find(&entries)
	return AggregateEntries(metric, entries)
}

// entryChronologicalOrder sorts entries by when their value applies — Entry
// Date, then period end — with the approval time as tie-break (approved
// entries are never edited, so updated_at is when they were approved), so
// "Latest Approved Value" is the latest-dated value, not the latest-typed one.
const entryChronologicalOrder = "entry_date ASC NULLS FIRST, period_end ASC NULLS FIRST, updated_at ASC, created_at ASC"

// AggregateEntries combines approved entries (in entryChronologicalOrder)
// into one Actual:
//   - Ratio/Percentage types: sum(numerator) / sum(denominator) recombined
//     into one ratio (BR-10) — never an average of each entry's own percentage.
//   - Everything else: combined per the metric's configured AggregationMethod
//     (Sum/Average/Minimum/Maximum/Latest Approved Value; "Weighted Average"
//     and "No Aggregation" fall back to Average and Latest respectively, since
//     KpiEntry carries no per-entry weight to drive a true weighted average).
func AggregateEntries(metric *models.KpiMetric, entries []models.KpiEntry) MetricPeriodRollup {
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
		default: // "Average", "Weighted Average" and unset
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

// ComputeAchievementRawCapped applies the shared achievement formula
// (models.EvaluatePerformance — Lower is Better = target / actual) to an
// aggregated period Actual against a Target value, with no target thresholds
// or range. capPercent is the display cap (e.g. 100); 0 or negative means
// no cap. Returns nil when achievement can't be calculated (Informational
// direction, or a Higher is Better zero target). Never returns Inf/NaN.
func ComputeAchievementRawCapped(actual, target float64, direction string, capPercent float64) *AchievementResult {
	res := models.EvaluatePerformance(actual, models.PerformanceRules{Direction: direction, Target: &target}, capPercent)
	if !res.Calculable {
		return nil
	}
	return &AchievementResult{Raw: res.Raw, Capped: res.Capped}
}

// PerformanceStatusFor buckets a (capped) achievement percentage with the
// global default thresholds (100/80/50). Where the KPI's band and the
// target's thresholds are known, use EvaluateMetricActual instead.
func PerformanceStatusFor(cappedAchievement float64) string {
	target := 100.0
	return models.EvaluatePerformance(cappedAchievement, models.PerformanceRules{Target: &target}, 0).Status
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
	// PerformanceStatus is set when the period has data and a target.
	PerformanceStatus string `json:"performance_status,omitempty"`
}

// BuildMetricPeriodSeries computes one MetricPeriodPoint per period of the
// year (every month/quarter/half per the metric's reporting frequency),
// letting the dashboard render the whole year's Baseline/Target/Actual trend
// in a single request instead of one round trip per period.
func BuildMetricPeriodSeries(db *gorm.DB, metric *models.KpiMetric, kpiID uuid.UUID, kpiCode, kpiType string, year int, organizationID *uuid.UUID, capPercent float64) []MetricPeriodPoint {
	periodCodes := PeriodCodesForFrequency(MetricFrequency(db, metric, kpiCode, kpiType))

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

		if target, err := FindUsableTarget(db, kpiCode, kpiType, metric.ID, year, periodCode, organizationID); err == nil {
			point.Target = &target.TargetValue
			if rollup.HasData {
				res := EvaluateMetricActual(db, kpiCode, metric.Direction, *rollup.Actual, target, capPercent)
				if res.Calculable {
					raw, capped := res.Raw, res.Capped
					point.AchievementRaw = &raw
					point.AchievementCap = &capped
				}
				point.PerformanceStatus = res.Status
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
	frequency := MetricFrequency(db, metric, kpiCode, kpiType)
	now := time.Now()
	currentYear := now.Year()
	currentPeriod := CurrentPeriodCode(frequency, now)

	rollup := AggregateMetricPeriod(db, metric, kpiID, kpiType, currentYear, currentPeriod, nil)
	if rollup.HasData {
		return currentYear, currentPeriod, false
	}
	if _, err := FindUsableTarget(db, kpiCode, kpiType, metric.ID, currentYear, currentPeriod, nil); err == nil {
		return currentYear, currentPeriod, false
	}

	var approvedEntries []models.KpiEntry
	db.Select("reporting_year", "period_code").
		Where("kpi_id = ? AND kpi_type = ? AND metric_id = ? AND status = ? AND organization_id IS NULL", kpiID, kpiType, metric.ID, models.KpiEntryStatusApproved).
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
		Where("kpi_code = ? AND kpi_type = ? AND metric_id = ? AND target_status IN ? AND organization_id IS NULL", kpiCode, kpiType, metric.ID, UsableTargetStatuses).
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

// PendingTargetStatuses are target statuses still on their way to approval
// (shown on the Metric Card as "waiting for approval", never used as the
// target in force).
var PendingTargetStatuses = []string{models.KpiTargetStatusDraft, models.KpiTargetStatusSubmitted, models.KpiTargetStatusReturned}

// FindPendingTarget returns the latest-version draft / submitted / returned
// unscoped target of one metric+year+period, or gorm.ErrRecordNotFound.
func FindPendingTarget(db *gorm.DB, kpiCode, kpiType string, metricID uuid.UUID, year int, periodCode string) (*models.KpiAnnualTarget, error) {
	var target models.KpiAnnualTarget
	err := db.Where("kpi_code = ? AND kpi_type = ? AND metric_id = ? AND target_year = ? AND period_code = ? AND target_status IN ? AND organization_id IS NULL",
		kpiCode, kpiType, metricID, year, periodCode, PendingTargetStatuses).
		Order("version DESC").First(&target).Error
	if err != nil {
		return nil, err
	}
	return &target, nil
}

// ResolveCardPeriod is the period the Metric Card shows:
//  1. the current period, when it has approved data or an approved target;
//  2. else the most recent period (the one ending last) that has approved
//     data or an approved target (periods that have not started yet are
//     skipped, so a target set for next year does not replace this year's);
//  3. else the most recent started period with a target waiting for
//     approval (the card shows it as such);
//  4. else the current period.
//
// The KPI's composite score keeps ResolveDisplayPeriod (a score needs data).
func ResolveCardPeriod(db *gorm.DB, metric *models.KpiMetric, kpiID uuid.UUID, kpiCode, kpiType string) (year int, periodCode string, isFallback bool) {
	frequency := MetricFrequency(db, metric, kpiCode, kpiType)
	now := time.Now()
	currentYear := now.Year()
	currentPeriod := CurrentPeriodCode(frequency, now)

	if AggregateMetricPeriod(db, metric, kpiID, kpiType, currentYear, currentPeriod, nil).HasData {
		return currentYear, currentPeriod, false
	}
	if _, err := FindUsableTarget(db, kpiCode, kpiType, metric.ID, currentYear, currentPeriod, nil); err == nil {
		return currentYear, currentPeriod, false
	}

	// "Most recent" is the started period that ends last, so periods of
	// different lengths compare correctly (H1 2026, ending 30 June, is more
	// recent than a custom 15 Feb – 15 Mar 2026 period).
	bestKey, bestYear, bestCode := int64(-1), 0, ""
	consider := func(y int, code string) {
		start, end, ok := PeriodDateRange(y, code)
		if ok && start.After(now) {
			return // not started yet
		}
		key := int64(PeriodSortKey(y, code)) * 100 // unknown code: approximate by its sort key
		if ok {
			key = end.Unix()
		}
		if key > bestKey {
			bestKey, bestYear, bestCode = key, y, code
		}
	}

	var approvedEntries []models.KpiEntry
	db.Select("reporting_year", "period_code").
		Where("kpi_id = ? AND kpi_type = ? AND metric_id = ? AND status = ? AND organization_id IS NULL", kpiID, kpiType, metric.ID, models.KpiEntryStatusApproved).
		Find(&approvedEntries)
	for _, e := range approvedEntries {
		consider(e.ReportingYear, e.PeriodCode)
	}
	targetPeriods := func(statuses []string) {
		var targets []models.KpiAnnualTarget
		db.Select("target_year", "period_code").
			Where("kpi_code = ? AND kpi_type = ? AND metric_id = ? AND target_status IN ? AND organization_id IS NULL", kpiCode, kpiType, metric.ID, statuses).
			Find(&targets)
		for _, t := range targets {
			consider(t.TargetYear, t.PeriodCode)
		}
	}
	targetPeriods(UsableTargetStatuses)
	if bestKey >= 0 {
		return bestYear, bestCode, true
	}
	targetPeriods(PendingTargetStatuses)
	if bestKey >= 0 {
		return bestYear, bestCode, true
	}
	return currentYear, currentPeriod, false
}

// AnnualRollupRow is one year's KPI-level Target/Actual/Achievement.
// Target/Actual are only set when exactly one metric contributes (a
// KPI-level sum of unrelated metrics' values would be meaningless).
type AnnualRollupRow struct {
	Year           int      `json:"year"`
	Target         *float64 `json:"target"`
	Actual         *float64 `json:"actual"`
	AchievementPct *float64 `json:"achievement_pct"`
	IsDerived      bool     `json:"is_derived"`
}

// ComputeAnnualRollups builds one row per year that has Approved entries for
// the KPI (unscoped/global entries only), from those entries and the
// approved/locked targets — never from the legacy kpi_performances table and
// never by averaging percentages (BR-10). Per Active, non-Informational metric:
//   - an "annual" target with only "annual" entries is used as-is;
//   - otherwise the year's entries are aggregated with the metric's own
//     aggregation (ratio metrics recombine sum(numerator)/sum(denominator)),
//     and the target is the annual target if there is one, else derived from
//     the targets of the periods that have data (Sum → their sum, Latest →
//     the latest period's, otherwise their average). Derived targets are
//     judged with the KPI band only (a single period's absolute thresholds
//     don't apply to a year-to-date figure). The row is then flagged derived.
//
// The KPI achievement is the weight-averaged raw achievement of the metrics
// with data (equal weights if their weights are all 0), capped at 100.
// Metrics without data/target are skipped and also flag the row as derived.
func ComputeAnnualRollups(db *gorm.DB, kpiID uuid.UUID, kpiCode, kpiType string) []AnnualRollupRow {
	var years []int
	db.Model(&models.KpiEntry{}).
		Where("kpi_id = ? AND kpi_type = ? AND status = ? AND organization_id IS NULL", kpiID, kpiType, models.KpiEntryStatusApproved).
		Distinct().Order("reporting_year ASC").Pluck("reporting_year", &years)

	var metrics []models.KpiMetric
	db.Where("kpi_id = ? AND kpi_type = ? AND metric_status = ?", kpiID, kpiType, "Active").
		Order("display_order ASC").Find(&metrics)

	rows := make([]AnnualRollupRow, 0, len(years))
	for _, year := range years {
		row := AnnualRollupRow{Year: year}
		type contribution struct{ raw, weight, target, actual float64 }
		var contribs []contribution
		for i := range metrics {
			metric := &metrics[i]
			if models.NormalizeDirection(metric.Direction) == models.DirectionInformational {
				continue
			}
			var entries []models.KpiEntry
			db.Where("kpi_id = ? AND kpi_type = ? AND metric_id = ? AND reporting_year = ? AND status = ? AND organization_id IS NULL",
				kpiID, kpiType, metric.ID, year, models.KpiEntryStatusApproved).
				Order(entryChronologicalOrder).Find(&entries)
			rollup := AggregateEntries(metric, entries)
			if !rollup.HasData {
				row.IsDerived = true
				continue
			}

			annualTarget, _ := FindUsableTarget(db, kpiCode, kpiType, metric.ID, year, "annual", nil)
			allAnnual := true
			var periods []string
			seen := map[string]bool{}
			for _, e := range entries {
				if e.PeriodCode != "annual" {
					allAnnual = false
				}
				if !seen[e.PeriodCode] {
					seen[e.PeriodCode] = true
					periods = append(periods, e.PeriodCode)
				}
			}

			var rules models.PerformanceRules
			switch {
			case annualTarget != nil && allAnnual:
				rules = PerformanceRulesFor(db, kpiCode, metric.Direction, annualTarget)
			case annualTarget != nil:
				row.IsDerived = true
				rules = PerformanceRulesFor(db, kpiCode, metric.Direction, nil)
				rules.Target = &annualTarget.TargetValue
			default:
				row.IsDerived = true
				var values []float64
				for _, p := range periods {
					if t, err := FindUsableTarget(db, kpiCode, kpiType, metric.ID, year, p, nil); err == nil {
						values = append(values, t.TargetValue)
					}
				}
				if len(values) == 0 {
					continue
				}
				isRatio := metric.CalculationType == "Percentage - Ratio" || metric.CalculationType == "Ratio"
				var derived float64
				switch {
				case metric.AggregationMethod == "Latest Approved Value" || metric.AggregationMethod == "No Aggregation":
					derived = values[len(values)-1]
				case metric.AggregationMethod == "Sum" && !isRatio:
					for _, v := range values {
						derived += v
					}
				default:
					for _, v := range values {
						derived += v
					}
					derived /= float64(len(values))
				}
				rules = PerformanceRulesFor(db, kpiCode, metric.Direction, nil)
				rules.Target = &derived
			}
			res := models.EvaluatePerformance(*rollup.Actual, rules, 0)
			if !res.Calculable {
				row.IsDerived = true
				continue
			}
			contribs = append(contribs, contribution{raw: res.Raw, weight: metric.Weight, target: *rules.Target, actual: *rollup.Actual})
		}

		if len(contribs) > 0 {
			var weightSum, weighted float64
			for _, c := range contribs {
				weightSum += c.weight
				weighted += c.raw * c.weight
			}
			if weightSum <= 0 {
				weighted, weightSum = 0, float64(len(contribs))
				for _, c := range contribs {
					weighted += c.raw
				}
			}
			pct := math.Round(weighted/weightSum*100) / 100
			if pct > 100 {
				pct = 100
			}
			row.AchievementPct = &pct
			if len(contribs) == 1 {
				t, a := contribs[0].target, contribs[0].actual
				row.Target, row.Actual = &t, &a
			}
		}
		rows = append(rows, row)
	}
	return rows
}
