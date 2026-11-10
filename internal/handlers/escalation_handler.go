package handlers

import (
	"fmt"
	"time"

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

// escalationLogQuery keeps date bounds as strings for manual parsing.
type escalationLogQuery struct {
	IncidentID       *uuid.UUID `query:"incident_id"`
	IncidentNumber   string     `query:"incident_number"`
	Level            *int       `query:"level"`
	ClassificationID *uuid.UUID `query:"classification_id"`
	DateFrom         string     `query:"date_from"`
	DateTo           string     `query:"date_to"`
	Person           string     `query:"person"`
	EscalationType   string     `query:"escalation_type"`
	Page             int        `query:"page"`
	Limit            int        `query:"limit"`
}

func parseEscalationLogFilter(c *fiber.Ctx) (*models.EscalationLogFilter, error) {
	var q escalationLogQuery
	if err := c.QueryParser(&q); err != nil {
		return nil, err
	}

	filter := &models.EscalationLogFilter{
		IncidentID:       q.IncidentID,
		IncidentNumber:   q.IncidentNumber,
		Level:            q.Level,
		ClassificationID: q.ClassificationID,
		Person:           q.Person,
		EscalationType:   q.EscalationType,
		Page:             q.Page,
		Limit:            q.Limit,
	}

	if q.DateFrom != "" {
		if t, err := time.Parse("2006-01-02", q.DateFrom); err == nil {
			localStart := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
			filter.DateFrom = &localStart
		} else if t, err := time.Parse(time.RFC3339, q.DateFrom); err == nil {
			filter.DateFrom = &t
		}
	}
	if q.DateTo != "" {
		if t, err := time.Parse("2006-01-02", q.DateTo); err == nil {
			localEnd := time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999999, time.Local)
			filter.DateTo = &localEnd
		} else if t, err := time.Parse(time.RFC3339, q.DateTo); err == nil {
			filter.DateTo = &t
		}
	}

	return filter, nil
}

// ListLogs returns matching escalation entries, newest first.
//
// GET /api/v1/escalation/logs
func (h *EscalationHandler) ListLogs(c *fiber.Ctx) error {
	filter, err := parseEscalationLogFilter(c)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "invalid escalation log filter")
	}

	result, err := h.service.SearchLogs(c.UserContext(), filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "sla_breach_logs_fetched"), result)
}

// ExportLogs exports matching escalation entries to Excel.
//
// GET /api/v1/escalation/logs/export
func (h *EscalationHandler) ExportLogs(c *fiber.Ctx) error {
	filter, err := parseEscalationLogFilter(c)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "invalid escalation log filter")
	}

	data, err := h.service.ExportLogs(c.UserContext(), filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	filename := fmt.Sprintf("escalation-log-%s.xlsx", time.Now().UTC().Format("20060102-150405"))
	c.Set(fiber.HeaderContentType, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Set(fiber.HeaderContentDisposition, fmt.Sprintf(`attachment; filename="%s"`, filename))
	return c.Send(data)
}
