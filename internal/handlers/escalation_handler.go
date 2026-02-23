package handlers

import (
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

type EscalationHandler struct {
	service *services.EscalationService
}

func NewEscalationHandler(service *services.EscalationService) *EscalationHandler {
	return &EscalationHandler{service: service}
}

func (h *EscalationHandler) Create(c *fiber.Ctx) error {
	var req models.EscalationSLA

	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())

	}

	if err := h.service.CreateConfig(c.Context(), &req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Escalation config created", nil)
}

func (h *EscalationHandler) List(c *fiber.Ctx) error {

	configs, err := h.service.GetConfigs(c.Context())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Escalation configs fetched", configs)
}
