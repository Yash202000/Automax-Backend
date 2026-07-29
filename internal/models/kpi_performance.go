package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

const (
	KPIPerfStatusDraft     = "draft"
	KPIPerfStatusSubmitted = "submitted"
	KPIPerfStatusReview    = "under_review"
	KPIPerfStatusApproved  = "approved"
	KPIPerfStatusRejected  = "rejected"
	KPIPerfStatusPublished = "published"
)

// ──────────────────────────────────────────────────────────
// KpiAnnualTarget — one row per KPI per metric per period
// ──────────────────────────────────────────────────────────

type KpiTargetSegmentationValue struct {
	Dimension string `json:"dimension"`
	Value     string `json:"value"`
}

// The real uniqueness constraint — one target per (kpi_code, kpi_type,
// metric_id, target_year, period_code) among non-deleted rows — is a partial
// unique index created via migrations.MigrateKpiTargetUniqueIndex, not a
// GORM struct tag: it needs to exclude soft-deleted rows (a plain unique
// index would let an old deleted target permanently block recreating one for
// the same period) and needs to key on metric_id/target_year, which the
// original idx_kpi_target_period_unique (kpi_code+kpi_type+period_type+
// period_key) never did — period_type is hardcoded to "annual" for every
// target regardless of actual frequency, so that index provided no real
// per-metric or per-year uniqueness at all.
type KpiAnnualTarget struct {
	ID                         uuid.UUID                  `gorm:"type:uuid;primary_key" json:"id"`
	KpiCode                    string                     `gorm:"size:50;not null;index" json:"kpi_code"`
	KpiType                    string                     `gorm:"size:20;not null;index" json:"kpi_type"`
	Year                       int                        `gorm:"not null;index" json:"year"`
	PeriodType                 string                     `gorm:"size:20;not null;default:'annual'" json:"period_type"`
	PeriodKey                  string                     `gorm:"size:20" json:"period_key"`
	TargetValue                float64                    `gorm:"default:0" json:"target_value"`

	// Phase 1 extended fields
	MetricID                    *uuid.UUID                 `gorm:"type:uuid;index" json:"metric_id"`
	Metric                      *KpiMetric                 `gorm:"foreignKey:MetricID" json:"metric,omitempty"`
	CalculationTypeSnapshot     string                     `gorm:"size:50" json:"calculation_type_snapshot"`
	DirectionSnapshot           string                     `gorm:"size:50" json:"direction_snapshot"`
	UnitSnapshot                string                     `gorm:"size:50" json:"unit_snapshot"`
	DecimalPrecisionSnapshot    int                        `gorm:"default:0" json:"decimal_precision_snapshot"`
	AggregationMethodSnapshot   string                     `gorm:"size:50" json:"aggregation_method_snapshot"`
	ReportingFrequencySnapshot  string                     `gorm:"size:50" json:"reporting_frequency_snapshot"`
	TargetYear                  int                        `gorm:"default:0" json:"target_year"`
	PeriodCode                  string                     `gorm:"size:50" json:"period_code"`
	PeriodStart                 *time.Time                 `json:"period_start"`
	PeriodEnd                   *time.Time                 `json:"period_end"`
	TargetType                  string                     `gorm:"size:50;default:'Period Target'" json:"target_type"`
	TargetBasis                 string                     `gorm:"size:50;default:'Management Decision'" json:"target_basis"`
	TargetRationale             string                     `gorm:"type:text" json:"target_rationale"`
	ThresholdMode               string                     `gorm:"size:50;default:'Use Global KPI Rules'" json:"threshold_mode"`
	ExcellentThreshold          *float64                   `json:"excellent_threshold"`
	AchievedThreshold           *float64                   `json:"achieved_threshold"`
	WarningThreshold            *float64                   `json:"warning_threshold"`
	TargetRangeMin              *float64                   `json:"target_range_min"`
	TargetRangeMax              *float64                   `json:"target_range_max"`
	SegmentationValues          datatypes.JSON             `gorm:"type:jsonb" json:"segmentation_values"`
	TargetStatus                string                     `gorm:"size:30;not null;default:'draft'" json:"target_status"`
	EffectiveFrom               *time.Time                 `json:"effective_from"`
	EffectiveTo                 *time.Time                 `json:"effective_to"`
	ApprovedByID                *uuid.UUID                 `gorm:"type:uuid;index" json:"approved_by_id"`
	ApprovedBy                  *User                      `gorm:"foreignKey:ApprovedByID" json:"approved_by,omitempty"`
	ApprovedAt                  *time.Time                 `json:"approved_at"`
	SupersedesEntryID           *uuid.UUID                 `gorm:"type:uuid" json:"supersedes_entry_id"`
	CreatedAt                   time.Time                  `json:"created_at"`
	UpdatedAt                   time.Time                  `json:"updated_at"`
	DeletedAt                   gorm.DeletedAt             `gorm:"index" json:"-"`
}

