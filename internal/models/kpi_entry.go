package models

import (
	"encoding/json"
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
)

type KpiEntry struct {
	ID                        uuid.UUID              `gorm:"type:uuid;primary_key" json:"id"`
	KpiID                     uuid.UUID              `gorm:"type:uuid;index:idx_kpi_entry_unique,unique;not null" json:"kpi_id"`
	KpiType                   string                 `gorm:"size:20;index:idx_kpi_entry_unique,unique;not null" json:"kpi_type"`
	MetricID                  uuid.UUID              `gorm:"type:uuid;index:idx_kpi_entry_unique,unique;not null" json:"metric_id"`
	Metric                    *KpiMetric             `gorm:"foreignKey:MetricID" json:"metric,omitempty"`
	ReportingYear             int                    `gorm:"not null;index:idx_kpi_entry_unique,unique" json:"reporting_year"`
	PeriodCode                string                 `gorm:"size:50;not null;index:idx_kpi_entry_unique,unique" json:"period_code"`
	PeriodStart               *time.Time             `json:"period_start"`
	PeriodEnd                 *time.Time             `json:"period_end"`
	CalculationTypeSnapshot   string                 `gorm:"size:50" json:"calculation_type_snapshot"`
	DirectionSnapshot         string                 `gorm:"size:50" json:"direction_snapshot"`
	UnitSnapshot              string                 `gorm:"size:50" json:"unit_snapshot"`
	DecimalPrecisionSnapshot  int                    `gorm:"default:0" json:"decimal_precision_snapshot"`
	NumeratorLabelSnapshot    string                 `gorm:"size:255" json:"numerator_label_snapshot"`
	DenominatorLabelSnapshot  string                 `gorm:"size:255" json:"denominator_label_snapshot"`
	AggregationMethodSnapshot string                 `gorm:"size:50" json:"aggregation_method_snapshot"`
	TargetID                  *uuid.UUID             `gorm:"type:uuid;index" json:"target_id"`
	TargetValueSnapshot       *float64               `json:"target_value_snapshot"`
	ThresholdModeSnapshot     string                 `gorm:"size:50" json:"threshold_mode_snapshot"`
	DirectActualValue         *float64               `json:"direct_actual_value"`
	NumeratorValue            *float64               `json:"numerator_value"`
	DenominatorValue          *float64               `json:"denominator_value"`
	ComponentValues           datatypes.JSON         `gorm:"type:jsonb" json:"component_values"`
	ActualValue               float64                `gorm:"default:0" json:"actual_value"`
	ActualCalculationTrace    string                 `gorm:"type:text" json:"actual_calculation_trace"`
	AchievementPercentage     *float64               `json:"achievement_percentage"`
	VarianceValue             *float64               `json:"variance_value"`
	PerformanceStatus         string                 `gorm:"size:50;default:'Not Calculable'" json:"performance_status"`
	AggregatedValue           *float64               `json:"aggregated_value"`
	DataSourceType            string                 `gorm:"size:50;default:'Manual'" json:"data_source_type"`
	SourceReference           string                 `gorm:"size:500" json:"source_reference"`
	DataCutoffDate            *time.Time             `json:"data_cutoff_date"`
	DataQualityStatus         string                 `gorm:"size:50;default:'Not Verified'" json:"data_quality_status"`
	DataQualityNotes          string                 `gorm:"type:text" json:"data_quality_notes"`
	PerformanceCommentary     string                 `gorm:"type:text" json:"performance_commentary"`
	ImprovementAction         string                 `gorm:"type:text" json:"improvement_action"`
	Status                    string                 `gorm:"size:30;not null;default:'draft'" json:"status"`
	SubmittedByID             *uuid.UUID             `gorm:"type:uuid;index" json:"submitted_by_id"`
	SubmittedBy               *User                  `gorm:"foreignKey:SubmittedByID" json:"submitted_by,omitempty"`
	ApprovedByID              *uuid.UUID             `gorm:"type:uuid;index" json:"approved_by_id"`
	ApprovedBy                *User                  `gorm:"foreignKey:ApprovedByID" json:"approved_by,omitempty"`
	WorkflowInstanceID        *uuid.UUID             `gorm:"type:uuid;index" json:"workflow_instance_id"`
	EntryVersion              int                    `gorm:"default:1" json:"entry_version"`
	SupersedesEntryID         *uuid.UUID             `gorm:"type:uuid" json:"supersedes_entry_id"`
	CreatedAt                 time.Time              `json:"created_at"`
	UpdatedAt                 time.Time              `json:"updated_at"`
	DeletedAt                 gorm.DeletedAt         `gorm:"index" json:"-"`
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
	MetricID            string                  `json:"metric_id" validate:"required,uuid"`
	ReportingYear       int                     `json:"reporting_year" validate:"required,min=2020,max=2040"`
	PeriodCode          string                  `json:"period_code" validate:"required,max=50"`
	PeriodStartDate     string                  `json:"period_start_date"`
	PeriodEndDate       string                  `json:"period_end_date"`
	DirectActualValue   *float64                `json:"direct_actual_value"`
	NumeratorValue      *float64                `json:"numerator_value"`
	DenominatorValue    *float64                `json:"denominator_value"`
	ComponentValues     []KpiEntryComponentValue `json:"component_values"`
	DataSourceType      string                  `json:"data_source_type"`
	SourceReference     string                  `json:"source_reference"`
	DataCutoffDate      string                  `json:"data_cutoff_date"`
	DataQualityStatus   string                  `json:"data_quality_status"`
	DataQualityNotes    string                  `json:"data_quality_notes"`
	PerformanceCommentary string                `json:"performance_commentary"`
	ImprovementAction   string                  `json:"improvement_action"`
}

