package handlers

import (
	"context"
	"io"
	"log"
	"path/filepath"
	"strings"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// userContext injects the *models.User from Fiber locals into the Go context
// so that service-layer helpers (e.g. isSuperAdmin) can read it.
func userContext(c *fiber.Ctx) context.Context {
	user, _ := c.Locals(constants.ContextKeys.User).(*models.User)
	return context.WithValue(c.UserContext(), constants.ContextKeys.User, user)
}

type GoalHandler struct {
	service          services.GoalService
	actionLogService services.ActionLogService
}

func NewGoalHandler(service services.GoalService, actionLogService services.ActionLogService) *GoalHandler {
	return &GoalHandler{service: service, actionLogService: actionLogService}
}

// ──────────────────────────────────────────────────
// Export & Clone
// ──────────────────────────────────────────────────

func (h *GoalHandler) ExportGoals(c *fiber.Ctx) error {
	var filter models.GoalFilter
	if err := c.QueryParser(&filter); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_query_parameters"))
	}

	format := c.Query("format", "csv")

	switch format {
	case "csv":
		data, err := h.service.ExportGoalsCSV(c.UserContext(), &filter)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_export_goals"))
		}
		c.Set("Content-Type", "text/csv")
		c.Set("Content-Disposition", "attachment; filename=goals_export.csv")
		return c.Send(data)

	case "json":
		goals, err := h.service.ExportGoalsJSON(c.UserContext(), &filter)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_export_goals"))
		}
		return c.JSON(fiber.Map{
			"success": true,
			"data":    goals,
			"total":   len(goals),
		})

	default:
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_format_csv_json"))
	}
}

func (h *GoalHandler) CloneGoal(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.GoalCloneRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	goal, err := h.service.CloneGoal(c.UserContext(), id, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to clone goal: "+err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "goal_cloned"), goal)
}

// ──────────────────────────────────────────────────
// Import & Bulk
// ──────────────────────────────────────────────────

func (h *GoalHandler) ImportGoals(c *fiber.Ctx) error {
	file, err := c.FormFile("file")
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "file_required"))
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext != ".csv" && ext != ".xlsx" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "only_csv_xlsx_supported"))
	}

	f, err := file.Open()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_open_file"))
	}
	defer f.Close()

	fileData, err := io.ReadAll(f)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_read_file"))
	}

	dryRun := c.FormValue("dry_run", "true") == "true"
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	result, err := h.service.ImportGoals(c.UserContext(), fileData, file.Filename, dryRun, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Import failed: "+err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "import_processed"), result)
}

func (h *GoalHandler) BulkAction(c *fiber.Ctx) error {
	var req models.BulkActionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if len(req.GoalIDs) == 0 {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "at_least_one_goal_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	result, err := h.service.BulkAction(c.UserContext(), &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Bulk operation failed: "+err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "bulk_operation_completed"), result)
}

// ──────────────────────────────────────────────────
// Goal CRUD
// ──────────────────────────────────────────────────

func (h *GoalHandler) CreateGoal(c *fiber.Ctx) error {
	var req models.GoalCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
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
		if strings.Contains(err.Error(), "parent goal not found") || strings.Contains(err.Error(), "maximum goal hierarchy depth") || strings.Contains(err.Error(), "already exists") {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"success": false,
				"message": err.Error(),
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"message": err.Error(),
		})
	}
	// Success response
	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"success": true,
		"message": "Goal created",
		"data":    goal,
	})
}

func (h *GoalHandler) GetGoal(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ctx := userContext(c)

	goal, err := h.service.GetGoal(ctx, id, userID)
	if err != nil {
		if strings.Contains(err.Error(), i18n.T(c.UserContext(), "access_denied")) {
			return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(c.UserContext(), "access_denied"))
		}
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "goal_not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "goal_retrieved"), goal)
}

