package handlers

import (
	"fmt"
	"strconv"
	"time"

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
	db           *gorm.DB
	validator    *validator.Validate
	workflowSvc  *services.KpiWorkflowService
	actionLogSvc services.ActionLogService
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
	q := h.db.WithContext(c.UserContext()).Model(&models.KpiAnnualTarget{}).
		Preload("Metric").Preload("ApprovedBy")

	if kpiCode := c.Query("kpi_code"); kpiCode != "" {
		q = q.Where("kpi_code = ?", kpiCode)
	}
	if yearStr := c.Query("year"); yearStr != "" {
		year, err := strconv.Atoi(yearStr)
		if err == nil {
			q = q.Where("year = ?", year)
		}
	}
	if metricID := c.Query("metric_id"); metricID != "" {
		q = q.Where("metric_id = ?", metricID)
	}
	if periodCode := c.Query("period_code"); periodCode != "" {
		q = q.Where("period_code = ?", periodCode)
	}
	if targetStatus := c.Query("target_status"); targetStatus != "" {
		q = q.Where("target_status = ?", targetStatus)
	}
	// Also accept the old `kpi_code` filter (already handled above)

	var items []models.KpiAnnualTarget
	if err := q.Order("target_year DESC, kpi_code ASC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	resp := make([]models.KpiAnnualTargetResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

func (h *KpiPerformanceHandler) SetTarget(c *fiber.Ctx) error {
	var req models.KpiAnnualTargetRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}

	db := h.db.WithContext(c.UserContext())

	// REL-03: Verify the KPI dictionary row exists
	if !kpiExistsByCode(db, req.KpiCode, req.KpiType) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "KPI not found in dictionary")
	}

	// REL-05: Validate period against KPI's reporting frequency
	freq := services.GetKPIReportingFrequency(db, req.KpiCode, req.KpiType)
	periodType := "annual"
	if freq == models.KPIFrequencyMonthly {
		periodType = "month"
	} else if freq == models.KPIFrequencyQuarterly {
		periodType = "quarter"
	} else if freq == models.KPIFrequencySemiAnnual {
		periodType = "semi_annual"
	}
	if err := services.ValidatePeriod(freq, periodType, req.PeriodCode); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	// REL-02: Validate metric exists and belongs to this KPI
	if req.MetricID != nil && *req.MetricID != "" {
		var metric models.KpiMetric
		if err := db.Where("id = ?", *req.MetricID).First(&metric).Error; err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "Metric not found")
		}
	}

	// Check for duplicate: same KPI + metric + period. The comment always
	// said "+ metric" but the query never actually filtered on metric_id, so
	// creating a second metric's target for a KPI+period that already had
	// one for a DIFFERENT metric was incorrectly rejected as a duplicate.
	dupQuery := db.Where("kpi_code = ? AND kpi_type = ? AND target_year = ? AND period_code = ?",
		req.KpiCode, req.KpiType, req.TargetYear, req.PeriodCode)
	if req.MetricID != nil && *req.MetricID != "" {
		dupQuery = dupQuery.Where("metric_id = ?", *req.MetricID)
	} else {
		dupQuery = dupQuery.Where("metric_id IS NULL")
	}
	dupErr := dupQuery.First(&models.KpiAnnualTarget{}).Error
	if dupErr == nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest,
			fmt.Sprintf("a target for %s already exists for this metric in period %s/%d", req.KpiCode, req.PeriodCode, req.TargetYear))
	} else if dupErr != gorm.ErrRecordNotFound {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	item := req.ToModel(db)

	if err := db.Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}

	// Reload with preloads
	var reloaded models.KpiAnnualTarget
	db.Preload("Metric").Preload("ApprovedBy").First(&reloaded, item.ID)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "create",
		Module:      "kpi",
		ResourceID:  reloaded.ID.String(),
		Description: fmt.Sprintf("Set target for %s period %s/%d", reloaded.KpiCode, reloaded.PeriodCode, reloaded.TargetYear),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusCreated, "", reloaded.ToResponse())
}