func (t *KpiAnnualTarget) BeforeCreate(tx *gorm.DB) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	return nil
}

type KpiAnnualTargetRequest struct {
	KpiCode          string                     `json:"kpi_code" validate:"required,max=50"`
	KpiType          string                     `json:"kpi_type" validate:"required,oneof=strategic operational award"`
	MetricID         *string                    `json:"metric_id"`
	TargetYear       int                        `json:"target_year" validate:"min=2020,max=2040"`
	PeriodCode       string                     `json:"period_code" validate:"required,max=50"`
	PeriodStart      string                     `json:"period_start"`
	PeriodEnd        string                     `json:"period_end"`
	TargetValue      *float64                   `json:"target_value"`
	TargetType       string                     `json:"target_type" validate:"omitempty,oneof='Period Target' 'Annual Target' 'Milestone / Ad Hoc'"`
	TargetBasis      string                     `json:"target_basis"`
	TargetRationale  string                     `json:"target_rationale"`
	ThresholdMode    string                     `json:"threshold_mode"`
	ExcellentThreshold  *float64                `json:"excellent_threshold"`
	AchievedThreshold   *float64                `json:"achieved_threshold"`
	WarningThreshold    *float64                `json:"warning_threshold"`
	TargetRangeMin      *float64                `json:"target_range_min"`
	TargetRangeMax      *float64                `json:"target_range_max"`
	SegmentationValues  []KpiTargetSegmentationValue `json:"segmentation_values"`
	EffectiveFrom    string                     `json:"effective_from"`
	EffectiveTo      string                     `json:"effective_to"`
	// TargetStatus lets the frontend's Save Draft / Submit Target buttons
	// actually set the resulting status — previously absent, so every
	// created target silently defaulted to "draft" regardless of which
	// button was clicked. Omitted (nil) still defaults to "draft".
	TargetStatus     *string                    `json:"target_status" validate:"omitempty,oneof=draft submitted approved returned rejected locked superseded"`
}

