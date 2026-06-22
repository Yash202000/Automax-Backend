package handlers

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/i18n"
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_query_parameters"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), filter); len(validationErrors) != 0 {
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

	// QueryParser cannot parse plain YYYY-MM-DD into *time.Time; do it manually.
	// Use time.Local so date-only strings cover the entire local day, not UTC midnight.
	if startStr := c.Query("start_date"); startStr != "" {
		if t, err := time.Parse("2006-01-02", startStr); err == nil {
			localStart := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
			filter.StartDate = &localStart
		} else if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			filter.StartDate = &t
		}
	}
	if endStr := c.Query("end_date"); endStr != "" {
		if t, err := time.Parse("2006-01-02", endStr); err == nil {
			localEnd := time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999999, time.Local)
			filter.EndDate = &localEnd
		} else if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			filter.EndDate = &t
		}
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_action_log_id"))
	}

	log, err := h.service.GetActionLog(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "action_log_not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "action_log_retrieved"), log)
}

// GetStats handles GET /admin/action-logs/stats
func (h *ActionLogHandler) GetStats(c *fiber.Ctx) error {
	stats, err := h.service.GetStats(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "stats_retrieved_success"), stats)
}

// GetFilterOptions handles GET /admin/action-logs/filter-options
func (h *ActionLogHandler) GetFilterOptions(c *fiber.Ctx) error {
	options, err := h.service.GetFilterOptions(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "filter_options_retrieved"), options)
}

// GetUserActions handles GET /admin/action-logs/user/:id
func (h *ActionLogHandler) GetUserActions(c *fiber.Ctx) error {
	userIDStr := c.Params("id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_user_id"))
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

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "old_logs_cleaned_up"), fiber.Map{
		"deleted_count":  deleted,
		"retention_days": retentionDays,
	})
}

// ExportActionLogs handles GET /admin/action-logs/export
func (h *ActionLogHandler) ExportActionLogs(c *fiber.Ctx) error {
	var filter models.ActionLogFilter
	if err := c.QueryParser(&filter); err != nil {
		log.Printf("Error parsing query parameters: %v", err)
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_query_parameters"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &filter); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	// QueryParser cannot parse plain YYYY-MM-DD into *time.Time; do it manually.
	// Use time.Local so date-only strings cover the entire local day, not UTC midnight.
	if startStr := c.Query("start_date"); startStr != "" {
		if t, err := time.Parse("2006-01-02", startStr); err == nil {
			localStart := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
			filter.StartDate = &localStart
		} else if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			filter.StartDate = &t
		}
	}
	if endStr := c.Query("end_date"); endStr != "" {
		if t, err := time.Parse("2006-01-02", endStr); err == nil {
			localEnd := time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999999, time.Local)
			filter.EndDate = &localEnd
		} else if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			filter.EndDate = &t
		}
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
