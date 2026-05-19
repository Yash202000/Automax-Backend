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

	// Resolve each participant: extension → user ID, or use phone number for guests.
	resolved := make([]models.ParticipantData, 0, len(req.Participants))
	for _, pi := range req.Participants {
		pd := models.ParticipantData{}
		if pi.Extension != "" {
			user, err := h.userSvc.FindByExtension(c.UserContext(), pi.Extension)
			if err != nil {
				return utils.ErrorResponse(c, fiber.StatusNotFound, "Participant not found: "+pi.Extension)
			}
			pd.UserID = &user.ID
		} else if pi.PhoneNumber != "" {
			phone := pi.PhoneNumber
			pd.Phone = &phone
		} else {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "Each participant must have extension or phone_number")
		}
		resolved = append(resolved, pd)
	}

	callLog, err := h.service.CreateCallLog(c.UserContext(), &req, resolved)
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
//
// Payload for a direct call:
//
//	{ "call_uuid":"...", "call_type":"direct",
//	  "initiator":{"extension":"101"}, "recipient":{"guest_phone":"+919876543210"} }
//
// Payload for a group call:
//
//	{ "call_uuid":"...", "call_type":"group",
//	  "initiator":{"extension":"101"}, "participants":[{"extension":"102"},{"guest_phone":"..."}] }
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

	if req.Initiator.Extension == "" && req.Initiator.GuestPhone == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Initiator must have either extension or guest_phone")
	}
	if req.CallType == "direct" && req.Recipient == nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Direct call requires a recipient")
	}
	if req.CallType == "group" && len(req.Participants) == 0 {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Group call requires at least one participant")
	}

	// Resolve initiator.
	initiator := models.ParticipantData{}
	if req.Initiator.Extension != "" {
		user, err := h.userSvc.FindByExtension(c.Context(), req.Initiator.Extension)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusNotFound, "Initiator not found")
		}
		initiator.UserID = &user.ID
	} else {
		phone := req.Initiator.GuestPhone
		initiator.Phone = &phone
	}

	// Collect recipient/participant parties.
	var parties []models.StartCallParty
	if req.CallType == "direct" {
		parties = []models.StartCallParty{*req.Recipient}
	} else {
		parties = req.Participants
	}

	recipients := make([]models.ParticipantData, 0, len(parties))
	for _, p := range parties {
		pd := models.ParticipantData{}
		if p.Extension != "" {
			user, err := h.userSvc.FindByExtension(c.UserContext(), p.Extension)
			if err != nil {
				return utils.ErrorResponse(c, fiber.StatusNotFound, "Participant not found: "+p.Extension)
			}
			pd.UserID = &user.ID
		} else if p.GuestPhone != "" {
			phone := p.GuestPhone
			pd.Phone = &phone
		} else {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "Each participant must have extension or guest_phone")
		}
		recipients = append(recipients, pd)
	}

	callLog, err := h.service.StartCall(c.UserContext(), req.CallUUID, req.CallType, initiator, recipients)
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
		Status string     `json:"status" validate:"required,oneof=ended missed"`
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
		Extension  string `json:"extension"`
		GuestPhone string `json:"guest_phone"`
	}

	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if req.Extension == "" && req.GuestPhone == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Either extension or guest_phone is required")
	}

	var userID *uuid.UUID
	if req.Extension != "" {
		log.Println("payload req : ", req.Extension)
		user, err := h.userSvc.FindByExtension(c.Context(), req.Extension)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusNotFound, "User not found")
		}
		userID = &user.ID
		log.Println("user id ;", userID)
	}

	if err := h.service.JoinCall(c.UserContext(), callUUID, userID, req.GuestPhone); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Extension is required")
	}

	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))

	user, err := h.userSvc.FindByExtension(c.UserContext(), extension)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "User with extension not found")
	}

	callLogs, total, err := h.service.GetCallLogsByUserID(c.UserContext(), user.ID, page, limit)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	totalPages := (int(total) + limit - 1) / limit

	return c.JSON(fiber.Map{
		"success":      true,
		"extension_id": extension,
		"user_id":      user.ID,
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Call UUID is required")
	}

	file, err := c.FormFile("file")
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "No file uploaded")
	}

	src, err := file.Open()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to read file")
	}
	defer src.Close()

	log.Printf("[UploadAttachment] call_uuid=%s filename=%s declared_size=%d mime=%s",
		callUUID, file.Filename, file.Size, file.Header.Get("Content-Type"))

	folder := fmt.Sprintf("calls/%s", callUUID)
	filePath, err := h.storage.UploadFile(c.UserContext(), src, file, folder)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to upload file")
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
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Attachment uploaded", map[string]interface{}{"success": true})
}

func (h *CallLogHandler) PreviewAttachment(c *fiber.Ctx) error {
	attachmentIDStr := c.Params("attachment_id")
	attachmentID, err := uuid.Parse(attachmentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid attachment ID")
	}

	attachment, err := h.service.GetAttachment(c.UserContext(), attachmentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Attachment not found")
	}

	log.Printf("[PreviewAttachment] id=%s file_path=%s mime=%s db_size=%d",
		attachmentID, attachment.FilePath, attachment.MimeType, attachment.FileSize)

	file, err := h.storage.GetFile(c.UserContext(), attachment.FilePath)
	if err != nil {
		log.Printf("[PreviewAttachment] GetFile error: %v", err)
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to retrieve file")
	}
	defer file.Close()

	fileData, err := io.ReadAll(file)
	if err != nil {
		log.Printf("[PreviewAttachment] ReadAll error: %v", err)
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to read file")
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
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    sipInfo,
	})
}
