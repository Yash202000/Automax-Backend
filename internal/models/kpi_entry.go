package models

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// ─── KPI Entry ──────────────────────────────────────────────────────────
// A KPI Entry is a single data submission for a metric within a given period.
// It follows the same (kpi_id, kpi_type) scoping as metrics/evidence/etc.

const (
	KpiEntryStatusDraft     = "draft"
	KpiEntryStatusSubmitted = "submitted"
	KpiEntryStatusApproved  = "approved"
	KpiEntryStatusRejected  = "rejected"
	// An approved Entry is never edited. It is corrected by a new Entry
	// (SupersedesEntryID -> original, EntryVersion+1) that goes through the
	// normal approval workflow; when that correction is approved the
	// original moves to Superseded (adjustment) or Reversed (reversal), and
	// an approved reversal record itself ends as Reversal. Only "approved"
	// entries count toward a period's Actual, so none of these do.
	KpiEntryStatusSuperseded = "superseded"
	KpiEntryStatusReversed   = "reversed"
	KpiEntryStatusReversal   = "reversal"
)

const (
	KpiEntryCorrectionAdjustment = "adjustment"
	KpiEntryCorrectionReversal   = "reversal"
)

type KpiEntry struct {
	ID       uuid.UUID  `gorm:"type:uuid;primary_key" json:"id"`
	KpiID    uuid.UUID  `gorm:"type:uuid;index;not null" json:"kpi_id"`
	KpiType  string     `gorm:"size:20;index;not null" json:"kpi_type"`
	MetricID uuid.UUID  `gorm:"type:uuid;index;not null" json:"metric_id"`
	Metric   *KpiMetric `gorm:"foreignKey:MetricID" json:"metric,omitempty"`
	// Multiple approved Entries are allowed for the same metric+period —
	// the period's Actual is an aggregate across all of them (see
	// services.AggregateMetricPeriod), not any single Entry's own value.
	// There is deliberately no unique index on
	// (kpi_id, kpi_type, metric_id, reporting_year, period_code) anymore.
	ReportingYear int        `gorm:"not null;index" json:"reporting_year"`
	PeriodCode    string     `gorm:"size:50;not null;index" json:"period_code"`
	PeriodStart   *time.Time `json:"period_start"`
	PeriodEnd     *time.Time `json:"period_end"`
	// EntryDate is the date the value was actually recorded, chosen by the
	// user and within the period. Separate from CreatedAt (the audit
	// timestamp). Nil only on entries created before the field existed.
	EntryDate *time.Time `gorm:"type:date;index" json:"entry_date"`
	// OrganizationID/SegmentationValues scope this Entry the same way a
	// Target can be scoped (BR-12 "scope consistency") — nil means
	// unscoped/global, matching the vast majority of KPIs that don't use
	// Organization or Segment dimensions at all.
	OrganizationID            *uuid.UUID       `gorm:"type:uuid;index" json:"organization_id"`
	Organization              *KpiOrganization `gorm:"foreignKey:OrganizationID" json:"organization,omitempty"`
	SegmentationValues        datatypes.JSON   `gorm:"type:jsonb" json:"segmentation_values"`
	CalculationTypeSnapshot   string           `gorm:"size:50" json:"calculation_type_snapshot"`
	DirectionSnapshot         string           `gorm:"size:50" json:"direction_snapshot"`
	UnitSnapshot              string           `gorm:"size:50" json:"unit_snapshot"`
	DecimalPrecisionSnapshot  int              `gorm:"default:0" json:"decimal_precision_snapshot"`
	NumeratorLabelSnapshot    string           `gorm:"size:255" json:"numerator_label_snapshot"`
	DenominatorLabelSnapshot  string           `gorm:"size:255" json:"denominator_label_snapshot"`
	AggregationMethodSnapshot string           `gorm:"size:50" json:"aggregation_method_snapshot"`
	TargetID                  *uuid.UUID       `gorm:"type:uuid;index" json:"target_id"`
	TargetValueSnapshot       *float64         `json:"target_value_snapshot"`
	ThresholdModeSnapshot     string           `gorm:"size:50" json:"threshold_mode_snapshot"`
	DirectActualValue         *float64         `json:"direct_actual_value"`
	NumeratorValue            *float64         `json:"numerator_value"`
	DenominatorValue          *float64         `json:"denominator_value"`
	ComponentValues           datatypes.JSON   `gorm:"type:jsonb" json:"component_values"`
	ActualValue               float64          `gorm:"default:0" json:"actual_value"`
	ActualCalculationTrace    string           `gorm:"type:text" json:"actual_calculation_trace"`
	AchievementPercentage     *float64         `json:"achievement_percentage"`
	VarianceValue             *float64         `json:"variance_value"`
	PerformanceStatus         string           `gorm:"size:50;default:'Not Calculable'" json:"performance_status"`
	AggregatedValue           *float64         `json:"aggregated_value"`
	DataSourceType            string           `gorm:"size:50;default:'Manual'" json:"data_source_type"`
	SourceReference           string           `gorm:"size:500" json:"source_reference"`
	DataCutoffDate            *time.Time       `json:"data_cutoff_date"`
	DataQualityStatus         string           `gorm:"size:50;default:'Not Verified'" json:"data_quality_status"`
	DataQualityNotes          string           `gorm:"type:text" json:"data_quality_notes"`
	PerformanceCommentary     string           `gorm:"type:text" json:"performance_commentary"`
	ImprovementAction         string           `gorm:"type:text" json:"improvement_action"`
	Status                    string           `gorm:"size:30;not null;default:'draft'" json:"status"`
	SubmittedByID             *uuid.UUID       `gorm:"type:uuid;index" json:"submitted_by_id"`
	SubmittedBy               *User            `gorm:"foreignKey:SubmittedByID" json:"submitted_by,omitempty"`
	ApprovedByID              *uuid.UUID       `gorm:"type:uuid;index" json:"approved_by_id"`
	ApprovedBy                *User            `gorm:"foreignKey:ApprovedByID" json:"approved_by,omitempty"`
	WorkflowInstanceID        *uuid.UUID       `gorm:"type:uuid;index" json:"workflow_instance_id"`
	EntryVersion              int              `gorm:"default:1" json:"entry_version"`
	SupersedesEntryID         *uuid.UUID       `gorm:"type:uuid;index" json:"supersedes_entry_id"`
	// CorrectionType/CorrectionReason are set only on correction entries
	// ("adjustment" or "reversal"); empty on an original entry.
	CorrectionType   string         `gorm:"size:20" json:"correction_type,omitempty"`
	CorrectionReason string         `gorm:"type:text" json:"correction_reason,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	UpdatedAt        time.Time      `json:"updated_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
}

