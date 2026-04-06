package handlers

import (
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type GoalTemplateHandler struct {
	service services.GoalTemplateService
}

func NewGoalTemplateHandler(service services.GoalTemplateService) *GoalTemplateHandler {
	return &GoalTemplateHandler{service: service}
}

func (h *GoalTemplateHandler) Create(c *fiber.Ctx) error {
	var req models.GoalTemplateCreateRequest
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

	template, err := h.service.Create(c.UserContext(), &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create goal template")
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Goal template created", template)
}

func (h *GoalTemplateHandler) GetByID(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	template, err := h.service.GetByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Goal template not found")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Goal template retrieved", template)
}

func (h *GoalTemplateHandler) List(c *fiber.Ctx) error {
	var filter models.GoalTemplateFilter
	if err := c.QueryParser(&filter); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid query parameters")
	}

	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 20
	}

	templates, total, err := h.service.List(c.UserContext(), &filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list goal templates")
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    templates,
		"total":   total,
		"page":    filter.Page,
		"limit":   filter.Limit,
	})
}

func (h *GoalTemplateHandler) ListActive(c *fiber.Ctx) error {
	templates, err := h.service.ListActive(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list active templates")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Active templates", templates)
}

func (h *GoalTemplateHandler) Update(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	var req models.GoalTemplateUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	template, err := h.service.Update(c.UserContext(), id, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to update goal template")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Goal template updated", template)
}

func (h *GoalTemplateHandler) Delete(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	if err := h.service.Delete(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to delete goal template")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Goal template deleted", nil)
}
