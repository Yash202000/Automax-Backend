package handlers

import (
	"fmt"
	"io"
	"log"
	"strconv"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/internal/storage"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type CallLogHandler struct {
	service   services.CallLogService
	validator *validator.Validate
	userSvc   services.UserService
	storage   *storage.MinIOStorage
}

func NewCallLogHandler(service services.CallLogService, validator *validator.Validate, userSvc services.UserService, storage *storage.MinIOStorage) *CallLogHandler {
	return &CallLogHandler{
		service:   service,
		validator: validator,
		userSvc:   userSvc,
		storage:   storage,
	}
}

// CreateCallLog handles POST /admin/call-logs
func (h *CallLogHandler) CreateCallLog(c *fiber.Ctx) error {
	var req models.CallLogCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	// Build participant data from extension or phone_number — stored as-is, resolved at read time.
	resolved := make([]models.ParticipantData, 0, len(req.Participants))
	for _, pi := range req.Participants {
		phone := pi.Extension
		if phone == "" {
			phone = pi.PhoneNumber
		}
		if phone == "" {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "each_participant_ext_or_phone"))
		}
		resolved = append(resolved, models.ParticipantData{Phone: phone})
	}

	callLog, err := h.service.CreateCallLog(c.UserContext(), &req, resolved)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"success": true,
		"data":    callLog,
	})
}

// GetCallLog handles GET /admin/call-logs/:id
func (h *CallLogHandler) GetCallLog(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_call_log_id"))
	}

	callLog, err := h.service.GetCallLog(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "call_log_not_found"))
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    callLog,
	})
}

// UpdateCallLog handles PUT /admin/call-logs/:id
func (h *CallLogHandler) UpdateCallLog(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_call_log_id"))
	}

	var req models.CallLogUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	callLog, err := h.service.UpdateCallLog(c.UserContext(), id, &req)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    callLog,
	})
}

// DeleteCallLog handles DELETE /admin/call-logs/:id
func (h *CallLogHandler) DeleteCallLog(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_call_log_id"))
	}

	if err := h.service.DeleteCallLog(c.UserContext(), id); err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Call log deleted successfully",
	})
}

// ListCallLogs handles GET /admin/call-logs
func (h *CallLogHandler) ListCallLogs(c *fiber.Ctx) error {
	var filter models.CallLogFilter

	if err := c.QueryParser(&filter); err != nil {
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

	user, ok := c.Locals(constants.ContextKeys.User).(*models.User)
	if !ok || user == nil || user.ID == uuid.Nil {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "user_not_authenticated"))
	}

	// Super admins, and users granted call-logs:view_all, may widen the scope with
	// ?agent_id= or ?all=true. Both params are silently ignored for everyone else,
	// who always stays scoped to their own calls.
	scope := "self"
	perspectiveID := user.ID        // whose phone/ext drives direction + duration
	filter.ParticipantID = &user.ID // whose calls are returned

	// HasPermission short-circuits to true for super admins, so this covers both.
	if filter.AgentID != nil || filter.All {
		if user.HasPermission("call-logs:view_all") {
			switch {
			case filter.AgentID != nil: // agent_id wins over all
				filter.ParticipantID = filter.AgentID
				perspectiveID = *filter.AgentID
				scope = "agent"
			case filter.All:
				filter.ParticipantID = nil // unscoped: every call log
				scope = "all"
			}
		} else {
			return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(c.UserContext(), "insufficient_permissions"))
		}
	}

	items, total, err := h.service.ListCallLogsSummary(c.UserContext(), &filter, perspectiveID)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	// ListCallLogsSummary clamps Page/Limit on the filter it was handed, so these
	// are populated by now; guard the division anyway.
	totalPages := 0
	if filter.Limit > 0 {
		totalPages = (int(total) + filter.Limit - 1) / filter.Limit
	}

	return c.JSON(fiber.Map{
		"success":     true,
		"data":        items,
		"scope":       scope,
		"total_items": total,
		"total_pages": totalPages,
		"page":        filter.Page,
		"limit":       filter.Limit,
	})
}

// GetStats handles GET /admin/call-logs/stats
func (h *CallLogHandler) GetStats(c *fiber.Ctx) error {
	stats, err := h.service.GetStats(c.UserContext())
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    stats,
	})
}

// resolvePhone wraps a raw phone/extension string into ParticipantData.
// Resolution against the users table happens at read time, not write time.
func resolvePhone(phone string) models.ParticipantData {
	return models.ParticipantData{Phone: phone}
}

// StartCall handles POST /api/v1/calls/start
//
// Payload for a direct call:
//
//	{ "call_uuid":"...", "call_type":"direct",
//	  "initiator":{"phone":"101"}, "recipient":{"phone":"+919876543210"} }
//
// Payload for a group call:
//
//	{ "call_uuid":"...", "call_type":"group",
//	  "initiator":{"phone":"101"}, "participants":[{"phone":"102"},{"phone":"..."}] }
func (h *CallLogHandler) StartCall(c *fiber.Ctx) error {
	var req models.StartCallRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	if req.Initiator.Phone == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "initiator_phone_required"))
	}
	if req.CallType == "direct" && req.Recipient == nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "direct_call_needs_recipient"))
	}
	if req.CallType == "group" && len(req.Participants) == 0 {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "group_call_needs_participant"))
	}

	initiator := resolvePhone(req.Initiator.Phone)

	var parties []models.StartCallParty
	if req.CallType == "direct" {
		parties = []models.StartCallParty{*req.Recipient}
	} else {
		parties = req.Participants
	}

	recipients := make([]models.ParticipantData, 0, len(parties))
	for _, p := range parties {
		if p.Phone == "" {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "each_participant_phone"))
		}
		recipients = append(recipients, resolvePhone(p.Phone))
	}

	callLog, err := h.service.StartCall(c.UserContext(), req.CallUUID, req.CallType, initiator, recipients)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"success": true,
		"data":    callLog,
	})
}

