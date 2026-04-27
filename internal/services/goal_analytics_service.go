package services

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// GoalAnalyticsService provides analytics and reporting for goals
type GoalAnalyticsService interface {
	GetStats(ctx context.Context, departmentID *uuid.UUID, ownerScope *uuid.UUID) (*models.GoalStatsResponse, error)
	GetDistributions(ctx context.Context, departmentID *uuid.UUID, ownerScope *uuid.UUID) (*models.GoalDistributionsResponse, error)
	GetProgressSummary(ctx context.Context, departmentID *uuid.UUID, ownerScope *uuid.UUID) (*models.ProgressSummaryResponse, error)
	GetAtRiskGoals(ctx context.Context, departmentID *uuid.UUID, ownerScope *uuid.UUID, page, limit int) ([]models.AtRiskGoalResponse, int64, error)
	GetTrendData(ctx context.Context, departmentID *uuid.UUID, ownerScope *uuid.UUID, months int) (*models.TrendDataResponse, error)
	GetOKRTree(ctx context.Context, departmentID *uuid.UUID, ownerScope *uuid.UUID, periodStart *time.Time, periodEnd *time.Time, status string) (*models.OKRTreeResponse, error)
}

// applyOwnerScope restricts results to goals owned or collaborated on by the given user.
// If ownerScope is nil, no filter is added. Uses the "goals" table alias — callers must
// ensure their base query either uses that alias or no alias (GORM default).
func applyOwnerScope(q *gorm.DB, ownerScope *uuid.UUID) *gorm.DB {
	if ownerScope == nil {
		return q
	}
	return q.Where(
		"goals.owner_id = ? OR goals.id IN (SELECT goal_id FROM goal_collaborators WHERE user_id = ?)",
		*ownerScope, *ownerScope,
	)
}

type goalAnalyticsService struct {
	db *gorm.DB
}

// filteredOutKey is used to pass filtered-out parent goal IDs through ctx within GetOKRTree.
type filteredOutKey struct{}

// NewGoalAnalyticsService creates a new GoalAnalyticsService
func NewGoalAnalyticsService(db *gorm.DB) GoalAnalyticsService {
	return &goalAnalyticsService{db: db}
}