func (r *KpiEntryRequest) ToModel(db *gorm.DB, kpiID uuid.UUID, kpiType string, userID uuid.UUID) *KpiEntry {
	entry := &KpiEntry{
		KpiID:             kpiID,
		KpiType:           kpiType,
		ReportingYear:     r.ReportingYear,
		PeriodCode:        r.PeriodCode,
		DirectActualValue: r.DirectActualValue,
		NumeratorValue:    r.NumeratorValue,
		DenominatorValue:  r.DenominatorValue,
		DataSourceType:    r.DataSourceType,
		SourceReference:   r.SourceReference,
		DataQualityStatus: r.DataQualityStatus,
		DataQualityNotes:  r.DataQualityNotes,
		PerformanceCommentary: r.PerformanceCommentary,
		ImprovementAction: r.ImprovementAction,
		Status:            "draft",
		SubmittedByID:     &userID,
		EntryVersion:      1,
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
	if r.PeriodEndDate != "" {
		if t, err := time.Parse("2006-01-02", r.PeriodEndDate); err == nil {
			entry.PeriodEnd = &t
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
				entry.computeActualValue()
			}
		}
	}

	return entry
}

// computeActualValue calculates the actual_value based on the calculation
// type and the provided input values (direct, numerator/denominator, components).
func (e *KpiEntry) computeActualValue() {
	switch e.CalculationTypeSnapshot {
	case "Direct Value":
		if e.DirectActualValue != nil {
			e.ActualValue = *e.DirectActualValue
		}
	case "Percentage - Ratio":
		if e.DenominatorValue != nil && *e.DenominatorValue != 0 && e.NumeratorValue != nil {
			e.ActualValue = (*e.NumeratorValue / *e.DenominatorValue) * 100
		}
	case "Ratio":
		if e.DenominatorValue != nil && *e.DenominatorValue != 0 && e.NumeratorValue != nil {
			e.ActualValue = *e.NumeratorValue / *e.DenominatorValue
		}
	case "Average":
		if len(e.ComponentValues) > 0 {
			var comps []KpiEntryComponentValue
			if err := json.Unmarshal(e.ComponentValues, &comps); err == nil && len(comps) > 0 {
				var sum float64
				for _, c := range comps {
					sum += c.Value
				}
				e.ActualValue = sum / float64(len(comps))
			}
		}
	case "Sum":
		if len(e.ComponentValues) > 0 {
			var comps []KpiEntryComponentValue
			if err := json.Unmarshal(e.ComponentValues, &comps); err == nil {
				for _, c := range comps {
					e.ActualValue += c.Value
				}
			}
		}
	case "Difference":
		if e.NumeratorValue != nil && e.DenominatorValue != nil {
			e.ActualValue = *e.NumeratorValue - *e.DenominatorValue
		}
	case "Weighted Average":
		if len(e.ComponentValues) > 0 {
			var comps []KpiEntryComponentValue
			if err := json.Unmarshal(e.ComponentValues, &comps); err == nil {
				var totalWeight, weightedSum float64
				for _, c := range comps {
					w := c.Weight
					if w == 0 {
						w = 1
					}
					weightedSum += c.Value * w
					totalWeight += w
				}
				if totalWeight > 0 {
					e.ActualValue = weightedSum / totalWeight
				}
			}
		}
	}
}

