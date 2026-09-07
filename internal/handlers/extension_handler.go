package handlers

import (
	"errors"
	"log"
	"strconv"
	"strings"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type ExtensionHandler struct {
	service services.ExtensionService
	cfg     *config.Config
}

func NewExtensionHandler(service services.ExtensionService, cfg *config.Config) *ExtensionHandler {
	return &ExtensionHandler{service: service, cfg: cfg}
}

// clientCodeAllowed guards the feature to the EPM940 deployment (defense-in-depth;
// routes are also only registered when CLIENT_CODE == EPM940).
func (h *ExtensionHandler) clientCodeAllowed() bool {
	return strings.EqualFold(strings.TrimSpace(h.cfg.ClientCode), constants.CLIENT_CODE.EPM940)
}

func (h *ExtensionHandler) forbiddenClient(c *fiber.Ctx) error {
	return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(c.UserContext(), "extension_mgmt_disabled"))
}

func (h *ExtensionHandler) actorID(c *fiber.Ctx) (uuid.UUID, bool) {
	id, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	return id, ok
}

// List returns the PBX extension pool with assignment status.
// Optional query: ?status=available|assigned
func (h *ExtensionHandler) List(c *fiber.Ctx) error {
	if !h.clientCodeAllowed() {
		return h.forbiddenClient(c)
	}
	result, err := h.service.ListExtensions(c.UserContext(), c.Query("status"))
	if err != nil {
		log.Printf("[ExtensionHandler] PBX request failed: %v", err)
		return utils.ErrorResponse(c, fiber.StatusBadGateway, i18n.T(c.UserContext(), "pbx_request_failed"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Extensions retrieved", result)
}

// Mine returns the caller's currently assigned extension (or null).
func (h *ExtensionHandler) Mine(c *fiber.Ctx) error {
	if !h.clientCodeAllowed() {
		return h.forbiddenClient(c)
	}
	actor, ok := h.actorID(c)
	if !ok {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "unauthorized"))
	}
	result, err := h.service.MyExtension(c.UserContext(), actor)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Current extension retrieved", result)
}

// History returns the assignment history for a given extension.
func (h *ExtensionHandler) History(c *fiber.Ctx) error {
	if !h.clientCodeAllowed() {
		return h.forbiddenClient(c)
	}
	extension := strings.TrimSpace(c.Params("extension"))
	if extension == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "extension_required"))
	}
	limit := 100
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	result, err := h.service.GetHistory(c.UserContext(), extension, limit)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Extension history retrieved", result)
}

// Assign assigns (or reassigns/takes over) an extension to a user.
func (h *ExtensionHandler) Assign(c *fiber.Ctx) error {
	if !h.clientCodeAllowed() {
		return h.forbiddenClient(c)
	}
	actor, ok := h.actorID(c)
	if !ok {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "unauthorized"))
	}
	var req models.ExtensionAssignRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}

	resp, err := h.service.AssignExtension(c.UserContext(), req, actor)
	if err != nil {
		if errors.Is(err, services.ErrExtensionNotInPool) {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), err.Error()))
		}
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}
	if resp == nil {
		return utils.SuccessResponse(c, fiber.StatusOK, "Extension already assigned to this user", nil)
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Extension assigned", resp)
}

// Release releases an extension from its current holder.
func (h *ExtensionHandler) Release(c *fiber.Ctx) error {
	if !h.clientCodeAllowed() {
		return h.forbiddenClient(c)
	}
	actor, ok := h.actorID(c)
	if !ok {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "unauthorized"))
	}
	extension := strings.TrimSpace(c.Params("extension"))
	if extension == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "extension_required"))
	}
	if err := h.service.ReleaseExtension(c.UserContext(), extension, actor); err != nil {
		if errors.Is(err, services.ErrExtensionNotAssigned) {
			return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), err.Error()))
		}
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Extension released", nil)
}

// Create creates a new extension on the PBX (does not assign it).
func (h *ExtensionHandler) Create(c *fiber.Ctx) error {
	if !h.clientCodeAllowed() {
		return h.forbiddenClient(c)
	}
	actor, ok := h.actorID(c)
	if !ok {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "unauthorized"))
	}
	var req models.ExtensionCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	if err := h.service.CreateExtension(c.UserContext(), req, actor); err != nil {
		log.Printf("[ExtensionHandler] PBX request failed: %v", err)
		return utils.ErrorResponse(c, fiber.StatusBadGateway, i18n.T(c.UserContext(), "pbx_request_failed"))
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "Extension created", fiber.Map{"extension": strings.TrimSpace(req.Extension)})
}
