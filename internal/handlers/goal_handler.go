package handlers

import (
	"io"
	"log"
	"strings"

	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type GoalHandler struct {
	service services.GoalService
}

func NewGoalHandler(service services.GoalService) *GoalHandler {
	return &GoalHandler{service: service}
}

// ──────────────────────────────────────────────────
// Export & Clone
// ──────────────────────────────────────────────────

func (h *GoalHandler) ExportGoals(c *fiber.Ctx) error {
	var filter models.GoalFilter
	if err := c.QueryParser(&filter); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid query parameters")
	}

	format := c.Query("format", "csv")

	switch format {
	case "csv":
		data, err := h.service.ExportGoalsCSV(c.UserContext(), &filter)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to export goals")
		}
		c.Set("Content-Type", "text/csv")
		c.Set("Content-Disposition", "attachment; filename=goals_export.csv")
		return c.Send(data)

	case "json":
		goals, err := h.service.ExportGoalsJSON(c.UserContext(), &filter)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to export goals")
		}
		return c.JSON(fiber.Map{
			"success": true,
			"data":    goals,
			"total":   len(goals),
		})

	default:
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid format. Use 'csv' or 'json'")
	}
}

func (h *GoalHandler) CloneGoal(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	var req models.GoalCloneRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	goal, err := h.service.CloneGoal(c.UserContext(), id, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to clone goal: "+err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Goal cloned", goal)
}

// ──────────────────────────────────────────────────
// Goal CRUD
// ──────────────────────────────────────────────────

func (h *GoalHandler) CreateGoal(c *fiber.Ctx) error {
	var req models.GoalCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	goal, err := h.service.CreateGoal(c.UserContext(), &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create goal")
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Goal created", goal)
}

func (h *GoalHandler) GetGoal(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	goal, err := h.service.GetGoal(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Goal not found")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Goal retrieved", goal)
}

func (h *GoalHandler) ListGoals(c *fiber.Ctx) error {
	var filter models.GoalFilter
	if err := c.QueryParser(&filter); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid query parameters")
	}

	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 20
	}

	goals, total, err := h.service.ListGoals(c.UserContext(), &filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list goals")
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    goals,
		"total":   total,
		"page":    filter.Page,
		"limit":   filter.Limit,
	})
}

func (h *GoalHandler) UpdateGoal(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	var req models.GoalUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	goal, err := h.service.UpdateGoal(c.UserContext(), id, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to update goal")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Goal updated", goal)
}

func (h *GoalHandler) DeleteGoal(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	if err := h.service.DeleteGoal(c.UserContext(), id, userID); err != nil {
		log.Printf("[GoalHandler] DeleteGoal error for goal %s by user %s: %v", id, userID, err)
		if strings.Contains(err.Error(), "not found") {
			return utils.ErrorResponse(c, fiber.StatusNotFound, "Goal not found")
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to delete goal: "+err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Goal deleted", nil)
}

func (h *GoalHandler) TransitionStatus(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	var req models.GoalTransitionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	goal, err := h.service.TransitionGoalStatus(c.UserContext(), id, req.Status, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Goal status transitioned", goal)
}

// ──────────────────────────────────────────────────
// Collaborators
// ──────────────────────────────────────────────────

func (h *GoalHandler) AddCollaborator(c *fiber.Ctx) error {
	goalID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	var req models.CollaboratorAddRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	if err := h.service.AddCollaborator(c.UserContext(), goalID, &req, userID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to add collaborator")
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Collaborator added", nil)
}

func (h *GoalHandler) RemoveCollaborator(c *fiber.Ctx) error {
	goalID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid goal ID")
	}

	collaboratorUserID, err := uuid.Parse(c.Params("user_id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid user ID")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	if err := h.service.RemoveCollaborator(c.UserContext(), goalID, collaboratorUserID, userID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to remove collaborator")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Collaborator removed", nil)
}

// ──────────────────────────────────────────────────
// Metrics
// ──────────────────────────────────────────────────

func (h *GoalHandler) CreateMetric(c *fiber.Ctx) error {
	goalID, err := uuid.Parse(c.Params("gid"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid goal ID")
	}

	var req models.GoalMetricCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	metric, err := h.service.CreateMetric(c.UserContext(), goalID, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create metric")
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Metric created", metric)
}

func (h *GoalHandler) UpdateMetric(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	var req models.GoalMetricUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	metric, err := h.service.UpdateMetric(c.UserContext(), id, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to update metric")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Metric updated", metric)
}

func (h *GoalHandler) DeleteMetric(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	if err := h.service.DeleteMetric(c.UserContext(), id, userID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to delete metric")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Metric deleted", nil)
}

func (h *GoalHandler) UpdateMetricValue(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	var req models.MetricValueUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	metric, err := h.service.UpdateMetricValue(c.UserContext(), id, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to update metric value")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Metric value updated", metric)
}

func (h *GoalHandler) GetMetricHistory(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 20)

	history, total, err := h.service.GetMetricHistory(c.UserContext(), id, page, limit)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to get metric history")
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    history,
		"total":   total,
		"page":    page,
		"limit":   limit,
	})
}

// ──────────────────────────────────────────────────
// Evidence
// ──────────────────────────────────────────────────

func (h *GoalHandler) UploadEvidence(c *fiber.Ctx) error {
	goalID, err := uuid.Parse(c.Params("gid"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid goal ID")
	}

	file, err := c.FormFile("file")
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "File is required")
	}

	f, err := file.Open()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to open file")
	}
	defer f.Close()

	fileData, err := io.ReadAll(f)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to read file")
	}

	title := c.FormValue("title")
	evidenceType := c.FormValue("evidence_type")
	comment := c.FormValue("comment")
	metricIDStr := c.FormValue("metric_id")

	var metricID *uuid.UUID
	if metricIDStr != "" {
		parsed, parseErr := uuid.Parse(metricIDStr)
		if parseErr == nil {
			metricID = &parsed
		}
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	result, err := h.service.CreateEvidence(
		c.UserContext(),
		goalID,
		title,
		evidenceType,
		comment,
		metricID,
		file.Filename,
		int64(len(fileData)),
		file.Header.Get("Content-Type"),
		fileData,
		userID,
	)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to upload evidence")
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Evidence uploaded", result)
}