// KpiEntryUpdateRequest carries the editable fields of an existing draft
// entry. MetricID/period cannot be changed after creation (that would change
// the entry's identity) — to correct those, delete and re-create instead.
type KpiEntryUpdateRequest struct {
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
}

// ApplyTo mutates an existing entry with the request's editable fields and
// recomputes ActualValue from the (possibly changed) input values, using the
// entry's already-snapshotted CalculationTypeSnapshot.
func (r *KpiEntryUpdateRequest) ApplyTo(e *KpiEntry) {
	e.DirectActualValue = r.DirectActualValue
	e.NumeratorValue = r.NumeratorValue
	e.DenominatorValue = r.DenominatorValue
	if r.ComponentValues != nil {
		data, _ := json.Marshal(r.ComponentValues)
		e.ComponentValues = datatypes.JSON(data)
	}
	if r.DataSourceType != "" {
		e.DataSourceType = r.DataSourceType
	}
	e.SourceReference = r.SourceReference
	if r.DataCutoffDate != "" {
		if t, err := time.Parse("2006-01-02", r.DataCutoffDate); err == nil {
			e.DataCutoffDate = &t
		}
	}
	if r.DataQualityStatus != "" {
		e.DataQualityStatus = r.DataQualityStatus
	}
	e.DataQualityNotes = r.DataQualityNotes
	e.PerformanceCommentary = r.PerformanceCommentary
	e.ImprovementAction = r.ImprovementAction
	e.computeActualValue()
}

// ─── KPI Entry Evidence ──────────────────────────────────────────────────

type KpiEntryEvidence struct {
	ID            uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	EntryID       uuid.UUID      `gorm:"type:uuid;index;not null" json:"entry_id"`
	Entry         *KpiEntry      `gorm:"foreignKey:EntryID" json:"-"`
	Title         string         `gorm:"size:255;not null" json:"title"`
	EvidenceType  string         `gorm:"size:50;not null;default:'Report'" json:"evidence_type"`
	Description   string         `gorm:"type:text" json:"description"`
	FileURL       string         `gorm:"size:500" json:"file_url"`
	FileName      string         `gorm:"size:255" json:"file_name"`
	FileSize      int64          `json:"file_size"`
	MimeType      string         `gorm:"size:100" json:"mime_type"`
	UploadedByID  uuid.UUID      `gorm:"type:uuid;not null" json:"uploaded_by_id"`
	UploadedBy    *User          `gorm:"foreignKey:UploadedByID" json:"uploaded_by,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
}

func (e *KpiEntryEvidence) BeforeCreate(tx *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}
