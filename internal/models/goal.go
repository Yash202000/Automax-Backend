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
	ID                uuid.UUID          `gorm:"type:uuid;primary_key" json:"id"`
	Title             string             `gorm:"size:255;not null" json:"title"`
	Description       string             `gorm:"type:text" json:"description"`
	Category          string             `gorm:"size:100" json:"category"`
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
	Collaborators     []GoalCollaborator `gorm:"foreignKey:GoalID" json:"collaborators,omitempty"`
	Metrics           []GoalMetric       `gorm:"foreignKey:GoalID" json:"metrics,omitempty"`
	Evidences         []Evidence         `gorm:"foreignKey:GoalID" json:"evidences,omitempty"`
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
	ID            uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	GoalID        uuid.UUID      `gorm:"type:uuid;index;not null" json:"goal_id"`
	Goal          *Goal          `gorm:"foreignKey:GoalID" json:"goal,omitempty"`
	Name          string         `gorm:"size:255;not null" json:"name"`
	MetricType    string         `gorm:"size:20;not null" json:"metric_type"`
	Unit          string         `gorm:"size:50" json:"unit"`
	BaselineValue float64        `gorm:"default:0" json:"baseline_value"`
	CurrentValue  float64        `gorm:"default:0" json:"current_value"`
	TargetValue   float64        `gorm:"not null" json:"target_value"`
	Weight        float64        `gorm:"default:1.0" json:"weight"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

func (m *GoalMetric) BeforeCreate(tx *gorm.DB) error {
	if m.ID == uuid.Nil {
		m.ID = uuid.New()
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

// Evidence is a file/document uploaded as proof of goal progress.
// Workflow state is tracked via CurrentStateID referencing the shared WorkflowState model.
type Evidence struct {
	ID              uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	GoalID          uuid.UUID      `gorm:"type:uuid;index;not null" json:"goal_id"`
	Goal            *Goal          `gorm:"foreignKey:GoalID" json:"goal,omitempty"`
	MetricID        *uuid.UUID     `gorm:"type:uuid;index" json:"metric_id"`
	Metric          *GoalMetric    `gorm:"foreignKey:MetricID" json:"metric,omitempty"`
	Title           string         `gorm:"size:255;not null" json:"title"`
	EvidenceType    string         `gorm:"size:50;not null;default:'Other'" json:"evidence_type"`
	Comment         string         `gorm:"type:text;not null" json:"comment"`
	Status          string         `gorm:"size:30;not null;default:'Draft'" json:"status"`
	DocumentaFileID string         `gorm:"size:255" json:"documenta_file_id"`
	FileName        string         `gorm:"size:255" json:"file_name"`
	FileSize        int64          `json:"file_size"`
	MimeType        string         `gorm:"size:100" json:"mime_type"`
	UploadedByID    uuid.UUID      `gorm:"type:uuid;index" json:"uploaded_by_id"`
	UploadedBy      *User          `gorm:"foreignKey:UploadedByID" json:"uploaded_by,omitempty"`
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
// REQUEST TYPES
// ════════════════════════════════════════════════════

type GoalCreateRequest struct {
	Title        string     `json:"title" validate:"required,max=255"`
	Description  string     `json:"description"`
	Category     string     `json:"category" validate:"max=100"`
	Priority     string     `json:"priority" validate:"required,oneof=Critical High Medium Low"`
	DepartmentID *uuid.UUID `json:"department_id"`
	OwnerID      uuid.UUID  `json:"owner_id" validate:"required"`
	StartDate    *time.Time `json:"start_date"`
	TargetDate   *time.Time `json:"target_date"`
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
	Priority     *string    `json:"priority" validate:"omitempty,oneof=Critical High Medium Low"`
	DepartmentID *uuid.UUID `json:"department_id"`
	OwnerID      *uuid.UUID `json:"owner_id"`
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
	Category     string     `query:"category"`
	Search       string     `query:"search"`
	StartFrom    *time.Time `query:"start_from"`
	StartTo      *time.Time `query:"start_to"`
	TargetFrom   *time.Time `query:"target_from"`
	TargetTo     *time.Time `query:"target_to"`
	SortBy       string     `query:"sort_by"`
	SortOrder    string     `query:"sort_order"`
}

type GoalMetricCreateRequest struct {
	Name          string  `json:"name" validate:"required,max=255"`
	MetricType    string  `json:"metric_type" validate:"required,oneof=Numeric Percentage Currency Boolean"`
	Unit          string  `json:"unit" validate:"max=50"`
	BaselineValue float64 `json:"baseline_value"`
	CurrentValue  float64 `json:"current_value"`
	TargetValue   float64 `json:"target_value" validate:"required"`
	Weight        float64 `json:"weight" validate:"gt=0"`
}

type GoalMetricUpdateRequest struct {
	Name          *string  `json:"name" validate:"omitempty,max=255"`
	MetricType    *string  `json:"metric_type" validate:"omitempty,oneof=Numeric Percentage Currency Boolean"`
	Unit          *string  `json:"unit" validate:"omitempty,max=50"`
	BaselineValue *float64 `json:"baseline_value"`
	TargetValue   *float64 `json:"target_value"`
	Weight        *float64 `json:"weight" validate:"omitempty,gt=0"`
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

type EvidenceFilter struct {
	Page         int    `query:"page"`
	Limit        int    `query:"limit"`
	Status       string `query:"status"`
	Search       string `query:"search"`
	EvidenceType string `query:"evidence_type"`
	StartDate    string `query:"start_date"`
	EndDate      string `query:"end_date"`
}

// ════════════════════════════════════════════════════
// RESPONSE TYPES
// ════════════════════════════════════════════════════

type GoalResponse struct {
	ID                uuid.UUID                  `json:"id"`
	Title             string                     `json:"title"`
	Description       string                     `json:"description"`
	Category          string                     `json:"category"`
	Priority          string                     `json:"priority"`
	Status            string                     `json:"status"`
	OwnerID           uuid.UUID                  `json:"owner_id"`
	Owner             *UserBriefResponse         `json:"owner,omitempty"`
	DepartmentID      *uuid.UUID                 `json:"department_id"`
	Department        *DepartmentBriefResponse   `json:"department,omitempty"`
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
	ID            uuid.UUID `json:"id"`
	GoalID        uuid.UUID `json:"goal_id"`
	Name          string    `json:"name"`
	MetricType    string    `json:"metric_type"`
	Unit          string    `json:"unit"`
	BaselineValue float64   `json:"baseline_value"`
	CurrentValue  float64   `json:"current_value"`
	TargetValue   float64   `json:"target_value"`
	Weight        float64   `json:"weight"`
	Progress      float64   `json:"progress"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
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

func (g *Goal) ToResponse() GoalResponse {
	resp := GoalResponse{
		ID:                g.ID,
		Title:             g.Title,
		Description:       g.Description,
		Category:          g.Category,
		Priority:          g.Priority,
		Status:            g.Status,
		OwnerID:           g.OwnerID,
		Owner:             ToUserBriefResponse(g.Owner),
		DepartmentID:      g.DepartmentID,
		Department:        ToDepartmentBriefResponse(g.Department),
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
		ID:            m.ID,
		GoalID:        m.GoalID,
		Name:          m.Name,
		MetricType:    m.MetricType,
		Unit:          m.Unit,
		BaselineValue: m.BaselineValue,
		CurrentValue:  m.CurrentValue,
		TargetValue:   m.TargetValue,
		Weight:        m.Weight,
		Progress:      progress,
		CreatedAt:     m.CreatedAt,
		UpdatedAt:     m.UpdatedAt,
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