// ToModel hydrates a KpiAnnualTarget from the request, applying
// snapshot fields from the linked KpiMetric if available.
func (r *KpiAnnualTargetRequest) ToModel(db *gorm.DB) *KpiAnnualTarget {
	item := &KpiAnnualTarget{
		KpiCode:        r.KpiCode,
		KpiType:        r.KpiType,
		TargetYear:     r.TargetYear,
		PeriodCode:     r.PeriodCode,
		TargetValue:    float64(0),
		TargetType:     r.TargetType,
		TargetBasis:    r.TargetBasis,
		TargetRationale: r.TargetRationale,
		ThresholdMode:  r.ThresholdMode,
		TargetStatus:   "draft",
	}
	if r.TargetStatus != nil && *r.TargetStatus != "" {
		item.TargetStatus = *r.TargetStatus
	}
	if r.TargetValue != nil {
		item.TargetValue = *r.TargetValue
	}
	if r.TargetType == "" {
		item.TargetType = "Period Target"
	}
	if r.TargetBasis == "" {
		item.TargetBasis = "Management Decision"
	}
	if r.ThresholdMode == "" {
		item.ThresholdMode = "Use Global KPI Rules"
	}
	if r.ExcellentThreshold != nil {
		item.ExcellentThreshold = r.ExcellentThreshold
	}
	if r.AchievedThreshold != nil {
		item.AchievedThreshold = r.AchievedThreshold
	}
	if r.WarningThreshold != nil {
		item.WarningThreshold = r.WarningThreshold
	}
	if r.TargetRangeMin != nil {
		item.TargetRangeMin = r.TargetRangeMin
	}
	if r.TargetRangeMax != nil {
		item.TargetRangeMax = r.TargetRangeMax
	}
	if r.PeriodStart != "" {
		if t, err := time.Parse("2006-01-02", r.PeriodStart); err == nil {
			item.PeriodStart = &t
		}
	}
	if r.PeriodEnd != "" {
		if t, err := time.Parse("2006-01-02", r.PeriodEnd); err == nil {
			item.PeriodEnd = &t
		}
	}
	if r.EffectiveFrom != "" {
		if t, err := time.Parse("2006-01-02", r.EffectiveFrom); err == nil {
			item.EffectiveFrom = &t
		}
	}
	if r.EffectiveTo != "" {
		if t, err := time.Parse("2006-01-02", r.EffectiveTo); err == nil {
			item.EffectiveTo = &t
		}
	}
	if r.MetricID != nil && *r.MetricID != "" {
		if mid, err := uuid.Parse(*r.MetricID); err == nil {
			item.MetricID = &mid
			// Snapshot metric configuration fields at target creation time
			var metric KpiMetric
			if err := db.Where("id = ?", mid).First(&metric).Error; err == nil {
				item.CalculationTypeSnapshot = metric.CalculationType
				item.DirectionSnapshot = metric.Direction
				item.UnitSnapshot = metric.Unit
				item.DecimalPrecisionSnapshot = metric.DecimalPrecision
				item.AggregationMethodSnapshot = metric.AggregationMethod
			}
		}
	}
	if len(r.SegmentationValues) > 0 {
		data, _ := json.Marshal(r.SegmentationValues)
		item.SegmentationValues = datatypes.JSON(data)
	}
	// Backward compat: set legacy fields
	item.Year = r.TargetYear
	item.PeriodType = "annual"
	item.PeriodKey = r.PeriodCode

	return item
}

// KpiAnnualTargetResponse is the JSON response shape returned by the API.
// It mirrors the frontend KpiTarget type.
type KpiAnnualTargetResponse struct {
	ID                          uuid.UUID                    `json:"id"`
	KpiCode                     string                       `json:"kpi_code"`
	KpiType                     string                       `json:"kpi_type"`
	MetricID                    *uuid.UUID                   `json:"metric_id"`
	Metric                      *KpiMetricBrief              `json:"metric,omitempty"`
	CalculationTypeSnapshot     string                       `json:"calculation_type_snapshot"`
	DirectionSnapshot           string                       `json:"direction_snapshot"`
	UnitSnapshot                string                       `json:"unit_snapshot"`
	DecimalPrecisionSnapshot    int                          `json:"decimal_precision_snapshot"`
	AggregationMethodSnapshot   string                       `json:"aggregation_method_snapshot"`
	ReportingFrequencySnapshot  string                       `json:"reporting_frequency_snapshot"`
	TargetYear                  int                          `json:"target_year"`
	PeriodCode                  string                       `json:"period_code"`
	PeriodStart                 *time.Time                   `json:"period_start"`
	PeriodEnd                   *time.Time                   `json:"period_end"`
	TargetValue                 float64                      `json:"target_value"`
	TargetType                  string                       `json:"target_type"`
	TargetBasis                 string                       `json:"target_basis"`
	TargetRationale             string                       `json:"target_rationale"`
	ThresholdMode               string                       `json:"threshold_mode"`
	ExcellentThreshold          *float64                     `json:"excellent_threshold"`
	AchievedThreshold           *float64                     `json:"achieved_threshold"`
	WarningThreshold            *float64                     `json:"warning_threshold"`
	TargetRangeMin              *float64                     `json:"target_range_min"`
	TargetRangeMax              *float64                     `json:"target_range_max"`
	SegmentationValues          datatypes.JSON               `json:"segmentation_values"`
	TargetStatus                string                       `json:"target_status"`
	EffectiveFrom               *time.Time                   `json:"effective_from"`
	EffectiveTo                 *time.Time                   `json:"effective_to"`
	ApprovedByID                *uuid.UUID                   `json:"approved_by_id"`
	ApprovedBy                  *UserBriefResponse           `json:"approved_by,omitempty"`
	ApprovedAt                  *time.Time                   `json:"approved_at"`
	SupersedesEntryID           *uuid.UUID                   `json:"supersedes_entry_id"`
	CreatedAt                   time.Time                    `json:"created_at"`
	UpdatedAt                   time.Time                    `json:"updated_at"`
}

