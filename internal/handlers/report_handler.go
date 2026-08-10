package handlers

import (
	"context"
	"errors"
	"os"
	"strconv"
	"strings"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	cstmContext "github.com/automax/backend/pkg/context"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type ReportHandler struct {
	service   services.ReportService
	userRepo  repository.UserRepository
	validator *validator.Validate
}

func NewReportHandler(service services.ReportService, userRepo repository.UserRepository) *ReportHandler {
	return &ReportHandler{
		service:   service,
		userRepo:  userRepo,
		validator: validator.New(),
	}
}

// incidentDataSources lists data sources that query the incidents table
// and support classification_id / location_id filtering.
var incidentDataSources = map[string]bool{
	"incidents":                 true,
	"requests":                  true,
	"users_performance":         true,
	"locations_by_count":        true,
	"locations_by_status":       true,
	"classifications_by_count":  true,
	"classifications_by_status": true,
	"departments_by_count":      true,
	"departments_by_status":     true,
}

// injectReportScope applies all user-level report scoping when
// RESTRICT_REPORT_SCOPE=true. This mirrors the incident list handler:
//   - classification/location filtering (user's assigned set)
//   - my_record + viewable_roles (state-level visibility)
//   - view_department_only (department scoping)
//
// Returns the enriched filters and context. Super admins bypass all scoping
// unless RESTRICT_ADMIN_SCOPE=true.
func (h *ReportHandler) injectReportScope(c *fiber.Ctx, ctx context.Context, dataSource string, filters []models.ReportFilterConfig) ([]models.ReportFilterConfig, context.Context) {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("RESTRICT_REPORT_SCOPE")), "true") {
		return filters, ctx
	}
	if !incidentDataSources[dataSource] {
		return filters, ctx
	}

	restrictAdminRole := strings.EqualFold(strings.TrimSpace(os.Getenv("RESTRICT_ADMIN_SCOPE")), "true")
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	user, err := h.userRepo.FindByIDWithRelations(c.UserContext(), userID)
	if err != nil || user == nil {
		return filters, ctx
	}
	if user.IsSuperAdmin || (!restrictAdminRole && user.HasRole("admin")) {
		return filters, ctx
	}

	// ── Classification / location / department scoping ─────────────────
	noAccessSentinel := []interface{}{"00000000-0000-0000-0000-000000000000"}

	scope := &models.ReportAccessScope{
		UserID:  userID,
		ViewAll: user.HasPermission("incidents:view_all"),
	}

	if user.HasPermission("incidents:view_department_only") {
		// Department-scoped: use DeptManager fields (mirrors ListIncidents lines 273-298)
		scope.ViewDepartmentOnly = true
		scope.DepartmentIDs = user.ScopeDepartmentIDs()

		if user.DeptManagerClassificationID != nil {
			filters = append(filters, models.ReportFilterConfig{
				Field:    "classification_id",
				Operator: "equals",
				Value:    user.DeptManagerClassificationID.String(),
			})
		}
		if user.DeptManagerLocationID != nil {
			filters = append(filters, models.ReportFilterConfig{
				Field:    "location_id",
				Operator: "equals",
				Value:    user.DeptManagerLocationID.String(),
			})
		}
	} else {
		// Regular user: use M2M classification/location assignments
		hasClassification := false
		hasLocation := false
		for _, f := range filters {
			if f.Field == "classification_id" && f.Value != nil {
				hasClassification = true
			}
			if f.Field == "location_id" && f.Value != nil {
				hasLocation = true
			}
		}

		if !hasClassification {
			classIDs := make([]interface{}, 0, len(user.Classifications))
			for _, cls := range user.Classifications {
				classIDs = append(classIDs, cls.ID.String())
			}
			if len(classIDs) == 0 {
				classIDs = noAccessSentinel
			}
			filters = append(filters, models.ReportFilterConfig{
				Field:    "classification_id",
				Operator: "in",
				Value:    classIDs,
			})
		}

		if !hasLocation {
			locIDs := make([]interface{}, 0, len(user.Locations))
			for _, loc := range user.Locations {
				locIDs = append(locIDs, loc.ID.String())
			}
			if len(locIDs) == 0 {
				locIDs = noAccessSentinel
			}
			filters = append(filters, models.ReportFilterConfig{
				Field:    "location_id",
				Operator: "in",
				Value:    locIDs,
			})
		}
	}

	roleIDs := make([]uuid.UUID, 0, len(user.Roles))
	for _, r := range user.Roles {
		roleIDs = append(roleIDs, r.ID)
	}
	scope.RoleIDs = roleIDs

	return filters, cstmContext.WithReportAccessScope(ctx, scope)
}

