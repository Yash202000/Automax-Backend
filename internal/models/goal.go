package models

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ──────────────────────────────────────────────────
// Goal Status & Transitions
// ──────────────────────────────────────────────────

const (
	GoalStatusDraft       = "Draft"
	GoalStatusActive      = "Active"
	GoalStatusUnderReview = "Under_Review"
	GoalStatusAchieved    = "Achieved"
	GoalStatusMissed      = "Missed"
	GoalStatusClosed      = "Closed"
)

const (
	GoalPriorityCritical = "Critical"
	GoalPriorityHigh     = "High"
	GoalPriorityMedium   = "Medium"
	GoalPriorityLow      = "Low"
)

var ValidGoalTransitions = map[string][]string{
	GoalStatusDraft:       {GoalStatusActive},
	GoalStatusActive:      {GoalStatusUnderReview},
	GoalStatusUnderReview: {GoalStatusAchieved, GoalStatusMissed, GoalStatusActive},
	GoalStatusAchieved:    {GoalStatusClosed},
	GoalStatusMissed:      {GoalStatusClosed, GoalStatusActive},
	GoalStatusClosed:      {},
}

func IsValidGoalTransition(from, to string) bool {
	allowed, ok := ValidGoalTransitions[from]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == to {
			return true
		}
	}
	return false
}

func IsValidGoalPriority(p string) bool {
	switch p {
	case GoalPriorityCritical, GoalPriorityHigh, GoalPriorityMedium, GoalPriorityLow:
		return true
	}
	return false
}

// ──────────────────────────────────────────────────
// Evidence Status Constants (kept for backward compat / convenience)
// ──────────────────────────────────────────────────

const (
	EvidenceStatusDraft            = "Draft"
	EvidenceStatusSubmitted        = "Submitted"
	EvidenceStatusInReview         = "In_Review"
	EvidenceStatusApproved         = "Approved"
	EvidenceStatusRejected         = "Rejected"
	EvidenceStatusChangesRequested = "Changes_Requested"
)

// ──────────────────────────────────────────────────
// Metric Types
// ──────────────────────────────────────────────────

const (
	MetricTypeNumeric    = "Numeric"
	MetricTypePercentage = "Percentage"
	MetricTypeCurrency   = "Currency"
	MetricTypeBoolean    = "Boolean"
)

func IsValidMetricType(t string) bool {
	switch t {
	case MetricTypeNumeric, MetricTypePercentage, MetricTypeCurrency, MetricTypeBoolean:
		return true
	}
	return false
}

// ──────────────────────────────────────────────────
// Evidence Types
// ──────────────────────────────────────────────────

const (
	EvidenceTypeReport      = "Report"
	EvidenceTypePhoto       = "Photo"
	EvidenceTypeCertificate = "Certificate"
	EvidenceTypeInvoice     = "Invoice"
	EvidenceTypeOther       = "Other"
)

// ──────────────────────────────────────────────────
// Collaborator Roles
// ──────────────────────────────────────────────────

const (
	CollaboratorRoleCollaborator = "collaborator"
	CollaboratorRoleReviewerL1   = "reviewer_l1"
	CollaboratorRoleReviewerL2   = "reviewer_l2"
)

// ════════════════════════════════════════════════════
// DATABASE MODELS
// ════════════════════════════════════════════════════

// Goal is the main goal entity
type Goal struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	Title       string    `gorm:"size:255;not null" json:"title"`
	Description string    `gorm:"type:text" json:"description"`
	// Category is the legacy free-text category (kept for backward compatibility).
	// New goals should use CategoryID to reference the hierarchical Category tree.
	Category          string             `gorm:"size:100" json:"category"`
	CategoryID        *uuid.UUID         `gorm:"type:uuid;index" json:"category_id"`
	CategoryRef       *Category          `gorm:"foreignKey:CategoryID" json:"category_ref,omitempty"`
	Priority          string             `gorm:"size:20;not null;default:'Medium'" json:"priority"`
	Status            string             `gorm:"size:30;not null;default:'Draft'" json:"status"`
	OwnerID           uuid.UUID          `gorm:"type:uuid;index;not null" json:"owner_id"`
	Owner             *User              `gorm:"foreignKey:OwnerID" json:"owner,omitempty"`
	DepartmentID      *uuid.UUID         `gorm:"type:uuid;index" json:"department_id"`
	Department        *Department        `gorm:"foreignKey:DepartmentID" json:"department,omitempty"`
	StartDate         *time.Time         `json:"start_date"`
	TargetDate        *time.Time         `json:"target_date"`
	ReviewDate        *time.Time         `json:"review_date"`
	Progress          float64            `gorm:"default:0" json:"progress"`
	DocumentaFolderID string             `gorm:"size:255" json:"documenta_folder_id"`
	Metadata          string             `gorm:"type:jsonb;default:'{}'" json:"metadata"`
	CreatedByID       uuid.UUID          `gorm:"type:uuid;index" json:"created_by_id"`
	CreatedBy         *User              `gorm:"foreignKey:CreatedByID" json:"created_by,omitempty"`
	ParentGoalID      *uuid.UUID         `gorm:"type:uuid;index" json:"parent_goal_id"`
	ParentGoal        *Goal              `gorm:"foreignKey:ParentGoalID" json:"parent_goal,omitempty"`
	Children          []Goal             `gorm:"foreignKey:ParentGoalID" json:"children,omitempty"`
	Level             int                `gorm:"default:0" json:"level"`
	Path              string             `gorm:"size:1000" json:"path"`
	Collaborators     []GoalCollaborator `gorm:"foreignKey:GoalID" json:"collaborators,omitempty"`
	Metrics           []GoalMetric       `gorm:"foreignKey:GoalID" json:"metrics,omitempty"`
	Evidences         []Evidence         `gorm:"foreignKey:GoalID" json:"evidences,omitempty"`
	CheckIns          []GoalCheckIn      `gorm:"foreignKey:GoalID" json:"check_ins,omitempty"`
	CreatedAt         time.Time          `json:"created_at"`
	UpdatedAt         time.Time          `json:"updated_at"`
	DeletedAt         gorm.DeletedAt     `gorm:"index" json:"-"`
}

func (g *Goal) BeforeCreate(tx *gorm.DB) error {
	if g.ID == uuid.Nil {
		g.ID = uuid.New()
	}
	return nil
}

// GoalMetric defines a measurable metric for a goal
type GoalMetric struct {
	ID            uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	GoalID        uuid.UUID `gorm:"type:uuid;index;not null" json:"goal_id"`
	Goal          *Goal     `gorm:"foreignKey:GoalID" json:"goal,omitempty"`
	Name          string    `gorm:"size:255;not null" json:"name"`
	MetricType    string    `gorm:"size:20;not null" json:"metric_type"`
	Unit          string    `gorm:"size:50" json:"unit"`
	BaselineValue float64   `gorm:"default:0" json:"baseline_value"`
	CurrentValue  float64   `gorm:"default:0" json:"current_value"`
	TargetValue   float64   `gorm:"not null" json:"target_value"`
	Weight        float64   `gorm:"default:1.0" json:"weight"`
	// Formula is an optional expression that computes CurrentValue from sibling metrics.
	// Reference other metrics by name: "${tasks_completed} / ${tasks_total} * 100".
	// When set, UpdateMetricValue ignores the submitted raw value and evaluates the formula instead.
	// Evaluation uses github.com/expr-lang/expr with access restricted to numeric sibling values.
	Formula string `gorm:"type:text" json:"formula"`
	// Workflow engine fields — gates initial visibility of newly-created metrics.
	// Definition edits remain free for owners. Per-value-change approvals are tracked in goal_metric_value_changes.
	WorkflowID     *uuid.UUID     `gorm:"type:uuid;index" json:"workflow_id"`
	CurrentStateID *uuid.UUID     `gorm:"type:uuid;index" json:"current_state_id"`
	CurrentState   *WorkflowState `gorm:"foreignKey:CurrentStateID" json:"current_state,omitempty"`
	AssignedToID   *uuid.UUID     `gorm:"type:uuid;index" json:"assigned_to_id"`
	AssignedTo     *User          `gorm:"foreignKey:AssignedToID" json:"assigned_to,omitempty"`
	Version        int            `gorm:"default:1" json:"version"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (m *GoalMetric) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
	}
	return nil
}

// GoalMetricValueChange is a proposed value change for a metric that must go
// through the metric_value_change approval workflow before being applied.
// On a transition into the terminal-approved state the parent metric's
// CurrentValue is updated and ApplyAt is set.
type GoalMetricValueChange struct {
	ID             uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	MetricID       uuid.UUID      `gorm:"type:uuid;index;not null" json:"metric_id"`
	Metric         *GoalMetric    `gorm:"foreignKey:MetricID" json:"metric,omitempty"`
	ProposedValue  float64        `gorm:"not null" json:"proposed_value"`
	PreviousValue  float64        `gorm:"not null" json:"previous_value"`
	Comment        string         `gorm:"type:text" json:"comment"`
	SubmittedByID  uuid.UUID      `gorm:"type:uuid;index;not null" json:"submitted_by_id"`
	SubmittedBy    *User          `gorm:"foreignKey:SubmittedByID" json:"submitted_by,omitempty"`
	WorkflowID     uuid.UUID      `gorm:"type:uuid;index;not null" json:"workflow_id"`
	CurrentStateID uuid.UUID      `gorm:"type:uuid;index;not null" json:"current_state_id"`
	CurrentState   *WorkflowState `gorm:"foreignKey:CurrentStateID" json:"current_state,omitempty"`
	AssignedToID   *uuid.UUID     `gorm:"type:uuid;index" json:"assigned_to_id"`
	AssignedTo     *User          `gorm:"foreignKey:AssignedToID" json:"assigned_to,omitempty"`
	AppliedAt      *time.Time     `json:"applied_at"`
	Version        int            `gorm:"default:1" json:"version"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (c *GoalMetricValueChange) BeforeCreate(tx *gorm.DB) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return nil
}

