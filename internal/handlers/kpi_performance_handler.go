package handlers

import (
	"fmt"
	"strconv"

	"github.com/automax/backend/internal/middleware"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type KpiPerformanceHandler struct {
	db            *gorm.DB
	validator     *validator.Validate
	workflowSvc   *services.KpiWorkflowService
	actionLogSvc  services.ActionLogService
}

func NewKpiPerformanceHandler(db *gorm.DB, workflowSvc *services.KpiWorkflowService, actionLogSvc services.ActionLogService) *KpiPerformanceHandler {
	return &KpiPerformanceHandler{
		db:           db,
		validator:    validator.New(),
		workflowSvc:  workflowSvc,
		actionLogSvc: actionLogSvc,
	}
}

// ─── Annual Targets ───────────────────────────────────────────────────────────

func (h *KpiPerformanceHandler) ListTargets(c *fiber.Ctx) error {
	q := h.db.WithContext(c.UserContext()).Model(&models.KpiAnnualTarget{})

	if kpiCode := c.Query("kpi_code"); kpiCode != "" {
		q = q.Where("kpi_code = ?", kpiCode)
	}
	if yearStr := c.Query("year"); yearStr != "" {
		year, err := strconv.Atoi(yearStr)
		if err == nil {
			q = q.Where("year = ?", year)
		}
	}

	var items []models.KpiAnnualTarget
	if err := q.Order("year DESC, kpi_code ASC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", items)
}

func (h *KpiPerformanceHandler) SetTarget(c *fiber.Ctx) error {
	var req models.KpiAnnualTargetRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}

	item := &models.KpiAnnualTarget{
		KpiCode:     req.KpiCode,
		KpiType:     req.KpiType,
		Year:        req.Year,
		TargetValue: req.TargetValue,
	}

	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "create",
		Module:      "kpi",
		ResourceID:  item.ID.String(),
		Description: fmt.Sprintf("Set annual target for %s (%d)", item.KpiCode, item.Year),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusCreated, "", item)
}

