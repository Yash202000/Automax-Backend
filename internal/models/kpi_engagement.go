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
	ID            uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	KpiID         uuid.UUID      `gorm:"type:uuid;index;not null" json:"kpi_id"`
	KpiType       string         `gorm:"size:20;index;not null" json:"kpi_type"`
	Name          string         `gorm:"size:255;not null" json:"name"`
	MetricType    string         `gorm:"size:20;not null;default:'Numeric'" json:"metric_type"`
	Unit          string         `gorm:"size:50" json:"unit"`
	BaselineValue float64        `gorm:"default:0" json:"baseline_value"`
	CurrentValue  float64        `gorm:"default:0" json:"current_value"`
	TargetValue   float64        `gorm:"not null" json:"target_value"`
	Weight        float64        `gorm:"default:1.0" json:"weight"`
	Formula       string         `gorm:"type:text" json:"formula"`
	StartDate     *time.Time     `json:"start_date"`
	DueDate       *time.Time     `json:"due_date"`
	CreatedByID   uuid.UUID      `gorm:"type:uuid;not null" json:"created_by_id"`
	CreatedBy     *User          `gorm:"foreignKey:CreatedByID" json:"created_by,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

func (m *KpiMetric) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}

type KpiMetricRequest struct {
	Name          string     `json:"name" validate:"required"`
	MetricType    string     `json:"metric_type"`
	Unit          string     `json:"unit"`
	BaselineValue float64    `json:"baseline_value"`
	TargetValue   float64    `json:"target_value" validate:"required"`
	Weight        float64    `json:"weight"`
	Formula       string     `json:"formula"`
	StartDate     *time.Time `json:"start_date"`
	DueDate       *time.Time `json:"due_date"`
	// AttachmentTitle/AttachmentFileURL are optional — when FileURL is set on
	// create, a KpiEvidence row is also created so the file shows up under
	// the KPI's Evidence tab as a real, manageable entry.
	AttachmentTitle   string `json:"attachment_title"`
	AttachmentFileURL string `json:"attachment_file_url"`
}

type KpiMetricValueRequest struct {
	Value float64 `json:"value"`
}

// ─── KPI Evidence ───────────────────────────────────────────────────────────
// Attached to the KPI dictionary entry itself (not a specific performance
// period — see KpiPerformanceEvidence for that). Same simple text/URL shape.

type KpiEvidence struct {
	ID           uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	KpiID        uuid.UUID      `gorm:"type:uuid;index;not null" json:"kpi_id"`
	KpiType      string         `gorm:"size:20;index;not null" json:"kpi_type"`
	Title        string         `gorm:"size:255;not null" json:"title"`
	Description  string         `gorm:"type:text" json:"description"`
	FileURL      string         `gorm:"size:500" json:"file_url"`
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
	Title       string `json:"title" validate:"required"`
	Description string `json:"description"`
	FileURL     string `json:"file_url"`
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
