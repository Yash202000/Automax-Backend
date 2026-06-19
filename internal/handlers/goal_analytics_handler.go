package handlers

import (
	"strconv"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// analyticsScopeFromCtx derives both department and owner filters from the caller:
//   - super admin → no filters (full org)
//   - has goals:delete OR goals:approve → department filter as before, no owner filter
//   - else → scoped to own goals + collaborations (ownerID = current user); no dept filter
//
// Any explicit department_id query string override still wins (for admins / reviewers).
func analyticsScopeFromCtx(c *fiber.Ctx) (*uuid.UUID, *uuid.UUID, error) {
	if deptStr := c.Query("department_id"); deptStr != "" {
		id, err := uuid.Parse(deptStr)
		if err != nil {
			return nil, nil, err
		}
		return &id, nil, nil
	}
	user, _ := c.Locals(constants.ContextKeys.User).(*models.User)
	if user == nil || user.IsSuperAdmin {
		return nil, nil, nil
	}
	// Admin-like goal users keep the existing department-scoped behaviour.
	if userHasAny(user, "goals:delete", "goals:approve") {
		return user.DepartmentID, nil, nil
	}
	// Plain contributors: scope to their own goals + collaborations.
	return nil, &user.ID, nil
}

// userHasAny returns true if any of the given permission codes appears in the
// user's role-permission graph.
func userHasAny(u *models.User, codes ...string) bool {
	wanted := map[string]struct{}{}
	for _, c := range codes {
		wanted[c] = struct{}{}
	}
	for _, role := range u.Roles {
		for _, p := range role.Permissions {
			code := p.Module + ":" + p.Action
			if _, ok := wanted[code]; ok {
				return true
			}
		}
	}
	return false
}

// GoalAnalyticsHandler handles goal analytics endpoints
type GoalAnalyticsHandler struct {
	service services.GoalAnalyticsService
}

// NewGoalAnalyticsHandler creates a new GoalAnalyticsHandler
func NewGoalAnalyticsHandler(service services.GoalAnalyticsService) *GoalAnalyticsHandler {
	return &GoalAnalyticsHandler{service: service}
}

// GetGoalStats returns aggregate goal counts by status
func (h *GoalAnalyticsHandler) GetGoalStats(c *fiber.Ctx) error {
	deptFilter, ownerFilter, err := analyticsScopeFromCtx(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": i18n.T(c.UserContext(), "invalid_department_id")})
	}

	stats, err := h.service.GetStats(c.Context(), deptFilter, ownerFilter)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get goal statistics",
		})
	}
	return c.JSON(fiber.Map{"success": true, "data": stats})
}

// GetDistributions returns goal distributions by status, priority, department, category
func (h *GoalAnalyticsHandler) GetDistributions(c *fiber.Ctx) error {
	departmentID, ownerFilter, deptErr := analyticsScopeFromCtx(c)
	if deptErr != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": i18n.T(c.UserContext(), "invalid_department_id")})
	}

	dist, err := h.service.GetDistributions(c.Context(), departmentID, ownerFilter)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get goal distributions",
		})
	}
	return c.JSON(fiber.Map{"success": true, "data": dist})
}

// GetProgressSummary returns progress distribution data
func (h *GoalAnalyticsHandler) GetProgressSummary(c *fiber.Ctx) error {
	deptFilter, ownerFilter, err := analyticsScopeFromCtx(c)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": i18n.T(c.UserContext(), "invalid_department_id")})
	}

	summary, err := h.service.GetProgressSummary(c.Context(), deptFilter, ownerFilter)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get progress summary",
		})
	}
	return c.JSON(fiber.Map{"success": true, "data": summary})
}

// GetAtRiskGoals returns paginated list of at-risk goals
func (h *GoalAnalyticsHandler) GetAtRiskGoals(c *fiber.Ctx) error {
	deptFilter, ownerFilter, deptErr := analyticsScopeFromCtx(c)
	if deptErr != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": i18n.T(c.UserContext(), "invalid_department_id")})
	}

	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))

	goals, total, err := h.service.GetAtRiskGoals(c.Context(), deptFilter, ownerFilter, page, limit)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get at-risk goals",
		})
	}

	totalPages := int64(0)
	if total > 0 {
		totalPages = (total + int64(limit) - 1) / int64(limit)
	}

	return c.JSON(fiber.Map{
		"success":     true,
		"data":        goals,
		"total":       total,
		"page":        page,
		"limit":       limit,
		"total_pages": totalPages,
	})
}

// GetTrendData returns monthly goal creation/completion trend
func (h *GoalAnalyticsHandler) GetTrendData(c *fiber.Ctx) error {
	deptFilter, ownerFilter, deptErr := analyticsScopeFromCtx(c)
	if deptErr != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": i18n.T(c.UserContext(), "invalid_department_id")})
	}

	months, _ := strconv.Atoi(c.Query("months", "12"))

	trend, err := h.service.GetTrendData(c.Context(), deptFilter, ownerFilter, months)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get trend data",
		})
	}
	return c.JSON(fiber.Map{"success": true, "data": trend})
}

// GetOKRTree returns the OKR alignment tree organized by department
func (h *GoalAnalyticsHandler) GetOKRTree(c *fiber.Ctx) error {
	departmentID, ownerFilter, deptErr := analyticsScopeFromCtx(c)
	if deptErr != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": i18n.T(c.UserContext(), "invalid_department_id")})
	}

	var periodStart *time.Time
	if startStr := c.Query("period_start"); startStr != "" {
		t, err := time.Parse("2006-01-02", startStr)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid period_start format (use YYYY-MM-DD)"})
		}
		periodStart = &t
	}

	var periodEnd *time.Time
	if endStr := c.Query("period_end"); endStr != "" {
		t, err := time.Parse("2006-01-02", endStr)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid period_end format (use YYYY-MM-DD)"})
		}
		periodEnd = &t
	}

	status := c.Query("status")

	tree, err := h.service.GetOKRTree(c.Context(), departmentID, ownerFilter, periodStart, periodEnd, status)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get OKR tree",
		})
	}
	return c.JSON(fiber.Map{"success": true, "data": tree})
}
