package handlers

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
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

func getKPITargetRequirement(db *gorm.DB, kpiType string, id uuid.UUID) (string, error) {
	var val string
	var err error
	switch kpiType {
	case models.KPITypeOperational:
		err = db.Model(&models.OperationalKPI{}).Select("target_requirement").Where("id = ?", id).Scan(&val).Error
	case models.KPITypeAward:
		err = db.Model(&models.AwardKPI{}).Select("target_requirement").Where("id = ?", id).Scan(&val).Error
	default:
		err = db.Model(&models.StrategicKPI{}).Select("target_requirement").Where("id = ?", id).Scan(&val).Error
	}
	if err != nil {
		return "", err
	}
	if val == "" {
		return "mandatory", nil
	}
	return val, nil
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
	if err := q.Order("reporting_year DESC, entry_date DESC NULLS LAST, created_at DESC").Find(&items).Error; err != nil {
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
	scope := kpiScope(c)
	q = scope.Apply(q, scope.TypedIDCondition("kpi_type", "kpi_id"))

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
	// status filters the workflow status (draft/submitted/approved/...);
	// a performance status ("Not Calculable", "Warning", ...) is still
	// accepted there for older callers, and has its own performance_status
	// param.
	if status := c.Query("status"); status != "" {
		if isEntryWorkflowStatus(status) {
			q = q.Where("status = ?", status)
		} else {
			q = q.Where("performance_status = ?", status)
		}
	}
	if perfStatus := c.Query("performance_status"); perfStatus != "" {
		q = q.Where("performance_status = ?", perfStatus)
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
	if limit > maxEntryPageSize {
		limit = maxEntryPageSize
	}
	offset := (page - 1) * limit

	var items []models.KpiEntry
	if err := q.Order("reporting_year DESC, entry_date DESC NULLS LAST, created_at DESC").Offset(offset).Limit(limit).Find(&items).Error; err != nil {
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

// maxEntryPageSize caps the /kpi/entries page size.
const maxEntryPageSize = 200

// isEntryWorkflowStatus reports whether s is a KpiEntry workflow status.
func isEntryWorkflowStatus(s string) bool {
	switch s {
	case models.KpiEntryStatusDraft, models.KpiEntryStatusSubmitted, models.KpiEntryStatusApproved,
		models.KpiEntryStatusRejected, models.KpiEntryStatusSuperseded, models.KpiEntryStatusReversed,
		models.KpiEntryStatusReversal:
		return true
	}
	return false
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

	// REL-08: Validate metric exists, belongs to the KPI in the URL and is
	// Active — an entry recorded under another KPI's path would otherwise,
	// once approved, overwrite that other KPI's metric value.
	mid, _ := uuid.Parse(req.MetricID)
	var metric models.KpiMetric
	if err := db.Where("id = ?", mid).First(&metric).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Metric not found")
	}
	if metric.KpiID != kpiID || metric.KpiType != kpiType {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Metric does not belong to this KPI")
	}
	if metric.MetricStatus != "Active" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest,
			fmt.Sprintf("Metric %q is %s — entries can only be recorded for Active metrics", metric.Name, metric.MetricStatus))
	}

	// REL-17: Block Formula metric entries in Phase 1
	if metric.CalculationType == "Formula" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Formula (Phase 2) metrics are not supported yet")
	}

	kpiCode, kpiErr := getKPICode(db, kpiType, kpiID)
	if kpiErr != nil || kpiCode == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "KPI not found")
	}

	// The period must be one of the metric's reporting periods (for optional
	// KPIs too — no target lookup vouches for it there) and already over.
	if err := services.ValidateEntryPeriod(&metric, services.GetKPIReportingFrequency(db, kpiCode, kpiType),
		req.ReportingYear, req.PeriodCode, time.Now()); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	// Multiple approved Entries are allowed for the same metric+period — the
	// period's Actual is an aggregate across all of them (see
	// services.AggregateMetricPeriod), so there is deliberately no
	// duplicate-entry rejection here anymore.

	// REL-07: required inputs (and a non-zero denominator for ratio types)
	// are enforced by ComputeActualValue inside ToModel.
	item, err := req.ToModel(db, kpiID, kpiType, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	// A Milestone / Ad Hoc period carries its own dates in the code.
	if start, end, ok := services.ParseCustomPeriod(req.PeriodCode); ok {
		item.PeriodStart, item.PeriodEnd = &start, &end
	} else if start, end, ok := services.PeriodDateRange(req.ReportingYear, req.PeriodCode); ok && item.PeriodStart == nil && item.PeriodEnd == nil {
		item.PeriodStart, item.PeriodEnd = &start, &end
	}
	if err := validateEntryDate(c, item); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	// Look up the KPI's target_requirement setting
	targetReq, trErr := getKPITargetRequirement(db, kpiType, kpiID)
	if trErr != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	// REL-09: Require a matching approved (or locked) target unless target
	// is optional.
	target, err := services.FindUsableTarget(db, kpiCode, kpiType, mid, req.ReportingYear, req.PeriodCode, item.OrganizationID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) && targetReq == "optional" {
			// Optional KPIs may proceed without an approved target.
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.ErrorResponse(c, fiber.StatusBadRequest,
				fmt.Sprintf("No approved target exists for this metric in period %s — create and approve a target first.", services.FormatPeriodLabel(req.PeriodCode, req.ReportingYear)))
		} else {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
		}
	} else {
		item.TargetID = &target.ID
		item.TargetValueSnapshot = &target.TargetValue
		item.ThresholdModeSnapshot = target.ThresholdMode
	}

	services.ComputeEntryAchievement(db, kpiCode, item)

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
		Description: fmt.Sprintf("Created KPI entry %s", kpiEntryLabel(db, item)),
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

