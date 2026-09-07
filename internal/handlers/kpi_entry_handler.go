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

type KpiEntryHandler struct {
	db           *gorm.DB
	validator    *validator.Validate
	actionLogSvc services.ActionLogService
	workflowSvc  *services.KpiWorkflowService
}

func NewKpiEntryHandler(db *gorm.DB, actionLogSvc services.ActionLogService, workflowSvc *services.KpiWorkflowService) *KpiEntryHandler {
	return &KpiEntryHandler{
		db:           db,
		validator:    validator.New(),
		actionLogSvc: actionLogSvc,
		workflowSvc:  workflowSvc,
	}
}

// getKPICode looks up the KPI code for a given (kpi_id, kpi_type) pair.
func getKPICode(db *gorm.DB, kpiType string, id uuid.UUID) (string, error) {
	switch kpiType {
	case models.KPITypeOperational:
		var k models.OperationalKPI
		if err := db.Select("code").Where("id = ?", id).First(&k).Error; err != nil {
			return "", err
		}
		return k.Code, nil
	case models.KPITypeAward:
		var k models.AwardKPI
		if err := db.Select("code").Where("id = ?", id).First(&k).Error; err != nil {
			return "", err
		}
		return k.Code, nil
	default:
		var k models.StrategicKPI
		if err := db.Select("code").Where("id = ?", id).First(&k).Error; err != nil {
			return "", err
		}
		return k.Code, nil
	}
}

// ListEntries returns all entries for a KPI dictionary row, optionally filtered by metric_id.
// GET /kpi/:type/:id/entries?metric_id=...
func (h *KpiEntryHandler) ListEntries(c *fiber.Ctx) error {
	kpiType := c.Params("type")
	if !isValidKpiType(kpiType) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	kpiID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	q := h.db.WithContext(c.UserContext()).Model(&models.KpiEntry{}).
		Preload("Metric").Preload("SubmittedBy").Preload("ApprovedBy").
		Where("kpi_id = ? AND kpi_type = ?", kpiID, kpiType)

	if metricID := c.Query("metric_id"); metricID != "" {
		q = q.Where("metric_id = ?", metricID)
	}

	var items []models.KpiEntry
	if err := q.Order("reporting_year DESC, created_at DESC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", items)
}

// ListAllEntries returns all KPI entries across all KPIs with optional filters.
// GET /kpi/entries?kpi_code=&metric_name=&reporting_year=&period_code=&status=&search=&page=&limit=
func (h *KpiEntryHandler) ListAllEntries(c *fiber.Ctx) error {
	db := h.db.WithContext(c.UserContext())
	q := db.Model(&models.KpiEntry{}).
		Preload("Metric").Preload("SubmittedBy").Preload("ApprovedBy")

	if code := c.Query("kpi_code"); code != "" {
		like := "%" + code + "%"
		subOp := db.Where(
			`(kpi_type = ? AND kpi_id IN (SELECT id FROM strategic_kpis WHERE code ILIKE ?))`, "strategic", like,
		).Or(
			`(kpi_type = ? AND kpi_id IN (SELECT id FROM operational_kpis WHERE code ILIKE ?))`, "operational", like,
		).Or(
			`(kpi_type = ? AND kpi_id IN (SELECT id FROM award_kpis WHERE code ILIKE ?))`, "award", like,
		)
		q = q.Where(subOp)
	}
	if metricName := c.Query("metric_name"); metricName != "" {
		q = q.Where("metric_id IN (SELECT id FROM kpi_metrics WHERE name ILIKE ?)", "%"+metricName+"%")
	}
	if yearStr := c.Query("reporting_year"); yearStr != "" {
		if year, err := strconv.Atoi(yearStr); err == nil {
			q = q.Where("reporting_year = ?", year)
		}
	}
	if periodCode := c.Query("period_code"); periodCode != "" {
		q = q.Where("period_code ILIKE ?", "%"+periodCode+"%")
	}
	if status := c.Query("status"); status != "" {
		q = q.Where("performance_status = ?", status)
	}
	if search := c.Query("search"); search != "" {
		like := "%" + search + "%"
		q = q.Where(
			"(period_code ILIKE ? OR source_reference ILIKE ? OR data_quality_notes ILIKE ?)",
			like, like, like,
		)
	}

	var total int64
	q.Count(&total)

	page, _ := parseIntQuery(c.Query("page"), 1)
	limit, _ := parseIntQuery(c.Query("limit"), 20)
	offset := (page - 1) * limit

	var items []models.KpiEntry
	if err := q.Order("reporting_year DESC, created_at DESC").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success":     true,
		"data":        items,
		"page":        page,
		"limit":       limit,
		"total_items": total,
	})
}

