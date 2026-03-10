package handlers

import (
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type ActionLogHandler struct {
	service   services.ActionLogService
	validator *validator.Validate
}

func NewActionLogHandler(service services.ActionLogService, validator *validator.Validate) *ActionLogHandler {
	return &ActionLogHandler{
		service:   service,
		validator: validator,
	}
}

// ListActionLogs handles GET /admin/action-logs
func (h *ActionLogHandler) ListActionLogs(c *fiber.Ctx) error {
	filter := &models.ActionLogFilter{}
	if err := c.QueryParser(filter); err != nil {
		log.Printf("Error parsing query parameters: %v", err)
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid query parameters")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &filter); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	if filter.Page == 0 {
		filter.Page = 1
	}

	if filter.Limit == 0 {
		filter.Limit = 20
	}

	logs, total, err := h.service.ListActionLogs(c.UserContext(), filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	totalPages := (int(total) + filter.Limit - 1) / filter.Limit

	return c.JSON(fiber.Map{
		"success":     true,
		"data":        logs,
		"total_items": total,
		"total_pages": totalPages,
		"page":        filter.Page,
		"limit":       filter.Limit,
	})
}

// GetActionLog handles GET /admin/action-logs/:id
func (h *ActionLogHandler) GetActionLog(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid action log ID")
	}

	log, err := h.service.GetActionLog(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Action log not found")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Action log retrieved successfully", log)
}

// GetStats handles GET /admin/action-logs/stats
func (h *ActionLogHandler) GetStats(c *fiber.Ctx) error {
	stats, err := h.service.GetStats(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Stats retrieved successfully", stats)
}

// GetFilterOptions handles GET /admin/action-logs/filter-options
func (h *ActionLogHandler) GetFilterOptions(c *fiber.Ctx) error {
	options, err := h.service.GetFilterOptions(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Filter options retrieved successfully", options)
}

// GetUserActions handles GET /admin/action-logs/user/:id
func (h *ActionLogHandler) GetUserActions(c *fiber.Ctx) error {
	userIDStr := c.Params("id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid user ID")
	}

	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))

	logs, total, err := h.service.GetUserActions(c.UserContext(), userID, page, limit)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	totalPages := (int(total) + limit - 1) / limit

	return c.JSON(fiber.Map{
		"success":     true,
		"data":        logs,
		"total_items": total,
		"total_pages": totalPages,
		"page":        page,
		"limit":       limit,
	})
}

// CleanupOldLogs handles DELETE /admin/action-logs/cleanup
func (h *ActionLogHandler) CleanupOldLogs(c *fiber.Ctx) error {
	retentionDays, _ := strconv.Atoi(c.Query("retention_days", "90"))
	if retentionDays < 7 {
		retentionDays = 7 // Minimum 7 days retention
	}

	deleted, err := h.service.CleanupOldLogs(c.UserContext(), retentionDays)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Old logs cleaned up", fiber.Map{
		"deleted_count":  deleted,
		"retention_days": retentionDays,
	})
}

// ExportActionLogs handles GET /admin/action-logs/export
func (h *ActionLogHandler) ExportActionLogs(c *fiber.Ctx) error {
	var filter models.ActionLogFilter
	if err := c.QueryParser(&filter); err != nil {
		log.Printf("Error parsing query parameters: %v", err)
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid query parameters")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &filter); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	logs, _, err := h.service.ListActionLogs(c.UserContext(), &filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Determine export format based on Accept header or format query param
	format := c.Query("format", "csv")
	if accept := c.Get("Accept"); accept == "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		format = "excel"
	}

	if format == "excel" {
		return h.exportToExcel(c, logs)
	} else {
		return h.exportToCSV(c, logs)
	}
}

// exportToCSV exports action logs to CSV format
func (h *ActionLogHandler) exportToCSV(c *fiber.Ctx, logs []models.ActionLogResponse) error {
	// Set headers for CSV download
	c.Set("Content-Type", "text/csv")
	c.Set("Content-Disposition", "attachment; filename=audit_logs.csv")

	// Write CSV header
	c.Write([]byte("ID,User ID,Username,Action,Module,Resource ID,Description,IP Address,Status,Created At\n"))

	// Write CSV rows
	for _, log := range logs {
		username := ""
		if log.User != nil {
			username = log.User.Username
		}

		row := fmt.Sprintf(`"%s","%s","%s","%s","%s","%s","%s","%s","%s","%s"`+"\n",
			log.ID,
			log.UserID,
			username,
			log.Action,
			log.Module,
			log.ResourceID,
			strings.ReplaceAll(log.Description, `"`, `""`), // Escape quotes in description
			log.IPAddress,
			log.Status,
			log.CreatedAt.Format("2006-01-02 15:04:05"),
		)
		c.Write([]byte(row))
	}

	return nil
}

// exportToExcel exports action logs to Excel format
func (h *ActionLogHandler) exportToExcel(c *fiber.Ctx, logs []models.ActionLogResponse) error {
	// For now, we'll return a CSV with Excel-compatible formatting
	// In a real implementation, we would use a library like excelize to generate actual Excel files
	c.Set("Content-Type", "text/csv")
	c.Set("Content-Disposition", "attachment; filename=audit_logs.xlsx")

	// Write CSV header
	c.Write([]byte("ID,User ID,Username,Action,Module,Resource ID,Description,IP Address,Status,Created At\n"))

	// Write CSV rows
	for _, log := range logs {
		username := ""
		if log.User != nil {
			username = log.User.Username
		}

		row := fmt.Sprintf(`"%s","%s","%s","%s","%s","%s","%s","%s","%s","%s"`+"\n",
			log.ID,
			log.UserID,
			username,
			log.Action,
			log.Module,
			log.ResourceID,
			strings.ReplaceAll(log.Description, `"`, `""`), // Escape quotes in description
			log.IPAddress,
			log.Status,
			log.CreatedAt.Format("2006-01-02 15:04:05"),
		)
		c.Write([]byte(row))
	}

	return nil
}