func (e *KpiEntry) BeforeCreate(tx *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}

type KpiEntryComponentValue struct {
	Component string  `json:"component"`
	Value     float64 `json:"value"`
	Weight    float64 `json:"weight,omitempty"`
	Sequence  int     `json:"sequence"`
}

type KpiEntryRequest struct {
	MetricID              string                       `json:"metric_id" validate:"required,uuid"`
	ReportingYear         int                          `json:"reporting_year" validate:"required,min=2020,max=2040"`
	PeriodCode            string                       `json:"period_code" validate:"required,max=50"`
	PeriodStartDate       string                       `json:"period_start_date"`
	PeriodEndDate         string                       `json:"period_end_date"`
	EntryDate             string                       `json:"entry_date"`
	OrganizationID        *string                      `json:"organization_id"`
	SegmentationValues    []KpiTargetSegmentationValue `json:"segmentation_values"`
	DirectActualValue     *float64                     `json:"direct_actual_value"`
	NumeratorValue        *float64                     `json:"numerator_value"`
	DenominatorValue      *float64                     `json:"denominator_value"`
	ComponentValues       []KpiEntryComponentValue     `json:"component_values"`
	DataSourceType        string                       `json:"data_source_type"`
	SourceReference       string                       `json:"source_reference"`
	DataCutoffDate        string                       `json:"data_cutoff_date"`
	DataQualityStatus     string                       `json:"data_quality_status"`
	DataQualityNotes      string                       `json:"data_quality_notes"`
	PerformanceCommentary string                       `json:"performance_commentary"`
	ImprovementAction     string                       `json:"improvement_action"`
}

