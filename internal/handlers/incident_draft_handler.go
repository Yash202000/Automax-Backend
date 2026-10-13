package handlers

import (
	"errors"
	"fmt"
	"io"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// IncidentDraftHandler serves the owner-only draft endpoints. Every call is scoped to
// the authenticated user, so another user's draft is indistinguishable from a missing one.
type IncidentDraftHandler struct {
	service services.IncidentDraftService
}

func NewIncidentDraftHandler(service services.IncidentDraftService) *IncidentDraftHandler {
	return &IncidentDraftHandler{service: service}
}

func draftOwner(c *fiber.Ctx) uuid.UUID {
	return c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
}

// parseSaveRequest binds and validates a save body (format checks only, nothing required).
// On failure it has already written the response, and ok is false.
func (h *IncidentDraftHandler) parseSaveRequest(c *fiber.Ctx) (req models.IncidentDraftSaveRequest, ok bool, err error) {
	if perr := c.BodyParser(&req); perr != nil {
		return req, false, utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	validationErrors := validation.ValidateStruct(c.UserContext(), &req.IncidentDraftPayload)
	if req.AlternateMobileNumber != "" && utils.SameMobile(req.AlternateMobileNumber, req.CallerPhone) {
		if validationErrors == nil {
			validationErrors = map[string]string{}
		}
		validationErrors["caller_alternate_mobile_number"] = i18n.T(c.UserContext(), "alternate_mobile_same_as_primary")
	}
	if len(validationErrors) != 0 {
		return req, false, c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	return req, true, nil
}

// draftError maps service errors to HTTP responses.
func (h *IncidentDraftHandler) draftError(c *fiber.Ctx, err error) error {
	ctx := c.UserContext()
	var verr *services.DraftValidationError
	switch {
	case errors.As(err, &verr):
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   i18n.T(ctx, "incident_draft_missing_fields"),
			"errors":  verr.Fields,
		})
	case errors.Is(err, services.ErrDraftNotFound), errors.Is(err, repository.ErrDraftAttachmentNotFound):
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(ctx, err.Error()))
	case errors.Is(err, services.ErrDraftVersionConflict), errors.Is(err, services.ErrDraftRequestPending), errors.Is(err, services.ErrDraftPublished):
		return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(ctx, err.Error()))
	case errors.Is(err, services.ErrDraftEmpty):
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(ctx, err.Error()))

	// Publish runs CreateIncident, so its sentinels apply here too.
	case errors.Is(err, services.ErrEmptyWorkflow):
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(ctx, "empty_workflow"))
	case errors.Is(err, services.ErrDuplicateIncident):
		return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(ctx, "duplicate_incident"))
	case errors.Is(err, services.ErrInvalidLocation):
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(ctx, "invalid_location_class"))
	case errors.Is(err, services.ErrLocationNotFound),
		errors.Is(err, services.ErrClassificationNotFound),
		errors.Is(err, services.ErrLocationNotSelectable),
		errors.Is(err, services.ErrClassificationNotSelectable):
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(ctx, err.Error()))
	default:
		return utils.InternalErrorResponse(c, err, i18n.T(ctx, "internal_server_error"))
	}
}

func parseDraftID(c *fiber.Ctx) (uuid.UUID, error) {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return uuid.Nil, utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	return id, nil
}

// Create handles POST /incident-drafts
func (h *IncidentDraftHandler) Create(c *fiber.Ctx) error {
	req, ok, err := h.parseSaveRequest(c)
	if !ok {
		return err
	}
	resp, err := h.service.Create(c.UserContext(), &req, draftOwner(c))
	if err != nil {
		return h.draftError(c, err)
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "incident_draft_created"), resp)
}

// Update handles PUT /incident-drafts/:id (full replace; body carries the version).
func (h *IncidentDraftHandler) Update(c *fiber.Ctx) error {
	id, err := parseDraftID(c)
	if err != nil {
		return err
	}
	req, ok, err := h.parseSaveRequest(c)
	if !ok {
		return err
	}
	resp, err := h.service.Update(c.UserContext(), id, &req, draftOwner(c))
	if err != nil {
		return h.draftError(c, err)
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "incident_draft_updated"), resp)
}

// GetByID handles GET /incident-drafts/:id
func (h *IncidentDraftHandler) GetByID(c *fiber.Ctx) error {
	id, err := parseDraftID(c)
	if err != nil {
		return err
	}
	resp, err := h.service.Get(c.UserContext(), id, draftOwner(c))
	if err != nil {
		return h.draftError(c, err)
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "incident_draft_retrieved"), resp)
}