// EndCall handles POST /api/v1/calls/:call_uuid/end
func (h *CallLogHandler) EndCall(c *fiber.Ctx) error {
	callUUID := c.Params("call_uuid")
	if callUUID == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "call_uuid_required"))
	}

	var req struct {
		EndAt  *time.Time `json:"end_at,omitempty"`
		Status string     `json:"status" validate:"required"`
	}

	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	callLog, err := h.service.EndCall(c.UserContext(), callUUID, req.EndAt, req.Status)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    callLog,
	})
}

// JoinCall handles POST /api/v1/calls/:call_uuid/join
func (h *CallLogHandler) JoinCall(c *fiber.Ctx) error {
	callUUID := c.Params("call_uuid")
	if callUUID == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "call_uuid_required"))
	}

	var req struct {
		Phone string `json:"phone"`
	}

	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if req.Phone == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "phone_required"))
	}

	if err := h.service.JoinCall(c.UserContext(), callUUID, req.Phone); err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Successfully joined the call",
	})
}

// GetCallLogsByExtension handles GET /api/v1/call-logs/extension/:extension
func (h *CallLogHandler) GetCallLogsByExtension(c *fiber.Ctx) error {
	extension := c.Params("extension")
	if extension == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "extension_required"))
	}

	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))

	// Verify the extension exists, then find calls by the stored phone/extension value.
	if _, err := h.userSvc.FindByExtension(c.UserContext(), extension); err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "user_with_ext_not_found"))
	}

	callLogs, total, err := h.service.GetCallLogsByPhone(c.UserContext(), extension, page, limit)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	totalPages := (int(total) + limit - 1) / limit

	return c.JSON(fiber.Map{
		"success":      true,
		"extension_id": extension,
		"data":         callLogs,
		"total_items":  total,
		"total_pages":  totalPages,
		"page":         page,
		"limit":        limit,
	})
}

// Attachments

func (h *CallLogHandler) UploadAttachment(c *fiber.Ctx) error {
	callUUID := c.Params("call_uuid")
	if callUUID == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "call_uuid_required"))
	}

	file, err := c.FormFile("file")
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "no_file_uploaded"))
	}

	src, err := file.Open()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_read_file"))
	}
	defer src.Close()

	log.Printf("[UploadAttachment] call_uuid=%s filename=%s declared_size=%d mime=%s",
		callUUID, file.Filename, file.Size, file.Header.Get("Content-Type"))

	folder := fmt.Sprintf("calls/%s", callUUID)
	filePath, err := h.storage.UploadFile(c.UserContext(), src, file, folder)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_upload_file"))
	}

	log.Printf("[UploadAttachment] stored at path=%s", filePath)

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	mimeType := file.Header.Get("Content-Type")
	if mimeType == "audio/wave" {
		mimeType = "audio/wav"
	}

	attachment := &models.CallLogAttachment{
		FileName:     file.Filename,
		FileSize:     file.Size,
		MimeType:     mimeType,
		FilePath:     filePath,
		UploadedByID: userID,
	}

	if err := h.service.AddAttachment(c.UserContext(), callUUID, attachment); err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "attachment_uploaded"), map[string]interface{}{"success": true})
}

func (h *CallLogHandler) PreviewAttachment(c *fiber.Ctx) error {
	attachmentIDStr := c.Params("attachment_id")
	attachmentID, err := uuid.Parse(attachmentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_attachment_id"))
	}

	attachment, err := h.service.GetAttachment(c.UserContext(), attachmentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "attachment_not_found"))
	}

	log.Printf("[PreviewAttachment] id=%s file_path=%s mime=%s db_size=%d",
		attachmentID, attachment.FilePath, attachment.MimeType, attachment.FileSize)

	file, err := h.storage.GetFile(c.UserContext(), attachment.FilePath)
	if err != nil {
		log.Printf("[PreviewAttachment] GetFile error: %v", err)
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_retrieve_file"))
	}
	defer file.Close()

	fileData, err := io.ReadAll(file)
	if err != nil {
		log.Printf("[PreviewAttachment] ReadAll error: %v", err)
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_read_file"))
	}

	log.Printf("[PreviewAttachment] serving %d bytes mime=%s filename=%s", len(fileData), attachment.MimeType, attachment.FileName)

	c.Set("Content-Type", attachment.MimeType)
	c.Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", attachment.FileName))
	c.Set("Content-Length", fmt.Sprintf("%d", len(fileData)))
	return c.Send(fileData)
}

func (h *CallLogHandler) GetSipInfo(c *fiber.Ctx) error {
	sipInfo, err := h.service.GetSipInfo(c.UserContext())
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    sipInfo,
	})
}