// ToModel builds a draft entry from the request, snapshotting the metric's
// configuration and computing ActualValue from the inputs the metric's
// Calculation Type needs. An error means those inputs are missing or invalid
// (e.g. no direct value, a zero denominator) and the entry must be rejected
// rather than silently recorded as 0.
func (r *KpiEntryRequest) ToModel(db *gorm.DB, kpiID uuid.UUID, kpiType string, userID uuid.UUID) (*KpiEntry, error) {
	entry := &KpiEntry{
		KpiID:                 kpiID,
		KpiType:               kpiType,
		ReportingYear:         r.ReportingYear,
		PeriodCode:            r.PeriodCode,
		DirectActualValue:     r.DirectActualValue,
		NumeratorValue:        r.NumeratorValue,
		DenominatorValue:      r.DenominatorValue,
		DataSourceType:        r.DataSourceType,
		SourceReference:       r.SourceReference,
		DataQualityStatus:     r.DataQualityStatus,
		DataQualityNotes:      r.DataQualityNotes,
		PerformanceCommentary: r.PerformanceCommentary,
		ImprovementAction:     r.ImprovementAction,
		Status:                "draft",
		SubmittedByID:         &userID,
		EntryVersion:          1,
	}
	if r.ComponentValues != nil {
		data, _ := json.Marshal(r.ComponentValues)
		entry.ComponentValues = datatypes.JSON(data)
	}

	if r.DataSourceType == "" {
		entry.DataSourceType = "Manual"
	}
	if r.DataQualityStatus == "" {
		entry.DataQualityStatus = "Not Verified"
	}
	if r.DataCutoffDate != "" {
		if t, err := time.Parse("2006-01-02", r.DataCutoffDate); err == nil {
			entry.DataCutoffDate = &t
		}
	}
	if r.PeriodStartDate != "" {
		if t, err := time.Parse("2006-01-02", r.PeriodStartDate); err == nil {
			entry.PeriodStart = &t
		}
	}
	entry.EntryDate = parseEntryDate(r.EntryDate)
	if r.PeriodEndDate != "" {
		if t, err := time.Parse("2006-01-02", r.PeriodEndDate); err == nil {
			entry.PeriodEnd = &t
		}
	}
	if r.OrganizationID != nil && *r.OrganizationID != "" {
		if oid, err := uuid.Parse(*r.OrganizationID); err == nil {
			entry.OrganizationID = &oid
		}
	}
	if len(r.SegmentationValues) > 0 {
		if b, err := json.Marshal(r.SegmentationValues); err == nil {
			entry.SegmentationValues = datatypes.JSON(b)
		}
	}

	// Parse metric and snapshot its configuration
	if r.MetricID != "" {
		if mid, err := uuid.Parse(r.MetricID); err == nil {
			entry.MetricID = mid
			var metric KpiMetric
			if err := db.Where("id = ?", mid).First(&metric).Error; err == nil {
				entry.CalculationTypeSnapshot = metric.CalculationType
				entry.DirectionSnapshot = metric.Direction
				entry.UnitSnapshot = metric.Unit
				entry.DecimalPrecisionSnapshot = metric.DecimalPrecision
				entry.NumeratorLabelSnapshot = metric.NumeratorLabel
				entry.DenominatorLabelSnapshot = metric.DenominatorLabel
				entry.AggregationMethodSnapshot = metric.AggregationMethod

				// Calculate actual value based on metric type
				if err := entry.ComputeActualValue(); err != nil {
					return nil, err
				}

				// No target snapshot yet — the handler sets this afterward
				// if a real approved KpiAnnualTarget exists for this period.
				// There's deliberately no fallback to a metric-level static
				// target here: a single timeless number can't represent a
				// target meant to progress period over period, so achievement
				// correctly comes back "Not Calculable" (see
				// EvaluatePerformance) rather than being measured against an
				// unrelated number just because one happened to be set once.
			}
		}
	}

	return entry, nil
}

// ─── Achievement & performance status ───────────────────────────────────
// EvaluatePerformance is the ONE place achievement % and performance status
// are derived — entries, period rollups, composite scores, the annual rollup
// and the dashboards all go through it, so they can never disagree.

const (
	PerformanceStatusExceeded      = "Exceeded"
	PerformanceStatusAchieved      = "Achieved"
	PerformanceStatusWarning       = "Warning"
	PerformanceStatusBelowTarget   = "Below Target"
	PerformanceStatusInRange       = "In Range"
	PerformanceStatusOutOfRange    = "Out of Range"
	PerformanceStatusInformational = "Informational"
	PerformanceStatusNotCalculable = "Not Calculable"
)

const (
	DirectionHigherIsBetter = "Higher is Better"
	DirectionLowerIsBetter  = "Lower is Better"
	DirectionTargetRange    = "Target Range"
	DirectionExactTarget    = "Exact Target"
	DirectionInformational  = "Informational"
)