// List handles GET /incident-drafts
func (h *IncidentDraftHandler) List(c *fiber.Ctx) error {
	var filter models.IncidentDraftFilter
	if err := c.QueryParser(&filter); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &filter); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 20
	}

	items, total, err := h.service.List(c.UserContext(), draftOwner(c), &filter)
	if err != nil {
		return h.draftError(c, err)
	}
	return utils.PaginatedSuccessResponse(c, items, filter.Page, filter.Limit, total)
}

// Count handles GET /incident-drafts/count (the nav badge).
func (h *IncidentDraftHandler) Count(c *fiber.Ctx) error {
	n, err := h.service.Count(c.UserContext(), draftOwner(c))
	if err != nil {
		return h.draftError(c, err)
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "incident_drafts_retrieved"), fiber.Map{"count": n})
}

// Delete handles DELETE /incident-drafts/:id
func (h *IncidentDraftHandler) Delete(c *fiber.Ctx) error {
	id, err := parseDraftID(c)
	if err != nil {
		return err
	}
	if err := h.service.Delete(c.UserContext(), id, draftOwner(c)); err != nil {
		return h.draftError(c, err)
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "incident_draft_deleted"), nil)
}

// Publish handles POST /incident-drafts/:id/publish
func (h *IncidentDraftHandler) Publish(c *fiber.Ctx) error {
	id, err := parseDraftID(c)
	if err != nil {
		return err
	}
	incident, err := h.service.Publish(c.UserContext(), id, draftOwner(c))
	if err != nil {
		return h.draftError(c, err)
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "incident_draft_published"), incident)
}

// UploadAttachment handles POST /incident-drafts/:id/attachments (multipart field "file").
func (h *IncidentDraftHandler) UploadAttachment(c *fiber.Ctx) error {
	id, err := parseDraftID(c)
	if err != nil {
		return err
	}
	header, ferr := c.FormFile("file")
	if ferr != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "incident_draft_file_required"))
	}
	src, oerr := header.Open()
	if oerr != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_read_file"))
	}
	defer src.Close()

	resp, err := h.service.AddAttachment(c.UserContext(), id, draftOwner(c), src, header)
	if err != nil {
		if errors.Is(err, services.ErrDraftNotFound) || errors.Is(err, services.ErrDraftPublished) {
			return h.draftError(c, err)
		}
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "failed_to_upload_file"))
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "incident_draft_attachment_uploaded"), resp)
}

// ListAttachments handles GET /incident-drafts/:id/attachments
func (h *IncidentDraftHandler) ListAttachments(c *fiber.Ctx) error {
	id, err := parseDraftID(c)
	if err != nil {
		return err
	}
	resp, err := h.service.ListAttachments(c.UserContext(), id, draftOwner(c))
	if err != nil {
		return h.draftError(c, err)
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "incident_draft_attachments_retrieved"), resp)
}

// DownloadAttachment handles GET /incident-drafts/:id/attachments/:attachment_id
func (h *IncidentDraftHandler) DownloadAttachment(c *fiber.Ctx) error {
	id, err := parseDraftID(c)
	if err != nil {
		return err
	}
	attID, perr := uuid.Parse(c.Params("attachment_id"))
	if perr != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_attachment_id"))
	}
	att, file, err := h.service.OpenAttachment(c.UserContext(), id, attID, draftOwner(c))
	if err != nil {
		if errors.Is(err, services.ErrDraftNotFound) || errors.Is(err, repository.ErrDraftAttachmentNotFound) {
			return h.draftError(c, err)
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_retrieve_file"))
	}
	defer file.Close()

	data, rerr := io.ReadAll(file)
	if rerr != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_read_file"))
	}
	c.Set("Content-Type", att.MimeType)
	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", att.FileName))
	return c.Send(data)
}

// DeleteAttachment handles DELETE /incident-drafts/:id/attachments/:attachment_id
func (h *IncidentDraftHandler) DeleteAttachment(c *fiber.Ctx) error {
	id, err := parseDraftID(c)
	if err != nil {
		return err
	}
	attID, perr := uuid.Parse(c.Params("attachment_id"))
	if perr != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_attachment_id"))
	}
	if err := h.service.DeleteAttachment(c.UserContext(), id, attID, draftOwner(c)); err != nil {
		return h.draftError(c, err)
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "incident_draft_attachment_deleted"), nil)
}
