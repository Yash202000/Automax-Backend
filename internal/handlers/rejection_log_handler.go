package handlers

import (
	"encoding/json"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type RejectionLogHandler struct {
	repo repository.RejectionLogRepository
}

func NewRejectionLogHandler(repo repository.RejectionLogRepository) *RejectionLogHandler {
	return &RejectionLogHandler{repo: repo}
}

// GetByIncident returns all rejection log entries for a specific incident.
// GET /api/v1/incidents/:id/rejection-logs
func (h *RejectionLogHandler) GetByIncident(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "invalid incident id")
	}

	logs, err := h.repo.GetByIncident(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "failed to fetch rejection logs")
	}

	responses := make([]models.IncidentRejectionLogResponse, len(logs))
	for i, l := range logs {
		responses[i] = toRejectionLogResponse(&l)
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    responses,
		"total":   len(responses),
	})
}

// List returns a paginated, filtered list of all rejection logs across all incidents.
// GET /api/v1/admin/rejection-logs
func (h *RejectionLogHandler) List(c *fiber.Ctx) error {
	filter := &models.IncidentRejectionLogFilter{
		Page:  1,
		Limit: 20,
	}

	if p := c.QueryInt("page", 1); p > 0 {
		filter.Page = p
	}
	if l := c.QueryInt("limit", 20); l > 0 && l <= 100 {
		filter.Limit = l
	}
	if v := c.Query("record_type"); v != "" {
		filter.RecordType = &v
	}
	if v := c.Query("sla_status"); v != "" {
		filter.SLAStatus = &v
	}
	if v := c.Query("incident_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			filter.IncidentID = &id
		}
	}
	if v := c.Query("rejected_by_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			filter.RejectedByID = &id
		}
	}
	if v := c.Query("department_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			filter.DepartmentID = &id
		}
	}
	if v := c.Query("classification_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			filter.ClassificationID = &id
		}
	}
	if v := c.Query("start_date"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.StartDate = &t
		}
	}
	if v := c.Query("end_date"); v != "" {
		if t, err := time.Parse(time.RFC3339, v); err == nil {
			filter.EndDate = &t
		}
	}

	logs, total, err := h.repo.List(c.UserContext(), filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "failed to fetch rejection logs")
	}

	responses := make([]models.IncidentRejectionLogResponse, len(logs))
	for i, l := range logs {
		responses[i] = toRejectionLogResponse(&l)
	}

	totalPages := int(total) / filter.Limit
	if int(total)%filter.Limit > 0 {
		totalPages++
	}

	return c.JSON(fiber.Map{
		"success":     true,
		"data":        responses,
		"total":       total,
		"page":        filter.Page,
		"limit":       filter.Limit,
		"total_pages": totalPages,
	})
}

// toRejectionLogResponse converts a model to its API response shape.
func toRejectionLogResponse(l *models.IncidentRejectionLog) models.IncidentRejectionLogResponse {
	resp := models.IncidentRejectionLogResponse{
		ID:                     l.ID,
		IncidentID:             l.IncidentID,
		IncidentNumber:         l.IncidentNumber,
		IncidentTitle:          l.IncidentTitle,
		RecordType:             l.RecordType,
		RejectionSequence:      l.RejectionSequence,
		TotalRejectionCount:    l.TotalRejectionCount,
		ReceivedAt:             l.ReceivedAt,
		RejectedAt:             l.RejectedAt,
		ReactionTimeMinutes:    l.ReactionTimeMinutes,
		RejectionReason:        l.RejectionReason,
		RejectedByUsername:     l.RejectedByUsername,
		SLAThresholdHours:      l.SLAThresholdHours,
		SLAThresholdMinutes:    l.SLAThresholdMinutes,
		SLABreachedAtRejection: l.SLABreachedAtRejection,
		SLAStatus:              l.SLAStatus,
		DepartmentID:           l.DepartmentID,
		ClassificationID:       l.ClassificationID,
		TransitionHistoryID:    l.TransitionHistoryID,
		CreatedAt:              l.CreatedAt,
	}

	// Deserialize roles snapshot
	if l.RejectedByRolesSnapshot != "" {
		var roles []string
		if json.Unmarshal([]byte(l.RejectedByRolesSnapshot), &roles) == nil {
			resp.RejectedByRolesSnapshot = roles
		}
	}
	if resp.RejectedByRolesSnapshot == nil {
		resp.RejectedByRolesSnapshot = []string{}
	}

	if l.FromState != nil {
		s := models.ToWorkflowStateResponse(l.FromState)
		resp.FromState = &s
	}
	if l.ToState != nil {
		s := models.ToWorkflowStateResponse(l.ToState)
		resp.ToState = &s
	}
	if l.RejectedBy != nil {
		u := models.ToUserResponse(l.RejectedBy)
		resp.RejectedBy = &u
	}

	return resp
}