// Global default status thresholds (achievement %), used when neither the
// Target's own thresholds nor a Performance Band apply. Exceeded is always
// "at or above 100% of target"; Achieved/Warning are the band's green/amber
// minimums (see DefaultKpiPerformanceBand, which carries the same values).
const (
	DefaultExceededMin = 100.0
	DefaultAchievedMin = 80.0
	DefaultWarningMin  = 50.0
)

// Target threshold modes (KpiAnnualTarget.ThresholdMode).
const (
	ThresholdModeGlobal       = "Use Global KPI Rules"
	ThresholdModePercentage   = "Percentage of Target"
	ThresholdModeAbsolute     = "Absolute Values"
	ThresholdModeTargetRange  = "Target Range"
	ThresholdModeNoThresholds = "No Thresholds"
)

// PerformanceRules is everything that decides how one Actual is judged: the
// metric's Direction, the Target value, the Target's own threshold settings
// and the KPI's effective Performance Band (GreenMin/AmberMin; nil = the
// global defaults).
type PerformanceRules struct {
	Direction          string
	Target             *float64
	ThresholdMode      string
	ExcellentThreshold *float64
	AchievedThreshold  *float64
	WarningThreshold   *float64
	RangeMin           *float64
	RangeMax           *float64
	GreenMin           *float64
	AmberMin           *float64
}

// PerformanceResult: Raw is the uncapped achievement % (kept for audit,
// BR-11, floored at 0), Capped is Raw limited to the display cap. Calculable
// is false for Informational metrics and when no usable Target exists; Raw
// and Capped are then 0 and must not be shown or aggregated.
type PerformanceResult struct {
	Calculable bool
	Raw        float64
	Capped     float64
	Variance   *float64
	Status     string
}

// WithTarget fills the target-derived parts of the rules (value, threshold
// mode, thresholds, range) from an approved target. Nil leaves them empty.
func (r PerformanceRules) WithTarget(t *KpiAnnualTarget) PerformanceRules {
	if t == nil {
		return r
	}
	v := t.TargetValue
	r.Target = &v
	r.ThresholdMode = t.ThresholdMode
	r.ExcellentThreshold = t.ExcellentThreshold
	r.AchievedThreshold = t.AchievedThreshold
	r.WarningThreshold = t.WarningThreshold
	r.RangeMin = t.TargetRangeMin
	r.RangeMax = t.TargetRangeMax
	return r
}