func (h *GoalHandler) ListGoals(c *fiber.Ctx) error {
	var filter models.GoalFilter
	if err := c.QueryParser(&filter); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_query_parameters"))
	}

	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 20
	}

	// Apply user-scoped filtering (super admins see all by default).
	// scope=mine is a UX-level narrowing for the "My Goals" view; it must
	// apply to every caller including super admins, so set UserID unconditionally
	// when scope=mine is requested.
	user, _ := c.Locals(constants.ContextKeys.User).(*models.User)
	if filter.Scope == "mine" || user == nil || !user.IsSuperAdmin {
		userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
		filter.UserID = &userID
	}

	goals, total, err := h.service.ListGoals(c.UserContext(), &filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_list_goals"))
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.GoalUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ctx := userContext(c)

	goal, err := h.service.UpdateGoal(ctx, id, &req, userID)
	if err != nil {
		if strings.Contains(err.Error(), i18n.T(c.UserContext(), "access_denied")) {
			return utils.ErrorResponse(c, fiber.StatusForbidden, err.Error())
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update_goal"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "goal_updated"), goal)
}

func (h *GoalHandler) DeleteGoal(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ctx := userContext(c)

	if err := h.service.DeleteGoal(ctx, id, userID); err != nil {
		log.Printf("[GoalHandler] DeleteGoal error for goal %s by user %s: %v", id, userID, err)
		if strings.Contains(err.Error(), i18n.T(c.UserContext(), "access_denied")) {
			return utils.ErrorResponse(c, fiber.StatusForbidden, err.Error())
		}
		if strings.Contains(err.Error(), "not found") {
			return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "goal_not_found"))
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to delete goal: "+err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "goal_deleted"), nil)
}

func (h *GoalHandler) TransitionStatus(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.GoalTransitionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ctx := userContext(c)

	goal, err := h.service.TransitionGoalStatus(ctx, id, req.Status, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "goal_status_transitioned"), goal)
}

// ──────────────────────────────────────────────────
// Collaborators
// ──────────────────────────────────────────────────

func (h *GoalHandler) AddCollaborator(c *fiber.Ctx) error {
	goalID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.CollaboratorAddRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ctx := userContext(c)

	if err := h.service.AddCollaborator(ctx, goalID, &req, userID); err != nil {
		if strings.Contains(err.Error(), i18n.T(c.UserContext(), "access_denied")) {
			return utils.ErrorResponse(c, fiber.StatusForbidden, err.Error())
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_add_collaborator"))
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "collaborator_added"), nil)
}

func (h *GoalHandler) RemoveCollaborator(c *fiber.Ctx) error {
	goalID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_goal_id"))
	}

	collaboratorUserID, err := uuid.Parse(c.Params("user_id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_user_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ctx := userContext(c)

	if err := h.service.RemoveCollaborator(ctx, goalID, collaboratorUserID, userID); err != nil {
		if strings.Contains(err.Error(), i18n.T(c.UserContext(), "access_denied")) {
			return utils.ErrorResponse(c, fiber.StatusForbidden, err.Error())
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_remove_collaborator"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "collaborator_removed"), nil)
}

// ──────────────────────────────────────────────────
// Metrics
// ──────────────────────────────────────────────────

func (h *GoalHandler) CreateMetric(c *fiber.Ctx) error {
	goalID, err := uuid.Parse(c.Params("gid"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_goal_id"))
	}

	var req models.GoalMetricCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ctx := userContext(c)

	metric, err := h.service.CreateMetric(ctx, goalID, &req, userID)
	if err != nil {
		if strings.Contains(err.Error(), i18n.T(c.UserContext(), "access_denied")) {
			return utils.ErrorResponse(c, fiber.StatusForbidden, err.Error())
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create_metric"))
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "metric_created"), metric)
}

func (h *GoalHandler) UpdateMetric(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.GoalMetricUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ctx := userContext(c)

	metric, err := h.service.UpdateMetric(ctx, id, &req, userID)
	if err != nil {
		if strings.Contains(err.Error(), i18n.T(c.UserContext(), "access_denied")) {
			return utils.ErrorResponse(c, fiber.StatusForbidden, err.Error())
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update_metric"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "metric_updated"), metric)
}

func (h *GoalHandler) DeleteMetric(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ctx := userContext(c)

	if err := h.service.DeleteMetric(ctx, id, userID); err != nil {
		if strings.Contains(err.Error(), i18n.T(c.UserContext(), "access_denied")) {
			return utils.ErrorResponse(c, fiber.StatusForbidden, err.Error())
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_delete_metric"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "metric_deleted"), nil)
}

func (h *GoalHandler) UpdateMetricValue(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.MetricValueUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ctx := userContext(c)

	change, err := h.service.UpdateMetricValue(ctx, id, &req, userID)
	if err != nil {
		if strings.Contains(err.Error(), i18n.T(c.UserContext(), "access_denied")) {
			return utils.ErrorResponse(c, fiber.StatusForbidden, err.Error())
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_submit_metric_value"))
	}

	return utils.SuccessResponse(c, fiber.StatusAccepted, i18n.T(c.UserContext(), "metric_value_change_submitted"), change)
}

func (h *GoalHandler) GetMetricHistory(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 20)

	history, total, err := h.service.GetMetricHistory(c.UserContext(), id, page, limit)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_get_metric_history"))
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_goal_id"))
	}

	file, err := c.FormFile("file")
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "file_required"))
	}

	f, err := file.Open()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_open_file"))
	}
	defer f.Close()

	fileData, err := io.ReadAll(f)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_read_file"))
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
	ctx := userContext(c)

	result, err := h.service.CreateEvidence(
		ctx,
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
		log.Printf("[GoalHandler] UploadEvidence error: %v", err)
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_upload_evidence"))
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "evidence_uploaded"), result)
}

func (h *GoalHandler) ListEvidences(c *fiber.Ctx) error {
	goalID, err := uuid.Parse(c.Params("gid"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_goal_id"))
	}

	var filter models.EvidenceFilter
	if err := c.QueryParser(&filter); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_query_parameters"))
	}

	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 20
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ctx := userContext(c)

	evidences, total, err := h.service.ListEvidences(ctx, goalID, &filter, userID)
	if err != nil {
		if strings.Contains(err.Error(), i18n.T(c.UserContext(), "access_denied")) {
			return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(c.UserContext(), "access_denied"))
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_list_evidences"))
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	evidence, err := h.service.GetEvidence(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "evidence_not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "evidence_retrieved"), evidence)
}

func (h *GoalHandler) GetEvidencePreview(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	url, err := h.service.GetEvidencePreview(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "evidence_preview_unavail"))
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"preview_url": url,
		},
	})
}

// DownloadEvidence streams the evidence file bytes back to the caller.
//
// Previously this issued a 307 redirect to the Documenta-relative path
// "/api/v1/documents/files/<uuid>/download". That breaks any deployment
// where Automax is served under a path prefix (e.g. VITE_BASE_PATH=/ax3/
// with nginx proxying /ax3/api/v1/*): the Location header has no awareness
// of the caller's base path, so the browser resolves it against the origin
// root and hits a 404. Streaming bytes through this handler avoids the
// path-sensitive redirect entirely.
func (h *GoalHandler) DownloadEvidence(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	reader, info, contentType, filename, err := h.service.DownloadEvidenceFile(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "evidence_download_unavail"))
	}

	// Buffer the body before returning because Fiber's c.SendStream runs
	// after the handler returns, so a deferred reader.Close would truncate
	// the response. Evidence files are typical doc sizes; buffering is safe.
	payload, readErr := io.ReadAll(reader)
	reader.Close()
	if readErr != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to read evidence bytes: "+readErr.Error())
	}

	if contentType == "" {
		contentType = "application/octet-stream"
	}
	if filename == "" {
		if info != nil && info.Name != "" {
			filename = info.Name
		} else {
			filename = id.String()
		}
	}

	c.Set("Content-Type", contentType)
	c.Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	return c.Send(payload)
}

