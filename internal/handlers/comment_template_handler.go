package handlers

import (
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type CommentTemplateHandler struct {
	service services.CommentTemplateService
}

func NewCommentTemplateHandler(service services.CommentTemplateService) *CommentTemplateHandler {
	return &CommentTemplateHandler{service: service}
}

// Create handles POST /admin/comment-templates
func (h *CommentTemplateHandler) Create(c *fiber.Ctx) error {
	var req models.CommentTemplateCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	exist, err := h.service.CommentTemplateExist(c.UserContext(), req.CommentText, req.WorkflowTransitionID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to check existence")
	}

	if exist {
		return utils.ErrorResponse(c, fiber.StatusConflict, "Comment template already exists")
	}

	resp, err := h.service.Create(c.UserContext(), &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Comment template created successfully", resp)
}

// GetByID handles GET /admin/comment-templates/:id
func (h *CommentTemplateHandler) GetByID(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	resp, err := h.service.Get(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Comment template retrieved successfully", resp)
}

// GetByWorkflowTransitionID handles GET /admin/comment-templates/workflow-transition/:workflowTransitionId
func (h *CommentTemplateHandler) GetByWorkflowTransitionID(c *fiber.Ctx) error {
	workflowTransitionIDStr := c.Params("workflowTransitionId")
	workflowTransitionID, err := uuid.Parse(workflowTransitionIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid workflow transition ID")
	}

	resp, err := h.service.GetByWorkflowTransitionID(c.UserContext(), workflowTransitionID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Comment templates retrieved successfully", resp)
}

// List handles GET /admin/comment-templates
func (h *CommentTemplateHandler) List(c *fiber.Ctx) error {
	includeInactive := c.QueryBool("include_inactive", false)

	resp, err := h.service.List(c.UserContext(), includeInactive)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Comment templates retrieved successfully", resp)
}

// Update handles PUT /admin/comment-templates/:id
func (h *CommentTemplateHandler) Update(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	var req models.CommentTemplateUpdateRequest
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

	return utils.SuccessResponse(c, fiber.StatusOK, "Comment template updated successfully", resp)
}

// Delete handles DELETE /admin/comment-templates/:id
func (h *CommentTemplateHandler) Delete(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	err = h.service.Delete(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Comment template deleted successfully", nil)
}
