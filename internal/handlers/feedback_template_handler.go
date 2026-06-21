package handlers

import (
	"errors"
	"log"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	exist, err := h.service.FeedbackTemplateExist(c.UserContext(), req.FeedbackText, req.WorkflowTransitionID)
	if err != nil {
		log.Print(err)
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_check_exist"))
	}

	if exist {
		return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "feedback_template_exists"))
	}

	resp, err := h.service.Create(c.UserContext(), &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "feedback_template_created"), resp)
}

// GetByID handles GET /admin/feedback -templates/:id
func (h *FeedbackTemplateHandler) GetByID(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	resp, err := h.service.Get(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "feedback_template_retrieved"), resp)
}

// GetByWorkflowTransitionID handles GET /admin/feedback -templates/workflow-transition/:workflowTransitionId
func (h *FeedbackTemplateHandler) GetByWorkflowTransitionID(c *fiber.Ctx) error {
	workflowTransitionIDStr := c.Params("workflowTransitionId")
	workflowTransitionID, err := uuid.Parse(workflowTransitionIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_workflow_transition_id"))
	}

	resp, err := h.service.GetByWorkflowTransitionID(c.UserContext(), workflowTransitionID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "feedback_templates_retrieved"), resp)
}

// List handles GET /admin/feedback -templates
func (h *FeedbackTemplateHandler) List(c *fiber.Ctx) error {
	includeInactive := c.QueryBool("include_inactive", false)

	resp, err := h.service.List(c.UserContext(), includeInactive)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "feedback_templates_retrieved"), resp)
}

// Update handles PUT /admin/feedback -templates/:id
func (h *FeedbackTemplateHandler) Update(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.FeedbackTemplateUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	feedbackTemplate, err := h.service.Get(c.UserContext(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "feedback_template_not_found"))
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_retrieve_feedback_template"))
	}

	if feedbackTemplate == nil || feedbackTemplate.WorkflowTransition == nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "feedback_template_not_found"))
	}

	if feedbackTemplate.FeedbackText != *req.FeedbackText {
		exist, err := h.service.FeedbackTemplateExist(c.UserContext(), *req.FeedbackText, feedbackTemplate.WorkflowTransitionID)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_check_exist"))
		}
		if exist {
			return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "feedback_template_text_exists"))
		}
	}

	resp, err := h.service.Update(c.UserContext(), id, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "feedback_template_updated"), resp)
}

// Delete handles DELETE /admin/feedback -templates/:id
func (h *FeedbackTemplateHandler) Delete(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	err = h.service.Delete(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "feedback_template_deleted"), nil)
}