// MetricTransitionHistory records each state change for a goal metric.
type MetricTransitionHistory struct {
	ID             uuid.UUID           `gorm:"type:uuid;primary_key" json:"id"`
	MetricID       uuid.UUID           `gorm:"type:uuid;index;not null" json:"metric_id"`
	Metric         *GoalMetric         `gorm:"foreignKey:MetricID" json:"metric,omitempty"`
	TransitionID   *uuid.UUID          `gorm:"type:uuid;index" json:"transition_id"`
	Transition     *WorkflowTransition `gorm:"foreignKey:TransitionID" json:"transition,omitempty"`
	FromStateID    uuid.UUID           `gorm:"type:uuid;not null;index" json:"from_state_id"`
	FromState      *WorkflowState      `gorm:"foreignKey:FromStateID" json:"from_state,omitempty"`
	ToStateID      uuid.UUID           `gorm:"type:uuid;not null;index" json:"to_state_id"`
	ToState        *WorkflowState      `gorm:"foreignKey:ToStateID" json:"to_state,omitempty"`
	PerformedByID  uuid.UUID           `gorm:"type:uuid;not null;index" json:"performed_by_id"`
	PerformedBy    *User               `gorm:"foreignKey:PerformedByID" json:"performed_by,omitempty"`
	Comment        string              `gorm:"type:text" json:"comment"`
	IsSystemAction bool                `gorm:"default:false" json:"is_system_action"`
	TransitionedAt time.Time           `gorm:"index" json:"transitioned_at"`
	CreatedAt      time.Time           `json:"created_at"`
}

func (h *MetricTransitionHistory) BeforeCreate(tx *gorm.DB) error {
	if h.ID == uuid.Nil {
		h.ID = uuid.New()
	}
	return nil
}

// MetricValueChangeTransitionHistory records each state change for a value change.
type MetricValueChangeTransitionHistory struct {
	ID                  uuid.UUID              `gorm:"type:uuid;primary_key" json:"id"`
	MetricValueChangeID uuid.UUID              `gorm:"type:uuid;index;not null" json:"metric_value_change_id"`
	MetricValueChange   *GoalMetricValueChange `gorm:"foreignKey:MetricValueChangeID" json:"metric_value_change,omitempty"`
	TransitionID        *uuid.UUID             `gorm:"type:uuid;index" json:"transition_id"`
	Transition          *WorkflowTransition    `gorm:"foreignKey:TransitionID" json:"transition,omitempty"`
	FromStateID         uuid.UUID              `gorm:"type:uuid;not null;index" json:"from_state_id"`
	FromState           *WorkflowState         `gorm:"foreignKey:FromStateID" json:"from_state,omitempty"`
	ToStateID           uuid.UUID              `gorm:"type:uuid;not null;index" json:"to_state_id"`
	ToState             *WorkflowState         `gorm:"foreignKey:ToStateID" json:"to_state,omitempty"`
	PerformedByID       uuid.UUID              `gorm:"type:uuid;not null;index" json:"performed_by_id"`
	PerformedBy         *User                  `gorm:"foreignKey:PerformedByID" json:"performed_by,omitempty"`
	Comment             string                 `gorm:"type:text" json:"comment"`
	IsSystemAction      bool                   `gorm:"default:false" json:"is_system_action"`
	TransitionedAt      time.Time              `gorm:"index" json:"transitioned_at"`
	CreatedAt           time.Time              `json:"created_at"`
}

func (h *MetricValueChangeTransitionHistory) BeforeCreate(tx *gorm.DB) error {
	if h.ID == uuid.Nil {
		h.ID = uuid.New()
	}
	return nil
}

// MetricHistory tracks value changes over time
type MetricHistory struct {
	ID          uuid.UUID   `gorm:"type:uuid;primary_key" json:"id"`
	MetricID    uuid.UUID   `gorm:"type:uuid;index;not null" json:"metric_id"`
	Metric      *GoalMetric `gorm:"foreignKey:MetricID" json:"metric,omitempty"`
	OldValue    float64     `json:"old_value"`
	NewValue    float64     `json:"new_value"`
	ChangedByID uuid.UUID   `gorm:"type:uuid;index" json:"changed_by_id"`
	ChangedBy   *User       `gorm:"foreignKey:ChangedByID" json:"changed_by,omitempty"`
	Comment     string      `gorm:"size:500" json:"comment"`
	CreatedAt   time.Time   `json:"created_at"`
}

func (h *MetricHistory) BeforeCreate(tx *gorm.DB) error {
	if h.ID == uuid.Nil {
		h.ID = uuid.New()
	}
	return nil
}

// GoalCollaborator links users to goals with a specific role
type GoalCollaborator struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	GoalID    uuid.UUID `gorm:"type:uuid;index;not null;uniqueIndex:idx_goal_user" json:"goal_id"`
	Goal      *Goal     `gorm:"foreignKey:GoalID" json:"goal,omitempty"`
	UserID    uuid.UUID `gorm:"type:uuid;index;not null;uniqueIndex:idx_goal_user" json:"user_id"`
	User      *User     `gorm:"foreignKey:UserID" json:"user,omitempty"`
	Role      string    `gorm:"size:50;not null;default:'collaborator'" json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

func (c *GoalCollaborator) BeforeCreate(tx *gorm.DB) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return nil
}

// ──────────────────────────────────────────────────
// Check-In Status Constants
// ──────────────────────────────────────────────────

const (
	CheckInStatusOnTrack = "on_track"
	CheckInStatusAtRisk  = "at_risk"
	CheckInStatusBehind  = "behind"
	CheckInStatusBlocked = "blocked"
)

func IsValidCheckInStatus(s string) bool {
	switch s {
	case CheckInStatusOnTrack, CheckInStatusAtRisk, CheckInStatusBehind, CheckInStatusBlocked:
		return true
	}
	return false
}

// GoalCheckIn represents a periodic progress update on a goal
type GoalCheckIn struct {
	ID            uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	GoalID        uuid.UUID      `gorm:"type:uuid;index;not null" json:"goal_id"`
	Goal          *Goal          `gorm:"foreignKey:GoalID" json:"goal,omitempty"`
	AuthorID      uuid.UUID      `gorm:"type:uuid;index;not null" json:"author_id"`
	Author        *User          `gorm:"foreignKey:AuthorID" json:"author,omitempty"`
	Status        string         `gorm:"size:20;not null" json:"status"`
	Content       string         `gorm:"type:text;not null" json:"content"`
	ProgressSnap  float64        `json:"progress_snapshot"`
	MetricUpdates string         `gorm:"type:jsonb;default:'[]'" json:"metric_updates"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

