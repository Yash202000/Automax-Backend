package handlers

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/automax/backend/internal/middleware"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type ConfigHandler struct {
	service      services.ConfigService
	actionLogSvc services.ActionLogService
}

func NewConfigHandler(service services.ConfigService, actionLogSvc services.ActionLogService) *ConfigHandler {
	return &ConfigHandler{service: service, actionLogSvc: actionLogSvc}
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
	case errors.Is(err, models.ErrConfigNotFound):
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(ctx, err.Error()))
	case errors.Is(err, models.ErrConfigExists), errors.Is(err, models.ErrConfigVersionConflict):
		return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(ctx, err.Error()))
	case errors.Is(err, models.ErrConfigNameRequired), errors.Is(err, models.ErrConfigNameInvalid),
		errors.Is(err, models.ErrConfigValueNotJSON), errors.Is(err, models.ErrConfigInvalidEnvironment):
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(ctx, err.Error()))
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
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "config_updated"), resp)
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
	items := h.service.ListCached()
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "configs_retrieved"), fiber.Map{
		"environment": h.service.Environment(),
		"configs":     items,
		"total":       len(items),
	})
}

// GetCurrent GET /configs/:module/:name
// Served from the in-memory cache. A missing or secret config is a 404; no default is substituted.
func (h *ConfigHandler) GetCurrent(c *fiber.Ctx) error {
	resp, ok := h.service.GetCached(c.Params("module"), c.Params("name"))
	if !ok {
		return h.respondError(c, models.ErrConfigNotFound)
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "config_retrieved"), resp)
}