// UpdateTarget edits an existing target in place. There was previously no
// update endpoint at all — the frontend's "Edit" action called the same
// create endpoint as "Add Target", which always inserted a brand new row
// instead of changing the one being edited.
func (h *KpiPerformanceHandler) UpdateTarget(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.KpiAnnualTargetRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}

	db := h.db.WithContext(c.UserContext())

	var existing models.KpiAnnualTarget
	if err := db.First(&existing, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	// Only draft/returned/rejected targets can be edited — matches the
	// frontend's Actions column, which only ever offers "Edit" for those
	// statuses (approved/submitted/superseded/locked targets are locked).
	if existing.TargetStatus != "draft" && existing.TargetStatus != "returned" && existing.TargetStatus != "rejected" && !isAdminOverride(c) {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "Only draft targets can be edited.")
	}

	if !kpiExistsByCode(db, req.KpiCode, req.KpiType) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "KPI not found in dictionary")
	}

	freq := services.GetKPIReportingFrequency(db, req.KpiCode, req.KpiType)
	periodType := "annual"
	if freq == models.KPIFrequencyMonthly {
		periodType = "month"
	} else if freq == models.KPIFrequencyQuarterly {
		periodType = "quarter"
	} else if freq == models.KPIFrequencySemiAnnual {
		periodType = "semi_annual"
	}
	if err := services.ValidatePeriod(freq, periodType, req.PeriodCode); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	if req.MetricID != nil && *req.MetricID != "" {
		var metric models.KpiMetric
		if err := db.Where("id = ?", *req.MetricID).First(&metric).Error; err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "Metric not found")
		}
	}

	// Duplicate check, excluding this record itself — editing a target's own
	// unchanged period/metric must not trip over its own existing row. Also
	// scoped to metric_id (previously omitted, same bug as SetTarget above),
	// so editing one metric's target didn't get blocked by a different
	// metric's target for the same KPI+period.
	dupQuery := db.Model(&models.KpiAnnualTarget{}).Where(
		"kpi_code = ? AND kpi_type = ? AND target_year = ? AND period_code = ? AND id != ?",
		req.KpiCode, req.KpiType, req.TargetYear, req.PeriodCode, id,
	)
	if req.MetricID != nil && *req.MetricID != "" {
		dupQuery = dupQuery.Where("metric_id = ?", *req.MetricID)
	} else {
		dupQuery = dupQuery.Where("metric_id IS NULL")
	}
	var dupCount int64
	dupQuery.Count(&dupCount)
	if dupCount > 0 {
		return utils.ErrorResponse(c, fiber.StatusBadRequest,
			fmt.Sprintf("a target for %s already exists for this metric in period %s/%d", req.KpiCode, req.PeriodCode, req.TargetYear))
	}

	updated := req.ToModel(db)
	updated.ID = existing.ID
	updated.CreatedAt = existing.CreatedAt
	updated.ApprovedByID = existing.ApprovedByID
	updated.ApprovedAt = existing.ApprovedAt
	if err := db.Save(updated).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update"))
	}

	var reloaded models.KpiAnnualTarget
	db.Preload("Metric").Preload("ApprovedBy").First(&reloaded, id)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "update",
		Module:      "kpi",
		ResourceID:  reloaded.ID.String(),
		Description: fmt.Sprintf("Updated target for %s period %s/%d", reloaded.KpiCode, reloaded.PeriodCode, reloaded.TargetYear),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", reloaded.ToResponse())
}

type targetTransitionRequest struct {
	Action string `json:"action" validate:"required,oneof=approve reject return"`
}