// parseIntQuery is a small helper to parse int query params with a default.
func parseIntQuery(val string, defaultVal int) (int, error) {
	if val == "" {
		return defaultVal, nil
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return defaultVal, err
	}
	if n < 1 {
		return defaultVal, nil
	}
	return n, nil
}

// CreateEntry creates a new KPI entry with full cross-sheet validation.
// POST /kpi/:type/:id/entries
func (h *KpiEntryHandler) CreateEntry(c *fiber.Ctx) error {
	kpiType := c.Params("type")
	if !isValidKpiType(kpiType) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	kpiID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.KpiEntryRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	db := h.db.WithContext(c.UserContext())

	// Metric entries may only be created while the KPI Workflow is in the
	// Active state (Draft/Reviewed/Approved/Closed all reject) — enforced
	// here rather than only in the UI, since the create endpoint is the only
	// insertion point for a KpiEntry row.
	kpiStatus, err := services.GetKpiActivationStatus(db, kpiType, kpiID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "KPI not found")
	}
	if kpiStatus != models.KPIStatusActive {
		return utils.ErrorResponse(c, fiber.StatusBadRequest,
			fmt.Sprintf("Metric entries can only be created while the KPI is Active (current status: %s)", kpiStatus))
	}

	// REL-08: Validate metric exists and get its configuration
	mid, _ := uuid.Parse(req.MetricID)
	var metric models.KpiMetric
	if err := db.Where("id = ?", mid).First(&metric).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Metric not found")
	}

	// REL-17: Block Formula metric entries in Phase 1
	if metric.CalculationType == "Formula" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Formula (Phase 2) metrics are not supported yet")
	}

	// Multiple approved Entries are allowed for the same metric+period — the
	// period's Actual is an aggregate across all of them (see
	// services.AggregateMetricPeriod), so there is deliberately no
	// duplicate-entry rejection here anymore.

	// REL-07: Validate denominator is not zero for ratio types
	if (metric.CalculationType == "Percentage - Ratio" || metric.CalculationType == "Ratio") &&
		req.DenominatorValue != nil && *req.DenominatorValue == 0 {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Denominator cannot be zero for ratio-type metrics")
	}

	item := req.ToModel(db, kpiID, kpiType, userID)

	// REL-09: Require a matching approved target for this exact KPI+metric+
	// period before an entry can even be created — recording actuals with no
	// target to measure them against just produces entries permanently stuck
	// at "Not Calculable", which is more confusing than helpful. Also now
	// scoped by metric_id (previously omitted here, same bug fixed on the
	// Targets endpoints), so a different metric's approved target for the
	// same KPI+period is never mistakenly attached to this one.
	kpiCode, kpiErr := getKPICode(db, kpiType, kpiID)
	if kpiErr != nil || kpiCode == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "KPI not found")
	}
	// BR-12 "scope consistency": the Entry's Organization must match the
	// Target scope it's measured against — compared NULL-safely since most
	// KPIs use neither Organization nor Segment scoping at all.
	var target models.KpiAnnualTarget
	targetQuery := db.Where("kpi_code = ? AND kpi_type = ? AND metric_id = ? AND target_year = ? AND period_code = ? AND target_status = 'approved'",
		kpiCode, kpiType, mid, req.ReportingYear, req.PeriodCode)
	if item.OrganizationID != nil {
		targetQuery = targetQuery.Where("organization_id = ?", *item.OrganizationID)
	} else {
		targetQuery = targetQuery.Where("organization_id IS NULL")
	}
	if err := targetQuery.Order("version DESC").First(&target).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest,
			fmt.Sprintf("No approved target exists for this metric in period %s/%d — create and approve a target first.", req.PeriodCode, req.ReportingYear))
	}
	item.TargetID = &target.ID
	item.TargetValueSnapshot = &target.TargetValue
	item.ThresholdModeSnapshot = target.ThresholdMode

	// Compute achievement/variance/status now that TargetValueSnapshot is set.
	item.ComputeAchievement()

	if err := db.Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}

	// Reload with preloads
	var reloaded models.KpiEntry
	db.
		Preload("Metric").Preload("SubmittedBy").Preload("ApprovedBy").
		First(&reloaded, item.ID)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "create",
		Module:      "kpi",
		ResourceID:  kpiID.String(),
		Description: fmt.Sprintf("Created KPI entry for metric %s period %s/%d", req.MetricID, req.PeriodCode, req.ReportingYear),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusCreated, "", reloaded)
}