func (ci *GoalCheckIn) BeforeCreate(tx *gorm.DB) error {
	if ci.ID == uuid.Nil {
		ci.ID = uuid.New()
	}
	return nil
}

// Evidence is a file/document uploaded as proof of goal progress.
// Workflow state is tracked via CurrentStateID referencing the shared WorkflowState model.
type Evidence struct {
	ID              uuid.UUID   `gorm:"type:uuid;primary_key" json:"id"`
	GoalID          uuid.UUID   `gorm:"type:uuid;index;not null" json:"goal_id"`
	Goal            *Goal       `gorm:"foreignKey:GoalID" json:"goal,omitempty"`
	MetricID        *uuid.UUID  `gorm:"type:uuid;index" json:"metric_id"`
	Metric          *GoalMetric `gorm:"foreignKey:MetricID" json:"metric,omitempty"`
	Title           string      `gorm:"size:255;not null" json:"title"`
	EvidenceType    string      `gorm:"size:50;not null;default:'Other'" json:"evidence_type"`
	Comment         string      `gorm:"type:text;not null" json:"comment"`
	Status          string      `gorm:"size:30;not null;default:'Draft'" json:"status"`
	DocumentaFileID string      `gorm:"size:255" json:"documenta_file_id"`
	FileName        string      `gorm:"size:255" json:"file_name"`
	FileSize        int64       `json:"file_size"`
	MimeType        string      `gorm:"size:100" json:"mime_type"`
	UploadedByID    uuid.UUID   `gorm:"type:uuid;index" json:"uploaded_by_id"`
	UploadedBy      *User       `gorm:"foreignKey:UploadedByID" json:"uploaded_by,omitempty"`
	// Workflow engine fields
	WorkflowID     *uuid.UUID     `gorm:"type:uuid;index" json:"workflow_id"`
	CurrentStateID *uuid.UUID     `gorm:"type:uuid;index" json:"current_state_id"`
	CurrentState   *WorkflowState `gorm:"foreignKey:CurrentStateID" json:"current_state,omitempty"`
	AssignedToID   *uuid.UUID     `gorm:"type:uuid;index" json:"assigned_to_id"`
	AssignedTo     *User          `gorm:"foreignKey:AssignedToID" json:"assigned_to,omitempty"`
	Version        int            `gorm:"default:1" json:"version"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
}

func (e *Evidence) BeforeCreate(tx *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}

// EvidenceTransitionHistory records each state change for an evidence record.
type EvidenceTransitionHistory struct {
	ID             uuid.UUID           `gorm:"type:uuid;primary_key" json:"id"`
	EvidenceID     uuid.UUID           `gorm:"type:uuid;index;not null" json:"evidence_id"`
	Evidence       *Evidence           `gorm:"foreignKey:EvidenceID" json:"evidence,omitempty"`
	TransitionID   *uuid.UUID          `gorm:"type:uuid;index" json:"transition_id"`
	Transition     *WorkflowTransition `gorm:"foreignKey:TransitionID" json:"transition,omitempty"`
	FromStateID    uuid.UUID           `gorm:"type:uuid;not null;index" json:"from_state_id"`
	FromState      *WorkflowState      `gorm:"foreignKey:FromStateID" json:"from_state,omitempty"`
	ToStateID      uuid.UUID           `gorm:"type:uuid;not null;index" json:"to_state_id"`
	ToState        *WorkflowState      `gorm:"foreignKey:ToStateID" json:"to_state,omitempty"`
	PerformedByID  uuid.UUID           `gorm:"type:uuid;not null;index" json:"performed_by_id"`
	PerformedBy    *User               `gorm:"foreignKey:PerformedByID" json:"performed_by,omitempty"`
	Comment        string              `gorm:"type:text" json:"comment"`
	IsSystemAction bool                `gorm:"default:false" json:"is_system_action"`
	TransitionedAt time.Time           `gorm:"index" json:"transitioned_at"`
	CreatedAt      time.Time           `json:"created_at"`
}

func (h *EvidenceTransitionHistory) BeforeCreate(tx *gorm.DB) error {
	if h.ID == uuid.Nil {
		h.ID = uuid.New()
	}
	return nil
}

// ════════════════════════════════════════════════════
// METRIC IMPORT BATCH MODELS
// ════════════════════════════════════════════════════

// MetricImportBatch represents a single bulk import of metric values
// that must go through the evidence approval workflow before being applied.
type MetricImportBatch struct {
	ID             uuid.UUID          `gorm:"type:uuid;primary_key" json:"id"`
	Title          string             `gorm:"size:255;not null" json:"title"`
	Comment        string             `gorm:"type:text" json:"comment"`
	Status         string             `gorm:"size:30;not null;default:'Draft'" json:"status"`
	ItemCount      int                `gorm:"not null;default:0" json:"item_count"`
	GoalCount      int                `gorm:"not null;default:0" json:"goal_count"`
	FileName       string             `gorm:"size:255" json:"file_name"`
	ImportedByID   uuid.UUID          `gorm:"type:uuid;index;not null" json:"imported_by_id"`
	ImportedBy     *User              `gorm:"foreignKey:ImportedByID" json:"imported_by,omitempty"`
	PrimaryGoalID  uuid.UUID          `gorm:"type:uuid;index;not null" json:"primary_goal_id"`
	PrimaryGoal    *Goal              `gorm:"foreignKey:PrimaryGoalID" json:"primary_goal,omitempty"`
	WorkflowID     *uuid.UUID         `gorm:"type:uuid;index" json:"workflow_id"`
	CurrentStateID *uuid.UUID         `gorm:"type:uuid;index" json:"current_state_id"`
	CurrentState   *WorkflowState     `gorm:"foreignKey:CurrentStateID" json:"current_state,omitempty"`
	AssignedToID   *uuid.UUID         `gorm:"type:uuid;index" json:"assigned_to_id"`
	AssignedTo     *User              `gorm:"foreignKey:AssignedToID" json:"assigned_to,omitempty"`
	Version        int                `gorm:"default:1" json:"version"`
	Items          []MetricImportItem `gorm:"foreignKey:BatchID" json:"items,omitempty"`
	CreatedAt      time.Time          `json:"created_at"`
	UpdatedAt      time.Time          `json:"updated_at"`
	DeletedAt      gorm.DeletedAt     `gorm:"index" json:"-"`
}

func (b *MetricImportBatch) BeforeCreate(tx *gorm.DB) error {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	return nil
}

// MetricImportItem represents a single metric value change within a batch.
type MetricImportItem struct {
	ID        uuid.UUID   `gorm:"type:uuid;primary_key" json:"id"`
	BatchID   uuid.UUID   `gorm:"type:uuid;index;not null" json:"batch_id"`
	GoalID    uuid.UUID   `gorm:"type:uuid;index;not null" json:"goal_id"`
	Goal      *Goal       `gorm:"foreignKey:GoalID" json:"goal,omitempty"`
	MetricID  uuid.UUID   `gorm:"type:uuid;index;not null" json:"metric_id"`
	Metric    *GoalMetric `gorm:"foreignKey:MetricID" json:"metric,omitempty"`
	OldValue  float64     `json:"old_value"`
	NewValue  float64     `json:"new_value"`
	Applied   bool        `gorm:"default:false" json:"applied"`
	CreatedAt time.Time   `json:"created_at"`
}

func (i *MetricImportItem) BeforeCreate(tx *gorm.DB) error {
	if i.ID == uuid.Nil {
		i.ID = uuid.New()
	}
	return nil
}

// MetricImportBatchTransitionHistory records each state change for a metric import batch.
type MetricImportBatchTransitionHistory struct {
	ID             uuid.UUID           `gorm:"type:uuid;primary_key" json:"id"`
	BatchID        uuid.UUID           `gorm:"type:uuid;index;not null" json:"batch_id"`
	TransitionID   *uuid.UUID          `gorm:"type:uuid;index" json:"transition_id"`
	Transition     *WorkflowTransition `gorm:"foreignKey:TransitionID" json:"transition,omitempty"`
	FromStateID    uuid.UUID           `gorm:"type:uuid;not null;index" json:"from_state_id"`
	FromState      *WorkflowState      `gorm:"foreignKey:FromStateID" json:"from_state,omitempty"`
	ToStateID      uuid.UUID           `gorm:"type:uuid;not null;index" json:"to_state_id"`
	ToState        *WorkflowState      `gorm:"foreignKey:ToStateID" json:"to_state,omitempty"`
	PerformedByID  uuid.UUID           `gorm:"type:uuid;not null;index" json:"performed_by_id"`
	PerformedBy    *User               `gorm:"foreignKey:PerformedByID" json:"performed_by,omitempty"`
	Comment        string              `gorm:"type:text" json:"comment"`
	IsSystemAction bool                `gorm:"default:false" json:"is_system_action"`
	TransitionedAt time.Time           `gorm:"index" json:"transitioned_at"`
	CreatedAt      time.Time           `json:"created_at"`
}

func (h *MetricImportBatchTransitionHistory) BeforeCreate(tx *gorm.DB) error {
	if h.ID == uuid.Nil {
		h.ID = uuid.New()
	}
	return nil
}

// ════════════════════════════════════════════════════
// REQUEST TYPES
// ════════════════════════════════════════════════════

type GoalCreateRequest struct {
	Title        string     `json:"title" validate:"required,max=255"`
	Description  string     `json:"description" validate:"required"`
	Category     string     `json:"category" validate:"max=100"`
	CategoryID   *uuid.UUID `json:"category_id"`
	Priority     string     `json:"priority" validate:"required,oneof=Critical High Medium Low"`
	DepartmentID *uuid.UUID `json:"department_id"`
	OwnerID      uuid.UUID  `json:"owner_id" validate:"required"`
	ParentGoalID *uuid.UUID `json:"parent_goal_id"`
	StartDate    *time.Time `json:"start_date" validate:"required"`
	TargetDate   *time.Time `json:"target_date" validate:"required"`
	ReviewDate   *time.Time `json:"review_date"`
	Metadata     string     `json:"metadata"`
}

func (r *GoalCreateRequest) Validate() error {
	if r.StartDate != nil && r.TargetDate != nil {
		if r.StartDate.After(*r.TargetDate) {
			return fmt.Errorf("start_date must be before target_date")
		}
	}
	return nil
}

type GoalUpdateRequest struct {
	Title        *string    `json:"title" validate:"omitempty,max=255"`
	Description  *string    `json:"description"`
	Category     *string    `json:"category" validate:"omitempty,max=100"`
	CategoryID   *uuid.UUID `json:"category_id"`
	Priority     *string    `json:"priority" validate:"omitempty,oneof=Critical High Medium Low"`
	DepartmentID *uuid.UUID `json:"department_id"`
	OwnerID      *uuid.UUID `json:"owner_id"`
	ParentGoalID *uuid.UUID `json:"parent_goal_id"`
	StartDate    *time.Time `json:"start_date"`
	TargetDate   *time.Time `json:"target_date"`
	ReviewDate   *time.Time `json:"review_date"`
	Metadata     *string    `json:"metadata"`
}

type GoalTransitionRequest struct {
	Status string `json:"status" validate:"required"`
}

type GoalCloneRequest struct {
	Title      string     `json:"title"`
	StartDate  *time.Time `json:"start_date"`
	TargetDate *time.Time `json:"target_date"`
	ReviewDate *time.Time `json:"review_date"`
	OwnerID    *uuid.UUID `json:"owner_id"`
}

type GoalFilter struct {
	Page         int        `query:"page"`
	Limit        int        `query:"limit"`
	Status       string     `query:"status"`
	Priority     string     `query:"priority"`
	OwnerID      *uuid.UUID `query:"owner_id"`
	DepartmentID *uuid.UUID `query:"department_id"`
	ParentGoalID *uuid.UUID `query:"parent_goal_id"`
	RootOnly     bool       `query:"root_only"`
	Category     string     `query:"category"`
	Search       string     `query:"search"`
	StartFrom    *time.Time `query:"start_from"`
	StartTo      *time.Time `query:"start_to"`
	TargetFrom   *time.Time `query:"target_from"`
	TargetTo     *time.Time `query:"target_to"`
	SortBy       string     `query:"sort_by"`
	SortOrder    string     `query:"sort_order"`
	// Scope restricts the listing. "mine" returns goals where the caller is
	// the owner or a collaborator. Any other value (including empty) returns
	// the full set subject to other filters.
	Scope  string     `query:"scope"`
	UserID *uuid.UUID `query:"-"` // Set by handler, not from query params
}

type GoalMetricCreateRequest struct {
	Name          string  `json:"name" validate:"required,max=255"`
	MetricType    string  `json:"metric_type" validate:"required,oneof=Numeric Percentage Currency Boolean"`
	Unit          string  `json:"unit" validate:"max=50"`
	BaselineValue float64 `json:"baseline_value"`
	CurrentValue  float64 `json:"current_value"`
	TargetValue   float64 `json:"target_value" validate:"required"`
	Weight        float64 `json:"weight" validate:"gt=0"`
	Formula       string  `json:"formula" validate:"max=500"`
}

type GoalMetricUpdateRequest struct {
	Name          *string  `json:"name" validate:"omitempty,max=255"`
	MetricType    *string  `json:"metric_type" validate:"omitempty,oneof=Numeric Percentage Currency Boolean"`
	Unit          *string  `json:"unit" validate:"omitempty,max=50"`
	BaselineValue *float64 `json:"baseline_value"`
	TargetValue   *float64 `json:"target_value"`
	Weight        *float64 `json:"weight" validate:"omitempty,gt=0"`
	Formula       *string  `json:"formula" validate:"omitempty,max=500"`
}

type MetricValueUpdateRequest struct {
	Value   float64 `json:"value" validate:"required"`
	Comment string  `json:"comment"`
}

type CollaboratorAddRequest struct {
	UserID uuid.UUID `json:"user_id" validate:"required"`
	Role   string    `json:"role" validate:"required,oneof=collaborator reviewer_l1 reviewer_l2"`
}

// EvidenceTransitionRequest is sent by the frontend to execute a workflow transition.
type EvidenceTransitionRequest struct {
	TransitionID string `json:"transition_id" validate:"required,uuid"`
	Comment      string `json:"comment"`
	Version      int    `json:"version" validate:"required,min=1"`
}

// MetricTransitionRequest is sent to execute a workflow transition on a metric definition or value change.
type MetricTransitionRequest struct {
	TransitionID string `json:"transition_id" validate:"required,uuid"`
	Comment      string `json:"comment"`
	Version      int    `json:"version" validate:"omitempty,min=1"`
}

type EvidenceFilter struct {
	Page         int    `query:"page"`
	Limit        int    `query:"limit"`
	Status       string `query:"status"`
	Search       string `query:"search"`
	EvidenceType string `query:"evidence_type"`
	StartDate    string `query:"start_date"`
	EndDate      string `query:"end_date"`
	// ApprovedOnly is set by the service layer (not a query param) to restrict
	// the listing to evidences in a terminal-approved state. Used to enforce
	// the visibility gate for view-only callers.
	ApprovedOnly bool `query:"-"`
}

// ════════════════════════════════════════════════════
// RESPONSE TYPES
// ════════════════════════════════════════════════════

type GoalResponse struct {
	ID                uuid.UUID                  `json:"id"`
	Title             string                     `json:"title"`
	Description       string                     `json:"description"`
	Category          string                     `json:"category"`
	CategoryID        *uuid.UUID                 `json:"category_id,omitempty"`
	CategoryRef       *CategoryResponse          `json:"category_ref,omitempty"`
	Priority          string                     `json:"priority"`
	Status            string                     `json:"status"`
	OwnerID           uuid.UUID                  `json:"owner_id"`
	Owner             *UserBriefResponse         `json:"owner,omitempty"`
	DepartmentID      *uuid.UUID                 `json:"department_id"`
	Department        *DepartmentBriefResponse   `json:"department,omitempty"`
	ParentGoalID      *uuid.UUID                 `json:"parent_goal_id"`
	ParentGoal        *GoalBriefResponse         `json:"parent_goal,omitempty"`
	Children          []GoalBriefResponse        `json:"children,omitempty"`
	Level             int                        `json:"level"`
	Path              string                     `json:"path"`
	StartDate         *time.Time                 `json:"start_date"`
	TargetDate        *time.Time                 `json:"target_date"`
	ReviewDate        *time.Time                 `json:"review_date"`
	Progress          float64                    `json:"progress"`
	DocumentaFolderID string                     `json:"documenta_folder_id"`
	Metadata          string                     `json:"metadata"`
	CreatedByID       uuid.UUID                  `json:"created_by_id"`
	CreatedBy         *UserBriefResponse         `json:"created_by,omitempty"`
	Collaborators     []GoalCollaboratorResponse `json:"collaborators,omitempty"`
	Metrics           []GoalMetricResponse       `json:"metrics,omitempty"`
	EvidenceCount     int                        `json:"evidence_count"`
	CreatedAt         time.Time                  `json:"created_at"`
	UpdatedAt         time.Time                  `json:"updated_at"`
}

type GoalBriefResponse struct {
	ID       uuid.UUID `json:"id"`
	Title    string    `json:"title"`
	Status   string    `json:"status"`
	Priority string    `json:"priority"`
	Progress float64   `json:"progress"`
	Level    int       `json:"level"`
}

type UserBriefResponse struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Avatar    string    `json:"avatar"`
}

type DepartmentBriefResponse struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
	Code string    `json:"code"`
}

type GoalCollaboratorResponse struct {
	ID        uuid.UUID          `json:"id"`
	UserID    uuid.UUID          `json:"user_id"`
	User      *UserBriefResponse `json:"user,omitempty"`
	Role      string             `json:"role"`
	CreatedAt time.Time          `json:"created_at"`
}

type GoalMetricResponse struct {
	ID             uuid.UUID           `json:"id"`
	GoalID         uuid.UUID           `json:"goal_id"`
	Name           string              `json:"name"`
	MetricType     string              `json:"metric_type"`
	Unit           string              `json:"unit"`
	BaselineValue  float64             `json:"baseline_value"`
	CurrentValue   float64             `json:"current_value"`
	TargetValue    float64             `json:"target_value"`
	Weight         float64             `json:"weight"`
	Formula        string              `json:"formula"`
	Progress       float64             `json:"progress"`
	WorkflowID     *uuid.UUID          `json:"workflow_id,omitempty"`
	CurrentStateID *uuid.UUID          `json:"current_state_id,omitempty"`
	CurrentState   *WorkflowStateBrief `json:"current_state,omitempty"`
	AssignedToID   *uuid.UUID          `json:"assigned_to_id,omitempty"`
	AssignedTo     *UserBriefResponse  `json:"assigned_to,omitempty"`
	Version        int                 `json:"version"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
}