// ──────────────────────────────────────────────────
// Evidence Management
// ──────────────────────────────────────────────────

func (h *GoalHandler) DeleteEvidence(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ctx := userContext(c)

	if err := h.service.DeleteEvidence(ctx, id, userID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return utils.ErrorResponse(c, fiber.StatusNotFound, err.Error())
		}
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "evidence_deleted"), nil)
}

func (h *GoalHandler) ReplaceEvidenceFile(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_evidence_id"))
	}

	file, err := c.FormFile("file")
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "file_required"))
	}

	f, err := file.Open()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_open_file"))
	}
	defer f.Close()

	fileData, err := io.ReadAll(f)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_read_file"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	mimeType := file.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	result, err := h.service.ReplaceEvidenceFile(
		c.UserContext(),
		id,
		file.Filename,
		int64(len(fileData)),
		mimeType,
		fileData,
		userID,
	)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "evidence_file_replaced"), result)
}

// ──────────────────────────────────────────────────
// Evidence Workflow Transitions
// ──────────────────────────────────────────────────

func (h *GoalHandler) GetAvailableEvidenceTransitions(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	transitions, err := h.service.GetAvailableEvidenceTransitions(c.UserContext(), id, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "available_transitions"), transitions)
}

func (h *GoalHandler) ExecuteEvidenceTransition(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.EvidenceTransitionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
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

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "transition_executed"), result)
}

func (h *GoalHandler) GetEvidenceTransitionHistory(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	history, err := h.service.GetEvidenceTransitionHistory(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_get_transition_history"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "transition_history"), history)
}

// ──────────────────────────────────────────────────
// Metric & Metric Value Change Workflow Transitions
// ──────────────────────────────────────────────────

func (h *GoalHandler) GetAvailableMetricTransitions(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	transitions, err := h.service.GetAvailableMetricTransitions(c.UserContext(), id, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "available_transitions"), transitions)
}