func (t *KpiAnnualTarget) ToResponse() KpiAnnualTargetResponse {
	r := KpiAnnualTargetResponse{
		ID:                         t.ID,
		KpiCode:                    t.KpiCode,
		KpiType:                    t.KpiType,
		MetricID:                   t.MetricID,
		CalculationTypeSnapshot:    t.CalculationTypeSnapshot,
		DirectionSnapshot:          t.DirectionSnapshot,
		UnitSnapshot:               t.UnitSnapshot,
		DecimalPrecisionSnapshot:   t.DecimalPrecisionSnapshot,
		AggregationMethodSnapshot:  "",
		ReportingFrequencySnapshot: t.ReportingFrequencySnapshot,
		TargetYear:                 t.TargetYear,
		PeriodCode:                 t.PeriodCode,
		PeriodStart:                t.PeriodStart,
		PeriodEnd:                  t.PeriodEnd,
		TargetValue:                t.TargetValue,
		TargetType:                 t.TargetType,
		TargetBasis:                t.TargetBasis,
		TargetRationale:            t.TargetRationale,
		ThresholdMode:              t.ThresholdMode,
		ExcellentThreshold:         t.ExcellentThreshold,
		AchievedThreshold:          t.AchievedThreshold,
		WarningThreshold:           t.WarningThreshold,
		TargetRangeMin:             t.TargetRangeMin,
		TargetRangeMax:             t.TargetRangeMax,
		SegmentationValues:         t.SegmentationValues,
		TargetStatus:               t.TargetStatus,
		EffectiveFrom:              t.EffectiveFrom,
		EffectiveTo:                t.EffectiveTo,
		ApprovedByID:               t.ApprovedByID,
		ApprovedBy:                 ToUserBriefResponse(t.ApprovedBy),
		ApprovedAt:                 t.ApprovedAt,
		SupersedesEntryID:          t.SupersedesEntryID,
		CreatedAt:                  t.CreatedAt,
		UpdatedAt:                  t.UpdatedAt,
	}
	if t.Metric != nil {
		r.Metric = t.Metric.ToBrief()
	}
	return r
}

type KpiMetricBrief struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (m *KpiMetric) ToBrief() *KpiMetricBrief {
	if m == nil {
		return nil
	}
	return &KpiMetricBrief{ID: m.ID.String(), Name: m.Name}
}

// ──────────────────────────────────────────────────────────
// KpiPerformance — quarterly actuals for any KPI type
// ──────────────────────────────────────────────────────────