// TransitionTarget moves a target between submitted/approved/rejected/draft.
// There was previously no way at all to approve a target — target_status
// could only ever be set to whatever the create/update form sent directly
// (draft/submitted), with no gated approval step, even though the
// "targets:approve" permission already existed and the Targets page's own
// mockup assumes an Approved state is reachable.
func (h *KpiPerformanceHandler) TransitionTarget(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req targetTransitionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}

	db := h.db.WithContext(c.UserContext())

	var target models.KpiAnnualTarget
	if err := db.First(&target, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	if target.TargetStatus != "submitted" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest,
			fmt.Sprintf("Only submitted targets can be %sd — this target is %s", req.Action, target.TargetStatus))
	}

	var newStatus string
	switch req.Action {
	case "approve":
		newStatus = "approved"
	case "reject":
		newStatus = "rejected"
	case "return":
		newStatus = "returned"
	}

	updates := map[string]interface{}{"target_status": newStatus}
	if req.Action == "approve" {
		userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
		now := time.Now()
		updates["approved_by_id"] = userID
		updates["approved_at"] = now
	}
	if err := db.Model(&target).Updates(updates).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update"))
	}

	var reloaded models.KpiAnnualTarget
	db.Preload("Metric").Preload("ApprovedBy").First(&reloaded, id)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "transition",
		Module:      "kpi",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("%s target %s (status -> %s)", req.Action, reloaded.KpiCode, newStatus),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", reloaded.ToResponse())
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

	db := h.db.WithContext(c.UserContext())
	polarity := services.GetKPIPolarity(db, req.KpiCode, req.KpiType)

	// REL-03: Verify KPI dictionary row exists
	if !kpiExistsByCode(db, req.KpiCode, req.KpiType) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "KPI not found in dictionary")
	}

	// REL-18: Verify an approved target exists for this KPI + period
	var target models.KpiAnnualTarget
	if err := db.Where("kpi_code = ? AND kpi_type = ? AND target_year = ? AND period_code = ? AND target_status = 'approved'",
		req.KpiCode, req.KpiType, req.Year, req.PeriodKey,
	).First(&target).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest,
			fmt.Sprintf("No approved target found for %s in period %s/%d", req.KpiCode, req.PeriodKey, req.Year))
	}

	periodType := req.PeriodType
	if periodType == "" {
		periodType = "quarter"
	}
	periodKey := req.PeriodKey
	if periodKey == "" {
		periodKey = fmt.Sprintf("%d-Q%d", req.Year, req.Quarter)
	}
	freq := services.GetKPIReportingFrequency(db, req.KpiCode, req.KpiType)
	if err := services.ValidatePeriod(freq, periodType, periodKey); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	item := &models.KpiPerformance{
		KpiCode:          req.KpiCode,
		KpiType:          req.KpiType,
		Year:             req.Year,
		Quarter:          req.Quarter,
		PeriodType:       periodType,
		PeriodKey:        periodKey,
		TargetID:         &target.ID,
		Target:           req.Target,
		Actual:           req.Actual,
		AchievementPct:   services.CalculateAchievement(req.Actual, req.Target, polarity),
		TrendDescription: req.TrendDescription,
		Justification:    req.Justification,
		CorrectiveAction: req.CorrectiveAction,
		Status:           models.KPIPerfStatusDraft,
	}

	if err := db.Create(item).Error; err != nil {
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
	result, err := h.workflowSvc.TransitionKpiPerformance(userContext(c), id, &req, userID)
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
		KpiCode         string  `json:"kpi_code"`
		Zone            string  `json:"zone"`
		BenchmarkEntity string  `json:"benchmark_entity"`
		AvgInternal     float64 `json:"avg_internal"`
		AvgBenchmark    float64 `json:"avg_benchmark"`
		AvgVariance     float64 `json:"avg_variance"`
		TotalRecords    int64   `json:"total_records"`
	}

	var results []BenchSummary
	if err := h.db.WithContext(c.UserContext()).Model(&models.KpiBenchmark{}).
		Select("kpi_code, zone, benchmark_entity, " +
			"AVG(internal_achievement) as avg_internal, " +
			"AVG(benchmark_achievement) as avg_benchmark, " +
			"AVG(internal_achievement - benchmark_achievement) as avg_variance, " +
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
		DimensionName  string  `json:"dimension_name"`
		SegmentName    string  `json:"segment_name"`
		AvgAchievement float64 `json:"avg_achievement"`
		AvgTarget      float64 `json:"avg_target"`
		AvgPct         float64 `json:"avg_pct"`
		TotalRecords   int64   `json:"total_records"`
	}

	var results []SegSummary
	if err := h.db.WithContext(c.UserContext()).Model(&models.KpiSegmentation{}).
		Select("dimension_name, segment_name, " +
			"AVG(achievement) as avg_achievement, " +
			"AVG(target) as avg_target, " +
			"CASE WHEN AVG(target) > 0 THEN (AVG(achievement) / AVG(target)) * 100 ELSE 0 END as avg_pct, " +
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

// isAdminOverride reports whether the current user may bypass the
// approved-entry immutability rules ("For the Admin role, when Admin edits
// an approved entry, the system permits an update").
func isAdminOverride(c *fiber.Ctx) bool {
	user, ok := c.Locals(constants.ContextKeys.User).(*models.User)
	if !ok || user == nil {
		return false
	}
	return user.IsSuperAdmin || user.HasPermission("perf:override_lock")
}

// ─── Update / Delete (approval-lock enforced) ──────────────────────────────────

func (h *KpiPerformanceHandler) UpdatePerformance(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.KpiPerformanceUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}

	db := h.db.WithContext(c.UserContext())
	var perf models.KpiPerformance
	if err := db.First(&perf, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	if perf.Status == models.KPIPerfStatusApproved && !isAdminOverride(c) {
		if req.Actual != perf.Actual {
			return utils.ErrorResponse(c, fiber.StatusForbidden, "Actual Value cannot be modified after approval.")
		}
		return utils.ErrorResponse(c, fiber.StatusForbidden, "Editing is not allowed. Approved KPI Entries cannot be modified.")
	}

	polarity := services.GetKPIPolarity(db, perf.KpiCode, perf.KpiType)
	updates := map[string]interface{}{
		"target":            req.Target,
		"actual":            req.Actual,
		"achievement_pct":   services.CalculateAchievement(req.Actual, req.Target, polarity),
		"trend_description": req.TrendDescription,
		"justification":     req.Justification,
		"corrective_action": req.CorrectiveAction,
	}
	if err := db.Model(&models.KpiPerformance{ID: id}).Updates(updates).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update"))
	}

	var reloaded models.KpiPerformance
	db.Preload("SubmittedBy").Preload("ApprovedBy").First(&reloaded, id)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "update",
		Module:      "kpi",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("Updated performance record for %s", reloaded.KpiCode),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", reloaded.ToResponse())
}