// NormalizeDirection maps a metric Direction (or a KPI polarity) onto one of
// the Direction* constants; anything unknown is Higher is Better.
func NormalizeDirection(direction string) string {
	switch strings.ToLower(strings.TrimSpace(direction)) {
	case "lower is better", KPIPolarityDescending, "lower":
		return DirectionLowerIsBetter
	case "target range", "range":
		return DirectionTargetRange
	case "exact target", "exact":
		return DirectionExactTarget
	case "informational":
		return DirectionInformational
	}
	return DirectionHigherIsBetter
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

// AchievementPercent is the raw achievement % of actual against target for a
// Higher/Lower is Better or Exact Target direction. ok is false when it
// cannot be calculated (Higher is Better with a zero target). Rules:
//   - Higher is Better: actual / target × 100. A negative target uses the
//     equivalent 100 + (actual − target) / |target| × 100, so a less
//     negative actual still scores above 100.
//   - Lower is Better: target / actual × 100. Actual ≤ 0 against a positive
//     target is the best possible result and scores 100 (never ±Inf). A zero
//     target scores 100 when actual ≤ 0 and 0 otherwise. A negative target
//     uses 100 + (target − actual) / |target| × 100.
//   - Exact Target: 100 − |actual − target| / |target| × 100 (deviation);
//     a zero target scores 100 only when hit exactly.
//
// The result is never negative and never NaN/Inf.
func AchievementPercent(actual, target float64, direction string) (float64, bool) {
	var raw float64
	switch NormalizeDirection(direction) {
	case DirectionLowerIsBetter:
		switch {
		case target > 0 && actual <= 0:
			raw = 100
		case target > 0:
			raw = target / actual * 100
		case target == 0 && actual <= 0:
			raw = 100
		case target == 0:
			raw = 0
		default:
			raw = 100 + (target-actual)/math.Abs(target)*100
		}
	case DirectionExactTarget, DirectionTargetRange:
		if target == 0 {
			if actual == 0 {
				raw = 100
			}
		} else {
			raw = 100 - math.Abs(actual-target)/math.Abs(target)*100
		}
	default:
		if target == 0 {
			return 0, false
		}
		raw = 100 + (actual-target)/math.Abs(target)*100
	}
	if math.IsNaN(raw) || math.IsInf(raw, 0) {
		return 0, false
	}
	if raw < 0 {
		raw = 0
	}
	return round2(raw), true
}

// rangeAchievement scores an actual against a [min, max] range: 100 inside,
// otherwise reduced by the distance to the nearest bound relative to that
// bound (or to the range width when the bound is 0).
func rangeAchievement(actual, min, max float64) (float64, bool) {
	if actual >= min && actual <= max {
		return 100, true
	}
	bound := min
	if actual > max {
		bound = max
	}
	ref := math.Abs(bound)
	if ref == 0 {
		ref = max - min
	}
	if ref <= 0 {
		return 0, false
	}
	raw := 100 - math.Abs(actual-bound)/ref*100
	if raw < 0 {
		raw = 0
	}
	return round2(raw), false
}

// statusFromLevels buckets value against excellent/achieved/warning levels
// (nil levels are skipped). higherIsBetter=false compares downwards, for
// absolute thresholds on a Lower is Better metric.
func statusFromLevels(value float64, excellent, achieved, warning *float64, higherIsBetter bool) string {
	meets := func(level *float64) bool {
		if level == nil {
			return false
		}
		if higherIsBetter {
			return value >= *level
		}
		return value <= *level
	}
	switch {
	case meets(excellent):
		return PerformanceStatusExceeded
	case meets(achieved):
		return PerformanceStatusAchieved
	case meets(warning):
		return PerformanceStatusWarning
	}
	return PerformanceStatusBelowTarget
}

func ptrOr(p *float64, def float64) *float64 {
	if p != nil {
		return p
	}
	return &def
}

// EvaluatePerformance judges actual against the rules. capPercent is the
// display cap applied to Capped (≤ 0 means no cap). Status precedence:
//  1. Informational direction → no achievement, status Informational.
//  2. Target Range direction (or a Target Range threshold mode with both
//     bounds) → inside the range is In Range at 100%; outside is Out of
//     Range with a distance-based achievement.
//  3. Target thresholds when the mode is Percentage of Target (compared to
//     the raw %) or Absolute Values (compared to the actual, respecting the
//     direction) and at least one threshold is set.
//  4. Otherwise the KPI's Performance Band (green → Achieved, amber →
//     Warning), falling back to the global 100/80/50 defaults.
//
// Exact Target can't be "exceeded": an exact hit is Achieved.
func EvaluatePerformance(actual float64, r PerformanceRules, capPercent float64) PerformanceResult {
	res := PerformanceResult{Status: PerformanceStatusNotCalculable}
	if r.Target != nil {
		v := round2(actual - *r.Target)
		res.Variance = &v
	}
	direction := NormalizeDirection(r.Direction)
	if direction == DirectionInformational {
		res.Status = PerformanceStatusInformational
		return res
	}
	finish := func(raw float64) PerformanceResult {
		res.Calculable = true
		res.Raw = raw
		res.Capped = raw
		if capPercent > 0 && res.Capped > capPercent {
			res.Capped = capPercent
		}
		return res
	}

	hasRange := r.RangeMin != nil && r.RangeMax != nil && *r.RangeMin <= *r.RangeMax
	if direction == DirectionTargetRange || (r.ThresholdMode == ThresholdModeTargetRange && hasRange) {
		var min, max float64
		switch {
		case hasRange:
			min, max = *r.RangeMin, *r.RangeMax
		case r.Target != nil:
			min, max = *r.Target, *r.Target
		default:
			return res
		}
		raw, inside := rangeAchievement(actual, min, max)
		res.Status = PerformanceStatusOutOfRange
		if inside {
			res.Status = PerformanceStatusInRange
		}
		return finish(raw)
	}

	if r.Target == nil {
		return res
	}
	raw, ok := AchievementPercent(actual, *r.Target, direction)
	if !ok {
		return res
	}

	hasThresholds := r.ExcellentThreshold != nil || r.AchievedThreshold != nil || r.WarningThreshold != nil
	switch {
	case r.ThresholdMode == ThresholdModePercentage && hasThresholds:
		res.Status = statusFromLevels(raw, ptrOr(r.ExcellentThreshold, DefaultExceededMin), r.AchievedThreshold, r.WarningThreshold, true)
	case r.ThresholdMode == ThresholdModeAbsolute && hasThresholds && direction != DirectionExactTarget:
		res.Status = statusFromLevels(actual, ptrOr(r.ExcellentThreshold, *r.Target), r.AchievedThreshold, r.WarningThreshold,
			direction != DirectionLowerIsBetter)
	default:
		exceeded := DefaultExceededMin
		res.Status = statusFromLevels(raw, &exceeded, ptrOr(r.GreenMin, DefaultAchievedMin), ptrOr(r.AmberMin, DefaultWarningMin), true)
	}
	if direction == DirectionExactTarget && res.Status == PerformanceStatusExceeded {
		res.Status = PerformanceStatusAchieved
	}
	return finish(raw)
}

// ComputeAchievement derives AchievementPercentage (capped at 100),
// VarianceValue and PerformanceStatus from ActualValue against
// TargetValueSnapshot and the metric direction only — global default
// thresholds. Callers that know the target's thresholds and the KPI's band
// use ComputeAchievementWith instead (see services.EntryPerformanceRules).
func (e *KpiEntry) ComputeAchievement() {
	e.ComputeAchievementWith(PerformanceRules{})
}

// ComputeAchievementWith is ComputeAchievement with the target thresholds /
// band in rules. Direction and Target always come from the entry's own
// snapshots, so the stored result matches what was captured at creation.
func (e *KpiEntry) ComputeAchievementWith(rules PerformanceRules) {
	rules.Direction = e.DirectionSnapshot
	rules.Target = e.TargetValueSnapshot
	res := EvaluatePerformance(e.ActualValue, rules, 100)
	e.VarianceValue = res.Variance
	e.PerformanceStatus = res.Status
	e.AchievementPercentage = nil
	if res.Calculable {
		pct := res.Capped
		e.AchievementPercentage = &pct
	}
}

// ComputeActualValue sets ActualValue from the inputs required by the
// entry's Calculation Type (always starting from 0, so a recalculation never
// accumulates onto a previous result). It returns an error, and leaves the
// entry unusable, when those inputs are missing or invalid:
//   - Direct Value: direct_actual_value is required.
//   - Percentage - Ratio / Ratio: numerator and a non-zero denominator.
//   - Sum / Average: at least one component value.
//   - Weighted Average: at least one component; weights must be ≥ 0 and
//     sum to more than 0 (a weight is taken as sent — 0 means "no weight").
//   - Difference: the first component minus the sum of the others (the
//     entry form's input, at least two components); numerator − denominator
//     is still accepted when no components are sent.
func (e *KpiEntry) ComputeActualValue() error {
	e.ActualValue = 0
	var comps []KpiEntryComponentValue
	if len(e.ComponentValues) > 0 {
		if err := json.Unmarshal(e.ComponentValues, &comps); err != nil {
			return fmt.Errorf("component_values is not valid")
		}
	}
	switch e.CalculationTypeSnapshot {
	case "Direct Value":
		if e.DirectActualValue == nil {
			return fmt.Errorf("Actual value (direct_actual_value) is required for a Direct Value metric")
		}
		e.ActualValue = *e.DirectActualValue
	case "Percentage - Ratio", "Ratio":
		if e.NumeratorValue == nil || e.DenominatorValue == nil {
			return fmt.Errorf("Both numerator and denominator are required for a %s metric", e.CalculationTypeSnapshot)
		}
		if *e.DenominatorValue == 0 {
			return fmt.Errorf("Denominator cannot be zero for ratio-type metrics")
		}
		e.ActualValue = *e.NumeratorValue / *e.DenominatorValue
		if e.CalculationTypeSnapshot == "Percentage - Ratio" {
			e.ActualValue *= 100
		}
	case "Average", "Sum":
		if len(comps) == 0 {
			return fmt.Errorf("At least one component value is required for a %s metric", e.CalculationTypeSnapshot)
		}
		var sum float64
		for _, c := range comps {
			sum += c.Value
		}
		if e.CalculationTypeSnapshot == "Average" {
			sum /= float64(len(comps))
		}
		e.ActualValue = sum
	case "Weighted Average":
		if len(comps) == 0 {
			return fmt.Errorf("At least one component value is required for a Weighted Average metric")
		}
		var totalWeight, weightedSum float64
		for _, c := range comps {
			if c.Weight < 0 {
				return fmt.Errorf("Component weights cannot be negative")
			}
			weightedSum += c.Value * c.Weight
			totalWeight += c.Weight
		}
		if totalWeight <= 0 {
			return fmt.Errorf("Component weights must add up to more than 0 for a Weighted Average metric")
		}
		e.ActualValue = weightedSum / totalWeight
	case "Difference":
		switch {
		case len(comps) >= 2:
			e.ActualValue = comps[0].Value
			for _, c := range comps[1:] {
				e.ActualValue -= c.Value
			}
		case len(comps) == 0 && e.NumeratorValue != nil && e.DenominatorValue != nil:
			e.ActualValue = *e.NumeratorValue - *e.DenominatorValue
		default:
			return fmt.Errorf("At least two component values are required for a Difference metric (first minus the rest)")
		}
	case "Formula":
		return fmt.Errorf("Formula (Phase 2) metrics are not supported yet")
	default:
		// Unknown/legacy calculation type: keep a direct value if one was given.
		if e.DirectActualValue != nil {
			e.ActualValue = *e.DirectActualValue
		}
	}
	if math.IsNaN(e.ActualValue) || math.IsInf(e.ActualValue, 0) {
		e.ActualValue = 0
		return fmt.Errorf("The entered values do not produce a valid actual value")
	}
	return nil
}

// parseEntryDate parses a "YYYY-MM-DD" Entry Date; nil when empty or invalid
// (the handler then rejects the entry as missing its date).
func parseEntryDate(s string) *time.Time {
	if len(s) > 10 {
		s = s[:10]
	}
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return nil
	}
	return &t
}

