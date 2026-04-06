package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ════════════════════════════════════════════════════
// REVIEW CYCLE STATUS CONSTANTS
// ════════════════════════════════════════════════════

const (
	ReviewCycleStatusDraft     = "draft"
	ReviewCycleStatusActive    = "active"
	ReviewCycleStatusCompleted = "completed"
	ReviewCycleStatusArchived  = "archived"
)

const (
	ReviewAssignmentStatusPending    = "pending"
	ReviewAssignmentStatusInProgress = "in_progress"
	ReviewAssignmentStatusCompleted  = "completed"
)

// ════════════════════════════════════════════════════
// DATABASE MODELS
// ════════════════════════════════════════════════════

// ReviewCycle represents a performance review period
type ReviewCycle struct {
	ID           uuid.UUID          `gorm:"type:uuid;primary_key" json:"id"`
	Title        string             `gorm:"size:255;not null" json:"title"`
	Description  string             `gorm:"type:text" json:"description"`
	PeriodStart  time.Time          `gorm:"not null" json:"period_start"`
	PeriodEnd    time.Time          `gorm:"not null" json:"period_end"`
	Status       string             `gorm:"size:30;not null;default:'draft'" json:"status"`
	DepartmentID *uuid.UUID         `gorm:"type:uuid;index" json:"department_id"`
	Department   *Department        `gorm:"foreignKey:DepartmentID" json:"department,omitempty"`
	CreatedByID  uuid.UUID          `gorm:"type:uuid;index;not null" json:"created_by_id"`
	CreatedBy    *User              `gorm:"foreignKey:CreatedByID" json:"created_by,omitempty"`
	Assignments  []ReviewAssignment `gorm:"foreignKey:CycleID" json:"assignments,omitempty"`
	CreatedAt    time.Time          `json:"created_at"`
	UpdatedAt    time.Time          `json:"updated_at"`
	DeletedAt    gorm.DeletedAt     `gorm:"index" json:"-"`
}

func (c *ReviewCycle) BeforeCreate(tx *gorm.DB) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return nil
}