// validateEntryDate checks that an entry has an Entry Date inside its
// reporting period (the period's own dates, or the entry's stored
// Period Start/End when the code isn't a known period) and not in the future.
func validateEntryDate(c *fiber.Ctx, e *models.KpiEntry) error {
	if e.EntryDate == nil {
		return errors.New(i18n.T(c.UserContext(), "kpi_entry_date_required"))
	}
	if e.EntryDate.Format("2006-01-02") > time.Now().Format("2006-01-02") {
		return errors.New("Entry Date cannot be in the future")
	}
	start, end, ok := services.PeriodDateRange(e.ReportingYear, e.PeriodCode)
	if !ok {
		if e.PeriodStart == nil || e.PeriodEnd == nil {
			return nil
		}
		start, end = *e.PeriodStart, *e.PeriodEnd
	}
	day := e.EntryDate.Format("2006-01-02")
	if day < start.Format("2006-01-02") || day > end.Format("2006-01-02") {
		return fmt.Errorf("%s (%s – %s)", i18n.T(c.UserContext(), "kpi_entry_date_outside_period"),
			start.Format("02 Jan 2006"), end.Format("02 Jan 2006"))
	}
	return nil
}

// UpdateEntry edits the data fields of a draft entry. Once an entry has
// moved past draft it is locked to non-admins, mirroring the approval-lock
// rule already enforced on KpiPerformance — except a rejected entry, which
// its submitter may edit: that reopens it as a draft (workflow back to its
// initial state) so it can be resubmitted, the way a returned/rejected
// target becomes editable again.
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
	req.MarkExplicitNulls(c.Body())

	db := h.db.WithContext(c.UserContext())
	var entry models.KpiEntry
	if err := db.First(&entry, entryID).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	if entryIsFinal(entry.Status) {
		return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(c.UserContext(), "kpi_entry_approved_locked"))
	}
	reopen := entry.Status == models.KpiEntryStatusRejected
	if entry.Status != models.KpiEntryStatusDraft && !isAdminOverride(c) && !(reopen && isEntrySubmitter(c, &entry)) {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "Only draft entries (or your own rejected entries) can be edited. Submit a correction request instead.")
	}

	if err := req.ApplyTo(&entry, false); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	if err := validateEntryDate(c, &entry); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	kpiCode, _ := getKPICode(db, entry.KpiType, entry.KpiID)
	services.ComputeEntryAchievement(db, kpiCode, &entry)

	err = db.Transaction(func(tx *gorm.DB) error {
		if reopen {
			if err := reopenRejectedEntry(tx, &entry); err != nil {
				return err
			}
		}
		return tx.Save(&entry).Error
	})
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update"))
	}

	var reloaded models.KpiEntry
	db.Preload("Metric").Preload("SubmittedBy").Preload("ApprovedBy").First(&reloaded, entry.ID)

	description := fmt.Sprintf("Updated KPI entry %s", kpiEntryLabel(db, &entry))
	if reopen {
		description = fmt.Sprintf("Reopened rejected KPI entry %s as draft", kpiEntryLabel(db, &entry))
	}
	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "update",
		Module:      "kpi",
		ResourceID:  entry.ID.String(),
		Description: description,
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", reloaded)
}

// isEntrySubmitter reports whether the current user created/submitted e.
func isEntrySubmitter(c *fiber.Ctx, e *models.KpiEntry) bool {
	userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	return ok && e.SubmittedByID != nil && *e.SubmittedByID == userID
}