type MetricHistoryResponse struct {
	ID          uuid.UUID          `json:"id"`
	MetricID    uuid.UUID          `json:"metric_id"`
	OldValue    float64            `json:"old_value"`
	NewValue    float64            `json:"new_value"`
	ChangedByID uuid.UUID          `json:"changed_by_id"`
	ChangedBy   *UserBriefResponse `json:"changed_by,omitempty"`
	Comment     string             `json:"comment"`
	CreatedAt   time.Time          `json:"created_at"`
}

type EvidenceResponse struct {
	ID              uuid.UUID           `json:"id"`
	GoalID          uuid.UUID           `json:"goal_id"`
	MetricID        *uuid.UUID          `json:"metric_id"`
	Title           string              `json:"title"`
	EvidenceType    string              `json:"evidence_type"`
	Comment         string              `json:"comment"`
	Status          string              `json:"status"`
	DocumentaFileID string              `json:"documenta_file_id"`
	FileName        string              `json:"file_name"`
	FileSize        int64               `json:"file_size"`
	MimeType        string              `json:"mime_type"`
	UploadedByID    uuid.UUID           `json:"uploaded_by_id"`
	UploadedBy      *UserBriefResponse  `json:"uploaded_by,omitempty"`
	WorkflowID      *uuid.UUID          `json:"workflow_id"`
	CurrentStateID  *uuid.UUID          `json:"current_state_id"`
	CurrentState    *WorkflowStateBrief `json:"current_state,omitempty"`
	AssignedToID    *uuid.UUID          `json:"assigned_to_id"`
	AssignedTo      *UserBriefResponse  `json:"assigned_to,omitempty"`
	Version         int                 `json:"version"`
	CreatedAt       time.Time           `json:"created_at"`
	UpdatedAt       time.Time           `json:"updated_at"`
}

