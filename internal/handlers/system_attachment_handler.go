package handlers

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/internal/storage"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SystemAttachmentHandler struct {
	db      *gorm.DB
	storage *storage.MinIOStorage
	service *services.SystemAttachmentService
}

func NewSystemAttachmentHandler(db *gorm.DB, storage *storage.MinIOStorage, service *services.SystemAttachmentService) *SystemAttachmentHandler {
	return &SystemAttachmentHandler{db: db, storage: storage, service: service}
}

func (h *SystemAttachmentHandler) toResponse(ctx context.Context, a *models.SystemAttachment) models.SystemAttachmentResponse {
	url, previewURL := services.SystemAttachmentURLs(utils.RequestBaseURL(ctx), a.ID)
	return models.SystemAttachmentResponse{
		ID: a.ID, Module: a.Module, BatchID: a.BatchID, FileName: a.FileName, FileSize: a.FileSize,
		MimeType: a.MimeType, URL: url, PreviewURL: previewURL, CreatedAt: a.CreatedAt,
	}
}

// writeSystemAttachmentError maps a service error to a translated response.
func writeSystemAttachmentError(c *fiber.Ctx, err error) error {
	ctx := c.UserContext()
	var pe *services.SystemAttachmentError
	if errors.As(err, &pe) {
		msg := i18n.T(ctx, pe.Key)
		if len(pe.Args) > 0 {
			msg = i18n.Tf(ctx, pe.Key, pe.Args...)
		}
		return utils.ErrorResponse(c, pe.Status, msg)
	}
	return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(ctx, "internal_server_error"))
}

// Upload handles POST /api/v1/system-attachments (authenticated).
// Multipart field "files" accepts multiple images; all go in one unique folder.
func (h *SystemAttachmentHandler) Upload(c *fiber.Ctx) error {
	userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	if !ok {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "unauthorized_not_authenticated"))
	}
	form, err := c.MultipartForm()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "pubatt_invalid_multipart"))
	}
	batchID, records, err := h.service.UploadImages(c.UserContext(), models.SystemAttachmentDefaultModule, form.File["files"], userID)
	if err != nil {
		return writeSystemAttachmentError(c, err)
	}
	out := make([]models.SystemAttachmentResponse, len(records))
	for i := range records {
		out[i] = h.toResponse(c.UserContext(), &records[i])
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "pubatt_uploaded"), fiber.Map{"batch_id": batchID, "attachments": out})
}

// ListByBatch handles GET /api/v1/system/attachments/batch/:batch_id (public).
func (h *SystemAttachmentHandler) ListByBatch(c *fiber.Ctx) error {
	batchID, err := uuid.Parse(c.Params("batch_id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "pubatt_invalid_batch_id"))
	}
	var records []models.SystemAttachment
	if err := h.db.WithContext(c.UserContext()).Where("batch_id = ?", batchID).Order("created_at ASC").Find(&records).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "pubatt_fetch_failed"))
	}
	if len(records) == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "attachment_not_found"))
	}
	out := make([]models.SystemAttachmentResponse, len(records))
	for i := range records {
		out[i] = h.toResponse(c.UserContext(), &records[i])
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "success"), fiber.Map{"batch_id": batchID, "attachments": out})
}

// Download handles GET /api/v1/system/attachments/:id (public).
func (h *SystemAttachmentHandler) Download(c *fiber.Ctx) error { return h.serve(c, "attachment") }

// Preview handles GET /api/v1/system/attachments/:id/preview (public, inline).
func (h *SystemAttachmentHandler) Preview(c *fiber.Ctx) error { return h.serve(c, "inline") }

func (h *SystemAttachmentHandler) serve(c *fiber.Ctx, disposition string) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	ctx := c.UserContext()
	var a models.SystemAttachment
	if err := h.db.WithContext(ctx).First(&a, "id = ?", id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "attachment_not_found"))
	}
	// Defence in depth: never serve anything outside the public folder.
	if !strings.HasPrefix(a.FilePath, services.SystemAttachmentFolder+"/") {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "attachment_not_found"))
	}
	size, err := h.storage.StatFile(ctx, a.FilePath)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "pubatt_file_not_found"))
	}
	c.Set("X-Content-Type-Options", "nosniff")
	c.Set("Cache-Control", "public, max-age=3600")
	fetch := func(start, end int64) (io.ReadCloser, error) {
		return h.storage.GetFileRange(ctx, a.FilePath, start, end)
	}
	return sendWithRange(c, fetch, size, a.MimeType, a.FileName, disposition)
}
