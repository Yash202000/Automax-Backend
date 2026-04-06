package models

import (
	"time"

	"github.com/google/uuid"
)

// ════════════════════════════════════════════════════
// ANALYTICS RESPONSE TYPES
// ════════════════════════════════════════════════════

// GoalStatsResponse contains aggregate goal counts
type GoalStatsResponse struct {
	Total       int64 `json:"total"`
	Active      int64 `json:"active"`
	Draft       int64 `json:"draft"`
	UnderReview int64 `json:"under_review"`
	Achieved    int64 `json:"achieved"`
	Missed      int64 `json:"missed"`
	Closed      int64 `json:"closed"`
	Overdue     int64 `json:"overdue"`
	AtRisk      int64 `json:"at_risk"`
}

// DistributionItem represents a single label-value pair for charts
type DistributionItem struct {
	Label string `json:"label"`
	Value int64  `json:"value"`
	Color string `json:"color,omitempty"`
}

// GoalDistributionsResponse contains goal distributions by various dimensions
type GoalDistributionsResponse struct {
	ByStatus     []DistributionItem `json:"by_status"`
	ByPriority   []DistributionItem `json:"by_priority"`
	ByDepartment []DistributionItem `json:"by_department"`
	ByCategory   []DistributionItem `json:"by_category"`
}

// ProgressSummaryResponse contains progress distribution data
type ProgressSummaryResponse struct {
	Average float64            `json:"average"`
	Ranges  []DistributionItem `json:"ranges"`
}

// AtRiskGoalResponse represents a goal that is at risk or overdue
type AtRiskGoalResponse struct {
	ID                uuid.UUID                `json:"id"`
	Title             string                   `json:"title"`
	Status            string                   `json:"status"`
	Priority          string                   `json:"priority"`
	Progress          float64                  `json:"progress"`
	TargetDate        *time.Time               `json:"target_date"`
	Owner             *UserBriefResponse       `json:"owner"`
	Department        *DepartmentBriefResponse `json:"department"`
	LastCheckInStatus string                   `json:"last_check_in_status"`
	DaysOverdue       int                      `json:"days_overdue"`
	RiskReason        string                   `json:"risk_reason"`
}

// TrendPoint represents a single month data point for trend charts
type TrendPoint struct {
	Month     string `json:"month"`
	Created   int64  `json:"created"`
	Completed int64  `json:"completed"`
}

// TrendDataResponse contains monthly trend data
type TrendDataResponse struct {
	Points []TrendPoint `json:"points"`
}

// ════════════════════════════════════════════════════
// OKR TREE RESPONSE TYPES
// ════════════════════════════════════════════════════

// OKRTreeResponse is the top-level OKR alignment response
type OKRTreeResponse struct {
	Departments []OKRDepartmentNode `json:"departments"`
	TotalGoals  int                 `json:"total_goals"`
}

// OKRDepartmentNode represents a department in the OKR tree
type OKRDepartmentNode struct {
	ID              uuid.UUID           `json:"id"`
	Name            string              `json:"name"`
	Code            string              `json:"code"`
	Level           int                 `json:"level"`
	GoalCount       int                 `json:"goal_count"`
	AverageProgress float64             `json:"average_progress"`
	Goals           []OKRGoalNode       `json:"goals"`
	Children        []OKRDepartmentNode `json:"children,omitempty"`
}

// OKRGoalNode represents a goal in the OKR tree
type OKRGoalNode struct {
	ID            uuid.UUID          `json:"id"`
	Title         string             `json:"title"`
	Status        string             `json:"status"`
	Priority      string             `json:"priority"`
	Progress      float64            `json:"progress"`
	Owner         *UserBriefResponse `json:"owner"`
	TargetDate    *time.Time         `json:"target_date"`
	Level         int                `json:"level"`
	MetricSummary string             `json:"metric_summary"`
	Health        string             `json:"health"`
	Children      []OKRGoalNode      `json:"children,omitempty"`
}