// GetStats returns aggregate goal counts by status plus overdue/at-risk counts
func (s *goalAnalyticsService) GetStats(ctx context.Context, departmentID *uuid.UUID, ownerScope *uuid.UUID) (*models.GoalStatsResponse, error) {
	stats := &models.GoalStatsResponse{}

	// Count by status
	type statusCount struct {
		Status string
		Count  int64
	}
	var counts []statusCount
	q := s.db.WithContext(ctx).
		Model(&models.Goal{}).
		Select("status, COUNT(*) as count")
	if departmentID != nil {
		q = q.Where("department_id = ?", *departmentID)
	}
	q = applyOwnerScope(q, ownerScope)
	err := q.Group("status").
		Scan(&counts).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get goal stats: %w", err)
	}

	for _, c := range counts {
		stats.Total += c.Count
		switch c.Status {
		case models.GoalStatusActive:
			stats.Active = c.Count
		case models.GoalStatusDraft:
			stats.Draft = c.Count
		case models.GoalStatusUnderReview:
			stats.UnderReview = c.Count
		case models.GoalStatusAchieved:
			stats.Achieved = c.Count
		case models.GoalStatusMissed:
			stats.Missed = c.Count
		case models.GoalStatusClosed:
			stats.Closed = c.Count
		}
	}

	// Count overdue goals (target_date in the past, status Active or Under_Review)
	overdueQuery := s.db.WithContext(ctx).
		Model(&models.Goal{}).
		Where("target_date < ? AND status IN ? AND deleted_at IS NULL", time.Now(), []string{models.GoalStatusActive, models.GoalStatusUnderReview})
	if departmentID != nil {
		overdueQuery = overdueQuery.Where("department_id = ?", *departmentID)
	}
	overdueQuery = applyOwnerScope(overdueQuery, ownerScope)
	err = overdueQuery.Count(&stats.Overdue).Error
	if err != nil {
		return nil, fmt.Errorf("failed to count overdue goals: %w", err)
	}

	// Count at-risk goals (latest check-in status is at_risk, behind, or blocked)
	atRiskSQL := `
		SELECT COUNT(DISTINCT g.id)
		FROM goals g
		INNER JOIN (
			SELECT DISTINCT ON (goal_id) goal_id, status
			FROM goal_check_ins
			WHERE deleted_at IS NULL
			ORDER BY goal_id, created_at DESC
		) latest_ci ON latest_ci.goal_id = g.id
		WHERE g.deleted_at IS NULL
		  AND g.status IN (?, ?)
		  AND latest_ci.status IN ('at_risk', 'behind', 'blocked')
	`
	atRiskArgs := []interface{}{models.GoalStatusActive, models.GoalStatusUnderReview}
	if departmentID != nil {
		atRiskSQL += " AND g.department_id = ?"
		atRiskArgs = append(atRiskArgs, *departmentID)
	}
	if ownerScope != nil {
		atRiskSQL += " AND (g.owner_id = ? OR g.id IN (SELECT goal_id FROM goal_collaborators WHERE user_id = ?))"
		atRiskArgs = append(atRiskArgs, *ownerScope, *ownerScope)
	}
	err = s.db.WithContext(ctx).Raw(atRiskSQL, atRiskArgs...).Scan(&stats.AtRisk).Error
	if err != nil {
		// Non-critical: at-risk count may fail if no check-ins exist
		stats.AtRisk = 0
	}

	return stats, nil
}