type KpiPerformance struct {
	ID      uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	KpiCode string    `gorm:"size:50;not null;index" json:"kpi_code"`
	KpiType string    `gorm:"size:20;not null" json:"kpi_type"`
	Year    int       `gorm:"not null" json:"year"`
	Quarter int       `gorm:"not null" json:"quarter"`
	// TargetID links back to the approved KpiAnnualTarget at submission time (REL-18)
	TargetID           *uuid.UUID     `gorm:"type:uuid;index" json:"target_id"`
	// PeriodType/PeriodKey let an actual apply to any reporting frequency.
	// Existing rows default to PeriodType="quarter", PeriodKey="{year}-Q{quarter}".
	PeriodType         string         `gorm:"size:20;not null;default:'quarter'" json:"period_type"`
	PeriodKey          string         `gorm:"size:20" json:"period_key"`
	Target             float64        `gorm:"default:0" json:"target"`
	Actual             float64        `gorm:"default:0" json:"actual"`
	AchievementPct     float64        `gorm:"default:0" json:"achievement_pct"`
	TrendDescription   string         `gorm:"type:text" json:"trend_description"`
	Justification      string         `gorm:"type:text" json:"justification"`
	CorrectiveAction   string         `gorm:"type:text" json:"corrective_action"`
	Status             string         `gorm:"size:30;not null;default:'draft'" json:"status"`
	SubmittedByID      *uuid.UUID     `gorm:"type:uuid;index" json:"submitted_by_id"`
	SubmittedBy        *User          `gorm:"foreignKey:SubmittedByID" json:"submitted_by,omitempty"`
	ApprovedByID       *uuid.UUID     `gorm:"type:uuid;index" json:"approved_by_id"`
	ApprovedBy         *User          `gorm:"foreignKey:ApprovedByID" json:"approved_by,omitempty"`
	WorkflowInstanceID *uuid.UUID     `gorm:"type:uuid;index" json:"workflow_instance_id"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	DeletedAt          gorm.DeletedAt `gorm:"index" json:"-"`
}

// AchievementPct is computed explicitly by callers (see services.CalculateAchievement)
// so that the KPI's polarity (ascending/descending) is taken into account. It is not
// recalculated automatically here because these hooks have no access to the KPI
// dictionary's Polarity field.
func (k *KpiPerformance) BeforeCreate(tx *gorm.DB) error {
	if k.ID == uuid.Nil {
		k.ID = uuid.New()
	}
	return nil
}

type KpiPerformanceRequest struct {
	KpiCode          string  `json:"kpi_code" validate:"required,max=50"`
	KpiType          string  `json:"kpi_type" validate:"required,oneof=strategic operational award"`
	Year             int     `json:"year" validate:"required,min=2020,max=2040"`
	Quarter          int     `json:"quarter" validate:"required,min=1,max=4"`
	PeriodType       string  `json:"period_type" validate:"omitempty,oneof=month quarter semi_annual annual custom"`
	PeriodKey        string  `json:"period_key" validate:"omitempty,max=20"`
	Target           float64 `json:"target"`
	Actual           float64 `json:"actual"`
	TrendDescription string  `json:"trend_description"`
	Justification    string  `json:"justification"`
	CorrectiveAction string  `json:"corrective_action"`
}

type KpiPerformanceTransitionRequest struct {
	TransitionID string `json:"transition_id" validate:"required,uuid"`
	Comment      string `json:"comment"`
}

type KpiPerformanceResponse struct {
	ID               uuid.UUID          `json:"id"`
	KpiCode          string             `json:"kpi_code"`
	KpiType          string             `json:"kpi_type"`
	Year             int                `json:"year"`
	Quarter          int                `json:"quarter"`
	PeriodType       string             `json:"period_type"`
	PeriodKey        string             `json:"period_key"`
	TargetID         *uuid.UUID         `json:"target_id"`
	Target           float64            `json:"target"`
	Actual           float64            `json:"actual"`
	AchievementPct   float64            `json:"achievement_pct"`
	TrendDescription string             `json:"trend_description"`
	Justification    string             `json:"justification"`
	CorrectiveAction string             `json:"corrective_action"`
	Status           string             `json:"status"`
	SubmittedByID    *uuid.UUID         `json:"submitted_by_id"`
	SubmittedBy      *UserBriefResponse `json:"submitted_by,omitempty"`
	ApprovedByID     *uuid.UUID         `json:"approved_by_id"`
	ApprovedBy       *UserBriefResponse `json:"approved_by,omitempty"`
	CreatedAt        time.Time          `json:"created_at"`
	UpdatedAt        time.Time          `json:"updated_at"`
}

func (k *KpiPerformance) ToResponse() KpiPerformanceResponse {
	return KpiPerformanceResponse{
		ID:               k.ID,
		KpiCode:          k.KpiCode,
		KpiType:          k.KpiType,
		Year:             k.Year,
		Quarter:          k.Quarter,
		PeriodType:       k.PeriodType,
		PeriodKey:        k.PeriodKey,
		TargetID:         k.TargetID,
		Target:           k.Target,
		Actual:           k.Actual,
		AchievementPct:   k.AchievementPct,
		TrendDescription: k.TrendDescription,
		Justification:    k.Justification,
		CorrectiveAction: k.CorrectiveAction,
		Status:           k.Status,
		SubmittedByID:    k.SubmittedByID,
		SubmittedBy:      ToUserBriefResponse(k.SubmittedBy),
		ApprovedByID:     k.ApprovedByID,
		ApprovedBy:       ToUserBriefResponse(k.ApprovedBy),
		CreatedAt:        k.CreatedAt,
		UpdatedAt:        k.UpdatedAt,
	}
}

// ──────────────────────────────────────────────────────────
// KpiBenchmark — external comparison per KPI per year
// ──────────────────────────────────────────────────────────

type KpiBenchmark struct {
	ID                   uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	KpiCode              string         `gorm:"size:50;not null;index" json:"kpi_code"`
	KpiType              string         `gorm:"size:20;not null" json:"kpi_type"`
	Year                 int            `gorm:"not null" json:"year"`
	Quarter              int            `gorm:"default:0" json:"quarter"`
	Zone                 string         `gorm:"size:100;default:''" json:"zone"`
	DepartmentID         *uuid.UUID     `gorm:"type:uuid;index" json:"department_id"`
	Department           *Department    `gorm:"foreignKey:DepartmentID" json:"department,omitempty"`
	BenchmarkEntity      string         `gorm:"size:255;not null" json:"benchmark_entity"`
	InternalAchievement  float64        `gorm:"default:0" json:"internal_achievement"`
	BenchmarkAchievement float64        `gorm:"default:0" json:"benchmark_achievement"`
	Variance             float64        `gorm:"-" json:"variance"`
	Notes                string         `gorm:"type:text" json:"notes"`
	CreatedAt            time.Time      `json:"created_at"`
	UpdatedAt            time.Time      `json:"updated_at"`
	DeletedAt            gorm.DeletedAt `gorm:"index" json:"-"`
}

func (b *KpiBenchmark) BeforeCreate(tx *gorm.DB) error {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	return nil
}

type KpiBenchmarkRequest struct {
	KpiCode              string  `json:"kpi_code" validate:"required,max=50"`
	KpiType              string  `json:"kpi_type" validate:"required,oneof=strategic operational award"`
	Year                 int     `json:"year" validate:"required,min=2020,max=2040"`
	Quarter              int     `json:"quarter"`
	Zone                 string  `json:"zone"`
	DepartmentID         *string `json:"department_id"`
	BenchmarkEntity      string  `json:"benchmark_entity" validate:"required,max=255"`
	InternalAchievement  float64 `json:"internal_achievement"`
	BenchmarkAchievement float64 `json:"benchmark_achievement"`
	Notes                string  `json:"notes"`
}

type KpiBenchmarkResponse struct {
	ID                   uuid.UUID                `json:"id"`
	KpiCode              string                   `json:"kpi_code"`
	KpiType              string                   `json:"kpi_type"`
	Year                 int                      `json:"year"`
	Quarter              int                      `json:"quarter"`
	Zone                 string                   `json:"zone"`
	DepartmentID         *uuid.UUID               `json:"department_id"`
	Department           *DepartmentBriefResponse `json:"department,omitempty"`
	BenchmarkEntity      string                   `json:"benchmark_entity"`
	InternalAchievement  float64                  `json:"internal_achievement"`
	BenchmarkAchievement float64                  `json:"benchmark_achievement"`
	Variance             float64                  `json:"variance"`
	Notes                string                   `json:"notes"`
	CreatedAt            time.Time                `json:"created_at"`
}

func (b *KpiBenchmark) ToBenchmarkResponse() KpiBenchmarkResponse {
	v := b.InternalAchievement - b.BenchmarkAchievement
	return KpiBenchmarkResponse{
		ID:                   b.ID,
		KpiCode:              b.KpiCode,
		KpiType:              b.KpiType,
		Year:                 b.Year,
		Quarter:              b.Quarter,
		Zone:                 b.Zone,
		DepartmentID:         b.DepartmentID,
		Department:           ToDepartmentBriefResponse(b.Department),
		BenchmarkEntity:      b.BenchmarkEntity,
		InternalAchievement:  b.InternalAchievement,
		BenchmarkAchievement: b.BenchmarkAchievement,
		Variance:             v,
		Notes:                b.Notes,
		CreatedAt:            b.CreatedAt,
	}
}

// ──────────────────────────────────────────────────────────
// KpiSegmentation — segment-level breakdown per KPI
// ──────────────────────────────────────────────────────────

type KpiSegmentation struct {
	ID             uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	KpiCode        string         `gorm:"size:50;not null;index" json:"kpi_code"`
	KpiType        string         `gorm:"size:20;not null" json:"kpi_type"`
	Year           int            `gorm:"not null" json:"year"`
	Quarter        int            `gorm:"not null" json:"quarter"`
	DimensionName  string         `gorm:"size:100;not null" json:"dimension_name"`
	SegmentName    string         `gorm:"size:255;not null" json:"segment_name"`
	Target         float64        `gorm:"default:0" json:"target"`
	Achievement    float64        `gorm:"default:0" json:"achievement"`
	AchievementPct float64        `gorm:"-" json:"achievement_pct"`
	DepartmentID   *uuid.UUID     `gorm:"type:uuid;index" json:"department_id"`
	Department     *Department    `gorm:"foreignKey:DepartmentID" json:"department,omitempty"`
	Zone           string         `gorm:"size:100;default:''" json:"zone"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (s *KpiSegmentation) BeforeCreate(tx *gorm.DB) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}

