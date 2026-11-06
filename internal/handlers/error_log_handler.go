package handlers

import (
	"errors"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ErrorLogHandler exposes the Error Log endpoints (admin only).
type ErrorLogHandler struct {
	service *services.ErrorLogService
}

func NewErrorLogHandler(service *services.ErrorLogService) *ErrorLogHandler {
	return &ErrorLogHandler{service: service}
}

// CanViewTechnical reports whether the caller may see technical details.
func CanViewTechnical(c *fiber.Ctx) bool {
	user, ok := c.Locals(constants.ContextKeys.User).(*models.User)
	if !ok || user == nil {
		return false
	}
	return user.IsSuperAdmin || user.HasPermission("error-logs:view_technical")
}

func ToErrorLogResponse(e *models.ErrorLog, withTechnical bool) models.ErrorLogResponse {
	resp := models.ErrorLogResponse{
		ID:           e.ID,
		ErrorCode:    e.ErrorCode,
		OccurredAt:   e.OccurredAt,
		Module:       e.Module,
		Action:       e.Action,
		ErrorType:    e.ErrorType,
		Severity:     e.Severity,
		Message:      utils.SanitizeErrorText(e.Message),
		UserID:       e.UserID,
		User:         e.UserName,
		RelatedType:  e.RelatedType,
		RelatedID:    e.RelatedID,
		RelatedLabel: utils.SanitizeErrorText(e.RelatedLabel),
		Status:       e.Status,
		RetryCount:   e.RetryCount,
	}
	if withTechnical && e.TechnicalDetails != "" {
		td := utils.SanitizeErrorText(e.TechnicalDetails)
		resp.TechnicalDetails = &td
	}
	for _, h := range e.History {
		resp.History = append(resp.History, models.ErrorLogHistoryResponse{
			FromStatus: h.FromStatus,
			ToStatus:   h.ToStatus,
			Note:       h.Note,
			ChangedBy:  h.ChangedBy,
			ChangedAt:  h.ChangedAt,
		})
	}
	return resp
}

// List returns error logs, newest first.
//
// GET /api/v1/error-logs
func (h *ErrorLogHandler) List(c *fiber.Ctx) error {
	var f models.ErrorLogFilter
	if err := c.QueryParser(&f); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid query parameters")
	}
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 200 {
		f.Limit = 20
	}

	logs, total, err := h.service.List(c.UserContext(), f)
	if err != nil {
		return utils.InternalErrorResponse(c, err, "Failed to load error logs")
	}
	items := make([]models.ErrorLogResponse, 0, len(logs))
	for i := range logs {
		items = append(items, ToErrorLogResponse(&logs[i], false))
	}
	return utils.PaginatedSuccessResponse(c, items, f.Page, f.Limit, total)
}

// GetByID returns one error log with its status history.
//
// GET /api/v1/error-logs/:id
func (h *ErrorLogHandler) GetByID(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid error log ID")
	}
	e, err := h.service.GetByID(c.UserContext(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.ErrorResponse(c, fiber.StatusNotFound, "Error log not found")
		}
		return utils.InternalErrorResponse(c, err, "Failed to load error log")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", ToErrorLogResponse(e, CanViewTechnical(c)))
}

// UpdateStatus changes the status of an error log and records a note in its history.
//
// PUT /api/v1/error-logs/:id/status
func (h *ErrorLogHandler) UpdateStatus(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid error log ID")
	}
	var req models.UpdateErrorLogStatusRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	if errs := validation.ValidateStruct(c.UserContext(), &req); len(errs) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": errs})
	}

	var userID *uuid.UUID
	if uid, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID); ok {
		userID = &uid
	}
	userName, _ := c.Locals(constants.ContextKeys.UserName).(string)

	if err := h.service.UpdateStatus(c.UserContext(), id, req, userID, userName); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.ErrorResponse(c, fiber.StatusNotFound, "Error log not found")
		}
		return utils.InternalErrorResponse(c, err, "Failed to update error log status")
	}

	e, err := h.service.GetByID(c.UserContext(), id)
	if err != nil {
		return utils.InternalErrorResponse(c, err, "Failed to load error log")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Status updated", ToErrorLogResponse(e, CanViewTechnical(c)))
}

// Export downloads the filtered list as CSV (same masking as the screen).
//
// GET /api/v1/error-logs/export
func (h *ErrorLogHandler) Export(c *fiber.Ctx) error {
	var f models.ErrorLogFilter
	if err := c.QueryParser(&f); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid query parameters")
	}
	data, err := h.service.ExportCSV(c.UserContext(), f, CanViewTechnical(c))
	if err != nil {
		return utils.InternalErrorResponse(c, err, "Failed to export error logs")
	}
	c.Set("Content-Type", "text/csv; charset=utf-8")
	c.Set("Content-Disposition", `attachment; filename="error_logs_`+time.Now().Format("2006-01-02")+`.csv"`)
	return c.Send(data)
}

// Report records an error that the web app detected in the browser (for example an
// import file that could not be read), so it also appears in the Error Log.
// Any logged-in user may call it; the user is taken from the token.
//
// POST /api/v1/error-logs/report
func (h *ErrorLogHandler) Report(c *fiber.Ctx) error {
	var req models.ReportErrorLogRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	if errs := validation.ValidateStruct(c.UserContext(), &req); len(errs) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": errs})
	}

	var userID *uuid.UUID
	if uid, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID); ok {
		userID = &uid
	}
	userName, _ := c.Locals(constants.ContextKeys.UserName).(string)

	severity := req.Severity
	if severity == "" {
		severity = models.ErrorSeverityMedium
	}
	services.LogError(models.ErrorLogEntry{
		Module:           req.Module,
		Action:           req.Action,
		ErrorType:        req.ErrorType,
		Severity:         severity,
		Message:          req.Message,
		TechnicalDetails: req.Details,
		UserID:           userID,
		UserName:         userName,
		RelatedType:      req.RelatedType,
		RelatedID:        req.RelatedID,
		RelatedLabel:     req.RelatedLabel,
	})
	return utils.SuccessResponse(c, fiber.StatusAccepted, "Error recorded", nil)
}

// ListUsers returns the users that appear in the error log, for the User filter.
//
// GET /api/v1/error-logs/users
func (h *ErrorLogHandler) ListUsers(c *fiber.Ctx) error {
	users, err := h.service.ListUsers(c.UserContext())
	if err != nil {
		return utils.InternalErrorResponse(c, err, "Failed to load users")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", users)
}