// GetDistributions returns goal distributions by status, priority, department, and category
func (s *goalAnalyticsService) GetDistributions(ctx context.Context, departmentID *uuid.UUID, ownerScope *uuid.UUID) (*models.GoalDistributionsResponse, error) {
	resp := &models.GoalDistributionsResponse{}
	baseQuery := s.db.WithContext(ctx).Model(&models.Goal{})
	if departmentID != nil {
		baseQuery = baseQuery.Where("department_id = ?", *departmentID)
	}

	statusColors := map[string]string{
		models.GoalStatusDraft:       "#94a3b8",
		models.GoalStatusActive:      "#3b82f6",
		models.GoalStatusUnderReview: "#f59e0b",
		models.GoalStatusAchieved:    "#22c55e",
		models.GoalStatusMissed:      "#ef4444",
		models.GoalStatusClosed:      "#6b7280",
	}

	priorityColors := map[string]string{
		models.GoalPriorityCritical: "#ef4444",
		models.GoalPriorityHigh:     "#f97316",
		models.GoalPriorityMedium:   "#f59e0b",
		models.GoalPriorityLow:      "#6b7280",
	}

	// By status
	type labelCount struct {
		Label string
		Value int64
	}
	var statusDist []labelCount
	q := s.db.WithContext(ctx).Model(&models.Goal{}).Select("status as label, COUNT(*) as value").Group("status")
	if departmentID != nil {
		q = q.Where("department_id = ?", *departmentID)
	}
	q = applyOwnerScope(q, ownerScope)
	if err := q.Scan(&statusDist).Error; err != nil {
		return nil, fmt.Errorf("failed to get status distribution: %w", err)
	}
	resp.ByStatus = make([]models.DistributionItem, len(statusDist))
	for i, d := range statusDist {
		resp.ByStatus[i] = models.DistributionItem{Label: d.Label, Value: d.Value, Color: statusColors[d.Label]}
	}

	// By priority
	var priorityDist []labelCount
	q = s.db.WithContext(ctx).Model(&models.Goal{}).Select("priority as label, COUNT(*) as value").Group("priority")
	if departmentID != nil {
		q = q.Where("department_id = ?", *departmentID)
	}
	q = applyOwnerScope(q, ownerScope)
	if err := q.Scan(&priorityDist).Error; err != nil {
		return nil, fmt.Errorf("failed to get priority distribution: %w", err)
	}
	resp.ByPriority = make([]models.DistributionItem, len(priorityDist))
	for i, d := range priorityDist {
		resp.ByPriority[i] = models.DistributionItem{Label: d.Label, Value: d.Value, Color: priorityColors[d.Label]}
	}

	// By department
	var deptDist []labelCount
	q = s.db.WithContext(ctx).
		Table("goals").
		Select("COALESCE(d.name, 'Unassigned') as label, COUNT(*) as value").
		Joins("LEFT JOIN departments d ON d.id = goals.department_id AND d.deleted_at IS NULL").
		Where("goals.deleted_at IS NULL")
	if departmentID != nil {
		q = q.Where("goals.department_id = ?", *departmentID)
	}
	q = applyOwnerScope(q, ownerScope)
	if err := q.Group("d.name").Order("value DESC").Limit(15).Scan(&deptDist).Error; err != nil {
		return nil, fmt.Errorf("failed to get department distribution: %w", err)
	}
	resp.ByDepartment = make([]models.DistributionItem, len(deptDist))
	for i, d := range deptDist {
		resp.ByDepartment[i] = models.DistributionItem{Label: d.Label, Value: d.Value}
	}

	// By category
	var catDist []labelCount
	q = s.db.WithContext(ctx).Model(&models.Goal{}).
		Select("COALESCE(NULLIF(category, ''), 'Uncategorized') as label, COUNT(*) as value").
		Group("label").Order("value DESC").Limit(15)
	if departmentID != nil {
		q = q.Where("department_id = ?", *departmentID)
	}
	q = applyOwnerScope(q, ownerScope)
	if err := q.Scan(&catDist).Error; err != nil {
		return nil, fmt.Errorf("failed to get category distribution: %w", err)
	}
	resp.ByCategory = make([]models.DistributionItem, len(catDist))
	for i, d := range catDist {
		resp.ByCategory[i] = models.DistributionItem{Label: d.Label, Value: d.Value}
	}

	return resp, nil
}

// GetProgressSummary returns average progress and progress range distribution
func (s *goalAnalyticsService) GetProgressSummary(ctx context.Context, departmentID *uuid.UUID, ownerScope *uuid.UUID) (*models.ProgressSummaryResponse, error) {
	resp := &models.ProgressSummaryResponse{}

	// Average progress (exclude Draft and Closed)
	avgQuery := s.db.WithContext(ctx).
		Model(&models.Goal{}).
		Where("status NOT IN ?", []string{models.GoalStatusDraft, models.GoalStatusClosed})
	if departmentID != nil {
		avgQuery = avgQuery.Where("department_id = ?", *departmentID)
	}
	avgQuery = applyOwnerScope(avgQuery, ownerScope)
	err := avgQuery.Select("COALESCE(AVG(progress), 0)").
		Scan(&resp.Average).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get average progress: %w", err)
	}
	resp.Average = math.Round(resp.Average*100) / 100

	// Progress ranges
	type rangeCount struct {
		RangeLabel string `gorm:"column:range_label"`
		Count      int64  `gorm:"column:count"`
	}
	var ranges []rangeCount
	// Note: GORM Raw uses parameterized SQL, not Sprintf. Single percent is
	// correct here — earlier '0-25%%' was treating the string as a format
	// directive and ended up with a literal double-percent label that never
	// matched the rangeColors lookup, so all ranges came back zero.
	rangeSQL := `
		SELECT
			CASE
				WHEN progress < 25 THEN '0-25%'
				WHEN progress < 50 THEN '25-50%'
				WHEN progress < 75 THEN '50-75%'
				ELSE '75-100%'
			END AS range_label,
			COUNT(*) AS count
		FROM goals
		WHERE deleted_at IS NULL
		  AND status NOT IN (?, ?)
	`
	rangeArgs := []interface{}{models.GoalStatusDraft, models.GoalStatusClosed}
	if departmentID != nil {
		rangeSQL += " AND department_id = ?"
		rangeArgs = append(rangeArgs, *departmentID)
	}
	if ownerScope != nil {
		rangeSQL += " AND (goals.owner_id = ? OR goals.id IN (SELECT goal_id FROM goal_collaborators WHERE user_id = ?))"
		rangeArgs = append(rangeArgs, *ownerScope, *ownerScope)
	}
	rangeSQL += " GROUP BY range_label ORDER BY range_label"
	err = s.db.WithContext(ctx).Raw(rangeSQL, rangeArgs...).Scan(&ranges).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get progress ranges: %w", err)
	}

	rangeColors := map[string]string{
		"0-25%":   "#ef4444",
		"25-50%":  "#f59e0b",
		"50-75%":  "#3b82f6",
		"75-100%": "#22c55e",
	}

	// Ensure all ranges present
	allRanges := []string{"0-25%", "25-50%", "50-75%", "75-100%"}
	rangeMap := make(map[string]int64)
	for _, r := range ranges {
		rangeMap[r.RangeLabel] = r.Count
	}
	resp.Ranges = make([]models.DistributionItem, len(allRanges))
	for i, label := range allRanges {
		resp.Ranges[i] = models.DistributionItem{
			Label: label,
			Value: rangeMap[label],
			Color: rangeColors[label],
		}
	}

	return resp, nil
}

