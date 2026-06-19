package handlers

import (
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// EscalationHandler exposes HTTP endpoints for SLA breach notification logs.
type EscalationHandler struct {
	service *services.EscalationService
}

func NewEscalationHandler(service *services.EscalationService) *EscalationHandler {
	return &EscalationHandler{service: service}
}

// List returns all SLA breach notification records (most recent first).
//
// GET /api/v1/escalation
func (h *EscalationHandler) List(c *fiber.Ctx) error {
	records, err := h.service.GetBreachLogs(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	resp := make([]models.EscalationSLAResponse, len(records))
	for i, r := range records {
		resp[i] = models.ToEscalationSLAResponse(&r)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "sla_breach_logs_fetched"), resp)
}

// ListByIncident returns all SLA breach notifications for a specific incident.
//
// GET /api/v1/escalation/incident/:incident_id
func (h *EscalationHandler) ListByIncident(c *fiber.Ctx) error {
	idStr := c.Params("incident_id")
	incidentID, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id_param"))
	}

	records, err := h.service.GetBreachLogsByIncident(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	resp := make([]models.EscalationSLAResponse, len(records))
	for i, r := range records {
		resp[i] = models.ToEscalationSLAResponse(&r)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "incident_sla_breach_logs"), resp)
}
