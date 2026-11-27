package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// KpiPerformanceBand defines the RAG (Red/Amber/Green) achievement thresholds used
// to color a KPI's performance. A row with KpiCode == nil is the global default;
// a row with KpiCode set overrides the global default for that specific KPI.
// Green applies when achievement_pct >= GreenMin, Amber when >= AmberMin, else Red.
type KpiPerformanceBand struct {
	ID        uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	KpiCode   *string        `gorm:"size:50;uniqueIndex" json:"kpi_code"`
	GreenMin  float64        `gorm:"not null;default:80" json:"green_min"`
	AmberMin  float64        `gorm:"not null;default:60" json:"amber_min"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (b *KpiPerformanceBand) BeforeCreate(tx *gorm.DB) error {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	return nil
}

// KpiPerformanceBandRequest: pointers so an explicit 0 (a valid amber
// minimum) is distinguishable from a missing value; the handler checks
// presence and 0 <= amber_min < green_min <= 100.
type KpiPerformanceBandRequest struct {
	KpiCode  *string  `json:"kpi_code"`
	GreenMin *float64 `json:"green_min"`
	AmberMin *float64 `json:"amber_min"`
}

type KpiPerformanceBandResponse struct {
	ID        uuid.UUID `json:"id"`
	KpiCode   *string   `json:"kpi_code"`
	GreenMin  float64   `json:"green_min"`
	AmberMin  float64   `json:"amber_min"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (b *KpiPerformanceBand) ToResponse() KpiPerformanceBandResponse {
	return KpiPerformanceBandResponse{
		ID:        b.ID,
		KpiCode:   b.KpiCode,
		GreenMin:  b.GreenMin,
		AmberMin:  b.AmberMin,
		CreatedAt: b.CreatedAt,
		UpdatedAt: b.UpdatedAt,
	}
}

// DefaultKpiPerformanceBand is the hardcoded fallback used only if no global
// default row exists in the database yet. It matches the global status
// defaults (DefaultAchievedMin/DefaultWarningMin) used by EvaluatePerformance.
var DefaultKpiPerformanceBand = KpiPerformanceBandResponse{GreenMin: DefaultAchievedMin, AmberMin: DefaultWarningMin}