// KpiEntryUpdateRequest carries the editable fields of an existing draft
// entry. MetricID/period cannot be changed after creation (that would change
// the entry's identity) — to correct those, delete and re-create instead.
// Numeric inputs left out of the request keep their current value; an
// explicit JSON null clears them (see MarkExplicitNulls), which then fails
// ComputeActualValue if the input is required.
type KpiEntryUpdateRequest struct {
	EntryDate             string                   `json:"entry_date"`
	DirectActualValue     *float64                 `json:"direct_actual_value"`
	NumeratorValue        *float64                 `json:"numerator_value"`
	DenominatorValue      *float64                 `json:"denominator_value"`
	ComponentValues       []KpiEntryComponentValue `json:"component_values"`
	DataSourceType        string                   `json:"data_source_type"`
	SourceReference       string                   `json:"source_reference"`
	DataCutoffDate        string                   `json:"data_cutoff_date"`
	DataQualityStatus     string                   `json:"data_quality_status"`
	DataQualityNotes      string                   `json:"data_quality_notes"`
	PerformanceCommentary string                   `json:"performance_commentary"`
	ImprovementAction     string                   `json:"improvement_action"`

	// cleared holds the numeric inputs sent as an explicit null.
	cleared map[string]bool
}

// MarkExplicitNulls records which numeric inputs the raw JSON body sets to
// null, so ApplyTo can tell "clear this value" from "not sent".
func (r *KpiEntryUpdateRequest) MarkExplicitNulls(body []byte) {
	var raw map[string]json.RawMessage
	if json.Unmarshal(body, &raw) != nil {
		return
	}
	for _, k := range []string{"direct_actual_value", "numerator_value", "denominator_value", "component_values"} {
		if v, ok := raw[k]; ok && strings.TrimSpace(string(v)) == "null" {
			if r.cleared == nil {
				r.cleared = map[string]bool{}
			}
			r.cleared[k] = true
		}
	}
}

