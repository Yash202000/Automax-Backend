package handlers

import (
	"fmt"
	"io"

	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/internal/storage"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type AttachmentHandler struct {
	incidentService      services.IncidentService
	notificationService  *services.NotificationService
	storage              *storage.MinIOStorage
}

func NewAttachmentHandler(
	incidentService services.IncidentService,
	notificationService *services.NotificationService,
	storage *storage.MinIOStorage,
) *AttachmentHandler {
	return &AttachmentHandler{
		incidentService:     incidentService,
		notificationService: notificationService,
		storage:             storage,
	}
}

// DownloadAttachment handles GET /api/v1/attachments/:attachment_id
// Works for both incident and notification attachments
func (h *AttachmentHandler) DownloadAttachment(c *fiber.Ctx) error {
	attachmentIDStr := c.Params("attachment_id")

	// Try as incident attachment first (UUID format)
	attachmentID, err := uuid.Parse(attachmentIDStr)
	if err == nil {
		// Try incident attachment
		attachment, err := h.incidentService.GetAttachment(c.Context(), attachmentID)
		if err == nil {
			file, err := h.storage.GetFile(c.Context(), attachment.FilePath)
			if err != nil {
				return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to retrieve file")
			}
			defer file.Close()

			c.Set("Content-Type", attachment.MimeType)
			c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", attachment.FileName))
			return c.SendStream(file)
		}
	}

	// If not found as incident attachment, try notification attachment
	notification, attachment, err := h.notificationService.FindNotificationByAttachmentID(c.Context(), attachmentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Attachment not found")
	}

	if attachment.StoragePath == "" {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Attachment storage path not found")
	}

	// Download from MinIO
	fileReader, err := h.storage.GetFile(c.Context(), attachment.StoragePath)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Failed to retrieve attachment: "+err.Error())
	}
	defer fileReader.Close()

	// Read the file content
	fileData, err := io.ReadAll(fileReader)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to read attachment")
	}

	// Set appropriate headers for download
	c.Set("Content-Type", attachment.ContentType)
	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", attachment.Filename))
	c.Set("Content-Length", fmt.Sprintf("%d", len(fileData)))

	_ = notification // Silence unused variable warning

	return c.Send(fileData)
}

// PreviewAttachment handles GET /api/v1/attachments/:attachment_id/preview
// Works for both incident and notification attachments (inline display)
func (h *AttachmentHandler) PreviewAttachment(c *fiber.Ctx) error {
	attachmentIDStr := c.Params("attachment_id")

	// Try as incident attachment first (UUID format)
	attachmentID, err := uuid.Parse(attachmentIDStr)
	if err == nil {
		// Try incident attachment
		attachment, err := h.incidentService.GetAttachment(c.Context(), attachmentID)
		if err == nil {
			file, err := h.storage.GetFile(c.Context(), attachment.FilePath)
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
	}

	// If not found as incident attachment, try notification attachment
	notification, attachment, err := h.notificationService.FindNotificationByAttachmentID(c.Context(), attachmentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Attachment not found")
	}

	if attachment.StoragePath == "" {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Attachment storage path not found")
	}

	// Download from MinIO
	fileReader, err := h.storage.GetFile(c.Context(), attachment.StoragePath)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Failed to retrieve attachment: "+err.Error())
	}
	defer fileReader.Close()

	// Read the file content
	fileData, err := io.ReadAll(fileReader)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to read attachment")
	}

	// Set appropriate headers for inline preview
	c.Set("Content-Type", attachment.ContentType)
	c.Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", attachment.Filename))
	c.Set("Content-Length", fmt.Sprintf("%d", len(fileData)))

	_ = notification // Silence unused variable warning

	return c.Send(fileData)
}
