package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// KpiID + KpiType together identify a single KPI dictionary row across the
// three separate tables (StrategicKPI/OperationalKPI/AwardKPI) — mirrors the
// (kpi_code, kpi_type) composite key already used by KpiPerformance/
// KpiAnnualTarget, but keyed on the dictionary row's own UUID since these
// engagement features attach to one specific KPI definition, not a period.

// ─── KPI Metrics ────────────────────────────────────────────────────────────
// Simplified counterpart to Goal's GoalMetric — direct value updates, no
// workflow-gated approval (KPI actual/target tracking already goes through
// the KpiPerformance approval workflow; this covers supplementary
// sub-metrics/breakdowns the KPI owner wants to track alongside it).

type KpiMetric struct {
	ID                       uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	KpiID                    uuid.UUID      `gorm:"type:uuid;index;not null" json:"kpi_id"`
	KpiType                  string         `gorm:"size:20;index;not null" json:"kpi_type"`
	Name                     string         `gorm:"size:255;not null" json:"name"`
	MetricCode               string         `gorm:"size:50;index" json:"metric_code"`
	MetricDescription        string         `gorm:"type:text" json:"metric_description"`
	MetricStatus             string         `gorm:"size:30;not null;default:'Active'" json:"metric_status"`
	DisplayOrder             int            `gorm:"default:0" json:"display_order"`
	MetricType               string         `gorm:"size:20;not null;default:'Numeric'" json:"metric_type"`
	Unit                     string         `gorm:"size:50" json:"unit"`
	CustomUnitLabel          string         `gorm:"size:255" json:"custom_unit_label"`
	BaselineValue            float64        `gorm:"default:0" json:"baseline_value"`
	CurrentValue             float64        `gorm:"default:0" json:"current_value"`
	// TargetValue is legacy/unused: achievement now only ever uses the
	// current period's approved KpiAnnualTarget (see EffectiveTargetValue
	// below) — a single timeless number here couldn't represent a target
	// that's meant to progress period over period, and silently substituting
	// it when no period target existed yet was misleading, not a sensible
	// fallback. Column kept (not dropped) to avoid a destructive migration;
	// it's no longer collected via the Metric Configuration form or read by
	// any calculation/display.
	TargetValue              float64        `gorm:"default:0" json:"-"`
	Weight                   float64        `gorm:"default:1.0" json:"weight"`
	Formula                  string         `gorm:"type:text" json:"formula"`
	CalculationType          string         `gorm:"size:50;default:'Direct Value'" json:"calculation_type"`
	Direction                string         `gorm:"size:50;default:'Higher is Better'" json:"direction"`
	DecimalPrecision         int            `gorm:"default:0" json:"decimal_precision"`
	AggregationMethod        string         `gorm:"size:50;default:'Sum'" json:"aggregation_method"`
	ReportingFrequency       string         `gorm:"size:30" json:"reporting_frequency"`
	NumeratorLabel           string         `gorm:"size:255" json:"numerator_label"`
	NumeratorVariableCode    string         `gorm:"size:100" json:"numerator_variable_code"`
	DenominatorLabel         string         `gorm:"size:255" json:"denominator_label"`
	DenominatorVariableCode  string         `gorm:"size:100" json:"denominator_variable_code"`
	DirectActualLabel        string         `gorm:"size:255" json:"direct_actual_label"`
	AllowManualActualOverride bool          `gorm:"default:false" json:"allow_manual_actual_override"`
	AdvancedFormulaEnabled   bool           `gorm:"default:false" json:"advanced_formula_enabled"`
	FormulaCode              string         `gorm:"size:100" json:"formula_code"`
	DivideByZeroHandling     string         `gorm:"size:50;default:'Block Submission'" json:"divide_by_zero_handling"`
	RoundingRule             string         `gorm:"size:50;default:'Standard Round'" json:"rounding_rule"`
	CalculationTraceRequired bool           `gorm:"default:true" json:"calculation_trace_required"`
	MetricOwnerID            *uuid.UUID     `gorm:"type:uuid;index" json:"metric_owner_id"`
	MetricOwner              *User          `gorm:"foreignKey:MetricOwnerID" json:"metric_owner,omitempty"`
	DataSource               string         `gorm:"size:100" json:"data_source"`
	EvidenceRequired         bool           `gorm:"default:false" json:"evidence_required"`
	StartDate                *time.Time     `json:"start_date"`
	DueDate                  *time.Time     `json:"due_date"`
	CreatedByID              uuid.UUID      `gorm:"type:uuid;not null" json:"created_by_id"`
	CreatedBy                *User          `gorm:"foreignKey:CreatedByID" json:"created_by,omitempty"`
	CreatedAt                time.Time      `json:"created_at"`
	UpdatedAt                time.Time      `json:"updated_at"`
	DeletedAt                gorm.DeletedAt `gorm:"index" json:"-"`

	// EffectiveTargetValue/Period are computed, not stored — the current
	// reporting period's APPROVED KpiAnnualTarget for this metric, if one
	// exists. Nil means no approved target exists for the current period —
	// callers should show that honestly ("No target set for this period"),
	// not substitute an unrelated static number. Populated by handlers via
	// services.GetEffectiveTarget so the Metric Card's Target tile shows the
	// same number an entry submitted right now would actually be measured
	// against.
	EffectiveTargetValue  *float64 `gorm:"-" json:"effective_target_value,omitempty"`
	EffectiveTargetPeriod string   `gorm:"-" json:"effective_target_period,omitempty"`
}

