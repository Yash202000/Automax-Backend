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
	ID      uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	KpiCode string    `gorm:"size:50;not null;index:idx_kpi_target_period_unique,unique" json:"kpi_code"`
	KpiType string    `gorm:"size:20;not null;index:idx_kpi_target_period_unique,unique" json:"kpi_type"`
	Year    int       `gorm:"not null;index" json:"year"`
	// PeriodType/PeriodKey let a target apply to any reporting frequency
	// (month/quarter/semi_annual/annual/custom), not just a calendar year.
	// Existing rows default to PeriodType="annual", PeriodKey=Year as string.
	PeriodType string `gorm:"size:20;not null;default:'annual';index:idx_kpi_target_period_unique,unique" json:"period_type"`
	// PeriodKey is not DB-NOT-NULL so AutoMigrate can add the column to a
	// table with existing rows; MigrateKpiPeriodBackfill fills it in right
	// after, and the application layer (SetTarget) always requires it.
	PeriodKey   string         `gorm:"size:20;index:idx_kpi_target_period_unique,unique" json:"period_key"`
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
	PeriodType  string  `json:"period_type" validate:"omitempty,oneof=month quarter semi_annual annual custom"`
	PeriodKey   string  `json:"period_key" validate:"omitempty,max=20"`
	TargetValue float64 `json:"target_value" validate:"required"`
}

type KpiAnnualTargetResponse struct {
	ID          uuid.UUID `json:"id"`
	KpiCode     string    `json:"kpi_code"`
	KpiType     string    `json:"kpi_type"`
	Year        int       `json:"year"`
	PeriodType  string    `json:"period_type"`
	PeriodKey   string    `json:"period_key"`
	TargetValue float64   `json:"target_value"`
	CreatedAt   time.Time `json:"created_at"`
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
