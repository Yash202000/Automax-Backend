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
			file, size, err := h.storage.GetFileSeeker(c.UserContext(), attachment.FilePath)
			if err != nil {
				return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_retrieve_file"))
			}
			return sendWithRange(c, file, size, attachment.MimeType, attachment.FileName, disposition)
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

	file, size, err := h.storage.GetFileSeeker(c.UserContext(), attachment.StoragePath)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "failed_to_retrieve_attachment"))
	}
	return sendWithRange(c, file, size, attachment.ContentType, attachment.Filename, disposition)
}

// limitedReadCloser bounds reads and closes the underlying reader when the
// response has been written (fasthttp closes streams that implement io.Closer).
type limitedReadCloser struct {
	io.Reader
	io.Closer
}

// sendWithRange streams file honouring a single-range Range header. It takes
// ownership of file and closes it.
func sendWithRange(c *fiber.Ctx, file io.ReadSeekCloser, size int64, contentType, filename, disposition string) error {
	c.Set("Content-Type", contentType)
	if cd := mime.FormatMediaType(disposition, map[string]string{"filename": filename}); cd != "" {
		c.Set("Content-Disposition", cd)
	} else {
		c.Set("Content-Disposition", disposition)
	}
	c.Set("Accept-Ranges", "bytes")

	rangeHeader := c.Get("Range")
	if rangeHeader == "" {
		return c.SendStream(file, int(size))
	}

	start, end, ok := parseByteRange(rangeHeader, size)
	if !ok {
		file.Close()
		c.Set("Content-Range", fmt.Sprintf("bytes */%d", size))
		return c.SendStatus(fiber.StatusRequestedRangeNotSatisfiable)
	}
	if _, err := file.Seek(start, io.SeekStart); err != nil {
		file.Close()
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_read_file"))
	}

	length := end - start + 1
	c.Status(fiber.StatusPartialContent)
	c.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, size))
	return c.SendStream(limitedReadCloser{Reader: io.LimitReader(file, length), Closer: file}, int(length))
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
