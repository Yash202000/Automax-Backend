package handlers

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/automax/backend/internal/middleware"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type ConfigHandler struct {
	service       services.ConfigService
	actionLogSvc  services.ActionLogService
	attachmentSvc *services.ConfigAttachmentService
}

func NewConfigHandler(service services.ConfigService, actionLogSvc services.ActionLogService, attachmentSvc *services.ConfigAttachmentService) *ConfigHandler {
	return &ConfigHandler{service: service, actionLogSvc: actionLogSvc, attachmentSvc: attachmentSvc}
}

const configLogModule = "configs"

// Audit entries record the acting user, IP and user agent (via middleware.LogAction).
// Every value passed in here comes from ConfigResponse / ConfigChange, which are already
// masked for secret configs, so secrets never reach the action log.
func (h *ConfigHandler) audit(c *fiber.Ctx, action string, id uuid.UUID, description string, oldValue, newValue any) {
	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      action,
		Module:      configLogModule,
		ResourceID:  id.String(),
		Description: description,
		OldValue:    oldValue,
		NewValue:    newValue,
	})
}

// respondError maps config errors to an HTTP status and a translated message (Accept-Language: en | ar).
// Config errors carry their i18n key as the message. Unexpected errors are logged and answered with a
// generic message, never the raw error text.
func (h *ConfigHandler) respondError(c *fiber.Ctx, err error) error {
	ctx := c.UserContext()
	switch {
	case errors.Is(err, models.ErrConfigNotFound), errors.Is(err, models.ErrConfigAttachmentNotFound):
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(ctx, err.Error()))
	case errors.Is(err, models.ErrConfigExists), errors.Is(err, models.ErrConfigVersionConflict):
		return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(ctx, err.Error()))
	case errors.Is(err, models.ErrConfigNameRequired), errors.Is(err, models.ErrConfigNameInvalid),
		errors.Is(err, models.ErrConfigValueNotJSON), errors.Is(err, models.ErrConfigInvalidEnvironment),
		errors.Is(err, models.ErrConfigPublicSecret), errors.Is(err, models.ErrConfigAttachmentType),
		errors.Is(err, models.ErrConfigAttachmentKeyBad), errors.Is(err, models.ErrConfigAttachmentModuleReserved),
		errors.Is(err, models.ErrConfigAttachmentsManaged):
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(ctx, err.Error()))
	case errors.As(err, new(*services.SystemAttachmentError)):
		return writeSystemAttachmentError(c, err)
	default:
		return utils.InternalErrorResponse(c, err, i18n.T(ctx, "internal_server_error"))
	}
}

func parseConfigID(c *fiber.Ctx) (uuid.UUID, error) {
	return uuid.Parse(c.Params("id"))
}

// Create POST /admin/configs
func (h *ConfigHandler) Create(c *fiber.Ctx) error {
	var req models.ConfigCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if errs := validation.ValidateStruct(c.UserContext(), &req); len(errs) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": errs})
	}
	resp, err := h.service.Create(c.UserContext(), &req)
	if err != nil {
		return h.respondError(c, err)
	}
	h.audit(c, "create", resp.ID,
		fmt.Sprintf("Config created: %s/%s (secret=%t)", resp.Environment, resp.Module+"."+resp.Name, resp.IsSecret), nil, resp)
	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "config_created"), resp)
}

// List GET /admin/configs?environment=&module=&search=&page=&limit=
// environment is optional: empty = the server's environment (APP_ENV), "all" = every environment.
func (h *ConfigHandler) List(c *fiber.Ctx) error {
	env := strings.ToLower(strings.TrimSpace(c.Query("environment")))
	if env != "" && env != "all" && !slices.Contains(models.ConfigEnvironments, env) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "config_invalid_environment_list"))
	}
	page := c.QueryInt("page", 1)
	if page < 1 {
		page = 1
	}
	limit := c.QueryInt("limit", 50)
	if limit < 1 || limit > 200 {
		limit = 50
	}

	items, total, err := h.service.List(c.UserContext(), &models.ConfigListFilter{
		Environment: env,
		Module:      strings.ToLower(strings.TrimSpace(c.Query("module"))),
		Search:      c.Query("search"),
		Page:        page,
		Limit:       limit,
	})
	if err != nil {
		return h.respondError(c, err)
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "configs_retrieved"), fiber.Map{
		"configs": items,
		"total":   total,
		"page":    page,
		"limit":   limit,
	})
}

// Get GET /admin/configs/:id
func (h *ConfigHandler) Get(c *fiber.Ctx) error {
	id, err := parseConfigID(c)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	resp, err := h.service.Get(c.UserContext(), id)
	if err != nil {
		return h.respondError(c, err)
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "config_retrieved"), resp)
}

// GetByName GET /admin/configs/by-name/:module/:name
// Reads from the database, for the server's own environment (APP_ENV).
func (h *ConfigHandler) GetByName(c *fiber.Ctx) error {
	resp, err := h.service.GetByName(c.UserContext(), c.Params("module"), c.Params("name"))
	if err != nil {
		return h.respondError(c, err)
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "config_retrieved"), resp)
}

// Update PUT /admin/configs/:id
func (h *ConfigHandler) Update(c *fiber.Ctx) error {
	id, err := parseConfigID(c)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var req models.ConfigUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if errs := validation.ValidateStruct(c.UserContext(), &req); len(errs) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": errs})
	}
	resp, changes, err := h.service.Update(c.UserContext(), id, &req)
	if err != nil {
		return h.respondError(c, err)
	}

	h.auditUpdate(c, resp, changes)
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "config_updated"), resp)
}