// ApplyTo mutates an existing entry with the request's fields and recomputes
// ActualValue from the resulting inputs, using the entry's already-
// snapshotted CalculationTypeSnapshot. Numeric inputs are only replaced when
// sent (or explicitly nulled). keepTextWhenEmpty leaves the text fields as
// they are when the request sends them empty — used for corrections, which
// start from the corrected entry's values; a draft edit sends the whole form
// so an empty text field there means "cleared". Achievement is NOT
// recomputed here: the caller does it with the target's thresholds and the
// KPI's band (services.EntryPerformanceRules).
func (r *KpiEntryUpdateRequest) ApplyTo(e *KpiEntry, keepTextWhenEmpty bool) error {
	if d := parseEntryDate(r.EntryDate); d != nil {
		e.EntryDate = d
	}
	setNum := func(dst **float64, v *float64, key string) {
		if v != nil {
			x := *v
			*dst = &x
		} else if r.cleared[key] {
			*dst = nil
		}
	}
	setNum(&e.DirectActualValue, r.DirectActualValue, "direct_actual_value")
	setNum(&e.NumeratorValue, r.NumeratorValue, "numerator_value")
	setNum(&e.DenominatorValue, r.DenominatorValue, "denominator_value")
	switch {
	case r.ComponentValues != nil:
		data, _ := json.Marshal(r.ComponentValues)
		e.ComponentValues = datatypes.JSON(data)
	case r.cleared["component_values"]:
		e.ComponentValues = nil
	case e.CalculationTypeSnapshot == "Difference" && (r.NumeratorValue != nil || r.DenominatorValue != nil):
		// A Difference corrected through numerator/denominator replaces the
		// components it was originally entered with.
		e.ComponentValues = nil
	}
	setText := func(dst *string, v string) {
		if v != "" || !keepTextWhenEmpty {
			*dst = v
		}
	}
	if r.DataSourceType != "" {
		e.DataSourceType = r.DataSourceType
	}
	setText(&e.SourceReference, r.SourceReference)
	if r.DataCutoffDate != "" {
		if t, err := time.Parse("2006-01-02", r.DataCutoffDate); err == nil {
			e.DataCutoffDate = &t
		}
	}
	if r.DataQualityStatus != "" {
		e.DataQualityStatus = r.DataQualityStatus
	}
	setText(&e.DataQualityNotes, r.DataQualityNotes)
	setText(&e.PerformanceCommentary, r.PerformanceCommentary)
	setText(&e.ImprovementAction, r.ImprovementAction)
	return e.ComputeActualValue()
}