// GetAtRiskGoals returns goals that are overdue or have at-risk check-in status
func (s *goalAnalyticsService) GetAtRiskGoals(ctx context.Context, departmentID *uuid.UUID, ownerScope *uuid.UUID, page, limit int) ([]models.AtRiskGoalResponse, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 50 {
		limit = 10
	}
	offset := (page - 1) * limit

	type atRiskRow struct {
		ID                uuid.UUID
		Title             string
		Status            string
		Priority          string
		Progress          float64
		TargetDate        *time.Time
		OwnerID           uuid.UUID
		OwnerEmail        string
		OwnerFirstName    string
		OwnerLastName     string
		OwnerAvatar       string
		DepartmentID      *uuid.UUID
		DepartmentName    *string
		DepartmentCode    *string
		LastCheckInStatus *string
		RiskReason        string
	}

	// Combined query: overdue OR at-risk check-in
	deptClause := ""
	var extraArgs []interface{}
	if departmentID != nil {
		deptClause = "AND g.department_id = ?"
		extraArgs = append(extraArgs, *departmentID)
	}
	ownerClause := ""
	if ownerScope != nil {
		ownerClause = "AND (g.owner_id = ? OR g.id IN (SELECT goal_id FROM goal_collaborators WHERE user_id = ?))"
		extraArgs = append(extraArgs, *ownerScope, *ownerScope)
	}

	query := fmt.Sprintf(`
		WITH latest_checkins AS (
			SELECT DISTINCT ON (goal_id) goal_id, status
			FROM goal_check_ins
			WHERE deleted_at IS NULL
			ORDER BY goal_id, created_at DESC
		)
		SELECT
			g.id, g.title, g.status, g.priority, g.progress, g.target_date,
			g.owner_id,
			u.email AS owner_email, u.first_name AS owner_first_name,
			u.last_name AS owner_last_name, u.avatar AS owner_avatar,
			g.department_id,
			d.name AS department_name, d.code AS department_code,
			lc.status AS last_check_in_status,
			CASE
				WHEN g.target_date < NOW() AND lc.status IN ('at_risk','behind','blocked') THEN 'overdue_and_at_risk'
				WHEN g.target_date < NOW() THEN 'overdue'
				WHEN lc.status IN ('at_risk','behind','blocked') THEN 'at_risk_checkin'
				ELSE 'unknown'
			END AS risk_reason
		FROM goals g
		LEFT JOIN users u ON u.id = g.owner_id
		LEFT JOIN departments d ON d.id = g.department_id AND d.deleted_at IS NULL
		LEFT JOIN latest_checkins lc ON lc.goal_id = g.id
		WHERE g.deleted_at IS NULL
		  AND g.status IN (?, ?)
		  %s
		  %s
		  AND (
			g.target_date < NOW()
			OR lc.status IN ('at_risk', 'behind', 'blocked')
		  )
		ORDER BY
			CASE WHEN g.target_date < NOW() THEN 0 ELSE 1 END,
			g.target_date ASC NULLS LAST
	`, deptClause, ownerClause)

	// Build args: status1, status2, [deptID]
	baseArgs := []interface{}{models.GoalStatusActive, models.GoalStatusUnderReview}
	baseArgs = append(baseArgs, extraArgs...)

	// Count total
	var total int64
	countQuery := fmt.Sprintf("SELECT COUNT(*) FROM (%s) sub", query)
	if err := s.db.WithContext(ctx).Raw(countQuery, baseArgs...).Scan(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count at-risk goals: %w", err)
	}

	// Fetch page
	var rows []atRiskRow
	pagedQuery := fmt.Sprintf("%s LIMIT ? OFFSET ?", query)
	pagedArgs := append(baseArgs, limit, offset)
	if err := s.db.WithContext(ctx).Raw(pagedQuery, pagedArgs...).Scan(&rows).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to get at-risk goals: %w", err)
	}

	now := time.Now()
	results := make([]models.AtRiskGoalResponse, len(rows))
	for i, r := range rows {
		var daysOverdue int
		if r.TargetDate != nil && r.TargetDate.Before(now) {
			daysOverdue = int(now.Sub(*r.TargetDate).Hours() / 24)
		}

		var lastCheckIn string
		if r.LastCheckInStatus != nil {
			lastCheckIn = *r.LastCheckInStatus
		}

		result := models.AtRiskGoalResponse{
			ID:                r.ID,
			Title:             r.Title,
			Status:            r.Status,
			Priority:          r.Priority,
			Progress:          r.Progress,
			TargetDate:        r.TargetDate,
			LastCheckInStatus: lastCheckIn,
			DaysOverdue:       daysOverdue,
			RiskReason:        r.RiskReason,
			Owner: &models.UserBriefResponse{
				ID:        r.OwnerID,
				Email:     r.OwnerEmail,
				FirstName: r.OwnerFirstName,
				LastName:  r.OwnerLastName,
				Avatar:    r.OwnerAvatar,
			},
		}

		if r.DepartmentID != nil && r.DepartmentName != nil {
			code := ""
			if r.DepartmentCode != nil {
				code = *r.DepartmentCode
			}
			result.Department = &models.DepartmentBriefResponse{
				ID:   *r.DepartmentID,
				Name: *r.DepartmentName,
				Code: code,
			}
		}

		results[i] = result
	}

	return results, total, nil
}

