package handlers

import (
	"log"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type ReportPdfTemplateHandler struct {
	svc services.ReportPdfTemplateService
}

func NewReportPdfTemplateHandler(svc services.ReportPdfTemplateService) *ReportPdfTemplateHandler {
	return &ReportPdfTemplateHandler{svc: svc}
}

func (h *ReportPdfTemplateHandler) Create(c *fiber.Ctx) error {
	var req models.ReportPdfTemplateCreateRequest
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
	template, err := h.svc.CreatePdfTemplate(c.UserContext(), &req, userID)
	if err != nil {
		log.Printf("[ReportPdfTemplateHandler] Create: %v", err)
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "PDF template created successfully", template)
}

func (h *ReportPdfTemplateHandler) GetByID(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid template ID")
	}
	template, err := h.svc.GetPdfTemplate(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "PDF template not found")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "PDF template retrieved successfully", template)
}

func (h *ReportPdfTemplateHandler) List(c *fiber.Ctx) error {
	filter := &models.ReportPdfTemplateFilter{
		Search: c.Query("search"),
		Page:   c.QueryInt("page", 1),
		Limit:  c.QueryInt("limit", 20),
	}
	templates, total, err := h.svc.ListPdfTemplates(c.UserContext(), filter)
	if err != nil {
		log.Printf("[ReportPdfTemplateHandler] List: %v", err)
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to list PDF templates")
	}
	return utils.PaginatedSuccessResponse(c, templates, filter.Page, filter.Limit, total)
}

func (h *ReportPdfTemplateHandler) Update(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid template ID")
	}
	var req models.ReportPdfTemplateUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	template, err := h.svc.UpdatePdfTemplate(c.UserContext(), id, &req, userID)
	if err != nil {
		log.Printf("[ReportPdfTemplateHandler] Update %s: %v", id, err)
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "PDF template updated successfully", template)
}

func (h *ReportPdfTemplateHandler) Delete(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid template ID")
	}
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	if err := h.svc.DeletePdfTemplate(c.UserContext(), id, userID); err != nil {
		log.Printf("[ReportPdfTemplateHandler] Delete %s: %v", id, err)
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "PDF template deleted successfully", nil)
}

func (h *ReportPdfTemplateHandler) Duplicate(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid template ID")
	}
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	template, err := h.svc.DuplicatePdfTemplate(c.UserContext(), id, userID)
	if err != nil {
		log.Printf("[ReportPdfTemplateHandler] Duplicate %s: %v", id, err)
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "PDF template duplicated successfully", template)
}

func (h *ReportPdfTemplateHandler) SetDefault(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid template ID")
	}
	if err := h.svc.SetDefaultPdfTemplate(c.UserContext(), id); err != nil {
		log.Printf("[ReportPdfTemplateHandler] SetDefault %s: %v", id, err)
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Default PDF template set successfully", nil)
}

func (h *ReportPdfTemplateHandler) GetDefault(c *fiber.Ctx) error {
	template, err := h.svc.GetDefaultPdfTemplate(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "No default PDF template found")
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Default PDF template retrieved successfully", template)
}