// auditUpdate logs an "update" action with the field-level diff returned by the service.
func (h *ConfigHandler) auditUpdate(c *fiber.Ctx, resp *models.ConfigResponse, changes []models.ConfigChange) {
	fields := make([]string, len(changes))
	oldVals := make(map[string]any, len(changes))
	newVals := make(map[string]any, len(changes))
	for i, ch := range changes {
		fields[i] = ch.Field
		oldVals[ch.Field] = ch.Old
		newVals[ch.Field] = ch.New
	}
	desc := fmt.Sprintf("Config updated: %s/%s (v%d), changed: %s", resp.Environment, resp.Module+"."+resp.Name, resp.Version, strings.Join(fields, ", "))
	if len(fields) == 0 {
		desc = fmt.Sprintf("Config updated: %s/%s (v%d), no field changes", resp.Environment, resp.Module+"."+resp.Name, resp.Version)
	}
	h.audit(c, "update", resp.ID, desc, oldVals, newVals)
}

// Delete DELETE /admin/configs/:id
func (h *ConfigHandler) Delete(c *fiber.Ctx) error {
	id, err := parseConfigID(c)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	existing, err := h.service.Get(c.UserContext(), id)
	if err != nil {
		return h.respondError(c, err)
	}
	if err := h.service.Delete(c.UserContext(), id); err != nil {
		return h.respondError(c, err)
	}
	h.audit(c, "delete", id,
		fmt.Sprintf("Config deleted: %s/%s (secret=%t)", existing.Environment, existing.Module+"."+existing.Name, existing.IsSecret), existing, nil)
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "config_deleted"), nil)
}

// ListCurrent GET /configs
// Served from the in-memory cache: all non-secret configs of the server's current environment.
func (h *ConfigHandler) ListCurrent(c *fiber.Ctx) error {
	items := h.service.ListCached(c.UserContext())
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "configs_retrieved"), fiber.Map{
		"environment": h.service.Environment(),
		"configs":     items,
		"total":       len(items),
	})
}

// GetCurrent GET /configs/:module/:name
// Served from the in-memory cache. A missing or secret config is a 404; no default is substituted.
func (h *ConfigHandler) GetCurrent(c *fiber.Ctx) error {
	resp, ok := h.service.GetCached(c.UserContext(), c.Params("module"), c.Params("name"))
	if !ok {
		return h.respondError(c, models.ErrConfigNotFound)
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "config_retrieved"), resp)
}

// ListPublic GET /public/configs (no authentication)
// Public, non-secret configs of the server's current environment, from the in-memory cache.
func (h *ConfigHandler) ListPublic(c *fiber.Ctx) error {
	items := h.service.ListPublic(c.UserContext())
	c.Set("Cache-Control", "public, max-age=60")
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "configs_retrieved"), fiber.Map{
		"configs": items,
		"total":   len(items),
	})
}

// GetPublic GET /public/configs/:module/:name (no authentication)
// A missing config, a non-public one and a secret one all answer 404.
func (h *ConfigHandler) GetPublic(c *fiber.Ctx) error {
	resp, ok := h.service.GetPublic(c.UserContext(), c.Params("module"), c.Params("name"))
	if !ok {
		return h.respondError(c, models.ErrConfigNotFound)
	}
	c.Set("Cache-Control", "public, max-age=60")
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "config_retrieved"), resp)
}

// UploadAttachments POST /admin/configs/:module/attachments/:name
// Adds images (multipart field "files"; "file" is accepted too) to the attachment config
// <module>-attachments/<name>, creating it on first use. module and name are lowercase letters
// and digits only; the server adds the "-attachments" suffix.
func (h *ConfigHandler) UploadAttachments(c *fiber.Ctx) error { return h.saveAttachments(c, false) }

// ReplaceAttachments PUT /admin/configs/:module/attachments/:name
// Same as UploadAttachments, but the uploaded images become the whole set (e.g. a single logo).
func (h *ConfigHandler) ReplaceAttachments(c *fiber.Ctx) error { return h.saveAttachments(c, true) }

func (h *ConfigHandler) saveAttachments(c *fiber.Ctx, replace bool) error {
	ctx := c.UserContext()
	userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	if !ok {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(ctx, "unauthorized_not_authenticated"))
	}
	form, err := c.MultipartForm()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(ctx, "pubatt_invalid_multipart"))
	}
	files := form.File["files"]
	if len(files) == 0 {
		files = form.File["file"] // a single image (e.g. a logo) is commonly sent as "file"
	}
	save := h.attachmentSvc.Add
	if replace {
		save = h.attachmentSvc.Replace
	}
	resp, changes, err := save(ctx, c.Params("module"), c.Params("name"), files, userID)
	if err != nil {
		return h.respondError(c, err)
	}
	h.auditUpdate(c, resp, changes)
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(ctx, "config_attachments_updated"), resp)
}

// DeleteAttachment DELETE /admin/configs/:module/attachments/:name/:attachment_id
// Removes one image from the attachment config and deletes its file.
func (h *ConfigHandler) DeleteAttachment(c *fiber.Ctx) error {
	ctx := c.UserContext()
	attachmentID, err := uuid.Parse(c.Params("attachment_id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(ctx, "invalid_id"))
	}
	resp, changes, err := h.attachmentSvc.Remove(ctx, c.Params("module"), c.Params("name"), attachmentID)
	if err != nil {
		return h.respondError(c, err)
	}
	h.auditUpdate(c, resp, changes)
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(ctx, "config_attachments_updated"), resp)
}
