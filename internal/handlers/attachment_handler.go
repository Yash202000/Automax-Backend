package handlers

import (
	"fmt"
	"io"

	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/internal/storage"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type AttachmentHandler struct {
	incidentService     services.IncidentService
	notificationService *services.NotificationService
	storage             *storage.MinIOStorage
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
		attachment, err := h.incidentService.GetAttachment(c.UserContext(), attachmentID)
		if err == nil {
			file, err := h.storage.GetFile(c.UserContext(), attachment.FilePath)
			if err != nil {
				return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_retrieve_file"))
			}
			defer file.Close()

			fileData, err := io.ReadAll(file)
			if err != nil {
				return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_read_file"))
			}

			c.Set("Content-Type", attachment.MimeType)
			c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", attachment.FileName))
			c.Set("Content-Length", fmt.Sprintf("%d", len(fileData)))
			return c.Send(fileData)
		}
	}

	// If not found as incident attachment, try notification attachment
	notification, attachment, err := h.notificationService.FindNotificationByAttachmentID(c.UserContext(), attachmentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "attachment_not_found"))
	}

	if attachment.StoragePath == "" {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "attachment_storage_path_not_found"))
	}

	// Download from MinIO
	fileReader, err := h.storage.GetFile(c.UserContext(), attachment.StoragePath)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "failed_to_retrieve_attachment"))
	}
	defer fileReader.Close()

	// Read the file content
	fileData, err := io.ReadAll(fileReader)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_read_attachment"))
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
		attachment, err := h.incidentService.GetAttachment(c.UserContext(), attachmentID)
		if err == nil {
			file, err := h.storage.GetFile(c.UserContext(), attachment.FilePath)
			if err != nil {
				return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_retrieve_file"))
			}
			defer file.Close()

			fileData, err := io.ReadAll(file)
			if err != nil {
				return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_read_file"))
			}

			c.Set("Content-Type", attachment.MimeType)
			c.Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", attachment.FileName))
			c.Set("Content-Length", fmt.Sprintf("%d", len(fileData)))
			return c.Send(fileData)
		}
	}

	// If not found as incident attachment, try notification attachment
	notification, attachment, err := h.notificationService.FindNotificationByAttachmentID(c.UserContext(), attachmentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "attachment_not_found"))
	}

	if attachment.StoragePath == "" {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "attachment_storage_path_not_found"))
	}

	// Download from MinIO
	fileReader, err := h.storage.GetFile(c.UserContext(), attachment.StoragePath)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "failed_to_retrieve_attachment"))
	}
	defer fileReader.Close()

	// Read the file content
	fileData, err := io.ReadAll(fileReader)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_read_attachment"))
	}

	// Set appropriate headers for inline preview
	c.Set("Content-Type", attachment.ContentType)
	c.Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", attachment.Filename))
	c.Set("Content-Length", fmt.Sprintf("%d", len(fileData)))

	_ = notification // Silence unused variable warning

	return c.Send(fileData)
}
