package models

import (
	"time"

	"github.com/google/uuid"
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
// KpiAnnualTarget — one row per KPI per year
// ──────────────────────────────────────────────────────────

type KpiAnnualTarget struct {
	ID          uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	KpiCode     string         `gorm:"size:50;not null;index:idx_kpi_target_unique" json:"kpi_code"`
	KpiType     string         `gorm:"size:20;not null;index:idx_kpi_target_unique" json:"kpi_type"`
	Year        int            `gorm:"not null;index:idx_kpi_target_unique" json:"year"`
	TargetValue float64        `gorm:"not null" json:"target_value"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (t *KpiAnnualTarget) BeforeCreate(tx *gorm.DB) error {
	if t.ID == uuid.Nil {
		t.ID = uuid.New()
	}
	return nil
}

type KpiAnnualTargetRequest struct {
	KpiCode     string  `json:"kpi_code" validate:"required,max=50"`
	KpiType     string  `json:"kpi_type" validate:"required,oneof=strategic operational award"`
	Year        int     `json:"year" validate:"required,min=2020,max=2040"`
	TargetValue float64 `json:"target_value" validate:"required"`
}

type KpiAnnualTargetResponse struct {
	ID          uuid.UUID `json:"id"`
	KpiCode     string    `json:"kpi_code"`
	KpiType     string    `json:"kpi_type"`
	Year        int       `json:"year"`
	TargetValue float64   `json:"target_value"`
	CreatedAt   time.Time `json:"created_at"`
}

// ──────────────────────────────────────────────────────────
// KpiPerformance — quarterly actuals for any KPI type
// ──────────────────────────────────────────────────────────

type KpiPerformance struct {
	ID                uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	KpiCode           string         `gorm:"size:50;not null;index" json:"kpi_code"`
	KpiType           string         `gorm:"size:20;not null" json:"kpi_type"`
	Year              int            `gorm:"not null" json:"year"`
	Quarter           int            `gorm:"not null" json:"quarter"`
	Target            float64        `gorm:"default:0" json:"target"`
	Actual            float64        `gorm:"default:0" json:"actual"`
	AchievementPct    float64        `gorm:"default:0" json:"achievement_pct"`
	TrendDescription  string         `gorm:"type:text" json:"trend_description"`
	Justification     string         `gorm:"type:text" json:"justification"`
	CorrectiveAction  string         `gorm:"type:text" json:"corrective_action"`
	Status            string         `gorm:"size:30;not null;default:'draft'" json:"status"`
	SubmittedByID     *uuid.UUID     `gorm:"type:uuid;index" json:"submitted_by_id"`
	SubmittedBy       *User          `gorm:"foreignKey:SubmittedByID" json:"submitted_by,omitempty"`
	ApprovedByID      *uuid.UUID     `gorm:"type:uuid;index" json:"approved_by_id"`
	ApprovedBy        *User          `gorm:"foreignKey:ApprovedByID" json:"approved_by,omitempty"`
	WorkflowInstanceID *uuid.UUID    `gorm:"type:uuid;index" json:"workflow_instance_id"`
	CreatedAt         time.Time      `json:"created_at"`
	UpdatedAt         time.Time      `json:"updated_at"`
	DeletedAt         gorm.DeletedAt `gorm:"index" json:"-"`
}

func (k *KpiPerformance) BeforeCreate(tx *gorm.DB) error {
	if k.ID == uuid.Nil {
		k.ID = uuid.New()
	}
	if k.Target != 0 {
		k.AchievementPct = (k.Actual / k.Target) * 100
	}
	return nil
}

func (k *KpiPerformance) BeforeUpdate(tx *gorm.DB) error {
	if k.Target != 0 {
		k.AchievementPct = (k.Actual / k.Target) * 100
	}
	return nil
}

type KpiPerformanceRequest struct {
	KpiCode          string  `json:"kpi_code" validate:"required,max=50"`
	KpiType          string  `json:"kpi_type" validate:"required,oneof=strategic operational award"`
	Year             int     `json:"year" validate:"required,min=2020,max=2040"`
	Quarter          int     `json:"quarter" validate:"required,min=1,max=4"`
	Target           float64 `json:"target"`
	Actual           float64 `json:"actual"`
	TrendDescription string  `json:"trend_description"`
	Justification    string  `json:"justification"`
	CorrectiveAction string  `json:"corrective_action"`
}

type KpiPerformanceTransitionRequest struct {
	Action  string `json:"action" validate:"required,oneof=submit review approve reject publish"`
	Comment string `json:"comment"`
}

type KpiPerformanceResponse struct {
	ID               uuid.UUID          `json:"id"`
	KpiCode          string             `json:"kpi_code"`
	KpiType          string             `json:"kpi_type"`
	Year             int                `json:"year"`
	Quarter          int                `json:"quarter"`
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
	BenchmarkEntity      string         `gorm:"size:255;not null" json:"benchmark_entity"`
	InternalAchievement  float64        `gorm:"default:0" json:"internal_achievement"`
	BenchmarkAchievement float64        `gorm:"default:0" json:"benchmark_achievement"`
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
	BenchmarkEntity      string  `json:"benchmark_entity" validate:"required,max=255"`
	InternalAchievement  float64 `json:"internal_achievement"`
	BenchmarkAchievement float64 `json:"benchmark_achievement"`
	Notes                string  `json:"notes"`
}

type KpiBenchmarkResponse struct {
	ID                   uuid.UUID `json:"id"`
	KpiCode              string    `json:"kpi_code"`
	KpiType              string    `json:"kpi_type"`
	Year                 int       `json:"year"`
	BenchmarkEntity      string    `json:"benchmark_entity"`
	InternalAchievement  float64   `json:"internal_achievement"`
	BenchmarkAchievement float64   `json:"benchmark_achievement"`
	Notes                string    `json:"notes"`
	CreatedAt            time.Time `json:"created_at"`
}

// ──────────────────────────────────────────────────────────
// KpiSegmentation — segment-level breakdown per KPI
// ──────────────────────────────────────────────────────────

type KpiSegmentation struct {
	ID            uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	KpiCode       string         `gorm:"size:50;not null;index" json:"kpi_code"`
	KpiType       string         `gorm:"size:20;not null" json:"kpi_type"`
	Year          int            `gorm:"not null" json:"year"`
	Quarter       int            `gorm:"not null" json:"quarter"`
	DimensionName string         `gorm:"size:100;not null" json:"dimension_name"`
	SegmentName   string         `gorm:"size:255;not null" json:"segment_name"`
	Achievement   float64        `gorm:"default:0" json:"achievement"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
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
	Achievement   float64 `json:"achievement"`
}

type KpiSegmentationResponse struct {
	ID            uuid.UUID `json:"id"`
	KpiCode       string    `json:"kpi_code"`
	KpiType       string    `json:"kpi_type"`
	Year          int       `json:"year"`
	Quarter       int       `json:"quarter"`
	DimensionName string    `json:"dimension_name"`
	SegmentName   string    `json:"segment_name"`
	Achievement   float64   `json:"achievement"`
	CreatedAt     time.Time `json:"created_at"`
}