// ReviewAssignment links an employee to a review cycle with a reviewer
type ReviewAssignment struct {
	ID            uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	CycleID       uuid.UUID      `gorm:"type:uuid;index;not null" json:"cycle_id"`
	Cycle         *ReviewCycle   `gorm:"foreignKey:CycleID" json:"cycle,omitempty"`
	EmployeeID    uuid.UUID      `gorm:"type:uuid;index;not null" json:"employee_id"`
	Employee      *User          `gorm:"foreignKey:EmployeeID" json:"employee,omitempty"`
	ReviewerID    uuid.UUID      `gorm:"type:uuid;index;not null" json:"reviewer_id"`
	Reviewer      *User          `gorm:"foreignKey:ReviewerID" json:"reviewer,omitempty"`
	Status        string         `gorm:"size:30;not null;default:'pending'" json:"status"`
	OverallRating *float64       `json:"overall_rating"`
	Comments      string         `gorm:"type:text" json:"comments"`
	CompletedAt   *time.Time     `json:"completed_at"`
	GoalScores    []GoalScore    `gorm:"foreignKey:AssignmentID" json:"goal_scores,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}

func (a *ReviewAssignment) BeforeCreate(tx *gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}

// GoalScore records the rating for a specific goal within a review
type GoalScore struct {
	ID             uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	AssignmentID   uuid.UUID `gorm:"type:uuid;index;not null;uniqueIndex:idx_assignment_goal" json:"assignment_id"`
	GoalID         uuid.UUID `gorm:"type:uuid;index;not null;uniqueIndex:idx_assignment_goal" json:"goal_id"`
	Goal           *Goal     `gorm:"foreignKey:GoalID" json:"goal,omitempty"`
	Weight         float64   `gorm:"default:1.0" json:"weight"`
	AchievementPct float64   `json:"achievement_pct"`
	Rating         *float64  `json:"rating"`
	Comments       string    `gorm:"type:text" json:"comments"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (s *GoalScore) BeforeCreate(tx *gorm.DB) error {
	if s.ID == uuid.Nil {
		s.ID = uuid.New()
	}
	return nil
}

// ════════════════════════════════════════════════════
// REQUEST TYPES
// ════════════════════════════════════════════════════

type ReviewCycleCreateRequest struct {
	Title        string     `json:"title" validate:"required,max=255"`
	Description  string     `json:"description"`
	PeriodStart  time.Time  `json:"period_start" validate:"required"`
	PeriodEnd    time.Time  `json:"period_end" validate:"required"`
	DepartmentID *uuid.UUID `json:"department_id"`
}

type ReviewCycleUpdateRequest struct {
	Title        *string    `json:"title" validate:"omitempty,max=255"`
	Description  *string    `json:"description"`
	PeriodStart  *time.Time `json:"period_start"`
	PeriodEnd    *time.Time `json:"period_end"`
	DepartmentID *uuid.UUID `json:"department_id"`
}

type ReviewAssignmentCreateRequest struct {
	EmployeeID uuid.UUID `json:"employee_id" validate:"required"`
	ReviewerID uuid.UUID `json:"reviewer_id" validate:"required"`
}

type BulkAssignRequest struct {
	Assignments []ReviewAssignmentCreateRequest `json:"assignments" validate:"required,min=1"`
}

type GoalScoreUpdateRequest struct {
	GoalID   uuid.UUID `json:"goal_id" validate:"required"`
	Weight   float64   `json:"weight"`
	Rating   *float64  `json:"rating"`
	Comments string    `json:"comments"`
}

type ReviewSubmitRequest struct {
	OverallRating float64                  `json:"overall_rating" validate:"required,min=1,max=5"`
	Comments      string                   `json:"comments"`
	GoalScores    []GoalScoreUpdateRequest `json:"goal_scores"`
}

// ════════════════════════════════════════════════════
// RESPONSE TYPES
// ════════════════════════════════════════════════════

type ReviewCycleResponse struct {
	ID              uuid.UUID                `json:"id"`
	Title           string                   `json:"title"`
	Description     string                   `json:"description"`
	PeriodStart     time.Time                `json:"period_start"`
	PeriodEnd       time.Time                `json:"period_end"`
	Status          string                   `json:"status"`
	DepartmentID    *uuid.UUID               `json:"department_id"`
	Department      *DepartmentBriefResponse `json:"department,omitempty"`
	CreatedByID     uuid.UUID                `json:"created_by_id"`
	CreatedBy       *UserBriefResponse       `json:"created_by,omitempty"`
	AssignmentCount int                      `json:"assignment_count"`
	CompletedCount  int                      `json:"completed_count"`
	CreatedAt       time.Time                `json:"created_at"`
	UpdatedAt       time.Time                `json:"updated_at"`
}

type ReviewAssignmentResponse struct {
	ID            uuid.UUID          `json:"id"`
	CycleID       uuid.UUID          `json:"cycle_id"`
	Cycle         *ReviewCycleBrief  `json:"cycle,omitempty"`
	EmployeeID    uuid.UUID          `json:"employee_id"`
	Employee      *UserBriefResponse `json:"employee,omitempty"`
	ReviewerID    uuid.UUID          `json:"reviewer_id"`
	Reviewer      *UserBriefResponse `json:"reviewer,omitempty"`
	Status        string             `json:"status"`
	OverallRating *float64           `json:"overall_rating"`
	Comments      string             `json:"comments"`
	CompletedAt   *time.Time         `json:"completed_at"`
	GoalScores    []GoalScoreResponse `json:"goal_scores,omitempty"`
	CreatedAt     time.Time          `json:"created_at"`
	UpdatedAt     time.Time          `json:"updated_at"`
}

type ReviewCycleBrief struct {
	ID          uuid.UUID `json:"id"`
	Title       string    `json:"title"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	Status      string    `json:"status"`
}

type GoalScoreResponse struct {
	ID             uuid.UUID          `json:"id"`
	GoalID         uuid.UUID          `json:"goal_id"`
	Goal           *GoalBriefResponse `json:"goal,omitempty"`
	Weight         float64            `json:"weight"`
	AchievementPct float64            `json:"achievement_pct"`
	Rating         *float64           `json:"rating"`
	Comments       string             `json:"comments"`
}

// ════════════════════════════════════════════════════
// CONVERTERS
// ════════════════════════════════════════════════════

func (c *ReviewCycle) ToResponse() ReviewCycleResponse {
	resp := ReviewCycleResponse{
		ID:          c.ID,
		Title:       c.Title,
		Description: c.Description,
		PeriodStart: c.PeriodStart,
		PeriodEnd:   c.PeriodEnd,
		Status:      c.Status,
		DepartmentID: c.DepartmentID,
		CreatedByID: c.CreatedByID,
		CreatedAt:   c.CreatedAt,
		UpdatedAt:   c.UpdatedAt,
	}

	if c.Department != nil {
		resp.Department = &DepartmentBriefResponse{
			ID:   c.Department.ID,
			Name: c.Department.Name,
			Code: c.Department.Code,
		}
	}

	if c.CreatedBy != nil {
		resp.CreatedBy = &UserBriefResponse{
			ID:        c.CreatedBy.ID,
			Email:     c.CreatedBy.Email,
			FirstName: c.CreatedBy.FirstName,
			LastName:  c.CreatedBy.LastName,
			Avatar:    c.CreatedBy.Avatar,
		}
	}

	if len(c.Assignments) > 0 {
		resp.AssignmentCount = len(c.Assignments)
		for _, a := range c.Assignments {
			if a.Status == ReviewAssignmentStatusCompleted {
				resp.CompletedCount++
			}
		}
	}

	return resp
}

func (a *ReviewAssignment) ToResponse() ReviewAssignmentResponse {
	resp := ReviewAssignmentResponse{
		ID:            a.ID,
		CycleID:       a.CycleID,
		EmployeeID:    a.EmployeeID,
		ReviewerID:    a.ReviewerID,
		Status:        a.Status,
		OverallRating: a.OverallRating,
		Comments:      a.Comments,
		CompletedAt:   a.CompletedAt,
		CreatedAt:     a.CreatedAt,
		UpdatedAt:     a.UpdatedAt,
	}

	if a.Cycle != nil {
		resp.Cycle = &ReviewCycleBrief{
			ID:          a.Cycle.ID,
			Title:       a.Cycle.Title,
			PeriodStart: a.Cycle.PeriodStart,
			PeriodEnd:   a.Cycle.PeriodEnd,
			Status:      a.Cycle.Status,
		}
	}

	if a.Employee != nil {
		resp.Employee = &UserBriefResponse{
			ID:        a.Employee.ID,
			Email:     a.Employee.Email,
			FirstName: a.Employee.FirstName,
			LastName:  a.Employee.LastName,
			Avatar:    a.Employee.Avatar,
		}
	}

	if a.Reviewer != nil {
		resp.Reviewer = &UserBriefResponse{
			ID:        a.Reviewer.ID,
			Email:     a.Reviewer.Email,
			FirstName: a.Reviewer.FirstName,
			LastName:  a.Reviewer.LastName,
			Avatar:    a.Reviewer.Avatar,
		}
	}

	if len(a.GoalScores) > 0 {
		resp.GoalScores = make([]GoalScoreResponse, len(a.GoalScores))
		for i, s := range a.GoalScores {
			resp.GoalScores[i] = s.ToResponse()
		}
	}

	return resp
}

func (s *GoalScore) ToResponse() GoalScoreResponse {
	resp := GoalScoreResponse{
		ID:             s.ID,
		GoalID:         s.GoalID,
		Weight:         s.Weight,
		AchievementPct: s.AchievementPct,
		Rating:         s.Rating,
		Comments:       s.Comments,
	}

	if s.Goal != nil {
		resp.Goal = &GoalBriefResponse{
			ID:       s.Goal.ID,
			Title:    s.Goal.Title,
			Status:   s.Goal.Status,
			Priority: s.Goal.Priority,
			Progress: s.Goal.Progress,
			Level:    s.Goal.Level,
		}
	}

	return resp
}