type KpiSegmentationRequest struct {
	KpiCode       string  `json:"kpi_code" validate:"required,max=50"`
	KpiType       string  `json:"kpi_type" validate:"required,oneof=strategic operational award"`
	Year          int     `json:"year" validate:"required,min=2020,max=2040"`
	Quarter       int     `json:"quarter" validate:"required,min=1,max=4"`
	DimensionName string  `json:"dimension_name" validate:"required,max=100"`
	SegmentName   string  `json:"segment_name" validate:"required,max=255"`
	Target        float64 `json:"target"`
	Achievement   float64 `json:"achievement"`
	DepartmentID  *string `json:"department_id"`
	Zone          string  `json:"zone"`
}

type KpiSegmentationResponse struct {
	ID             uuid.UUID                `json:"id"`
	KpiCode        string                   `json:"kpi_code"`
	KpiType        string                   `json:"kpi_type"`
	Year           int                      `json:"year"`
	Quarter        int                      `json:"quarter"`
	DimensionName  string                   `json:"dimension_name"`
	SegmentName    string                   `json:"segment_name"`
	Target         float64                  `json:"target"`
	Achievement    float64                  `json:"achievement"`
	AchievementPct float64                  `json:"achievement_pct"`
	DepartmentID   *uuid.UUID               `json:"department_id"`
	Department     *DepartmentBriefResponse `json:"department,omitempty"`
	Zone           string                   `json:"zone"`
	CreatedAt      time.Time                `json:"created_at"`
}

