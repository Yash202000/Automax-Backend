package handlers

import (
	"errors"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	exist, err := h.service.CommentTemplateExist(c.UserContext(), req.CommentText, req.WorkflowTransitionID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_check_exist"))
	}

	if exist {
		return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "comment_template_exists"))
	}

	resp, err := h.service.Create(c.UserContext(), &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "comment_template_created"), resp)
}

// GetByID handles GET /admin/comment-templates/:id
func (h *CommentTemplateHandler) GetByID(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	resp, err := h.service.Get(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "comment_template_retrieved"), resp)
}

// GetByWorkflowTransitionID handles GET /admin/comment-templates/workflow-transition/:workflowTransitionId
func (h *CommentTemplateHandler) GetByWorkflowTransitionID(c *fiber.Ctx) error {
	workflowTransitionIDStr := c.Params("workflowTransitionId")
	workflowTransitionID, err := uuid.Parse(workflowTransitionIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_workflow_transition_id"))
	}

	resp, err := h.service.GetByWorkflowTransitionID(c.UserContext(), workflowTransitionID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "comment_templates_retrieved"), resp)
}

// List handles GET /admin/comment-templates
func (h *CommentTemplateHandler) List(c *fiber.Ctx) error {
	includeInactive := c.QueryBool("include_inactive", false)

	resp, err := h.service.List(c.UserContext(), includeInactive)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "comment_templates_retrieved"), resp)
}

// Update handles PUT /admin/comment-templates/:id
func (h *CommentTemplateHandler) Update(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.CommentTemplateUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}
	commentTemplate, err := h.service.Get(c.UserContext(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "comment_template_not_found"))
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_retrieve_comment_template"))
	}

	if commentTemplate == nil || commentTemplate.WorkflowTransition == nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "comment_template_not_found"))
	}

	if commentTemplate.CommentText != *req.CommentText {
		exist, err := h.service.CommentTemplateExist(c.UserContext(), *req.CommentText, commentTemplate.WorkflowTransitionID)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_check_exist"))
		}
		if exist {
			return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "comment_template_text_exists"))
		}
	}

	resp, err := h.service.Update(c.UserContext(), id, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "comment_template_updated"), resp)
}

// Delete handles DELETE /admin/comment-templates/:id
func (h *CommentTemplateHandler) Delete(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	err = h.service.Delete(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "comment_template_deleted"), nil)
}