// WorkflowStateBrief is a minimal representation of WorkflowState for evidence responses.
type WorkflowStateBrief struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Code      string    `json:"code"`
	StateType string    `json:"state_type"`
	Color     string    `json:"color"`
}

type EvidenceTransitionHistoryResponse struct {
	ID             uuid.UUID          `json:"id"`
	EvidenceID     uuid.UUID          `json:"evidence_id"`
	TransitionName string             `json:"transition_name"`
	FromStateName  string             `json:"from_state_name"`
	ToStateName    string             `json:"to_state_name"`
	FromStateColor string             `json:"from_state_color"`
	ToStateColor   string             `json:"to_state_color"`
	PerformedByID  uuid.UUID          `json:"performed_by_id"`
	PerformedBy    *UserBriefResponse `json:"performed_by,omitempty"`
	Comment        string             `json:"comment"`
	IsSystemAction bool               `json:"is_system_action"`
	TransitionedAt time.Time          `json:"transitioned_at"`
	CreatedAt      time.Time          `json:"created_at"`
}

type ApprovalListResponse struct {
	ID              uuid.UUID          `json:"id"`
	EvidenceID      uuid.UUID          `json:"evidence_id"`
	EvidenceTitle   string             `json:"evidence_title"`
	EvidenceVersion int                `json:"evidence_version"`
	GoalID          uuid.UUID          `json:"goal_id"`
	GoalTitle       string             `json:"goal_title"`
	GoalPriority    string             `json:"goal_priority"`
	Status          string             `json:"status"`
	StateName       string             `json:"state_name"`
	StateColor      string             `json:"state_color"`
	SubmittedBy     *UserBriefResponse `json:"submitted_by,omitempty"`
	AssignedTo      *UserBriefResponse `json:"assigned_to,omitempty"`
	CreatedAt       time.Time          `json:"created_at"`
	UpdatedAt       time.Time          `json:"updated_at"`
}

// ════════════════════════════════════════════════════
// IMPORT REQUEST/RESPONSE TYPES
// ════════════════════════════════════════════════════

// ImportRowResult represents validation result for a single row
type ImportRowResult struct {
	RowNumber int      `json:"row_number"`
	Status    string   `json:"status"` // "valid", "warning", "error"
	Errors    []string `json:"errors,omitempty"`
	Warnings  []string `json:"warnings,omitempty"`
	GoalTitle string   `json:"goal_title"`
}

// GoalImportResponse is the response from import (dry-run or commit)
type GoalImportResponse struct {
	Mode           string            `json:"mode"` // "dry_run" or "committed"
	TotalRows      int               `json:"total_rows"`
	GoalsCount     int               `json:"goals_count"`
	MetricsCount   int               `json:"metrics_count"`
	ValidCount     int               `json:"valid_count"`
	ErrorCount     int               `json:"error_count"`
	WarningCount   int               `json:"warning_count"`
	Rows           []ImportRowResult `json:"rows"`
	CreatedGoalIDs []string          `json:"created_goal_ids,omitempty"`
}

// ════════════════════════════════════════════════════
// METRIC IMPORT REQUEST/RESPONSE TYPES
// ════════════════════════════════════════════════════

type MetricImportBatchTransitionRequest struct {
	TransitionID string `json:"transition_id" validate:"required,uuid"`
	Comment      string `json:"comment"`
	Version      int    `json:"version" validate:"required,min=1"`
}

type MetricImportBatchFilter struct {
	Page   int    `query:"page"`
	Limit  int    `query:"limit"`
	Status string `query:"status"`
}

type MetricImportValidationItem struct {
	RowNumber    int      `json:"row_number"`
	GoalID       string   `json:"goal_id"`
	GoalTitle    string   `json:"goal_title"`
	MetricID     string   `json:"metric_id"`
	MetricName   string   `json:"metric_name"`
	CurrentValue float64  `json:"current_value"`
	NewValue     float64  `json:"new_value"`
	Status       string   `json:"status"` // "valid", "warning", "error", "skipped"
	Errors       []string `json:"errors,omitempty"`
	Warnings     []string `json:"warnings,omitempty"`
}

