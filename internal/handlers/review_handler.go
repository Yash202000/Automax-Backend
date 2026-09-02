package handlers

import (
	"strconv"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type ReviewHandler struct {
	service  services.ReviewService
	validate *validator.Validate
}

func NewReviewHandler(service services.ReviewService) *ReviewHandler {
	return &ReviewHandler{
		service:  service,
		validate: validator.New(),
	}
}

// ──────────────────────────────────────────────────
// Cycle CRUD
// ──────────────────────────────────────────────────

func (h *ReviewHandler) CreateCycle(c *fiber.Ctx) error {
	var req models.ReviewCycleCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if err := h.validate.Struct(req); err != nil {
		return utils.FormatValidationError(c, err)
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	resp, err := h.service.CreateCycle(c.UserContext(), &req, userID)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "review_cycle_created"), resp)
}

func (h *ReviewHandler) ListCycles(c *fiber.Ctx) error {
	status := c.Query("status")
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))

	var departmentID *uuid.UUID
	if deptStr := c.Query("department_id"); deptStr != "" {
		id, err := uuid.Parse(deptStr)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_department_id"))
		}
		departmentID = &id
	}

	cycles, total, err := h.service.ListCycles(c.UserContext(), status, departmentID, page, limit)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return utils.PaginatedSuccessResponse(c, cycles, page, limit, total)
}

func (h *ReviewHandler) GetCycle(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_cycle_id"))
	}

	resp, err := h.service.GetCycle(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "review_cycle_retrieved"), resp)
}

func (h *ReviewHandler) UpdateCycle(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_cycle_id"))
	}

	var req models.ReviewCycleUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	resp, err := h.service.UpdateCycle(c.UserContext(), id, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "review_cycle_updated"), resp)
}

func (h *ReviewHandler) DeleteCycle(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_cycle_id"))
	}

	if err := h.service.DeleteCycle(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "review_cycle_deleted"), nil)
}

// ──────────────────────────────────────────────────
// Cycle Lifecycle
// ──────────────────────────────────────────────────

func (h *ReviewHandler) ActivateCycle(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_cycle_id"))
	}

	resp, err := h.service.ActivateCycle(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "review_cycle_activated"), resp)
}

func (h *ReviewHandler) CompleteCycle(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_cycle_id"))
	}

	resp, err := h.service.CompleteCycle(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "review_cycle_completed"), resp)
}

// ──────────────────────────────────────────────────
// Assignments
// ──────────────────────────────────────────────────

func (h *ReviewHandler) AssignReviewees(c *fiber.Ctx) error {
	cycleID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_cycle_id"))
	}

	var req models.BulkAssignRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if err := h.validate.Struct(req); err != nil {
		return utils.FormatValidationError(c, err)
	}

	assignments, err := h.service.AssignReviewees(c.UserContext(), cycleID, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "reviewees_assigned"), assignments)
}

func (h *ReviewHandler) ListCycleAssignments(c *fiber.Ctx) error {
	cycleID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_cycle_id"))
	}

	assignments, err := h.service.ListCycleAssignments(c.UserContext(), cycleID)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    assignments,
		"total":   len(assignments),
	})
}

func (h *ReviewHandler) GetAssignment(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_assignment_id"))
	}

	resp, err := h.service.GetAssignment(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "assignment_retrieved"), resp)
}

func (h *ReviewHandler) RemoveAssignment(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_assignment_id"))
	}

	if err := h.service.RemoveAssignment(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "assignment_removed"), nil)
}

// ──────────────────────────────────────────────────
// My Reviews
// ──────────────────────────────────────────────────

func (h *ReviewHandler) ListMyReviews(c *fiber.Ctx) error {
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))

	reviews, total, err := h.service.ListMyReviews(c.UserContext(), userID, page, limit)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return utils.PaginatedSuccessResponse(c, reviews, page, limit, total)
}

func (h *ReviewHandler) ListMyReviewTasks(c *fiber.Ctx) error {
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))

	tasks, total, err := h.service.ListMyReviewTasks(c.UserContext(), userID, page, limit)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return utils.PaginatedSuccessResponse(c, tasks, page, limit, total)
}

// ──────────────────────────────────────────────────
// Scoring
// ──────────────────────────────────────────────────

func (h *ReviewHandler) ScoreGoals(c *fiber.Ctx) error {
	assignmentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_assignment_id"))
	}

	var scores []models.GoalScoreUpdateRequest
	if err := c.BodyParser(&scores); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	resp, err := h.service.ScoreGoals(c.UserContext(), assignmentID, scores, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "goal_scores_saved"), resp)
}

func (h *ReviewHandler) SubmitReview(c *fiber.Ctx) error {
	assignmentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_assignment_id"))
	}

	var req models.ReviewSubmitRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if err := h.validate.Struct(req); err != nil {
		return utils.FormatValidationError(c, err)
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	resp, err := h.service.SubmitReview(c.UserContext(), assignmentID, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "review_submitted"), resp)
}