func (h *KpiPerformanceHandler) DeleteTarget(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	result := h.db.WithContext(c.UserContext()).Delete(&models.KpiAnnualTarget{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "delete",
		Module:      "kpi",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("Deleted annual target %s", id),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

func (h *KpiPerformanceHandler) GetPerformance(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var item models.KpiPerformance
	if err := h.db.WithContext(c.UserContext()).Preload("SubmittedBy").Preload("ApprovedBy").First(&item, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

// ─── Performance ──────────────────────────────────────────────────────────────

func (h *KpiPerformanceHandler) ListPerformance(c *fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))

	page = max(page, 1)
	if limit < 1 || limit > 100 {
		limit = 10
	}

	q := h.db.WithContext(c.UserContext()).Model(&models.KpiPerformance{}).
		Preload("SubmittedBy").Preload("ApprovedBy")

	if kpiCode := c.Query("kpi_code"); kpiCode != "" {
		q = q.Where("kpi_code = ?", kpiCode)
	}
	if yearStr := c.Query("year"); yearStr != "" {
		year, err := strconv.Atoi(yearStr)
		if err == nil {
			q = q.Where("year = ?", year)
		}
	}
	if quarterStr := c.Query("quarter"); quarterStr != "" {
		quarter, err := strconv.Atoi(quarterStr)
		if err == nil {
			q = q.Where("quarter = ?", quarter)
		}
	}
	if status := c.Query("status"); status != "" {
		q = q.Where("status = ?", status)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	var items []models.KpiPerformance
	if err := q.Order("year DESC, quarter DESC, created_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	resp := make([]models.KpiPerformanceResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}

	return utils.PaginatedSuccessResponse(c, resp, page, limit, total)
}

func (h *KpiPerformanceHandler) SubmitPerformance(c *fiber.Ctx) error {
	var req models.KpiPerformanceRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	item := &models.KpiPerformance{
		KpiCode:          req.KpiCode,
		KpiType:          req.KpiType,
		Year:             req.Year,
		Quarter:          req.Quarter,
		Target:           req.Target,
		Actual:           req.Actual,
		TrendDescription: req.TrendDescription,
		Justification:    req.Justification,
		CorrectiveAction: req.CorrectiveAction,
		Status:           models.KPIPerfStatusDraft,
	}

	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}

	if _, err := h.workflowSvc.InitiateKpiPerformanceWorkflow(c.UserContext(), item.ID, userID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}

	var reloaded models.KpiPerformance
	h.db.WithContext(c.UserContext()).Preload("SubmittedBy").Preload("ApprovedBy").First(&reloaded, item.ID)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "create",
		Module:      "kpi",
		ResourceID:  item.ID.String(),
		Description: fmt.Sprintf("Created performance record for %s Q%d/%d", item.KpiCode, item.Quarter, item.Year),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusCreated, "", reloaded.ToResponse())
}

func (h *KpiPerformanceHandler) TransitionPerformance(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.MetricTransitionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	result, err := h.workflowSvc.TransitionKpiPerformance(c.UserContext(), id, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "transition",
		Module:      "kpi",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("Transitioned KPI performance %s with transition %s", id, req.TransitionID),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "transition_executed"), result)
}

func (h *KpiPerformanceHandler) GetAvailableTransitions(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	transitions, err := h.workflowSvc.GetAvailableKpiPerformanceTransitions(c.UserContext(), id, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	if transitions == nil {
		transitions = []models.WorkflowTransition{}
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", transitions)
}

// ─── Benchmarks ───────────────────────────────────────────────────────────────

func (h *KpiPerformanceHandler) ListBenchmarks(c *fiber.Ctx) error {
	q := h.db.WithContext(c.UserContext()).Model(&models.KpiBenchmark{}).
		Preload("Department")

	if kpiCode := c.Query("kpi_code"); kpiCode != "" {
		q = q.Where("kpi_code = ?", kpiCode)
	}
	if zone := c.Query("zone"); zone != "" {
		q = q.Where("zone = ?", zone)
	}
	if deptID := c.Query("department_id"); deptID != "" {
		q = q.Where("department_id = ?", deptID)
	}
	if yearStr := c.Query("year"); yearStr != "" {
		year, err := strconv.Atoi(yearStr)
		if err == nil {
			q = q.Where("year = ?", year)
		}
	}

	var items []models.KpiBenchmark
	if err := q.Order("year DESC, kpi_code ASC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	resp := make([]models.KpiBenchmarkResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToBenchmarkResponse()
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

func (h *KpiPerformanceHandler) CreateBenchmark(c *fiber.Ctx) error {
	var req models.KpiBenchmarkRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}

	var deptID *uuid.UUID
	if req.DepartmentID != nil && *req.DepartmentID != "" {
		pid, err := uuid.Parse(*req.DepartmentID)
		if err == nil {
			deptID = &pid
		}
	}

	item := &models.KpiBenchmark{
		KpiCode:              req.KpiCode,
		KpiType:              req.KpiType,
		Year:                 req.Year,
		Quarter:              req.Quarter,
		Zone:                 req.Zone,
		DepartmentID:         deptID,
		BenchmarkEntity:      req.BenchmarkEntity,
		InternalAchievement:  req.InternalAchievement,
		BenchmarkAchievement: req.BenchmarkAchievement,
		Notes:                req.Notes,
	}

	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "create",
		Module:      "kpi",
		ResourceID:  item.ID.String(),
		Description: fmt.Sprintf("Created benchmark for %s - %s", item.KpiCode, item.BenchmarkEntity),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToBenchmarkResponse())
}

func (h *KpiPerformanceHandler) DeleteBenchmark(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	result := h.db.WithContext(c.UserContext()).Delete(&models.KpiBenchmark{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "delete",
		Module:      "kpi",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("Deleted benchmark %s", id),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

func (h *KpiPerformanceHandler) ListBenchmarkSummary(c *fiber.Ctx) error {
	type BenchSummary struct {
		KpiCode              string  `json:"kpi_code"`
		Zone                 string  `json:"zone"`
		BenchmarkEntity      string  `json:"benchmark_entity"`
		AvgInternal          float64 `json:"avg_internal"`
		AvgBenchmark         float64 `json:"avg_benchmark"`
		AvgVariance          float64 `json:"avg_variance"`
		TotalRecords         int64   `json:"total_records"`
	}

	var results []BenchSummary
	if err := h.db.WithContext(c.UserContext()).Model(&models.KpiBenchmark{}).
		Select("kpi_code, zone, benchmark_entity, "+
			"AVG(internal_achievement) as avg_internal, "+
			"AVG(benchmark_achievement) as avg_benchmark, "+
			"AVG(internal_achievement - benchmark_achievement) as avg_variance, "+
			"COUNT(*) as total_records").
		Group("kpi_code, zone, benchmark_entity").
		Order("kpi_code ASC").
		Scan(&results).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", results)
}

// ─── Segmentation ─────────────────────────────────────────────────────────────

func (h *KpiPerformanceHandler) ListSegmentation(c *fiber.Ctx) error {
	q := h.db.WithContext(c.UserContext()).Model(&models.KpiSegmentation{}).
		Preload("Department")

	if kpiCode := c.Query("kpi_code"); kpiCode != "" {
		q = q.Where("kpi_code = ?", kpiCode)
	}
	if yearStr := c.Query("year"); yearStr != "" {
		year, err := strconv.Atoi(yearStr)
		if err == nil {
			q = q.Where("year = ?", year)
		}
	}
	if quarterStr := c.Query("quarter"); quarterStr != "" {
		quarter, err := strconv.Atoi(quarterStr)
		if err == nil {
			q = q.Where("quarter = ?", quarter)
		}
	}
	if dim := c.Query("dimension"); dim != "" {
		q = q.Where("dimension_name = ?", dim)
	}
	if deptID := c.Query("department_id"); deptID != "" {
		q = q.Where("department_id = ?", deptID)
	}
	if zone := c.Query("zone"); zone != "" {
		q = q.Where("zone = ?", zone)
	}

	var items []models.KpiSegmentation
	if err := q.Order("year DESC, quarter DESC, dimension_name ASC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	resp := make([]models.KpiSegmentationResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToSegmentationResponse()
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

func (h *KpiPerformanceHandler) CreateSegmentation(c *fiber.Ctx) error {
	var req models.KpiSegmentationRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}

	var deptID *uuid.UUID
	if req.DepartmentID != nil && *req.DepartmentID != "" {
		pid, err := uuid.Parse(*req.DepartmentID)
		if err == nil {
			deptID = &pid
		}
	}

	item := &models.KpiSegmentation{
		KpiCode:       req.KpiCode,
		KpiType:       req.KpiType,
		Year:          req.Year,
		Quarter:       req.Quarter,
		DimensionName: req.DimensionName,
		SegmentName:   req.SegmentName,
		Target:        req.Target,
		Achievement:   req.Achievement,
		DepartmentID:  deptID,
		Zone:          req.Zone,
	}

	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "create",
		Module:      "kpi",
		ResourceID:  item.ID.String(),
		Description: fmt.Sprintf("Created segmentation for %s - %s/%s", item.KpiCode, item.DimensionName, item.SegmentName),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToSegmentationResponse())
}

func (h *KpiPerformanceHandler) DeleteSegmentation(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	result := h.db.WithContext(c.UserContext()).Delete(&models.KpiSegmentation{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "delete",
		Module:      "kpi",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("Deleted segmentation %s", id),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

func (h *KpiPerformanceHandler) ListSegmentationSummary(c *fiber.Ctx) error {
	type SegSummary struct {
		DimensionName string  `json:"dimension_name"`
		SegmentName   string  `json:"segment_name"`
		AvgAchievement float64 `json:"avg_achievement"`
		AvgTarget     float64 `json:"avg_target"`
		AvgPct        float64 `json:"avg_pct"`
		TotalRecords  int64   `json:"total_records"`
	}

	var results []SegSummary
	if err := h.db.WithContext(c.UserContext()).Model(&models.KpiSegmentation{}).
		Select("dimension_name, segment_name, "+
			"AVG(achievement) as avg_achievement, "+
			"AVG(target) as avg_target, "+
			"CASE WHEN AVG(target) > 0 THEN (AVG(achievement) / AVG(target)) * 100 ELSE 0 END as avg_pct, "+
			"COUNT(*) as total_records").
		Group("dimension_name, segment_name").
		Order("dimension_name ASC, segment_name ASC").
		Scan(&results).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", results)
}

func (h *KpiPerformanceHandler) GetPerformanceHistory(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var perf models.KpiPerformance
	if err := h.db.WithContext(c.UserContext()).First(&perf, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	if perf.WorkflowInstanceID == nil {
		return utils.SuccessResponse(c, fiber.StatusOK, "", []models.KpiWorkflowAction{})
	}

	var actions []models.KpiWorkflowAction
	if err := h.db.WithContext(c.UserContext()).
		Preload("Transition").
		Preload("FromState").
		Preload("ToState").
		Preload("PerformedBy").
		Where("workflow_instance_id = ?", *perf.WorkflowInstanceID).
		Order("performed_at ASC").
		Find(&actions).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	resp := make([]models.KpiWorkflowActionResponse, len(actions))
	for i, a := range actions {
		resp[i] = a.ToResponse()
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}