type MetricImportDryRunResponse struct {
	TotalRows    int                          `json:"total_rows"`
	ValidCount   int                          `json:"valid_count"`
	ErrorCount   int                          `json:"error_count"`
	WarningCount int                          `json:"warning_count"`
	SkippedCount int                          `json:"skipped_count"`
	GoalCount    int                          `json:"goal_count"`
	Items        []MetricImportValidationItem `json:"items"`
}

type MetricImportBatchResponse struct {
	ID               uuid.UUID                  `json:"id"`
	Title            string                     `json:"title"`
	Comment          string                     `json:"comment"`
	Status           string                     `json:"status"`
	ItemCount        int                        `json:"item_count"`
	GoalCount        int                        `json:"goal_count"`
	FileName         string                     `json:"file_name"`
	ImportedByID     uuid.UUID                  `json:"imported_by_id"`
	ImportedBy       *UserBriefResponse         `json:"imported_by,omitempty"`
	PrimaryGoalID    uuid.UUID                  `json:"primary_goal_id"`
	PrimaryGoalTitle string                     `json:"primary_goal_title,omitempty"`
	WorkflowID       *uuid.UUID                 `json:"workflow_id"`
	CurrentStateID   *uuid.UUID                 `json:"current_state_id"`
	CurrentState     *WorkflowStateBrief        `json:"current_state,omitempty"`
	AssignedToID     *uuid.UUID                 `json:"assigned_to_id"`
	AssignedTo       *UserBriefResponse         `json:"assigned_to,omitempty"`
	Version          int                        `json:"version"`
	Items            []MetricImportItemResponse `json:"items,omitempty"`
	CreatedAt        time.Time                  `json:"created_at"`
	UpdatedAt        time.Time                  `json:"updated_at"`
}

type MetricImportItemResponse struct {
	ID         uuid.UUID `json:"id"`
	GoalID     uuid.UUID `json:"goal_id"`
	GoalTitle  string    `json:"goal_title"`
	MetricID   uuid.UUID `json:"metric_id"`
	MetricName string    `json:"metric_name"`
	MetricType string    `json:"metric_type"`
	Unit       string    `json:"unit"`
	OldValue   float64   `json:"old_value"`
	NewValue   float64   `json:"new_value"`
	Applied    bool      `json:"applied"`
}

type MetricImportBatchTransitionHistoryResponse struct {
	ID             uuid.UUID          `json:"id"`
	BatchID        uuid.UUID          `json:"batch_id"`
	TransitionName string             `json:"transition_name"`
	FromStateName  string             `json:"from_state_name"`
	ToStateName    string             `json:"to_state_name"`
	FromStateColor string             `json:"from_state_color"`
	ToStateColor   string             `json:"to_state_color"`
	PerformedByID  uuid.UUID          `json:"performed_by_id"`
	PerformedBy    *UserBriefResponse `json:"performed_by,omitempty"`
	Comment        string             `json:"comment"`
	IsSystemAction bool               `json:"is_system_action"`
	TransitionedAt time.Time          `json:"transitioned_at"`
	CreatedAt      time.Time          `json:"created_at"`
}

// ════════════════════════════════════════════════════
// BULK OPERATION REQUEST/RESPONSE TYPES
// ════════════════════════════════════════════════════

type BulkActionRequest struct {
	GoalIDs    []uuid.UUID `json:"goal_ids" validate:"required,min=1"`
	Action     string      `json:"action" validate:"required,oneof=transition reassign close"`
	NewStatus  string      `json:"new_status,omitempty"`
	NewOwnerID *uuid.UUID  `json:"new_owner_id,omitempty"`
}

type BulkActionItemResult struct {
	GoalID  uuid.UUID `json:"goal_id"`
	Success bool      `json:"success"`
	Error   string    `json:"error,omitempty"`
}

type BulkActionResponse struct {
	TotalRequested int                    `json:"total_requested"`
	SuccessCount   int                    `json:"success_count"`
	FailureCount   int                    `json:"failure_count"`
	Results        []BulkActionItemResult `json:"results"`
}

// ════════════════════════════════════════════════════
// CHECK-IN REQUEST/RESPONSE TYPES
// ════════════════════════════════════════════════════

type CheckInCreateRequest struct {
	Status        string                `json:"status" validate:"required,oneof=on_track at_risk behind blocked"`
	Content       string                `json:"content" validate:"required,min=1"`
	MetricUpdates []CheckInMetricUpdate `json:"metric_updates,omitempty"`
}

type CheckInMetricUpdate struct {
	MetricID uuid.UUID `json:"metric_id" validate:"required"`
	Value    float64   `json:"value"`
	Comment  string    `json:"comment"`
}

type CheckInResponse struct {
	ID            uuid.UUID          `json:"id"`
	GoalID        uuid.UUID          `json:"goal_id"`
	AuthorID      uuid.UUID          `json:"author_id"`
	Author        *UserBriefResponse `json:"author,omitempty"`
	Status        string             `json:"status"`
	Content       string             `json:"content"`
	ProgressSnap  float64            `json:"progress_snapshot"`
	MetricUpdates string             `json:"metric_updates"`
	CreatedAt     time.Time          `json:"created_at"`
}

// ════════════════════════════════════════════════════
// CONVERTER FUNCTIONS
// ════════════════════════════════════════════════════

func ToUserBriefResponse(u *User) *UserBriefResponse {
	if u == nil {
		return nil
	}
	return &UserBriefResponse{
		ID:        u.ID,
		Email:     u.Email,
		FirstName: u.FirstName,
		LastName:  u.LastName,
		Avatar:    u.Avatar,
	}
}

func ToDepartmentBriefResponse(d *Department) *DepartmentBriefResponse {
	if d == nil {
		return nil
	}
	return &DepartmentBriefResponse{
		ID:   d.ID,
		Name: d.Name,
		Code: d.Code,
	}
}

func ToWorkflowStateBrief(s *WorkflowState) *WorkflowStateBrief {
	if s == nil {
		return nil
	}
	return &WorkflowStateBrief{
		ID:        s.ID,
		Name:      s.Name,
		Code:      s.Code,
		StateType: s.StateType,
		Color:     s.Color,
	}
}

func ToGoalBriefResponse(g *Goal) *GoalBriefResponse {
	if g == nil {
		return nil
	}
	return &GoalBriefResponse{
		ID:       g.ID,
		Title:    g.Title,
		Status:   g.Status,
		Priority: g.Priority,
		Progress: g.Progress,
		Level:    g.Level,
	}
}

func (g *Goal) ToResponse() GoalResponse {
	resp := GoalResponse{
		ID:                g.ID,
		Title:             g.Title,
		Description:       g.Description,
		Category:          g.Category,
		CategoryID:        g.CategoryID,
		Priority:          g.Priority,
		Status:            g.Status,
		OwnerID:           g.OwnerID,
		Owner:             ToUserBriefResponse(g.Owner),
		DepartmentID:      g.DepartmentID,
		Department:        ToDepartmentBriefResponse(g.Department),
		ParentGoalID:      g.ParentGoalID,
		ParentGoal:        ToGoalBriefResponse(g.ParentGoal),
		Level:             g.Level,
		Path:              g.Path,
		StartDate:         g.StartDate,
		TargetDate:        g.TargetDate,
		ReviewDate:        g.ReviewDate,
		Progress:          g.Progress,
		DocumentaFolderID: g.DocumentaFolderID,
		Metadata:          g.Metadata,
		CreatedByID:       g.CreatedByID,
		CreatedBy:         ToUserBriefResponse(g.CreatedBy),
		EvidenceCount:     len(g.Evidences),
		CreatedAt:         g.CreatedAt,
		UpdatedAt:         g.UpdatedAt,
	}

	if g.CategoryRef != nil {
		ref := ToCategoryResponse(g.CategoryRef)
		resp.CategoryRef = &ref
	}

	if g.Children != nil {
		resp.Children = make([]GoalBriefResponse, len(g.Children))
		for i, child := range g.Children {
			resp.Children[i] = *ToGoalBriefResponse(&child)
		}
	}

	if g.Collaborators != nil {
		resp.Collaborators = make([]GoalCollaboratorResponse, len(g.Collaborators))
		for i, c := range g.Collaborators {
			resp.Collaborators[i] = c.ToResponse()
		}
	}

	if g.Metrics != nil {
		resp.Metrics = make([]GoalMetricResponse, len(g.Metrics))
		for i, m := range g.Metrics {
			resp.Metrics[i] = m.ToResponse()
		}
	}

	return resp
}