// reopenRejectedEntry moves a rejected entry back to draft: its workflow
// instance returns to the workflow's initial state (keeping the instance, so
// the rejection stays in the entry's history) and it can be submitted again.
func reopenRejectedEntry(tx *gorm.DB, e *models.KpiEntry) error {
	e.Status = models.KpiEntryStatusDraft
	e.ApprovedByID = nil
	if e.WorkflowInstanceID == nil {
		return nil
	}
	var instance models.KpiWorkflowInstance
	if err := tx.First(&instance, *e.WorkflowInstanceID).Error; err != nil {
		return err
	}
	var initial models.WorkflowState
	if err := tx.Where("workflow_id = ? AND state_type = ? AND is_active = ?", instance.WorkflowID, "initial", true).
		First(&initial).Error; err != nil {
		return err
	}
	return tx.Model(&instance).Update("current_state_id", initial.ID).Error
}

// DeleteEntry removes a draft entry — or a rejected one, by its submitter.
// Locked once submitted/approved, same as UpdateEntry, unless the caller has
// the admin override permission.
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

	if entryIsFinal(entry.Status) {
		return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(c.UserContext(), "kpi_entry_approved_locked"))
	}
	ownRejected := entry.Status == models.KpiEntryStatusRejected && isEntrySubmitter(c, &entry)
	if entry.Status != models.KpiEntryStatusDraft && !ownRejected && !isAdminOverride(c) {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "Only draft entries (or your own rejected entries) can be deleted.")
	}

	result := db.Delete(&models.KpiEntry{}, entryID)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "delete",
		Module:      "kpi",
		ResourceID:  entryID.String(),
		Description: fmt.Sprintf("Deleted KPI entry %s", kpiEntryLabel(db, &entry)),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

// entryIsFinal reports whether an entry is approved or already retired by a
// correction. Such entries are the record of what was approved and can never
// be edited or deleted directly — not even with the admin override — only
// corrected through CreateCorrection.
func entryIsFinal(status string) bool {
	switch status {
	case models.KpiEntryStatusApproved, models.KpiEntryStatusSuperseded,
		models.KpiEntryStatusReversed, models.KpiEntryStatusReversal:
		return true
	}
	return false
}

// CreateCorrection starts an adjustment or reversal of an approved entry. The
// correction is a new draft entry (next version, linked to the original) that
// goes through the normal submit/approve workflow; the original stays as it
// is until the correction is approved (see TransitionKpiEntry).
// POST /kpi/entries/:entryId/corrections
func (h *KpiEntryHandler) CreateCorrection(c *fiber.Ctx) error {
	entryID, err := uuid.Parse(c.Params("entryId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var req models.KpiEntryCorrectionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	db := h.db.WithContext(c.UserContext())

	var original models.KpiEntry
	if err := db.First(&original, entryID).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	if original.Status != models.KpiEntryStatusApproved {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "kpi_entry_correction_not_approved"))
	}
	var open int64
	db.Model(&models.KpiEntry{}).
		Where("supersedes_entry_id = ? AND status IN ?", original.ID, []string{models.KpiEntryStatusDraft, models.KpiEntryStatusSubmitted}).
		Count(&open)
	if open > 0 {
		return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "kpi_entry_correction_open"))
	}

	// An adjustment starts from the original's values and only replaces the
	// ones it sends; missing/invalid resulting inputs (e.g. a zero
	// denominator) are rejected by ComputeActualValue.
	req.MarkExplicitNulls(c.Body())
	item, err := original.NewCorrection(&req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	// An adjustment keeps the original's Entry Date unless it sends a new
	// one; a reversal carries the original's as-is.
	if req.CorrectionType == models.KpiEntryCorrectionAdjustment {
		if err := validateEntryDate(c, item); err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
		}
		kpiCode, _ := getKPICode(db, item.KpiType, item.KpiID)
		services.ComputeEntryAchievement(db, kpiCode, item)
	}
	if err := db.Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "internal_server_error"))
	}

	var reloaded models.KpiEntry
	db.Preload("Metric").Preload("SubmittedBy").Preload("ApprovedBy").First(&reloaded, item.ID)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "create",
		Module:      "kpi",
		ResourceID:  original.KpiID.String(),
		Description: fmt.Sprintf("Created %s (v%d) of KPI entry %s", req.CorrectionType, item.EntryVersion, kpiEntryLabel(db, &original)),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusCreated, "", reloaded)
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

	var existing int64
	h.db.WithContext(c.UserContext()).Model(&models.KpiEntry{}).Where("id = ?", entryID).Count(&existing)
	if existing == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
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
		Description: fmt.Sprintf("Transitioned KPI entry %s (status -> %s)", kpiEntryLabel(h.db.WithContext(c.UserContext()), entry), entry.Status),
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
	var entry models.KpiEntry
	if err := h.db.WithContext(c.UserContext()).Select("kpi_type, kpi_id").First(&entry, entryID).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	transitions, err := h.workflowSvc.GetAvailableKpiEntryTransitions(c.UserContext(), entryID, userID)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}
	if transitions, err = allowedTransitions(c, entry.KpiType, entry.KpiID, transitions); err != nil {
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