func (h *GoalHandler) ListEvidences(c *fiber.Ctx) error {
	goalID, err := uuid.Parse(c.Params("gid"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid goal ID")
	}

	var filter models.EvidenceFilter
	if err := c.QueryParser(&filter); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid query parameters")
	}

	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 20
	}

	evidences, total, err := h.service.ListEvidences(c.UserContext(), goalID, &filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list evidences")
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    evidences,
		"total":   total,
		"page":    filter.Page,
		"limit":   filter.Limit,
	})
}

func (h *GoalHandler) GetEvidence(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	evidence, err := h.service.GetEvidence(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Evidence not found")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Evidence retrieved", evidence)
}

func (h *GoalHandler) GetEvidencePreview(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	url, err := h.service.GetEvidencePreview(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Evidence preview not available")
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"preview_url": url,
		},
	})
}

func (h *GoalHandler) DownloadEvidence(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	url, err := h.service.GetEvidenceDownloadURL(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Evidence download not available")
	}

	return c.Redirect(url, fiber.StatusTemporaryRedirect)
}

// ──────────────────────────────────────────────────
// Evidence Management
// ──────────────────────────────────────────────────

func (h *GoalHandler) DeleteEvidence(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	if err := h.service.DeleteEvidence(c.UserContext(), id, userID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Evidence deleted", nil)
}

// ──────────────────────────────────────────────────
// Evidence Workflow Transitions
// ──────────────────────────────────────────────────

func (h *GoalHandler) GetAvailableEvidenceTransitions(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	transitions, err := h.service.GetAvailableEvidenceTransitions(c.UserContext(), id, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Available transitions", transitions)
}

func (h *GoalHandler) ExecuteEvidenceTransition(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	var req models.EvidenceTransitionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	result, err := h.service.ExecuteEvidenceTransition(c.UserContext(), id, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Transition executed", result)
}

func (h *GoalHandler) GetEvidenceTransitionHistory(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	history, err := h.service.GetEvidenceTransitionHistory(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to get transition history")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Transition history", history)
}

// ──────────────────────────────────────────────────
// Approval Lists
// ──────────────────────────────────────────────────

func (h *GoalHandler) ListPendingApprovals(c *fiber.Ctx) error {
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 10)

	approvals, total, err := h.service.ListPendingApprovals(c.UserContext(), userID, page, limit)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list pending approvals")
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    approvals,
		"total":   total,
		"page":    page,
		"limit":   limit,
	})
}

func (h *GoalHandler) ListCompletedApprovals(c *fiber.Ctx) error {
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 10)

	approvals, total, err := h.service.ListCompletedApprovals(c.UserContext(), userID, page, limit)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list completed approvals")
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    approvals,
		"total":   total,
		"page":    page,
		"limit":   limit,
	})
}