func (s *KpiSegmentation) ToSegmentationResponse() KpiSegmentationResponse {
	pct := float64(0)
	if s.Target != 0 {
		pct = (s.Achievement / s.Target) * 100
	}
	return KpiSegmentationResponse{
		ID:             s.ID,
		KpiCode:        s.KpiCode,
		KpiType:        s.KpiType,
		Year:           s.Year,
		Quarter:        s.Quarter,
		DimensionName:  s.DimensionName,
		SegmentName:    s.SegmentName,
		Target:         s.Target,
		Achievement:    s.Achievement,
		AchievementPct: pct,
		DepartmentID:   s.DepartmentID,
		Department:     ToDepartmentBriefResponse(s.Department),
		Zone:           s.Zone,
		CreatedAt:      s.CreatedAt,
	}
}

// ──────────────────────────────────────────────────────────
// KpiPerformanceEvidence — supporting documentation (link + note) attached to
// a performance entry. Cannot be removed once the entry is approved (see
// KpiPerformanceHandler.DeletePerformanceEvidence).
// ──────────────────────────────────────────────────────────

type KpiPerformanceEvidence struct {
	ID               uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	KpiPerformanceID uuid.UUID      `gorm:"type:uuid;not null;index" json:"kpi_performance_id"`
	Description      string         `gorm:"type:text;not null" json:"description"`
	FileURL          string         `gorm:"size:500" json:"file_url"`
	UploadedByID     uuid.UUID      `gorm:"type:uuid;not null" json:"uploaded_by_id"`
	UploadedBy       *User          `gorm:"foreignKey:UploadedByID" json:"uploaded_by,omitempty"`
	CreatedAt        time.Time      `json:"created_at"`
	DeletedAt        gorm.DeletedAt `gorm:"index" json:"-"`
}