func (m *KpiMetric) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}

type KpiMetricRequest struct {
	Name                     string     `json:"name" validate:"required"`
	MetricCode               string     `json:"metric_code"`
	MetricDescription        string     `json:"metric_description"`
	MetricStatus             string     `json:"metric_status"`
	DisplayOrder             int        `json:"display_order"`
	MetricType               string     `json:"metric_type"`
	Unit                     string     `json:"unit"`
	CustomUnitLabel          string     `json:"custom_unit_label"`
	BaselineValue            float64    `json:"baseline_value"`
	Weight                   float64    `json:"weight"`
	Formula                  string     `json:"formula"`
	CalculationType          string     `json:"calculation_type"`
	Direction                string     `json:"direction"`
	DecimalPrecision         int        `json:"decimal_precision"`
	AggregationMethod        string     `json:"aggregation_method"`
	ReportingFrequency       string     `json:"reporting_frequency"`
	NumeratorLabel           string     `json:"numerator_label"`
	NumeratorVariableCode    string     `json:"numerator_variable_code"`
	DenominatorLabel         string     `json:"denominator_label"`
	DenominatorVariableCode  string     `json:"denominator_variable_code"`
	DirectActualLabel        string     `json:"direct_actual_label"`
	AllowManualActualOverride bool     `json:"allow_manual_actual_override"`
	AdvancedFormulaEnabled   bool       `json:"advanced_formula_enabled"`
	FormulaCode              string     `json:"formula_code"`
	DivideByZeroHandling     string     `json:"divide_by_zero_handling"`
	RoundingRule             string     `json:"rounding_rule"`
	CalculationTraceRequired bool       `json:"calculation_trace_required"`
	MetricOwnerID            *string    `json:"metric_owner_id"`
	DataSource               string     `json:"data_source"`
	EvidenceRequired         bool       `json:"evidence_required"`
	StartDate                *time.Time `json:"start_date"`
	DueDate                  *time.Time `json:"due_date"`
}

type KpiMetricValueRequest struct {
	Value float64 `json:"value"`
}

// ─── KPI Evidence ───────────────────────────────────────────────────────────
// Attached to the KPI dictionary entry itself (not a specific performance
// period — see KpiPerformanceEvidence for that), mirroring Goal's Evidence
// (Title/EvidenceType/Description-as-comment/optional Metric link + a real
// uploaded file), minus Goal's workflow-gated approval.

type KpiEvidence struct {
	ID           uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	KpiID        uuid.UUID `gorm:"type:uuid;index;not null" json:"kpi_id"`
	KpiType      string    `gorm:"size:20;index;not null" json:"kpi_type"`
	Title        string    `gorm:"size:255;not null" json:"title"`
	EvidenceType string    `gorm:"size:50;not null;default:'Report'" json:"evidence_type"`
	Description  string    `gorm:"type:text" json:"description"`
	// MetricID optionally ties this evidence to one of the KPI's metrics
	// (e.g. proof for a specific sub-metric's actual value); nil means it's
	// evidence for the KPI as a whole.
	MetricID *uuid.UUID `gorm:"type:uuid;index" json:"metric_id"`
	Metric   *KpiMetric `gorm:"foreignKey:MetricID" json:"metric,omitempty"`
	// FileURL holds either a storage object key (files uploaded via
	// POST /kpi/:type/:id/attachment, downloaded via the dedicated download
	// route) or, for evidence created the legacy way, a plain external URL.
	// FileName/FileSize/MimeType are only populated for real uploads.
	FileURL      string         `gorm:"size:500" json:"file_url"`
	FileName     string         `gorm:"size:255" json:"file_name"`
	FileSize     int64          `json:"file_size"`
	MimeType     string         `gorm:"size:100" json:"mime_type"`
	UploadedByID uuid.UUID      `gorm:"type:uuid;not null" json:"uploaded_by_id"`
	UploadedBy   *User          `gorm:"foreignKey:UploadedByID" json:"uploaded_by,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index" json:"-"`
}