func (ci *GoalCheckIn) ToResponse() CheckInResponse {
	return CheckInResponse{
		ID:            ci.ID,
		GoalID:        ci.GoalID,
		AuthorID:      ci.AuthorID,
		Author:        ToUserBriefResponse(ci.Author),
		Status:        ci.Status,
		Content:       ci.Content,
		ProgressSnap:  ci.ProgressSnap,
		MetricUpdates: ci.MetricUpdates,
		CreatedAt:     ci.CreatedAt,
	}
}

func (c *GoalCollaborator) ToResponse() GoalCollaboratorResponse {
	return GoalCollaboratorResponse{
		ID:        c.ID,
		UserID:    c.UserID,
		User:      ToUserBriefResponse(c.User),
		Role:      c.Role,
		CreatedAt: c.CreatedAt,
	}
}

func (m *GoalMetric) ToResponse() GoalMetricResponse {
	progress := 0.0
	if m.TargetValue != 0 {
		progress = (m.CurrentValue / m.TargetValue) * 100
		if progress > 100 {
			progress = 100
		}
		if progress < 0 {
			progress = 0
		}
	}
	if m.MetricType == MetricTypeBoolean {
		if m.CurrentValue >= 1 {
			progress = 100
		} else {
			progress = 0
		}
	}

	return GoalMetricResponse{
		ID:             m.ID,
		GoalID:         m.GoalID,
		Name:           m.Name,
		MetricType:     m.MetricType,
		Unit:           m.Unit,
		BaselineValue:  m.BaselineValue,
		CurrentValue:   m.CurrentValue,
		TargetValue:    m.TargetValue,
		Weight:         m.Weight,
		Formula:        m.Formula,
		Progress:       progress,
		WorkflowID:     m.WorkflowID,
		CurrentStateID: m.CurrentStateID,
		CurrentState:   ToWorkflowStateBrief(m.CurrentState),
		AssignedToID:   m.AssignedToID,
		AssignedTo:     ToUserBriefResponse(m.AssignedTo),
		Version:        m.Version,
		CreatedAt:      m.CreatedAt,
		UpdatedAt:      m.UpdatedAt,
	}
}

func (h *MetricHistory) ToResponse() MetricHistoryResponse {
	return MetricHistoryResponse{
		ID:          h.ID,
		MetricID:    h.MetricID,
		OldValue:    h.OldValue,
		NewValue:    h.NewValue,
		ChangedByID: h.ChangedByID,
		ChangedBy:   ToUserBriefResponse(h.ChangedBy),
		Comment:     h.Comment,
		CreatedAt:   h.CreatedAt,
	}
}

func (e *Evidence) ToResponse() EvidenceResponse {
	return EvidenceResponse{
		ID:              e.ID,
		GoalID:          e.GoalID,
		MetricID:        e.MetricID,
		Title:           e.Title,
		EvidenceType:    e.EvidenceType,
		Comment:         e.Comment,
		Status:          e.Status,
		DocumentaFileID: e.DocumentaFileID,
		FileName:        e.FileName,
		FileSize:        e.FileSize,
		MimeType:        e.MimeType,
		UploadedByID:    e.UploadedByID,
		UploadedBy:      ToUserBriefResponse(e.UploadedBy),
		WorkflowID:      e.WorkflowID,
		CurrentStateID:  e.CurrentStateID,
		CurrentState:    ToWorkflowStateBrief(e.CurrentState),
		AssignedToID:    e.AssignedToID,
		AssignedTo:      ToUserBriefResponse(e.AssignedTo),
		Version:         e.Version,
		CreatedAt:       e.CreatedAt,
		UpdatedAt:       e.UpdatedAt,
	}
}

func (h *EvidenceTransitionHistory) ToResponse() EvidenceTransitionHistoryResponse {
	resp := EvidenceTransitionHistoryResponse{
		ID:             h.ID,
		EvidenceID:     h.EvidenceID,
		PerformedByID:  h.PerformedByID,
		PerformedBy:    ToUserBriefResponse(h.PerformedBy),
		Comment:        h.Comment,
		IsSystemAction: h.IsSystemAction,
		TransitionedAt: h.TransitionedAt,
		CreatedAt:      h.CreatedAt,
	}
	if h.Transition != nil {
		resp.TransitionName = h.Transition.Name
	}
	if h.FromState != nil {
		resp.FromStateName = h.FromState.Name
		resp.FromStateColor = h.FromState.Color
	}
	if h.ToState != nil {
		resp.ToStateName = h.ToState.Name
		resp.ToStateColor = h.ToState.Color
	}
	return resp
}

// ════════════════════════════════════════════════════
// METRIC IMPORT BATCH CONVERTERS
// ════════════════════════════════════════════════════

func (b *MetricImportBatch) ToResponse() MetricImportBatchResponse {
	resp := MetricImportBatchResponse{
		ID:             b.ID,
		Title:          b.Title,
		Comment:        b.Comment,
		Status:         b.Status,
		ItemCount:      b.ItemCount,
		GoalCount:      b.GoalCount,
		FileName:       b.FileName,
		ImportedByID:   b.ImportedByID,
		ImportedBy:     ToUserBriefResponse(b.ImportedBy),
		PrimaryGoalID:  b.PrimaryGoalID,
		WorkflowID:     b.WorkflowID,
		CurrentStateID: b.CurrentStateID,
		CurrentState:   ToWorkflowStateBrief(b.CurrentState),
		AssignedToID:   b.AssignedToID,
		AssignedTo:     ToUserBriefResponse(b.AssignedTo),
		Version:        b.Version,
		CreatedAt:      b.CreatedAt,
		UpdatedAt:      b.UpdatedAt,
	}
	if b.PrimaryGoal != nil {
		resp.PrimaryGoalTitle = b.PrimaryGoal.Title
	}
	if b.Items != nil {
		resp.Items = make([]MetricImportItemResponse, len(b.Items))
		for i, item := range b.Items {
			resp.Items[i] = item.ToResponse()
		}
	}
	return resp
}

func (item *MetricImportItem) ToResponse() MetricImportItemResponse {
	resp := MetricImportItemResponse{
		ID:       item.ID,
		GoalID:   item.GoalID,
		MetricID: item.MetricID,
		OldValue: item.OldValue,
		NewValue: item.NewValue,
		Applied:  item.Applied,
	}
	if item.Goal != nil {
		resp.GoalTitle = item.Goal.Title
	}
	if item.Metric != nil {
		resp.MetricName = item.Metric.Name
		resp.MetricType = item.Metric.MetricType
		resp.Unit = item.Metric.Unit
	}
	return resp
}

// ──────────────────────────────────────────────────
// Goal Comments
// ──────────────────────────────────────────────────

// GoalComment represents a discussion comment on a goal.
type GoalComment struct {
	ID        uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	GoalID    uuid.UUID      `gorm:"type:uuid;index;not null" json:"goal_id"`
	Goal      *Goal          `gorm:"foreignKey:GoalID" json:"-"`
	AuthorID  uuid.UUID      `gorm:"type:uuid;index;not null" json:"author_id"`
	Author    *User          `gorm:"foreignKey:AuthorID" json:"author,omitempty"`
	Content   string         `gorm:"type:text;not null" json:"content"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (gc *GoalComment) BeforeCreate(tx *gorm.DB) error {
	if gc.ID == uuid.Nil {
		gc.ID = uuid.New()
	}
	return nil
}

type GoalCommentRequest struct {
	Content string `json:"content" validate:"required,min=1"`
}

type GoalCommentResponse struct {
	ID        uuid.UUID          `json:"id"`
	GoalID    uuid.UUID          `json:"goal_id"`
	Author    *UserBriefResponse `json:"author,omitempty"`
	Content   string             `json:"content"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
}

func ToGoalCommentResponse(c *GoalComment) GoalCommentResponse {
	return GoalCommentResponse{
		ID:        c.ID,
		GoalID:    c.GoalID,
		Author:    ToUserBriefResponse(c.Author),
		Content:   c.Content,
		CreatedAt: c.CreatedAt,
		UpdatedAt: c.UpdatedAt,
	}
}