// GetTrendData returns monthly goal creation and completion counts
func (s *goalAnalyticsService) GetTrendData(ctx context.Context, departmentID *uuid.UUID, ownerScope *uuid.UUID, months int) (*models.TrendDataResponse, error) {
	if months < 1 || months > 24 {
		months = 12
	}

	startDate := time.Now().AddDate(0, -months, 0)

	// Created per month
	type monthCount struct {
		Month string
		Count int64
	}

	createdSQL := `
		SELECT TO_CHAR(DATE_TRUNC('month', created_at), 'YYYY-MM') AS month, COUNT(*) AS count
		FROM goals
		WHERE deleted_at IS NULL AND created_at >= ?
	`
	createdArgs := []interface{}{startDate}
	if departmentID != nil {
		createdSQL += " AND department_id = ?"
		createdArgs = append(createdArgs, *departmentID)
	}
	if ownerScope != nil {
		createdSQL += " AND (goals.owner_id = ? OR goals.id IN (SELECT goal_id FROM goal_collaborators WHERE user_id = ?))"
		createdArgs = append(createdArgs, *ownerScope, *ownerScope)
	}
	createdSQL += " GROUP BY DATE_TRUNC('month', created_at) ORDER BY month"

	var created []monthCount
	err := s.db.WithContext(ctx).Raw(createdSQL, createdArgs...).Scan(&created).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get created trend: %w", err)
	}

	// Completed per month (achieved or closed, by updated_at)
	completedSQL := `
		SELECT TO_CHAR(DATE_TRUNC('month', updated_at), 'YYYY-MM') AS month, COUNT(*) AS count
		FROM goals
		WHERE deleted_at IS NULL
		  AND status IN (?, ?)
		  AND updated_at >= ?
	`
	completedArgs := []interface{}{models.GoalStatusAchieved, models.GoalStatusClosed, startDate}
	if departmentID != nil {
		completedSQL += " AND department_id = ?"
		completedArgs = append(completedArgs, *departmentID)
	}
	if ownerScope != nil {
		completedSQL += " AND (goals.owner_id = ? OR goals.id IN (SELECT goal_id FROM goal_collaborators WHERE user_id = ?))"
		completedArgs = append(completedArgs, *ownerScope, *ownerScope)
	}
	completedSQL += " GROUP BY DATE_TRUNC('month', updated_at) ORDER BY month"

	var completed []monthCount
	err = s.db.WithContext(ctx).Raw(completedSQL, completedArgs...).Scan(&completed).Error
	if err != nil {
		return nil, fmt.Errorf("failed to get completed trend: %w", err)
	}

	// Merge into ordered points
	createdMap := make(map[string]int64)
	for _, c := range created {
		createdMap[c.Month] = c.Count
	}
	completedMap := make(map[string]int64)
	for _, c := range completed {
		completedMap[c.Month] = c.Count
	}

	// Generate all months in range
	var points []models.TrendPoint
	current := time.Date(startDate.Year(), startDate.Month(), 1, 0, 0, 0, 0, time.UTC)
	now := time.Now()
	for current.Before(now) || current.Month() == now.Month() && current.Year() == now.Year() {
		key := current.Format("2006-01")
		points = append(points, models.TrendPoint{
			Month:     key,
			Created:   createdMap[key],
			Completed: completedMap[key],
		})
		current = current.AddDate(0, 1, 0)
	}

	return &models.TrendDataResponse{Points: points}, nil
}

