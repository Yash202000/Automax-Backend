package handlers

import (
	"strconv"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	cstmContext "github.com/automax/backend/pkg/context"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type ReportHandler struct {
	service   services.ReportService
	validator *validator.Validate
}

func NewReportHandler(service services.ReportService) *ReportHandler {
	return &ReportHandler{
		service:   service,
		validator: validator.New(),
	}
}

// Report CRUD

func (h *ReportHandler) CreateReport(c *fiber.Ctx) error {
	var req models.ReportCreateRequest
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

	report, err := h.service.CreateReport(c.UserContext(), &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Report created successfully", report)
}

func (h *ReportHandler) GetReport(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid report ID")
	}

	report, err := h.service.GetReport(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Report not found")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Report retrieved successfully", report)
}

func (h *ReportHandler) ListReports(c *fiber.Ctx) error {
	filter := &models.ReportFilter{}

	// Parse query parameters
	if page := c.Query("page"); page != "" {
		if p, err := strconv.Atoi(page); err == nil {
			filter.Page = p
		}
	}
	if filter.Page < 1 {
		filter.Page = 1
	}

	if limit := c.Query("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil {
			filter.Limit = l
		}
	}
	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 20
	}

	filter.Search = c.Query("search")

	if dataSource := c.Query("data_source"); dataSource != "" {
		filter.DataSource = &dataSource
	}

	if isPublic := c.Query("is_public"); isPublic != "" {
		pub := isPublic == "true"
		filter.IsPublic = &pub
	}

	// Get own reports or all if admin
	if mine := c.Query("mine"); mine == "true" {
		userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
		filter.CreatedByID = &userID
	}

	reports, total, err := h.service.ListReports(c.UserContext(), filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	totalPages := (int(total) + filter.Limit - 1) / filter.Limit

	return c.JSON(fiber.Map{
		"success":     true,
		"data":        reports,
		"page":        filter.Page,
		"limit":       filter.Limit,
		"total_items": total,
		"total_pages": totalPages,
	})
}

func (h *ReportHandler) UpdateReport(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid report ID")
	}

	var req models.ReportUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	report, err := h.service.UpdateReport(c.UserContext(), id, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Report updated successfully", report)
}

func (h *ReportHandler) DeleteReport(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid report ID")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	if err := h.service.DeleteReport(c.UserContext(), id, userID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Report deleted successfully", nil)
}

func (h *ReportHandler) DuplicateReport(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid report ID")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	report, err := h.service.DuplicateReport(c.UserContext(), id, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Report duplicated successfully", report)
}

// Report Execution

func (h *ReportHandler) ExecuteReport(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid report ID")
	}

	var req models.ReportExecuteRequest
	if err := c.BodyParser(&req); err != nil {
		// Allow empty body for simple execution
		req = models.ReportExecuteRequest{}
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	result, err := h.service.ExecuteReport(c.UserContext(), id, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Report executed successfully", result)
}

func (h *ReportHandler) PreviewReport(c *fiber.Ctx) error {
	var req models.ReportCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if req.DataSource == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "data_source is required")
	}

	result, err := h.service.PreviewReport(c.UserContext(), &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Preview generated successfully", result)
}

func (h *ReportHandler) QueryReport(c *fiber.Ctx) error {
	var req models.ReportQueryRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if req.DataSource == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "data_source is required")
	}

	if len(req.Columns) == 0 {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "columns are required")
	}

	// Set defaults
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Limit < 1 || req.Limit > 1000 {
		req.Limit = 50
	}

	ctx := cstmContext.WithReportDataSource(c.UserContext(), req.DataSource)
	ctx = cstmContext.WithReportColumns(ctx, req.Columns)
	result, err := h.service.QueryReport(ctx, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(result)
}

func (h *ReportHandler) ExportReport(c *fiber.Ctx) error {
	var req models.ReportExportRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); validationErrors != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	ctx := cstmContext.WithReportDataSource(c.UserContext(), req.DataSource)
	ctx = cstmContext.WithReportColumns(ctx, req.Columns)
	data, filename, contentType, err := h.service.ExportReport(ctx, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	c.Set("Content-Type", contentType)
	c.Set("Content-Disposition", "attachment; filename="+filename)
	return c.Send(data)
}

func (h *ReportHandler) GetExecutionHistory(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid report ID")
	}

	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))

	page = max(page, 1)
	if limit < 1 || limit > 100 {
		limit = 20
	}

	executions, total, err := h.service.GetExecutionHistory(c.UserContext(), id, page, limit)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	totalPages := (int(total) + limit - 1) / limit

	return c.JSON(fiber.Map{
		"success":     true,
		"data":        executions,
		"page":        page,
		"limit":       limit,
		"total_items": total,
		"total_pages": totalPages,
	})
}

// Metadata

func (h *ReportHandler) GetDataSources(c *fiber.Ctx) error {
	dataSources := h.service.GetDataSources(c.UserContext())
	return utils.SuccessResponse(c, fiber.StatusOK, "Data sources retrieved successfully", dataSources)
}