// ListEntryEvidence returns evidence attached to a specific entry.
// GET /kpi/entries/:entryId/evidence
func (h *KpiEntryHandler) ListEntryEvidence(c *fiber.Ctx) error {
	entryID, err := uuid.Parse(c.Params("entryId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var items []models.KpiEntryEvidence
	if err := h.db.WithContext(c.UserContext()).
		Preload("UploadedBy").
		Where("entry_id = ?", entryID).
		Order("created_at DESC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", items)
}

// UpdateEntry edits the data fields of a still-draft entry. Once an entry has
// moved past draft (submitted/approved/rejected) it is locked to non-admins,
// mirroring the approval-lock rule already enforced on KpiPerformance.
// PUT /kpi/:type/:id/entries/:entryId
func (h *KpiEntryHandler) UpdateEntry(c *fiber.Ctx) error {
	entryID, err := uuid.Parse(c.Params("entryId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.KpiEntryUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	db := h.db.WithContext(c.UserContext())
	var entry models.KpiEntry
	if err := db.First(&entry, entryID).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	if entry.Status != models.KpiEntryStatusDraft && !isAdminOverride(c) {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "Only draft entries can be edited. Submit a correction request instead.")
	}

	req.ApplyTo(&entry)
	if err := db.Save(&entry).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update"))
	}

	var reloaded models.KpiEntry
	db.Preload("Metric").Preload("SubmittedBy").Preload("ApprovedBy").First(&reloaded, entry.ID)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "update",
		Module:      "kpi",
		ResourceID:  entry.ID.String(),
		Description: fmt.Sprintf("Updated KPI entry %s", entry.ID),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", reloaded)
}

// DeleteEntry removes a draft entry. Locked once submitted/approved, same as
// UpdateEntry, unless the caller has the admin override permission.
// DELETE /kpi/:type/:id/entries/:entryId
func (h *KpiEntryHandler) DeleteEntry(c *fiber.Ctx) error {
	entryID, err := uuid.Parse(c.Params("entryId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	db := h.db.WithContext(c.UserContext())
	var entry models.KpiEntry
	if err := db.First(&entry, entryID).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	if entry.Status != models.KpiEntryStatusDraft && !isAdminOverride(c) {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "Only draft entries can be deleted.")
	}

	result := db.Delete(&models.KpiEntry{}, entryID)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "delete",
		Module:      "kpi",
		ResourceID:  entryID.String(),
		Description: fmt.Sprintf("Deleted KPI entry %s", entryID),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

// TransitionEntry moves an entry through the kpi_entry workflow
// (submit/approve/reject/request_changes). POST /kpi/entries/:entryId/transition
func (h *KpiEntryHandler) TransitionEntry(c *fiber.Ctx) error {
	entryID, err := uuid.Parse(c.Params("entryId"))
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
	entry, err := h.workflowSvc.TransitionKpiEntry(c.UserContext(), entryID, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "transition",
		Module:      "kpi",
		ResourceID:  entryID.String(),
		Description: fmt.Sprintf("Transitioned KPI entry %s (status -> %s)", entryID, entry.Status),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", entry)
}

// GetAvailableEntryTransitions lists the transitions the current entry state
// allows. GET /kpi/entries/:entryId/transitions
func (h *KpiEntryHandler) GetAvailableEntryTransitions(c *fiber.Ctx) error {
	entryID, err := uuid.Parse(c.Params("entryId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	transitions, err := h.workflowSvc.GetAvailableKpiEntryTransitions(c.UserContext(), entryID, userID)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", transitions)
}

// GetEntryHistory returns the full transition audit trail for an entry.
// GET /kpi/entries/:entryId/history
func (h *KpiEntryHandler) GetEntryHistory(c *fiber.Ctx) error {
	entryID, err := uuid.Parse(c.Params("entryId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var entry models.KpiEntry
	if err := h.db.WithContext(c.UserContext()).First(&entry, entryID).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	if entry.WorkflowInstanceID == nil {
		return utils.SuccessResponse(c, fiber.StatusOK, "", []models.KpiWorkflowActionResponse{})
	}

	var actions []models.KpiWorkflowAction
	h.db.WithContext(c.UserContext()).
		Preload("Transition").Preload("FromState").Preload("ToState").Preload("PerformedBy").
		Where("workflow_instance_id = ?", *entry.WorkflowInstanceID).
		Order("performed_at ASC").Find(&actions)

	resp := make([]models.KpiWorkflowActionResponse, len(actions))
	for i, a := range actions {
		resp[i] = a.ToResponse()
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

// kpiExistsByCode checks whether a KPI dictionary row exists for the given code + type.
func kpiExistsByCode(db *gorm.DB, kpiCode, kpiType string) bool {
	var count int64
	switch kpiType {
	case models.KPITypeOperational:
		db.Model(&models.OperationalKPI{}).Where("code = ?", kpiCode).Count(&count)
	case models.KPITypeAward:
		db.Model(&models.AwardKPI{}).Where("code = ?", kpiCode).Count(&count)
	default:
		db.Model(&models.StrategicKPI{}).Where("code = ?", kpiCode).Count(&count)
	}
	return count > 0
}