// GetOKRTree returns the full OKR alignment tree organized by department hierarchy
func (s *goalAnalyticsService) GetOKRTree(ctx context.Context, departmentID *uuid.UUID, ownerScope *uuid.UUID, periodStart *time.Time, periodEnd *time.Time, status string) (*models.OKRTreeResponse, error) {
	// Load departments
	var departments []models.Department
	deptQuery := s.db.WithContext(ctx).Where("is_active = true").Order("sort_order, name")
	if departmentID != nil {
		deptQuery = deptQuery.Where("id = ? OR path LIKE ?", *departmentID, fmt.Sprintf("%%%s%%", departmentID.String()))
	}
	if err := deptQuery.Find(&departments).Error; err != nil {
		return nil, fmt.Errorf("failed to load departments: %w", err)
	}

	// Load goals with filters
	goalQuery := s.db.WithContext(ctx).
		Model(&models.Goal{}).
		Preload("Owner").
		Preload("Metrics").
		Where("department_id IS NOT NULL")

	if departmentID != nil {
		deptIDs := make([]uuid.UUID, len(departments))
		for i, d := range departments {
			deptIDs[i] = d.ID
		}
		goalQuery = goalQuery.Where("department_id IN ?", deptIDs)
	}
	goalQuery = applyOwnerScope(goalQuery, ownerScope)
	if status != "" {
		goalQuery = goalQuery.Where("status = ?", status)
	}
	if periodStart != nil {
		goalQuery = goalQuery.Where("(start_date >= ? OR start_date IS NULL)", *periodStart)
	}
	if periodEnd != nil {
		goalQuery = goalQuery.Where("(target_date <= ? OR target_date IS NULL)", *periodEnd)
	}

	var goals []models.Goal
	if err := goalQuery.Order("level, title").Find(&goals).Error; err != nil {
		return nil, fmt.Errorf("failed to load goals: %w", err)
	}

	// Load any parent goals that were filtered out but are needed to preserve hierarchy.
	// These appear as context nodes (IsFilteredOut=true) in the tree.
	matchedIDs := make(map[uuid.UUID]bool, len(goals))
	for _, g := range goals {
		matchedIDs[g.ID] = true
	}
	missingParentIDs := make(map[uuid.UUID]bool)
	for _, g := range goals {
		if g.ParentGoalID != nil && !matchedIDs[*g.ParentGoalID] {
			missingParentIDs[*g.ParentGoalID] = true
		}
	}
	// Recursively resolve ancestors so full chain is included
	for len(missingParentIDs) > 0 {
		ids := make([]uuid.UUID, 0, len(missingParentIDs))
		for id := range missingParentIDs {
			ids = append(ids, id)
		}
		var ancestors []models.Goal
		if err := s.db.WithContext(ctx).
			Preload("Owner").
			Preload("Metrics").
			Where("id IN ?", ids).
			Find(&ancestors).Error; err != nil {
			break
		}
		filteredOutIDs := make(map[uuid.UUID]bool, len(ancestors))
		for i := range ancestors {
			filteredOutIDs[ancestors[i].ID] = true
			goals = append(goals, ancestors[i])
			matchedIDs[ancestors[i].ID] = true
		}
		// Mark these for post-processing (IsFilteredOut flag)
		if ctx.Value(filteredOutKey{}) == nil {
			ctx = context.WithValue(ctx, filteredOutKey{}, filteredOutIDs)
		} else {
			existing := ctx.Value(filteredOutKey{}).(map[uuid.UUID]bool)
			for id := range filteredOutIDs {
				existing[id] = true
			}
		}
		// Find next level of missing parents
		missingParentIDs = make(map[uuid.UUID]bool)
		for _, a := range ancestors {
			if a.ParentGoalID != nil && !matchedIDs[*a.ParentGoalID] {
				missingParentIDs[*a.ParentGoalID] = true
			}
		}
	}

	filteredOutIDs, _ := ctx.Value(filteredOutKey{}).(map[uuid.UUID]bool)
	if filteredOutIDs == nil {
		filteredOutIDs = map[uuid.UUID]bool{}
	}

	// Load latest check-in status for each goal
	type checkInInfo struct {
		GoalID uuid.UUID
		Status string
	}
	var checkIns []checkInInfo
	goalIDs := make([]uuid.UUID, len(goals))
	for i, g := range goals {
		goalIDs[i] = g.ID
	}
	if len(goalIDs) > 0 {
		s.db.WithContext(ctx).Raw(`
			SELECT DISTINCT ON (goal_id) goal_id, status
			FROM goal_check_ins
			WHERE deleted_at IS NULL AND goal_id IN ?
			ORDER BY goal_id, created_at DESC
		`, goalIDs).Scan(&checkIns)
	}
	checkInMap := make(map[uuid.UUID]string)
	for _, ci := range checkIns {
		checkInMap[ci.GoalID] = ci.Status
	}

	// Build goal nodes indexed by ID and by department
	goalNodeMap := make(map[uuid.UUID]*models.OKRGoalNode)
	goalsByDept := make(map[uuid.UUID][]models.OKRGoalNode)
	now := time.Now()

	// First pass: create all nodes
	for _, g := range goals {
		health := "on_track"
		if ciStatus, ok := checkInMap[g.ID]; ok {
			if ciStatus == "at_risk" {
				health = "at_risk"
			} else if ciStatus == "behind" || ciStatus == "blocked" {
				health = "behind"
			}
		}
		if g.TargetDate != nil && g.TargetDate.Before(now) && g.Status != models.GoalStatusAchieved && g.Status != models.GoalStatusClosed {
			health = "behind"
		}

		var metricSummary string
		if len(g.Metrics) > 0 {
			metricSummary = fmt.Sprintf("%d metrics", len(g.Metrics))
		}

		var owner *models.UserBriefResponse
		if g.Owner != nil {
			owner = &models.UserBriefResponse{
				ID:        g.Owner.ID,
				Email:     g.Owner.Email,
				FirstName: g.Owner.FirstName,
				LastName:  g.Owner.LastName,
				Avatar:    g.Owner.Avatar,
			}
		}

		node := models.OKRGoalNode{
			ID:            g.ID,
			Title:         g.Title,
			Status:        g.Status,
			Priority:      g.Priority,
			Progress:      g.Progress,
			Owner:         owner,
			TargetDate:    g.TargetDate,
			Level:         g.Level,
			MetricSummary: metricSummary,
			Health:        health,
			IsFilteredOut: filteredOutIDs[g.ID],
		}
		goalNodeMap[g.ID] = &node
	}

	// Second pass: nest children under parents, collect root goals per department
	for _, g := range goals {
		node := goalNodeMap[g.ID]
		if g.ParentGoalID != nil {
			if parent, ok := goalNodeMap[*g.ParentGoalID]; ok {
				parent.Children = append(parent.Children, *node)
				continue
			}
		}
		// Root goal (no parent or parent not in result set)
		if g.DepartmentID != nil {
			goalsByDept[*g.DepartmentID] = append(goalsByDept[*g.DepartmentID], *node)
		}
	}

	// Build department tree
	deptMap := make(map[uuid.UUID]*models.OKRDepartmentNode)
	var rootDepts []models.OKRDepartmentNode

	for _, d := range departments {
		deptGoals := goalsByDept[d.ID]
		var avgProgress float64
		if len(deptGoals) > 0 {
			var sum float64
			for _, g := range deptGoals {
				sum += g.Progress
			}
			avgProgress = math.Round(sum/float64(len(deptGoals))*100) / 100
		}

		node := models.OKRDepartmentNode{
			ID:              d.ID,
			Name:            d.Name,
			Code:            d.Code,
			Level:           d.Level,
			GoalCount:       len(deptGoals),
			AverageProgress: avgProgress,
			Goals:           deptGoals,
		}
		deptMap[d.ID] = &node
	}

	// Nest department children
	for _, d := range departments {
		node := deptMap[d.ID]
		if d.ParentID != nil {
			if parent, ok := deptMap[*d.ParentID]; ok {
				parent.Children = append(parent.Children, *node)
				parent.GoalCount += node.GoalCount
				continue
			}
		}
		rootDepts = append(rootDepts, *node)
	}

	// Filter out departments with no goals (and no children with goals)
	filtered := filterNonEmptyDepts(rootDepts)

	return &models.OKRTreeResponse{
		Departments: filtered,
		TotalGoals:  len(goals),
	}, nil
}

// filterNonEmptyDepts recursively removes departments with no goals
func filterNonEmptyDepts(depts []models.OKRDepartmentNode) []models.OKRDepartmentNode {
	var result []models.OKRDepartmentNode
	for _, d := range depts {
		d.Children = filterNonEmptyDepts(d.Children)
		if d.GoalCount > 0 || len(d.Children) > 0 {
			result = append(result, d)
		}
	}
	return result
}