// Report CRUD

func (h *ReportHandler) CreateReport(c *fiber.Ctx) error {
	var req models.ReportCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
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

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "report_created"), report)
}

func (h *ReportHandler) GetReport(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_report_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	report, err := h.service.GetReport(c.UserContext(), id, userID)
	if err != nil {
		if errors.Is(err, services.ErrReportAccessDenied) {
			return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(c.UserContext(), "access_denied"))
		}
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "report_not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "report_retrieved"), report)
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

	// Reports are visible to their creator, plus anyone once marked public.
	// This applies to every user, including super admins.
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	filter.VisibleToUserID = &userID

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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_report_id"))
	}

	var req models.ReportUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	report, err := h.service.UpdateReport(c.UserContext(), id, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "report_updated"), report)
}

func (h *ReportHandler) DeleteReport(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_report_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	if err := h.service.DeleteReport(c.UserContext(), id, userID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "report_deleted"), nil)
}

func (h *ReportHandler) DuplicateReport(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_report_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	report, err := h.service.DuplicateReport(c.UserContext(), id, userID)
	if err != nil {
		if errors.Is(err, services.ErrReportAccessDenied) {
			return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(c.UserContext(), "access_denied"))
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "report_duplicated"), report)
}

// Report Execution

func (h *ReportHandler) ExecuteReport(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_report_id"))
	}

	var req models.ReportExecuteRequest
	if err := c.BodyParser(&req); err != nil {
		// Allow empty body for simple execution
		req = models.ReportExecuteRequest{}
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	// Fetch the saved report to get DataSource and stored filters for access scoping.
	report, err := h.service.GetReport(c.UserContext(), id, userID)
	if err != nil {
		if errors.Is(err, services.ErrReportAccessDenied) {
			return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(c.UserContext(), "access_denied"))
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}
	// Mirror the service's filter resolution: use request filters if provided, else stored.
	filters := report.Config.Filters
	if len(req.Filters) > 0 {
		filters = req.Filters
	}
	scopedFilters, ctx := h.injectReportScope(c, c.UserContext(), report.DataSource, filters)
	req.Filters = scopedFilters
	result, err := h.service.ExecuteReport(ctx, id, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "report_executed"), result)
}

func (h *ReportHandler) PreviewReport(c *fiber.Ctx) error {
	var req models.ReportCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if req.DataSource == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "data_source_required"))
	}

	scopedFilters, ctx := h.injectReportScope(c, c.UserContext(), req.DataSource, req.Config.Filters)
	req.Config.Filters = scopedFilters
	result, err := h.service.PreviewReport(ctx, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "preview_generated"), result)
}

func (h *ReportHandler) QueryReport(c *fiber.Ctx) error {
	var req models.ReportQueryRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if req.DataSource == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "data_source_required"))
	}

	if len(req.Columns) == 0 {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "columns_required"))
	}

	// Set defaults
	if req.Page < 1 {
		req.Page = 1
	}
	if req.Limit < 1 || req.Limit > 1000 {
		req.Limit = 50
	}

	scopedFilters, ctx := h.injectReportScope(c, c.UserContext(), req.DataSource, req.Filters)
	req.Filters = scopedFilters
	ctx = cstmContext.WithReportDataSource(ctx, req.DataSource)
	ctx = cstmContext.WithReportColumns(ctx, req.Columns)
	ctx = cstmContext.WithReportTimezone(ctx, req.Timezone)
	result, err := h.service.QueryReport(ctx, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(result)
}

func (h *ReportHandler) ExportReport(c *fiber.Ctx) error {
	var req models.ReportExportRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); validationErrors != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	// Use timezone from top-level field, fall back to options.timezone
	tz := req.Timezone
	if tz == "" && req.Options != nil {
		tz = req.Options.Timezone
	}
	scopedFilters, ctx := h.injectReportScope(c, c.UserContext(), req.DataSource, req.Filters)
	req.Filters = scopedFilters
	ctx = cstmContext.WithReportDataSource(ctx, req.DataSource)
	ctx = cstmContext.WithReportColumns(ctx, req.Columns)
	ctx = cstmContext.WithReportTimezone(ctx, tz)
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_report_id"))
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
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "data_sources_retrieved"), dataSources)
}
