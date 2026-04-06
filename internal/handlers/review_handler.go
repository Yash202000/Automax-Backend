package handlers

import (
	"strconv"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.validate.Struct(req); err != nil {
		return utils.FormatValidationError(c, err)
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	resp, err := h.service.CreateCycle(c.Context(), &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Review cycle created", resp)
}

func (h *ReviewHandler) ListCycles(c *fiber.Ctx) error {
	status := c.Query("status")
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))

	var departmentID *uuid.UUID
	if deptStr := c.Query("department_id"); deptStr != "" {
		id, err := uuid.Parse(deptStr)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid department_id")
		}
		departmentID = &id
	}

	cycles, total, err := h.service.ListCycles(c.Context(), status, departmentID, page, limit)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.PaginatedSuccessResponse(c, cycles, page, limit, total)
}

func (h *ReviewHandler) GetCycle(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid cycle ID")
	}

	resp, err := h.service.GetCycle(c.Context(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Review cycle retrieved", resp)
}

func (h *ReviewHandler) UpdateCycle(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid cycle ID")
	}

	var req models.ReviewCycleUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	resp, err := h.service.UpdateCycle(c.Context(), id, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Review cycle updated", resp)
}

func (h *ReviewHandler) DeleteCycle(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid cycle ID")
	}

	if err := h.service.DeleteCycle(c.Context(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Review cycle deleted", nil)
}

// ──────────────────────────────────────────────────
// Cycle Lifecycle
// ──────────────────────────────────────────────────

func (h *ReviewHandler) ActivateCycle(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid cycle ID")
	}

	resp, err := h.service.ActivateCycle(c.Context(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Review cycle activated", resp)
}

func (h *ReviewHandler) CompleteCycle(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid cycle ID")
	}

	resp, err := h.service.CompleteCycle(c.Context(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Review cycle completed", resp)
}

// ──────────────────────────────────────────────────
// Assignments
// ──────────────────────────────────────────────────

func (h *ReviewHandler) AssignReviewees(c *fiber.Ctx) error {
	cycleID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid cycle ID")
	}

	var req models.BulkAssignRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.validate.Struct(req); err != nil {
		return utils.FormatValidationError(c, err)
	}

	assignments, err := h.service.AssignReviewees(c.Context(), cycleID, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Reviewees assigned", assignments)
}

func (h *ReviewHandler) ListCycleAssignments(c *fiber.Ctx) error {
	cycleID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid cycle ID")
	}

	assignments, err := h.service.ListCycleAssignments(c.Context(), cycleID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid assignment ID")
	}

	resp, err := h.service.GetAssignment(c.Context(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Assignment retrieved", resp)
}

func (h *ReviewHandler) RemoveAssignment(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid assignment ID")
	}

	if err := h.service.RemoveAssignment(c.Context(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Assignment removed", nil)
}

// ──────────────────────────────────────────────────
// My Reviews
// ──────────────────────────────────────────────────

func (h *ReviewHandler) ListMyReviews(c *fiber.Ctx) error {
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))

	reviews, total, err := h.service.ListMyReviews(c.Context(), userID, page, limit)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.PaginatedSuccessResponse(c, reviews, page, limit, total)
}

func (h *ReviewHandler) ListMyReviewTasks(c *fiber.Ctx) error {
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))

	tasks, total, err := h.service.ListMyReviewTasks(c.Context(), userID, page, limit)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.PaginatedSuccessResponse(c, tasks, page, limit, total)
}

// ──────────────────────────────────────────────────
// Scoring
// ──────────────────────────────────────────────────

func (h *ReviewHandler) ScoreGoals(c *fiber.Ctx) error {
	assignmentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid assignment ID")
	}

	var scores []models.GoalScoreUpdateRequest
	if err := c.BodyParser(&scores); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	resp, err := h.service.ScoreGoals(c.Context(), assignmentID, scores, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Goal scores saved", resp)
}

func (h *ReviewHandler) SubmitReview(c *fiber.Ctx) error {
	assignmentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid assignment ID")
	}

	var req models.ReviewSubmitRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.validate.Struct(req); err != nil {
		return utils.FormatValidationError(c, err)
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	resp, err := h.service.SubmitReview(c.Context(), assignmentID, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Review submitted", resp)
}