func (e *KpiPerformanceEvidence) BeforeCreate(tx *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}

type KpiPerformanceEvidenceRequest struct {
	Description string `json:"description" validate:"required"`
	FileURL     string `json:"file_url"`
}

type KpiPerformanceEvidenceResponse struct {
	ID               uuid.UUID          `json:"id"`
	KpiPerformanceID uuid.UUID          `json:"kpi_performance_id"`
	Description      string             `json:"description"`
	FileURL          string             `json:"file_url"`
	UploadedByID     uuid.UUID          `json:"uploaded_by_id"`
	UploadedBy       *UserBriefResponse `json:"uploaded_by,omitempty"`
	CreatedAt        time.Time          `json:"created_at"`
}

func (e *KpiPerformanceEvidence) ToResponse() KpiPerformanceEvidenceResponse {
	return KpiPerformanceEvidenceResponse{
		ID:               e.ID,
		KpiPerformanceID: e.KpiPerformanceID,
		Description:      e.Description,
		FileURL:          e.FileURL,
		UploadedByID:     e.UploadedByID,
		UploadedBy:       ToUserBriefResponse(e.UploadedBy),
		CreatedAt:        e.CreatedAt,
	}
}

// KpiPerformanceUpdateRequest carries the editable fields of an existing
// performance entry. Locking rules (approved entries are immutable to
// non-admins) are enforced in the handler, not here.
type KpiPerformanceUpdateRequest struct {
	Target           float64 `json:"target"`
	Actual           float64 `json:"actual"`
	TrendDescription string  `json:"trend_description"`
	Justification    string  `json:"justification"`
	CorrectiveAction string  `json:"corrective_action"`
}