func (h *GoalHandler) TransitionMetric(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.MetricTransitionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	result, err := h.service.TransitionMetric(c.UserContext(), id, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "transition_executed"), result)
}

func (h *GoalHandler) GetAvailableMetricValueChangeTransitions(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	transitions, err := h.service.GetAvailableMetricValueChangeTransitions(c.UserContext(), id, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "available_transitions"), transitions)
}

func (h *GoalHandler) TransitionMetricValueChange(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.MetricTransitionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	result, err := h.service.TransitionMetricValueChange(c.UserContext(), id, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "transition_executed"), result)
}

// ListMetricValueChanges returns all value changes for a specific metric.
// Open to anyone with access to the parent goal (owner/collaborator/dept).
func (h *GoalHandler) ListMetricValueChanges(c *fiber.Ctx) error {
	metricID, err := uuid.Parse(c.Params("metric_id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_metric_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	changes, err := h.service.ListMetricValueChanges(c.UserContext(), metricID, userID)
	if err != nil {
		if strings.Contains(err.Error(), i18n.T(c.UserContext(), "access_denied")) {
			return utils.ErrorResponse(c, fiber.StatusForbidden, err.Error())
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    changes,
		"total":   len(changes),
	})
}

func (h *GoalHandler) ListPendingMetricApprovals(c *fiber.Ctx) error {
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 10)

	approvals, total, err := h.service.ListPendingMetricApprovals(c.UserContext(), userID, page, limit)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_list_pending_metric_approvals"))
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    approvals,
		"total":   total,
		"page":    page,
		"limit":   limit,
	})
}

func (h *GoalHandler) ListPendingMetricValueChangeApprovals(c *fiber.Ctx) error {
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 10)

	approvals, total, err := h.service.ListPendingMetricValueChangeApprovals(c.UserContext(), userID, page, limit)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_list_pending_metric_value_approvals"))
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    approvals,
		"total":   total,
		"page":    page,
		"limit":   limit,
	})
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
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_list_pending_approvals"))
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
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_list_completed_approvals"))
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    approvals,
		"total":   total,
		"page":    page,
		"limit":   limit,
	})
}

// ──────────────────────────────────────────────────
// Hierarchy
// ──────────────────────────────────────────────────

func (h *GoalHandler) ListChildGoals(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_goal_id"))
	}

	children, err := h.service.ListChildGoals(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_list_child_goals"))
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    children,
	})
}

func (h *GoalHandler) GetGoalTree(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_goal_id"))
	}

	tree, err := h.service.GetGoalTree(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_get_goal_tree"))
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    tree,
	})
}

// ──────────────────────────────────────────────────
// Check-ins
// ──────────────────────────────────────────────────

func (h *GoalHandler) CreateCheckIn(c *fiber.Ctx) error {
	goalID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_goal_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ctx := userContext(c)

	var req models.CheckInCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	checkIn, err := h.service.CreateCheckIn(ctx, goalID, &req, userID)
	if err != nil {
		log.Printf("Failed to create check-in: %v", err)
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"success": true,
		"data":    checkIn,
	})
}

func (h *GoalHandler) ListCheckIns(c *fiber.Ctx) error {
	goalID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_goal_id"))
	}

	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 10)

	checkIns, total, err := h.service.ListCheckIns(c.UserContext(), goalID, page, limit)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_list_check_ins"))
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    checkIns,
		"total":   total,
		"page":    page,
		"limit":   limit,
	})
}

func (h *GoalHandler) DeleteCheckIn(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_check_in_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ctx := userContext(c)

	if err := h.service.DeleteCheckIn(ctx, id, userID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Check-in deleted",
	})
}

// ────��─────────────────────────────────────────────
// Metric Import/Export
// ────���─────────────────────────��───────────────────

func (h *GoalHandler) ExportMetricsTemplate(c *fiber.Ctx) error {
	var filter models.GoalFilter
	if err := c.QueryParser(&filter); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_query_parameters"))
	}

	format := c.Query("format", "csv")

	data, err := h.service.ExportMetricsTemplate(c.UserContext(), &filter, format)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to export metrics template: "+err.Error())
	}

	switch format {
	case "xlsx":
		c.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		c.Set("Content-Disposition", "attachment; filename=metrics_template.xlsx")
	default:
		c.Set("Content-Type", "text/csv")
		c.Set("Content-Disposition", "attachment; filename=metrics_template.csv")
	}

	return c.Send(data)
}