func (e *KpiEvidence) BeforeCreate(tx *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}

type KpiEvidenceRequest struct {
	Title        string     `json:"title" validate:"required"`
	EvidenceType string     `json:"evidence_type"`
	Description  string     `json:"description"`
	MetricID     *uuid.UUID `json:"metric_id"`
	FileURL      string     `json:"file_url"`
	FileName     string     `json:"file_name"`
	FileSize     int64      `json:"file_size"`
	MimeType     string     `json:"mime_type"`
}

// ─── KPI Collaborators ──────────────────────────────────────────────────────

const (
	KpiCollaboratorRoleCollaborator = "collaborator"
	KpiCollaboratorRoleReviewer     = "reviewer"
)

type KpiCollaborator struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	KpiID     uuid.UUID `gorm:"type:uuid;index;not null;uniqueIndex:idx_kpi_collab_user" json:"kpi_id"`
	KpiType   string    `gorm:"size:20;index;not null" json:"kpi_type"`
	UserID    uuid.UUID `gorm:"type:uuid;index;not null;uniqueIndex:idx_kpi_collab_user" json:"user_id"`
	User      *User     `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Role      string    `gorm:"size:50;not null;default:'collaborator'" json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

func (c *KpiCollaborator) BeforeCreate(tx *gorm.DB) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return nil
}

type KpiCollaboratorAddRequest struct {
	UserID uuid.UUID `json:"user_id" validate:"required"`
	Role   string    `json:"role"`
}

// ─── KPI Check-ins ──────────────────────────────────────────────────────────

const (
	KpiCheckInStatusOnTrack = "on_track"
	KpiCheckInStatusAtRisk  = "at_risk"
	KpiCheckInStatusBehind  = "behind"
	KpiCheckInStatusBlocked = "blocked"
)

func IsValidKpiCheckInStatus(s string) bool {
	switch s {
	case KpiCheckInStatusOnTrack, KpiCheckInStatusAtRisk, KpiCheckInStatusBehind, KpiCheckInStatusBlocked:
		return true
	}
	return false
}

type KpiCheckIn struct {
	ID        uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	KpiID     uuid.UUID      `gorm:"type:uuid;index;not null" json:"kpi_id"`
	KpiType   string         `gorm:"size:20;index;not null" json:"kpi_type"`
	AuthorID  uuid.UUID      `gorm:"type:uuid;index;not null" json:"author_id"`
	Author    *User          `gorm:"foreignKey:AuthorID" json:"author,omitempty"`
	Status    string         `gorm:"size:20;not null" json:"status"`
	Content   string         `gorm:"type:text;not null" json:"content"`
	CreatedAt time.Time      `json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (c *KpiCheckIn) BeforeCreate(tx *gorm.DB) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return nil
}

type KpiCheckInRequest struct {
	Status  string `json:"status" validate:"required"`
	Content string `json:"content" validate:"required"`
}

// ─── KPI Comments ───────────────────────────────────────────────────────────

type KpiComment struct {
	ID        uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	KpiID     uuid.UUID      `gorm:"type:uuid;index;not null" json:"kpi_id"`
	KpiType   string         `gorm:"size:20;index;not null" json:"kpi_type"`
	AuthorID  uuid.UUID      `gorm:"type:uuid;index;not null" json:"author_id"`
	Author    *User          `gorm:"foreignKey:AuthorID" json:"author,omitempty"`
	Content   string         `gorm:"type:text;not null" json:"content"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (c *KpiComment) BeforeCreate(tx *gorm.DB) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return nil
}

type KpiCommentRequest struct {
	Content string `json:"content" validate:"required,min=1"`
}
