package handlers

import (
	"strconv"
	"time"

	"github.com/automax/backend/internal/services"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

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
	stats, err := h.service.GetStats(c.Context())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get goal statistics",
		})
	}
	return c.JSON(fiber.Map{"success": true, "data": stats})
}

// GetDistributions returns goal distributions by status, priority, department, category
func (h *GoalAnalyticsHandler) GetDistributions(c *fiber.Ctx) error {
	var departmentID *uuid.UUID
	if deptStr := c.Query("department_id"); deptStr != "" {
		id, err := uuid.Parse(deptStr)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid department_id"})
		}
		departmentID = &id
	}

	dist, err := h.service.GetDistributions(c.Context(), departmentID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get goal distributions",
		})
	}
	return c.JSON(fiber.Map{"success": true, "data": dist})
}

// GetProgressSummary returns progress distribution data
func (h *GoalAnalyticsHandler) GetProgressSummary(c *fiber.Ctx) error {
	summary, err := h.service.GetProgressSummary(c.Context())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get progress summary",
		})
	}
	return c.JSON(fiber.Map{"success": true, "data": summary})
}

// GetAtRiskGoals returns paginated list of at-risk goals
func (h *GoalAnalyticsHandler) GetAtRiskGoals(c *fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))

	goals, total, err := h.service.GetAtRiskGoals(c.Context(), page, limit)
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
	months, _ := strconv.Atoi(c.Query("months", "12"))

	trend, err := h.service.GetTrendData(c.Context(), months)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get trend data",
		})
	}
	return c.JSON(fiber.Map{"success": true, "data": trend})
}

// GetOKRTree returns the OKR alignment tree organized by department
func (h *GoalAnalyticsHandler) GetOKRTree(c *fiber.Ctx) error {
	var departmentID *uuid.UUID
	if deptStr := c.Query("department_id"); deptStr != "" {
		id, err := uuid.Parse(deptStr)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"error": "Invalid department_id"})
		}
		departmentID = &id
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

	tree, err := h.service.GetOKRTree(c.Context(), departmentID, periodStart, periodEnd, status)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to get OKR tree",
		})
	}
	return c.JSON(fiber.Map{"success": true, "data": tree})
}
