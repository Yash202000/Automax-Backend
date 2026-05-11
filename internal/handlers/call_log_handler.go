package handlers

import (
	"fmt"
	"io"
	"strconv"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/internal/storage"
	"github.com/automax/backend/pkg/constants"
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	user, err := h.userSvc.GetUserByID(c.Context(), req.InitiatorID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "User not found")
	}

	userID := user.ID
	callLog, err := h.service.CreateCallLog(c.UserContext(), &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid call log ID")
	}

	callLog, err := h.service.GetCallLog(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Call log not found")
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid call log ID")
	}

	var req models.CallLogUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	callLog, err := h.service.UpdateCallLog(c.UserContext(), id, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid call log ID")
	}

	if err := h.service.DeleteCallLog(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid query parameters")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &filter); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	if filter.Limit == 0 {
		filter.Limit = 10
	}

	if filter.Page == 0 {
		filter.Page = 1
	}

	userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	if !ok {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, "User not authenticated")
	}

	filter.ParticipantID = &userID

	items, total, err := h.service.ListCallLogsSummary(c.UserContext(), &filter, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	totalPages := (int(total) + filter.Limit - 1) / filter.Limit

	return c.JSON(fiber.Map{
		"success":     true,
		"data":        items,
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
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    stats,
	})
}

// StartCall handles POST /api/v1/calls/start
func (h *CallLogHandler) StartCall(c *fiber.Ctx) error {
	var req struct {
		CallUUID     string        `json:"call_uuid" validate:"required"`
		Participants []interface{} `json:"participants,omitempty"`
		InitiatorID  string        `json:"initiator_id" validate:"required"`
	}

	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	// userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	// if !ok {
	// 	return utils.ErrorResponse(c, fiber.StatusUnauthorized, "User not authenticated")
	// }
	user, err := h.userSvc.FindByExtension(c.Context(), req.InitiatorID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "User not found")
	}

	userID := user.ID

	// Resolve participant IDs from user IDs or extension IDs
	var participantIDs []uuid.UUID
	for _, p := range req.Participants {
		var id uuid.UUID
		var err error

		switch v := p.(type) {
		case string:
			id, err = uuid.Parse(v)
			if err != nil {
				// Try to resolve by extension ID
				usr, err := h.userSvc.FindByExtension(c.UserContext(), v)
				if err != nil {
					return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid participant: "+v)
				}
				id = usr.ID
			}
		default:
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid participant format")
		}

		participantIDs = append(participantIDs, id)
	}

	callLog, err := h.service.StartCall(c.UserContext(), req.CallUUID, userID, participantIDs)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Call UUID is required")
	}

	var req struct {
		EndAt  *time.Time `json:"end_at,omitempty"`
		Status string     `json:"status,omitempty" validate:"required"` // e.g., "completed", "missed", etc.
	}

	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	callLog, err := h.service.EndCall(c.UserContext(), callUUID, req.EndAt, req.Status)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Call UUID is required")
	}

	var req struct {
		Extension string `json:"extension" validate:"required"` // callee extension from PBX
	}

	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	// userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	// if !ok {
	// 	return utils.ErrorResponse(c, fiber.StatusUnauthorized, "User not authenticated")
	// }
	user, err := h.userSvc.FindByExtension(c.Context(), req.Extension)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "User not found")
	}
	userID := user.ID

	// userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	// if !ok {
	// 	return utils.ErrorResponse(c, fiber.StatusUnauthorized, "User not authenticated")
	// }

	if err := h.service.JoinCall(c.UserContext(), callUUID, userID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Successfully joined the call",
	})
}

func (h *CallLogHandler) GetCallLogsByExtension(c *fiber.Ctx) error {
	extension := c.Params("extension")
	if extension == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Extension is required")
	}

	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))

	// Find the user
	user, err := h.userSvc.FindByExtension(c.UserContext(), extension)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "User with extension not found")
	}

	// Get call logs (Note: these are already models.CallLogResponse)
	callLogs, total, err := h.service.GetCallLogsByUserID(c.UserContext(), user.ID, page, limit)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Ensure slices are not nil (so JSON returns [] instead of null)
	for i := range callLogs {
		if callLogs[i].Participants == nil {
			callLogs[i].Participants = []models.UserMinimalResponse{}
		}
		if callLogs[i].JoinedUsers == nil {
			callLogs[i].JoinedUsers = []models.UserMinimalResponse{}
		}
		if callLogs[i].InvitedUsers == nil {
			callLogs[i].InvitedUsers = []models.UserMinimalResponse{}
		}
	}

	totalPages := (int(total) + limit - 1) / limit

	return c.JSON(fiber.Map{
		"success":      true,
		"extension_id": extension, // The extension ID from Params
		"user_id":      user.ID,   // The internal ID found via extension
		"data":         callLogs,
		"total_items":  total,
		"total_pages":  totalPages,
		"page":         page,
		"limit":        limit,
	})
}

// Attachments

func (h *CallLogHandler) UploadAttachment(c *fiber.Ctx) error {
	callUUIDStr := c.Params("call_uuid")
	callUUID, err := uuid.Parse(callUUIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid incident ID")
	}

	file, err := c.FormFile("file")
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "No file uploaded")
	}

	// Open the file
	src, err := file.Open()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to read file")
	}
	defer src.Close()

	// Upload to storage
	folder := fmt.Sprintf("calls/%s", callUUID.String())
	filePath, err := h.storage.UploadFile(c.UserContext(), src, file, folder)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to upload file")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	attachment := &models.CallLogAttachment{
		FileName:     file.Filename,
		FileSize:     file.Size,
		MimeType:     file.Header.Get("Content-Type"),
		FilePath:     filePath,
		UploadedByID: userID,
	}

	if err := h.service.AddAttachment(c.UserContext(), callUUID, attachment); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	result := map[string]interface{}{
		"success": true,
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "Attachment uploaded", result)
}

func (h *CallLogHandler) PreviewAttachment(c *fiber.Ctx) error {
	attachmentIDStr := c.Params("attachment_id")

	// Try as incident attachment first (UUID format)
	attachmentID, err := uuid.Parse(attachmentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Try incident attachment
	attachment, err := h.service.GetAttachment(c.UserContext(), attachmentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Attachment not found")
	}
	file, err := h.storage.GetFile(c.UserContext(), attachment.FilePath)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to retrieve file")
	}
	defer file.Close()

	fileData, err := io.ReadAll(file)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to read file")
	}

	c.Set("Content-Type", attachment.MimeType)
	c.Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", attachment.FileName))
	c.Set("Content-Length", fmt.Sprintf("%d", len(fileData)))
	return c.Send(fileData)

}

func (h *CallLogHandler) GetSipInfo(c *fiber.Ctx) error {
	sipInfo, err := h.service.GetSipInfo(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    sipInfo,
	})
}