func (h *KpiPerformanceHandler) DeletePerformance(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	db := h.db.WithContext(c.UserContext())
	var perf models.KpiPerformance
	if err := db.First(&perf, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	if perf.Status == models.KPIPerfStatusApproved && !isAdminOverride(c) {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "This KPI Entry has already been approved and cannot be deleted.")
	}

	result := db.Delete(&models.KpiPerformance{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "delete",
		Module:      "kpi",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("Deleted performance record %s", id),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

// ─── Evidence (removal blocked once approved) ──────────────────────────────────

func (h *KpiPerformanceHandler) ListPerformanceEvidence(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var items []models.KpiPerformanceEvidence
	if err := h.db.WithContext(c.UserContext()).Preload("UploadedBy").
		Where("kpi_performance_id = ?", id).Order("created_at DESC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	resp := make([]models.KpiPerformanceEvidenceResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

func (h *KpiPerformanceHandler) CreatePerformanceEvidence(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.KpiPerformanceEvidenceRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}

	if err := h.db.WithContext(c.UserContext()).First(&models.KpiPerformance{}, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "the referenced performance record does not exist")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	item := &models.KpiPerformanceEvidence{
		KpiPerformanceID: id,
		Description:      req.Description,
		FileURL:          req.FileURL,
		UploadedByID:     userID,
	}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}

	var reloaded models.KpiPerformanceEvidence
	h.db.WithContext(c.UserContext()).Preload("UploadedBy").First(&reloaded, item.ID)

	return utils.SuccessResponse(c, fiber.StatusCreated, "", reloaded.ToResponse())
}

func (h *KpiPerformanceHandler) DeletePerformanceEvidence(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	evidenceID, err := uuid.Parse(c.Params("evidenceId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var perf models.KpiPerformance
	if err := h.db.WithContext(c.UserContext()).First(&perf, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	if perf.Status == models.KPIPerfStatusApproved && !isAdminOverride(c) {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "Evidence cannot be removed from an approved KPI Entry.")
	}

	result := h.db.WithContext(c.UserContext()).Where("kpi_performance_id = ?", id).Delete(&models.KpiPerformanceEvidence{}, evidenceID)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}