// KpiEntryCorrectionRequest creates a correction of an approved entry. An
// adjustment carries the corrected values (same fields as a draft edit); a
// reversal cancels the original and ignores them.
type KpiEntryCorrectionRequest struct {
	KpiEntryUpdateRequest
	CorrectionType string `json:"correction_type" validate:"required,oneof=adjustment reversal"`
	Reason         string `json:"reason" validate:"required,max=2000"`
}

// NewCorrection builds the draft correction entry for an approved original:
// same metric, period, scope, configuration and target snapshots, next
// version, linked back via SupersedesEntryID. The original is not modified.
// An adjustment starts from the original's values and only replaces the
// ones it sends, so a note-only adjustment keeps the original's actual.
func (e *KpiEntry) NewCorrection(req *KpiEntryCorrectionRequest, userID uuid.UUID) (*KpiEntry, error) {
	c := *e
	c.ID = uuid.Nil
	c.Metric, c.Organization, c.SubmittedBy, c.ApprovedBy = nil, nil, nil, nil
	c.Status = KpiEntryStatusDraft
	c.SubmittedByID = &userID
	c.ApprovedByID = nil
	c.WorkflowInstanceID = nil
	c.EntryVersion = e.EntryVersion + 1
	c.SupersedesEntryID = &e.ID
	c.CorrectionType = req.CorrectionType
	c.CorrectionReason = req.Reason
	c.CreatedAt, c.UpdatedAt = time.Time{}, time.Time{}
	c.DeletedAt = gorm.DeletedAt{}
	if req.CorrectionType == KpiEntryCorrectionAdjustment {
		if err := req.KpiEntryUpdateRequest.ApplyTo(&c, true); err != nil {
			return nil, err
		}
	}
	return &c, nil
}

// ─── KPI Entry Evidence ──────────────────────────────────────────────────

type KpiEntryEvidence struct {
	ID           uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	EntryID      uuid.UUID `gorm:"type:uuid;index;not null" json:"entry_id"`
	Entry        *KpiEntry `gorm:"foreignKey:EntryID" json:"-"`
	Title        string    `gorm:"size:255;not null" json:"title"`
	EvidenceType string    `gorm:"size:50;not null;default:'Report'" json:"evidence_type"`
	Description  string    `gorm:"type:text" json:"description"`
	FileURL      string    `gorm:"size:500" json:"file_url"`
	FileName     string    `gorm:"size:255" json:"file_name"`
	FileSize     int64     `json:"file_size"`
	MimeType     string    `gorm:"size:100" json:"mime_type"`
	UploadedByID uuid.UUID `gorm:"type:uuid;not null" json:"uploaded_by_id"`
	UploadedBy   *User     `gorm:"foreignKey:UploadedByID" json:"uploaded_by,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

func (e *KpiEntryEvidence) BeforeCreate(tx *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}
