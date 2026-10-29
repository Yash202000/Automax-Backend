package handlers

import (
	"fmt"
	"io"
	"mime"
	"strconv"
	"strings"

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
	return h.serve(c, "attachment")
}

// PreviewAttachment handles GET /api/v1/attachments/:attachment_id/preview
// Works for both incident and notification attachments (inline display)
func (h *AttachmentHandler) PreviewAttachment(c *fiber.Ctx) error {
	return h.serve(c, "inline")
}

func (h *AttachmentHandler) serve(c *fiber.Ctx, disposition string) error {
	attachmentIDStr := c.Params("attachment_id")

	// Try as incident attachment first (UUID format)
	if attachmentID, err := uuid.Parse(attachmentIDStr); err == nil {
		if attachment, err := h.incidentService.GetAttachment(c.UserContext(), attachmentID); err == nil {
			ctx, path := c.UserContext(), attachment.FilePath
			size, err := h.storage.StatFile(ctx, path)
			if err != nil {
				return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(ctx, "failed_to_retrieve_file"))
			}
			fetch := func(start, end int64) (io.ReadCloser, error) { return h.storage.GetFileRange(ctx, path, start, end) }
			return sendWithRange(c, fetch, size, attachment.MimeType, attachment.FileName, disposition)
		}
	}

	// If not found as incident attachment, try notification attachment
	_, attachment, err := h.notificationService.FindNotificationByAttachmentID(c.UserContext(), attachmentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "attachment_not_found"))
	}

	if attachment.StoragePath == "" {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "attachment_storage_path_not_found"))
	}

	ctx, path := c.UserContext(), attachment.StoragePath
	size, err := h.storage.StatFile(ctx, path)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(ctx, "failed_to_retrieve_attachment"))
	}
	fetch := func(start, end int64) (io.ReadCloser, error) { return h.storage.GetFileRange(ctx, path, start, end) }
	return sendWithRange(c, fetch, size, attachment.ContentType, attachment.Filename, disposition)
}

// sendWithRange streams the requested bytes of an object, fetching only that
// range from storage. fetch returns a reader for bytes start..end inclusive.
func sendWithRange(c *fiber.Ctx, fetch func(start, end int64) (io.ReadCloser, error), size int64, contentType, filename, disposition string) error {
	c.Set("Content-Type", contentType)
	if cd := mime.FormatMediaType(disposition, map[string]string{"filename": filename}); cd != "" {
		c.Set("Content-Disposition", cd)
	} else {
		c.Set("Content-Disposition", disposition)
	}
	c.Set("Accept-Ranges", "bytes")

	start, end, status := int64(0), size-1, fiber.StatusOK
	if rangeHeader := c.Get("Range"); rangeHeader != "" {
		var ok bool
		start, end, ok = parseByteRange(rangeHeader, size)
		if !ok {
			c.Set("Content-Range", fmt.Sprintf("bytes */%d", size))
			return c.SendStatus(fiber.StatusRequestedRangeNotSatisfiable)
		}
		status = fiber.StatusPartialContent
	}
	if size == 0 {
		return c.SendStatus(fiber.StatusOK)
	}

	body, err := fetch(start, end)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_read_file"))
	}
	if status == fiber.StatusPartialContent {
		c.Status(status)
		c.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
	}
	return c.SendStream(body, int(end-start+1))
}

// parseByteRange parses a single "bytes=a-b", "bytes=a-" or "bytes=-n" range
// against size. ok is false for malformed, multi-range or unsatisfiable input.
func parseByteRange(header string, size int64) (start, end int64, ok bool) {
	spec, found := strings.CutPrefix(strings.TrimSpace(header), "bytes=")
	if !found || strings.Contains(spec, ",") || size <= 0 {
		return 0, 0, false
	}
	first, last, found := strings.Cut(strings.TrimSpace(spec), "-")
	if !found {
		return 0, 0, false
	}
	first, last = strings.TrimSpace(first), strings.TrimSpace(last)

	switch {
	case first == "": // suffix: last n bytes
		n, err := strconv.ParseInt(last, 10, 64)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		if n > size {
			n = size
		}
		return size - n, size - 1, true
	default:
		start, err := strconv.ParseInt(first, 10, 64)
		if err != nil || start < 0 || start >= size {
			return 0, 0, false
		}
		end := size - 1
		if last != "" {
			e, err := strconv.ParseInt(last, 10, 64)
			if err != nil || e < start {
				return 0, 0, false
			}
			if e < end {
				end = e
			}
		}
		return start, end, true
	}
}