func (h *GoalHandler) ImportMetrics(c *fiber.Ctx) error {
	file, err := c.FormFile("file")
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "file_required"))
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext != ".csv" && ext != ".xlsx" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "only_csv_xlsx_supported"))
	}

	f, err := file.Open()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_open_file"))
	}
	defer f.Close()

	fileData, err := io.ReadAll(f)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_read_file"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	dryRun := c.FormValue("dry_run", "true") == "true"

	if dryRun {
		result, err := h.service.ImportMetricsDryRun(c.UserContext(), fileData, file.Filename, userID)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "Validation failed: "+err.Error())
		}
		return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "dry_run_completed"), result)
	}

	// Commit mode
	title := c.FormValue("title", "")
	if title == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "title_required_import"))
	}
	comment := c.FormValue("comment", "")
	primaryGoalIDStr := c.FormValue("primary_goal_id", "")
	if primaryGoalIDStr == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "primary_goal_id_required"))
	}
	primaryGoalID, err := uuid.Parse(primaryGoalIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_primary_goal_id"))
	}

	result, err := h.service.ImportMetricsCommit(c.UserContext(), fileData, file.Filename, title, comment, primaryGoalID, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Import failed: "+err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "metric_import_batch_created"), result)
}

func (h *GoalHandler) ListMetricImportBatches(c *fiber.Ctx) error {
	var filter models.MetricImportBatchFilter
	if err := c.QueryParser(&filter); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_query_parameters"))
	}

	batches, total, err := h.service.ListMetricImportBatches(c.UserContext(), &filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_list_metric_batches"))
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    batches,
		"total":   total,
		"page":    filter.Page,
		"limit":   filter.Limit,
	})
}

func (h *GoalHandler) GetMetricImportBatch(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	batch, err := h.service.GetMetricImportBatch(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "metric_import_batch"), batch)
}

func (h *GoalHandler) DeleteMetricImportBatch(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	if err := h.service.DeleteMetricImportBatch(c.UserContext(), id, userID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Metric import batch deleted",
	})
}

func (h *GoalHandler) GetAvailableMetricBatchTransitions(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	transitions, err := h.service.GetAvailableMetricBatchTransitions(c.UserContext(), id, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "available_transitions"), transitions)
}

func (h *GoalHandler) ExecuteMetricBatchTransition(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.MetricImportBatchTransitionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	result, err := h.service.ExecuteMetricBatchTransition(c.UserContext(), id, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "transition_executed"), result)
}

func (h *GoalHandler) GetMetricBatchTransitionHistory(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	history, err := h.service.GetMetricBatchTransitionHistory(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_get_transition_history"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "transition_history"), history)
}

// ──────────────────────────────────────────────────
// Goal Activity
// ──────────────────────────────────────────────────

func (h *GoalHandler) GetGoalActivity(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 20)

	filter := &models.ActionLogFilter{
		Module:     "goals",
		ResourceID: id.String(),
		Page:       page,
		Limit:      limit,
	}
	logs, total, err := h.actionLogService.ListActionLogs(c.UserContext(), filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_get_goal_activity"))
	}
	return c.JSON(fiber.Map{
		"success": true,
		"data":    logs,
		"total":   total,
		"page":    page,
		"limit":   limit,
	})
}

// ──────────────────────────────────────────────────
// Goal Comments
// ──────────────────────────────────────────────────

func (h *GoalHandler) AddGoalComment(c *fiber.Ctx) error {
	goalID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.GoalCommentRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if strings.TrimSpace(req.Content) == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "content_required"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ctx := userContext(c)

	comment, err := h.service.AddComment(ctx, goalID, req.Content, userID)
	if err != nil {
		if strings.Contains(err.Error(), i18n.T(c.UserContext(), "access_denied")) {
			return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(c.UserContext(), "access_denied"))
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_add_comment"))
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "comment_added"), comment)
}

func (h *GoalHandler) ListGoalComments(c *fiber.Ctx) error {
	goalID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 20)

	comments, total, err := h.service.ListComments(c.UserContext(), goalID, page, limit)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_list_comments"))
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    comments,
		"total":   total,
		"page":    page,
		"limit":   limit,
	})
}

func (h *GoalHandler) DeleteGoalComment(c *fiber.Ctx) error {
	commentID, err := uuid.Parse(c.Params("cid"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid comment ID")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ctx := userContext(c)

	if err := h.service.DeleteComment(ctx, commentID, userID); err != nil {
		if strings.Contains(err.Error(), i18n.T(c.UserContext(), "access_denied")) {
			return utils.ErrorResponse(c, fiber.StatusForbidden, err.Error())
		}
		if strings.Contains(err.Error(), "not found") {
			return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "comment_not_found"))
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_delete_comment"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "comment_deleted"), nil)
}