func (h *MetricImportBatchTransitionHistory) ToResponse() MetricImportBatchTransitionHistoryResponse {
	resp := MetricImportBatchTransitionHistoryResponse{
		ID:             h.ID,
		BatchID:        h.BatchID,
		PerformedByID:  h.PerformedByID,
		PerformedBy:    ToUserBriefResponse(h.PerformedBy),
		Comment:        h.Comment,
		IsSystemAction: h.IsSystemAction,
		TransitionedAt: h.TransitionedAt,
		CreatedAt:      h.CreatedAt,
	}
	if h.Transition != nil {
		resp.TransitionName = h.Transition.Name
	}
	if h.FromState != nil {
		resp.FromStateName = h.FromState.Name
		resp.FromStateColor = h.FromState.Color
	}
	if h.ToState != nil {
		resp.ToStateName = h.ToState.Name
		resp.ToStateColor = h.ToState.Color
	}
	return resp
}

// ════════════════════════════════════════════════════
// METRIC VALUE CHANGE / TRANSITION RESPONSE TYPES
// ════════════════════════════════════════════════════

type GoalMetricValueChangeResponse struct {
	ID             uuid.UUID           `json:"id"`
	MetricID       uuid.UUID           `json:"metric_id"`
	MetricName     string              `json:"metric_name,omitempty"`
	GoalID         uuid.UUID           `json:"goal_id,omitempty"`
	GoalTitle      string              `json:"goal_title,omitempty"`
	ProposedValue  float64             `json:"proposed_value"`
	PreviousValue  float64             `json:"previous_value"`
	Comment        string              `json:"comment"`
	SubmittedByID  uuid.UUID           `json:"submitted_by_id"`
	SubmittedBy    *UserBriefResponse  `json:"submitted_by,omitempty"`
	WorkflowID     uuid.UUID           `json:"workflow_id"`
	CurrentStateID uuid.UUID           `json:"current_state_id"`
	CurrentState   *WorkflowStateBrief `json:"current_state,omitempty"`
	AssignedToID   *uuid.UUID          `json:"assigned_to_id,omitempty"`
	AssignedTo     *UserBriefResponse  `json:"assigned_to,omitempty"`
	AppliedAt      *time.Time          `json:"applied_at"`
	Version        int                 `json:"version"`
	CreatedAt      time.Time           `json:"created_at"`
	UpdatedAt      time.Time           `json:"updated_at"`
}

func (c *GoalMetricValueChange) ToResponse() GoalMetricValueChangeResponse {
	resp := GoalMetricValueChangeResponse{
		ID:             c.ID,
		MetricID:       c.MetricID,
		ProposedValue:  c.ProposedValue,
		PreviousValue:  c.PreviousValue,
		Comment:        c.Comment,
		SubmittedByID:  c.SubmittedByID,
		SubmittedBy:    ToUserBriefResponse(c.SubmittedBy),
		WorkflowID:     c.WorkflowID,
		CurrentStateID: c.CurrentStateID,
		CurrentState:   ToWorkflowStateBrief(c.CurrentState),
		AssignedToID:   c.AssignedToID,
		AssignedTo:     ToUserBriefResponse(c.AssignedTo),
		AppliedAt:      c.AppliedAt,
		Version:        c.Version,
		CreatedAt:      c.CreatedAt,
		UpdatedAt:      c.UpdatedAt,
	}
	if c.Metric != nil {
		resp.MetricName = c.Metric.Name
		resp.GoalID = c.Metric.GoalID
		if c.Metric.Goal != nil {
			resp.GoalTitle = c.Metric.Goal.Title
		}
	}
	return resp
}

type MetricTransitionHistoryResponse struct {
	ID             uuid.UUID          `json:"id"`
	MetricID       uuid.UUID          `json:"metric_id"`
	TransitionName string             `json:"transition_name"`
	FromStateName  string             `json:"from_state_name"`
	ToStateName    string             `json:"to_state_name"`
	FromStateColor string             `json:"from_state_color"`
	ToStateColor   string             `json:"to_state_color"`
	PerformedByID  uuid.UUID          `json:"performed_by_id"`
	PerformedBy    *UserBriefResponse `json:"performed_by,omitempty"`
	Comment        string             `json:"comment"`
	IsSystemAction bool               `json:"is_system_action"`
	TransitionedAt time.Time          `json:"transitioned_at"`
	CreatedAt      time.Time          `json:"created_at"`
}

func (h *MetricTransitionHistory) ToResponse() MetricTransitionHistoryResponse {
	resp := MetricTransitionHistoryResponse{
		ID:             h.ID,
		MetricID:       h.MetricID,
		PerformedByID:  h.PerformedByID,
		PerformedBy:    ToUserBriefResponse(h.PerformedBy),
		Comment:        h.Comment,
		IsSystemAction: h.IsSystemAction,
		TransitionedAt: h.TransitionedAt,
		CreatedAt:      h.CreatedAt,
	}
	if h.Transition != nil {
		resp.TransitionName = h.Transition.Name
	}
	if h.FromState != nil {
		resp.FromStateName = h.FromState.Name
		resp.FromStateColor = h.FromState.Color
	}
	if h.ToState != nil {
		resp.ToStateName = h.ToState.Name
		resp.ToStateColor = h.ToState.Color
	}
	return resp
}

type MetricValueChangeTransitionHistoryResponse struct {
	ID                  uuid.UUID          `json:"id"`
	MetricValueChangeID uuid.UUID          `json:"metric_value_change_id"`
	TransitionName      string             `json:"transition_name"`
	FromStateName       string             `json:"from_state_name"`
	ToStateName         string             `json:"to_state_name"`
	FromStateColor      string             `json:"from_state_color"`
	ToStateColor        string             `json:"to_state_color"`
	PerformedByID       uuid.UUID          `json:"performed_by_id"`
	PerformedBy         *UserBriefResponse `json:"performed_by,omitempty"`
	Comment             string             `json:"comment"`
	IsSystemAction      bool               `json:"is_system_action"`
	TransitionedAt      time.Time          `json:"transitioned_at"`
	CreatedAt           time.Time          `json:"created_at"`
}

func (h *MetricValueChangeTransitionHistory) ToResponse() MetricValueChangeTransitionHistoryResponse {
	resp := MetricValueChangeTransitionHistoryResponse{
		ID:                  h.ID,
		MetricValueChangeID: h.MetricValueChangeID,
		PerformedByID:       h.PerformedByID,
		PerformedBy:         ToUserBriefResponse(h.PerformedBy),
		Comment:             h.Comment,
		IsSystemAction:      h.IsSystemAction,
		TransitionedAt:      h.TransitionedAt,
		CreatedAt:           h.CreatedAt,
	}
	if h.Transition != nil {
		resp.TransitionName = h.Transition.Name
	}
	if h.FromState != nil {
		resp.FromStateName = h.FromState.Name
		resp.FromStateColor = h.FromState.Color
	}
	if h.ToState != nil {
		resp.ToStateName = h.ToState.Name
		resp.ToStateColor = h.ToState.Color
	}
	return resp
}

// MetricApprovalListResponse is the row format for pending/completed approval lists
// of metric definitions or value changes — mirrors ApprovalListResponse for evidence.
type MetricApprovalListResponse struct {
	ID            uuid.UUID          `json:"id"`
	MetricID      uuid.UUID          `json:"metric_id"`
	MetricName    string             `json:"metric_name"`
	ChangeID      *uuid.UUID         `json:"change_id,omitempty"`
	ProposedValue *float64           `json:"proposed_value,omitempty"`
	PreviousValue *float64           `json:"previous_value,omitempty"`
	GoalID        uuid.UUID          `json:"goal_id"`
	GoalTitle     string             `json:"goal_title"`
	GoalPriority  string             `json:"goal_priority"`
	StateName     string             `json:"state_name"`
	StateColor    string             `json:"state_color"`
	SubmittedBy   *UserBriefResponse `json:"submitted_by,omitempty"`
	AssignedTo    *UserBriefResponse `json:"assigned_to,omitempty"`
	Version       int                `json:"version"`
	CreatedAt     time.Time          `json:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at"`
}
