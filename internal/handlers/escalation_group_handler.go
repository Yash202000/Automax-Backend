package handlers

import (
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// EscalationGroupHandler exposes HTTP endpoints for managing custom escalation groups.
type EscalationGroupHandler struct {
	service *services.EscalationGroupService
}

func NewEscalationGroupHandler(service *services.EscalationGroupService) *EscalationGroupHandler {
	return &EscalationGroupHandler{service: service}
}

// Create creates a new escalation group.
//
// POST /api/v1/admin/escalation-groups
func (h *EscalationGroupHandler) Create(c *fiber.Ctx) error {
	var req models.CreateEscalationGroupRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if errs := validation.ValidateStruct(c.UserContext(), &req); len(errs) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": errs})
	}
	resp, err := h.service.Create(c.UserContext(), &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "escalation_group_created"), resp)
}

// List returns all escalation groups (active and inactive).
//
// GET /api/v1/admin/escalation-groups
func (h *EscalationGroupHandler) List(c *fiber.Ctx) error {
	resp, err := h.service.List(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "escalation_groups_fetched"), resp)
}

// GetByID returns a single escalation group by ID.
//
// GET /api/v1/admin/escalation-groups/:id
func (h *EscalationGroupHandler) GetByID(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	resp, err := h.service.GetByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "escalation_group_fetched"), resp)
}

// Update updates an existing escalation group.
// PUT /api/v1/admin/escalation-groups/:id
func (h *EscalationGroupHandler) Update(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var req models.UpdateEscalationGroupRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if errs := validation.ValidateStruct(c.UserContext(), &req); len(errs) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": errs})
	}
	resp, err := h.service.Update(c.UserContext(), id, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "escalation_group_updated"), resp)
}

// Delete permanently deletes an escalation group.

func (h *EscalationGroupHandler) Delete(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	if err := h.service.Delete(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "escalation_group_deleted"), nil)
}
