package handlers

import (
	"strconv"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	cstmContext "github.com/automax/backend/pkg/context"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type ReportTemplateHandler struct {
	templateService services.ReportTemplateService
}

func NewReportTemplateHandler(templateService services.ReportTemplateService) *ReportTemplateHandler {
	return &ReportTemplateHandler{
		templateService: templateService,
	}
}

// CreateTemplate creates a new report template
func (h *ReportTemplateHandler) CreateTemplate(c *fiber.Ctx) error {
	var req models.ReportTemplateCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	template, err := h.templateService.CreateTemplate(c.UserContext(), &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Template created successfully", template)
}

// GetTemplate retrieves a template by ID
func (h *ReportTemplateHandler) GetTemplate(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid template ID")
	}

	template, err := h.templateService.GetTemplate(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Template not found")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Template retrieved successfully", template)
}

// ListTemplates lists all templates with optional filters
func (h *ReportTemplateHandler) ListTemplates(c *fiber.Ctx) error {
	filter := &models.ReportTemplateFilter{
		Search: c.Query("search"),
		Page:   c.QueryInt("page", 1),
		Limit:  c.QueryInt("limit", 20),
	}

	if c.Query("is_public") != "" {
		isPublic := c.Query("is_public") == "true"
		filter.IsPublic = &isPublic
	}

	templates, total, err := h.templateService.ListTemplates(c.UserContext(), filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.PaginatedSuccessResponse(c, templates, filter.Page, filter.Limit, total)
}

// UpdateTemplate updates an existing template
func (h *ReportTemplateHandler) UpdateTemplate(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid template ID")
	}

	var req models.ReportTemplateUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	template, err := h.templateService.UpdateTemplate(c.UserContext(), id, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Template updated successfully", template)
}

// DeleteTemplate deletes a template
func (h *ReportTemplateHandler) DeleteTemplate(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid template ID")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	if err := h.templateService.DeleteTemplate(c.UserContext(), id, userID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Template deleted successfully", nil)
}

// DuplicateTemplate creates a copy of an existing template
func (h *ReportTemplateHandler) DuplicateTemplate(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid template ID")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	template, err := h.templateService.DuplicateTemplate(c.UserContext(), id, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Template duplicated successfully", template)
}

// SetDefaultTemplate sets a template as the default
func (h *ReportTemplateHandler) SetDefaultTemplate(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid template ID")
	}

	if err := h.templateService.SetDefaultTemplate(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Default template set successfully", nil)
}

// GetDefaultTemplate retrieves the default template
func (h *ReportTemplateHandler) GetDefaultTemplate(c *fiber.Ctx) error {
	template, err := h.templateService.GetDefaultTemplate(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "No default template found")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Default template retrieved successfully", template)
}

// GenerateReport generates a report from a template
func (h *ReportTemplateHandler) GenerateReport(c *fiber.Ctx) error {
	var req models.GenerateReportRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ctx := cstmContext.WithReportDataSource(c.UserContext(), req.DataSource)
	ctx = cstmContext.WithReportColumns(ctx, req.Columns)

	data, filename, contentType, err := h.templateService.GenerateReport(ctx, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	c.Set("Content-Type", contentType)
	c.Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	c.Set("Content-Length", strconv.Itoa(len(data)))

	return c.Send(data)
}

// PreviewTemplate generates a preview of a template
func (h *ReportTemplateHandler) PreviewTemplate(c *fiber.Ctx) error {
	var req struct {
		Template   models.TemplateConfig `json:"template"`
		DataSource string                `json:"data_source"`
		Limit      int                   `json:"limit"`
	}

	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if req.Limit == 0 {
		req.Limit = 10
	}

	data, err := h.templateService.PreviewTemplate(c.UserContext(), &req.Template, req.DataSource, req.Limit)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	c.Set("Content-Type", "application/pdf")
	c.Set("Content-Disposition", "inline; filename=\"preview.pdf\"")

	return c.Send(data)
}
