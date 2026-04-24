package handlers

import (
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type FeedbackTemplateHandler struct {
	service services.FeedbackTemplateService
}

func NewFeedbackTemplateHandler(service services.FeedbackTemplateService) *FeedbackTemplateHandler {
	return &FeedbackTemplateHandler{service: service}
}

// Create handles POST /admin/feedback -templates
func (h *FeedbackTemplateHandler) Create(c *fiber.Ctx) error {
	var req models.FeedbackTemplateCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	resp, err := h.service.Create(c.UserContext(), &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "feedback template created successfully", resp)
}

// GetByID handles GET /admin/feedback -templates/:id
func (h *FeedbackTemplateHandler) GetByID(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	resp, err := h.service.Get(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "feedback template retrieved successfully", resp)
}

// GetByWorkflowTransitionID handles GET /admin/feedback -templates/workflow-transition/:workflowTransitionId
func (h *FeedbackTemplateHandler) GetByWorkflowTransitionID(c *fiber.Ctx) error {
	workflowTransitionIDStr := c.Params("workflowTransitionId")
	workflowTransitionID, err := uuid.Parse(workflowTransitionIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid workflow transition ID")
	}

	resp, err := h.service.GetByWorkflowTransitionID(c.UserContext(), workflowTransitionID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "feedback templates retrieved successfully", resp)
}

// List handles GET /admin/feedback -templates
func (h *FeedbackTemplateHandler) List(c *fiber.Ctx) error {
	includeInactive := c.QueryBool("include_inactive", false)

	resp, err := h.service.List(c.UserContext(), includeInactive)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "feedback templates retrieved successfully", resp)
}

// Update handles PUT /admin/feedback -templates/:id
func (h *FeedbackTemplateHandler) Update(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	var req models.FeedbackTemplateUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	resp, err := h.service.Update(c.UserContext(), id, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "feedback template updated successfully", resp)
}

// Delete handles DELETE /admin/feedback -templates/:id
func (h *FeedbackTemplateHandler) Delete(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	err = h.service.Delete(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "feedback template deleted successfully", nil)
}
