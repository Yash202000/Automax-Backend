package handlers

import (
	"encoding/json"
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
	"gorm.io/gorm/clause"
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
	scope := kpiScope(c)
	q = scope.Apply(q, scope.CodeCondition("kpi_type", "kpi_code"))

	bad := func(param string) error {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, fmt.Sprintf("Invalid %s filter", param))
	}
	if kpiCode := c.Query("kpi_code"); kpiCode != "" {
		q = q.Where("kpi_code = ?", kpiCode)
	}
	if kpiType := c.Query("kpi_type"); kpiType != "" {
		if !isValidKpiType(kpiType) {
			return bad("kpi_type")
		}
		q = q.Where("kpi_type = ?", kpiType)
	}
	// year (legacy mirror column) and target_year filter the same value.
	for _, param := range []string{"year", "target_year"} {
		if yearStr := c.Query(param); yearStr != "" {
			year, err := strconv.Atoi(yearStr)
			if err != nil {
				return bad(param)
			}
			q = q.Where("target_year = ?", year)
		}
	}
	if metricID := c.Query("metric_id"); metricID != "" {
		id, err := uuid.Parse(metricID)
		if err != nil {
			return bad("metric_id")
		}
		q = q.Where("metric_id = ?", id)
	}
	if orgID := c.Query("organization_id"); orgID != "" {
		id, err := uuid.Parse(orgID)
		if err != nil {
			return bad("organization_id")
		}
		q = q.Where("organization_id = ?", id)
	}
	if periodCode := c.Query("period_code"); periodCode != "" {
		code, _, err := services.CanonicalTargetPeriodCode(periodCode, 0)
		if err != nil {
			return bad("period_code")
		}
		q = q.Where("period_code = ?", code)
	}
	if targetStatus := c.Query("target_status"); targetStatus != "" {
		q = q.Where("target_status = ?", targetStatus)
	}

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

// buildTarget validates a create/update request and turns it into a target.
// It canonicalises the period ("Q3", "2026-Q3" → "q3"; a Milestone's code and
// year come from its dates), then applies the rules shared with the import
// (services.ValidateKpiTargetPeriod, NormalizeKpiTargetThresholds, …):
// REL-01 (the metric belongs to this KPI), REL-05 (the period follows the
// metric's — else the KPI's — reporting frequency and validity window), the
// Target Type ⇒ period rule, and the 2020–2040 year range. Status, version
// and approval stamps are left to the caller.
func buildTarget(db *gorm.DB, req *models.KpiAnnualTargetRequest) (*models.KpiAnnualTarget, error) {
	if req.TargetType == "" {
		req.TargetType = models.KpiTargetTypePeriod
	}
	if n := len([]rune(req.TargetRationale)); n > models.KpiTargetRationaleMaxLen {
		return nil, fmt.Errorf("Target Rationale must be at most %d characters (got %d)", models.KpiTargetRationaleMaxLen, n)
	}

	if req.TargetType == models.KpiTargetTypeMilestone {
		// Milestone / Ad Hoc targets cover the date range the user picked;
		// the period code and year are derived from it so they can't disagree.
		start, errStart := models.ParseTargetDate("Period Start", req.PeriodStart)
		end, errEnd := models.ParseTargetDate("Period End", req.PeriodEnd)
		if errStart != nil {
			return nil, errStart
		}
		if errEnd != nil {
			return nil, errEnd
		}
		if start == nil || end == nil {
			return nil, fmt.Errorf("Period Start and Period End are required for a Milestone / Ad Hoc target")
		}
		if end.Before(*start) {
			return nil, fmt.Errorf("Period End cannot be before Period Start")
		}
		if err := services.ValidateMilestoneYears(*start, *end); err != nil {
			return nil, err
		}
		req.PeriodCode = services.CustomPeriodCode(*start, *end)
		req.TargetYear = start.Year()
		req.PeriodStart, req.PeriodEnd = start.Format("2006-01-02"), end.Format("2006-01-02")
	} else {
		if services.IsCustomPeriodCode(req.PeriodCode) {
			return nil, fmt.Errorf("Custom date ranges are only allowed for Milestone / Ad Hoc targets")
		}
		code, year, err := services.CanonicalTargetPeriodCode(req.PeriodCode, req.TargetYear)
		if err != nil {
			return nil, err
		}
		if err := services.ValidateKpiTargetYear(year); err != nil {
			return nil, err
		}
		req.PeriodCode, req.TargetYear = code, year
		// Period dates always follow the period itself.
		req.PeriodStart, req.PeriodEnd = "", ""
		if start, end, ok := services.PeriodDateRange(year, code); ok {
			req.PeriodStart, req.PeriodEnd = start.Format("2006-01-02"), end.Format("2006-01-02")
		}
	}

	item, err := req.ToModel(db)
	if err != nil {
		return nil, err
	}

	var metric *models.KpiMetric
	if item.MetricID != nil {
		var m models.KpiMetric
		if err := db.Where("id = ?", *item.MetricID).First(&m).Error; err != nil {
			return nil, fmt.Errorf("Metric not found")
		}
		if m.KpiType != req.KpiType || m.KpiID != kpiIDByCode(db, req.KpiCode, req.KpiType) {
			return nil, fmt.Errorf("Metric %q does not belong to KPI %s", m.Name, req.KpiCode)
		}
		metric = &m
	}
	if item.OrganizationID != nil {
		var count int64
		if err := db.Model(&models.KpiOrganization{}).Where("id = ?", *item.OrganizationID).Count(&count).Error; err != nil || count == 0 {
			return nil, fmt.Errorf("Organization not found")
		}
	}

	freq := services.GetKPIReportingFrequency(db, req.KpiCode, req.KpiType)
	if err := services.ValidateKpiTargetPeriod(metric, freq, item.TargetType, item.TargetYear, item.PeriodCode); err != nil {
		return nil, err
	}

	direction := services.KpiTargetDirection(db, req.KpiCode, req.KpiType, metric)
	if req.TargetValue == nil && services.KpiTargetValueRequired(direction) {
		return nil, fmt.Errorf("Target Value is required")
	}
	if err := services.NormalizeKpiTargetThresholds(item, direction); err != nil {
		return nil, err
	}
	if item.SegmentationValues, err = services.CanonicalKpiTargetSegmentation(req.SegmentationValues); err != nil {
		return nil, err
	}
	return item, nil
}

// requestedTargetStatus is the status a create/update may produce: draft
// (default) or submitted. approved/returned/rejected are only reachable
// through TransitionTarget and superseded through a revision's approval;
// the validate tag already rejects them, this is the belt to those braces.
func requestedTargetStatus(req *models.KpiAnnualTargetRequest, fallback string) (string, error) {
	if req.TargetStatus == nil || *req.TargetStatus == "" {
		return fallback, nil
	}
	switch *req.TargetStatus {
	case models.KpiTargetStatusDraft, models.KpiTargetStatusSubmitted:
		return *req.TargetStatus, nil
	}
	return "", fmt.Errorf("Target status %q cannot be set directly — save as draft or submit, then use approve / return / reject", *req.TargetStatus)
}

// requestKeys lists the top-level JSON keys of the request body, to tell an
// omitted field from one sent empty.
func requestKeys(c *fiber.Ctx) map[string]bool {
	var raw map[string]json.RawMessage
	keys := map[string]bool{}
	if err := json.Unmarshal(c.Body(), &raw); err == nil {
		for k := range raw {
			keys[k] = true
		}
	}
	return keys
}

// customTargetOverlap returns an existing Milestone / Ad Hoc target of the
// same KPI, metric, organization and segment whose date range overlaps
// item's, if any. Rejected and superseded targets don't count. Regular period
// targets aren't compared: a custom range is matched by its own period code,
// not by month.
func customTargetOverlap(db *gorm.DB, item *models.KpiAnnualTarget, excludeIDs ...uuid.UUID) (*models.KpiAnnualTarget, error) {
	if !services.IsCustomPeriodCode(item.PeriodCode) || item.PeriodStart == nil || item.PeriodEnd == nil {
		return nil, nil
	}
	excludeIDs = append(excludeIDs, uuid.Nil)
	q := db.Where("kpi_code = ? AND kpi_type = ? AND id NOT IN ? AND period_code LIKE ? AND target_status NOT IN ?",
		item.KpiCode, item.KpiType, excludeIDs, services.CustomPeriodPrefix+"%", []string{"rejected", "superseded"}).
		Where("period_start <= ? AND period_end >= ?", *item.PeriodEnd, *item.PeriodStart)
	if item.MetricID != nil {
		q = q.Where("metric_id = ?", *item.MetricID)
	} else {
		q = q.Where("metric_id IS NULL")
	}
	if item.OrganizationID != nil {
		q = q.Where("organization_id = ?", *item.OrganizationID)
	} else {
		q = q.Where("organization_id IS NULL")
	}
	if len(item.SegmentationValues) > 0 {
		q = q.Where("segmentation_values = ?::jsonb", string(item.SegmentationValues))
	} else {
		q = q.Where("(segmentation_values IS NULL OR segmentation_values = 'null'::jsonb OR segmentation_values = '[]'::jsonb)")
	}
	var other models.KpiAnnualTarget
	if err := q.First(&other).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &other, nil
}

// openTargetRevisionStatuses are the statuses of a revision that is still in
// progress; an approved target can have at most one at a time.
var openTargetRevisionStatuses = []string{"draft", "submitted", "returned"}

// loadSupersededTarget loads the approved original a revision supersedes.
func loadSupersededTarget(db *gorm.DB, originalID uuid.UUID) (*models.KpiAnnualTarget, error) {
	var original models.KpiAnnualTarget
	if err := db.First(&original, originalID).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("The target being superseded was not found")
		}
		return nil, err
	}
	if original.TargetStatus != models.KpiTargetStatusApproved {
		return nil, fmt.Errorf("Only an approved target can be superseded — this target is %s", original.TargetStatus)
	}
	return &original, nil
}

// inheritRevisionScope fills a revision request's omitted organization and
// segment from the original (the form has no inputs for them).
func inheritRevisionScope(req *models.KpiAnnualTargetRequest, present map[string]bool, original *models.KpiAnnualTarget) {
	if !present["organization_id"] && original.OrganizationID != nil {
		org := original.OrganizationID.String()
		req.OrganizationID = &org
	}
	if !present["segmentation_values"] {
		req.SegmentationValues = original.Segmentation()
	}
}

// checkRevision validates a revision against the original it supersedes: it
// must cover the same KPI, metric, period, organization and segment (only its
// values may change), and the original may have only one open revision.
// selfID is the revision's own ID when it is being edited (uuid.Nil on create).
func checkRevision(db *gorm.DB, item, original *models.KpiAnnualTarget, selfID uuid.UUID) error {
	sameID := func(a, b *uuid.UUID) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }
	if item.KpiCode != original.KpiCode || item.KpiType != original.KpiType || !sameID(item.MetricID, original.MetricID) ||
		!sameID(item.OrganizationID, original.OrganizationID) || item.TargetYear != original.TargetYear ||
		!strings.EqualFold(item.PeriodCode, original.PeriodCode) ||
		services.KpiTargetSegmentationKey(item.SegmentationValues) != services.KpiTargetSegmentationKey(original.SegmentationValues) {
		return fmt.Errorf("A revision must keep the KPI, Metric, Organization, Segment and Period of the target it supersedes")
	}
	var open models.KpiAnnualTarget
	err := db.Where("supersedes_entry_id = ? AND id <> ? AND target_status IN ?", original.ID, selfID, openTargetRevisionStatuses).
		First(&open).Error
	if err == nil {
		return fmt.Errorf("This target already has a revision in progress (version %d, status '%s') — edit or submit that one instead", open.Version, open.TargetStatus)
	} else if err != gorm.ErrRecordNotFound {
		return err
	}
	return nil
}

// nextTargetVersion returns the next free Version for the target's
// Metric+Year+Period+Org+Segment key, so a revision never collides with the
// original (or an earlier, rejected revision) under the unique index.
func nextTargetVersion(db *gorm.DB, t *models.KpiAnnualTarget) (int, error) {
	var maxVersion int
	q := services.KpiTargetScope(db.Model(&models.KpiAnnualTarget{}), t)
	if err := q.Select("COALESCE(MAX(version), 0)").Scan(&maxVersion).Error; err != nil {
		return 0, err
	}
	return maxVersion + 1, nil
}

// existingTargetConflict returns another target with the same identity and
// version (BR-01), excluding excludeID.
func existingTargetConflict(db *gorm.DB, t *models.KpiAnnualTarget, excludeID uuid.UUID) (*models.KpiAnnualTarget, error) {
	var other models.KpiAnnualTarget
	err := services.KpiTargetScope(db.Model(&models.KpiAnnualTarget{}), t).
		Where("version = ? AND id <> ?", t.Version, excludeID).First(&other).Error
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &other, nil
}

func targetDuplicateMessage(existing *models.KpiAnnualTarget) string {
	return fmt.Sprintf("A target with status '%s' already exists for %s in period %s — edit or supersede it instead of creating a new one",
		existing.TargetStatus, existing.KpiCode, services.FormatPeriodLabel(existing.PeriodCode, existing.TargetYear))
}

func customOverlapMessage(other *models.KpiAnnualTarget) string {
	return fmt.Sprintf("This date range overlaps an existing Milestone / Ad Hoc target (%s, status '%s')",
		services.FormatPeriodLabel(other.PeriodCode, other.TargetYear), other.TargetStatus)
}

// kpiIDByCode returns the dictionary row ID for a KPI code, or uuid.Nil.
func kpiIDByCode(db *gorm.DB, kpiCode, kpiType string) uuid.UUID {
	var row struct{ ID uuid.UUID }
	switch kpiType {
	case models.KPITypeOperational:
		db.Model(&models.OperationalKPI{}).Where("code = ?", kpiCode).Select("id").Scan(&row)
	case models.KPITypeAward:
		db.Model(&models.AwardKPI{}).Where("code = ?", kpiCode).Select("id").Scan(&row)
	default:
		db.Model(&models.StrategicKPI{}).Where("code = ?", kpiCode).Select("id").Scan(&row)
	}
	return row.ID
}

func (h *KpiPerformanceHandler) SetTarget(c *fiber.Ctx) error {
	var req models.KpiAnnualTargetRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	status, err := requestedTargetStatus(&req, models.KpiTargetStatusDraft)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	db := h.db.WithContext(c.UserContext())

	// REL-03: Verify the KPI dictionary row exists
	if !kpiExistsByCode(db, req.KpiCode, req.KpiType) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "KPI not found in dictionary")
	}

	// Supersede: a new version of an approved target, linked back to it.
	var superseded *models.KpiAnnualTarget
	if req.SupersedesTargetID != nil && *req.SupersedesTargetID != "" {
		originalID, err := uuid.Parse(*req.SupersedesTargetID)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
		}
		if superseded, err = loadSupersededTarget(db, originalID); err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
		}
		inheritRevisionScope(&req, requestKeys(c), superseded)
	}

	item, err := buildTarget(db, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	item.TargetStatus = status
	// The server assigns the version; a client-sent one is ignored.
	item.Version = 1
	excludeIDs := []uuid.UUID{}
	if superseded != nil {
		if err := checkRevision(db, item, superseded, uuid.Nil); err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
		}
		version, err := nextTargetVersion(db, item)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to check for an existing target")
		}
		item.Version = version
		item.SupersedesEntryID = &superseded.ID
		excludeIDs = append(excludeIDs, superseded.ID)
	}
	if other, err := customTargetOverlap(db, item, excludeIDs...); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to check for an existing target")
	} else if other != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, customOverlapMessage(other))
	}

	// Duplicate: same KPI + metric + year + period + organization + segment
	// + version (BR-01). The unique index is the final guard for requests
	// racing past this check (mapped to 409 below).
	if existing, err := existingTargetConflict(db, item, uuid.Nil); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to check for an existing target")
	} else if existing != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, targetDuplicateMessage(existing))
	}

	if err := db.Create(item).Error; err != nil {
		if services.IsUniqueViolation(err) {
			return utils.ErrorResponse(c, fiber.StatusConflict,
				"A target for this KPI, metric, period, organization and segment was just created by another request — reload and edit it instead")
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create target")
	}

	// Reload with preloads
	var reloaded models.KpiAnnualTarget
	db.Preload("Metric").Preload("ApprovedBy").First(&reloaded, item.ID)

	description := fmt.Sprintf("Set target for %s period %s/%d", reloaded.KpiCode, reloaded.PeriodCode, reloaded.TargetYear)
	if superseded != nil {
		description = fmt.Sprintf("Created version %d of target for %s period %s/%d, superseding %s",
			reloaded.Version, reloaded.KpiCode, reloaded.PeriodCode, reloaded.TargetYear, superseded.ID)
	}
	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "create",
		Module:      "kpi",
		ResourceID:  reloaded.ID.String(),
		Description: description,
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusCreated, "", reloaded.ToResponse())
}

// UpdateTarget edits an existing target in place. Fields the body omits keep
// their stored values (so a partial payload such as the list's quick Submit
// never wipes the organization, segment or effective dates). Only draft,
// returned and rejected targets are editable; a submitted target only through
// the admin override (perf:override_lock / Super Admin), which keeps its
// status and is audited. Approved, locked and superseded targets are
// immutable for everyone (BR-08) — a correction is a revision.
func (h *KpiPerformanceHandler) UpdateTarget(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.KpiAnnualTargetRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	db := h.db.WithContext(c.UserContext())

	var existing models.KpiAnnualTarget
	if err := db.First(&existing, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	adminOverride := false
	switch {
	case models.IsKpiTargetImmutableStatus(existing.TargetStatus):
		return utils.ErrorResponse(c, fiber.StatusForbidden,
			fmt.Sprintf("This target is %s and cannot be changed — create a revision (Supersede) to correct it.", existing.TargetStatus))
	case models.IsKpiTargetEditableStatus(existing.TargetStatus):
	case isAdminOverride(c):
		adminOverride = true
	default:
		return utils.ErrorResponse(c, fiber.StatusForbidden,
			fmt.Sprintf("This target is %s and waiting for approval — it can't be edited until it is returned or rejected.", existing.TargetStatus))
	}

	present := requestKeys(c)
	req.FillOmitted(&existing, present)
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	// The route's access check was made against this target's KPI; moving
	// the target to another KPI would bypass it.
	if req.KpiCode != existing.KpiCode || req.KpiType != existing.KpiType {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "A target's KPI cannot be changed — create a target for the other KPI instead")
	}
	// An omitted status keeps a draft/submitted target's status; a returned
	// or rejected one goes back to draft once edited.
	fallback := models.KpiTargetStatusDraft
	if existing.TargetStatus == models.KpiTargetStatusSubmitted {
		fallback = existing.TargetStatus
	}
	status, err := requestedTargetStatus(&req, fallback)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	updated, err := buildTarget(db, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	updated.TargetStatus = status
	// The version and revision link never change on edit.
	updated.Version = existing.Version
	updated.SupersedesEntryID = existing.SupersedesEntryID
	excludeIDs := []uuid.UUID{existing.ID}
	if existing.SupersedesEntryID != nil {
		original, err := loadSupersededTarget(db, *existing.SupersedesEntryID)
		if err == nil {
			err = checkRevision(db, updated, original, existing.ID)
		}
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
		}
		excludeIDs = append(excludeIDs, *existing.SupersedesEntryID)
	}
	if other, err := customTargetOverlap(db, updated, excludeIDs...); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to check for an existing target")
	} else if other != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, customOverlapMessage(other))
	}
	if conflicting, err := existingTargetConflict(db, updated, id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to check for an existing target")
	} else if conflicting != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, targetDuplicateMessage(conflicting))
	}

	updated.ID = existing.ID
	updated.CreatedAt = existing.CreatedAt
	// Only an approved target carries approval stamps, and an edit never
	// produces one.
	updated.ApprovedByID = nil
	updated.ApprovedAt = nil
	if err := db.Save(updated).Error; err != nil {
		if services.IsUniqueViolation(err) {
			return utils.ErrorResponse(c, fiber.StatusConflict, "Another target already exists for this KPI, metric, period, organization and segment")
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update"))
	}

	var reloaded models.KpiAnnualTarget
	db.Preload("Metric").Preload("ApprovedBy").First(&reloaded, id)

	description := fmt.Sprintf("Updated target for %s period %s/%d", reloaded.KpiCode, reloaded.PeriodCode, reloaded.TargetYear)
	if adminOverride {
		description = fmt.Sprintf("Admin override: updated %s target for %s period %s/%d (status kept %s)",
			existing.TargetStatus, reloaded.KpiCode, reloaded.PeriodCode, reloaded.TargetYear, reloaded.TargetStatus)
	}
	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "update",
		Module:      "kpi",
		ResourceID:  reloaded.ID.String(),
		Description: description,
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", reloaded.ToResponse())
}

type targetTransitionRequest struct {
	Action string `json:"action"`
}

// targetTransitions maps each review action to the status it produces and
// its past-tense label for messages.
var targetTransitions = map[string]struct{ status, past string }{
	"approve": {models.KpiTargetStatusApproved, "approved"},
	"reject":  {models.KpiTargetStatusRejected, "rejected"},
	"return":  {models.KpiTargetStatusReturned, "returned"},
}

// TransitionTarget moves a submitted target to approved, returned or
// rejected — the only way to reach those statuses. Approving a revision
// supersedes the original in the same transaction; approving is refused
// while another approved target exists for the same KPI, metric, period,
// organization and segment (exactly one approved target per key).
func (h *KpiPerformanceHandler) TransitionTarget(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req targetTransitionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	transition, ok := targetTransitions[req.Action]
	if !ok {
		return utils.ErrorResponse(c, fiber.StatusBadRequest,
			fmt.Sprintf("Unknown action %q — use approve, return or reject (submit by saving the target with status submitted)", req.Action))
	}

	// The route only requires targets:approve OR targets:reject (either
	// grants access to this endpoint at all, since the action isn't known
	// until the body is parsed) — reject specifically requires targets:reject
	// and approve/return require targets:approve, so a user with only one of
	// the two can't perform the other action just by changing the request.
	requiredPerm := "targets:approve"
	if req.Action == "reject" {
		requiredPerm = "targets:reject"
	}
	if !hasTargetPermission(c, requiredPerm) {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "Insufficient permissions")
	}

	db := h.db.WithContext(c.UserContext())

	var target models.KpiAnnualTarget
	if err := db.First(&target, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	errNotSubmitted := fmt.Errorf("not submitted")
	errOriginalNotApproved := fmt.Errorf("the superseded target is no longer approved")
	var conflict *models.KpiAnnualTarget
	err = db.Transaction(func(tx *gorm.DB) error {
		// Lock the row so two approvers can't act on it at once.
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&target, id).Error; err != nil {
			return err
		}
		if target.TargetStatus != models.KpiTargetStatusSubmitted {
			return errNotSubmitted
		}
		updates := map[string]interface{}{"target_status": transition.status}
		if req.Action == "approve" {
			// Approving a revision retires the version it supersedes, so
			// exactly one approved target exists for the period. The
			// original row is only re-labelled Superseded; its values stay
			// for audit.
			if target.SupersedesEntryID != nil {
				res := tx.Model(&models.KpiAnnualTarget{}).
					Where("id = ? AND target_status = ?", *target.SupersedesEntryID, models.KpiTargetStatusApproved).
					Update("target_status", models.KpiTargetStatusSuperseded)
				if res.Error != nil {
					return res.Error
				}
				if res.RowsAffected == 0 {
					return errOriginalNotApproved
				}
			}
			var other models.KpiAnnualTarget
			err := services.KpiTargetScope(tx.Model(&models.KpiAnnualTarget{}), &target).
				Where("target_status = ? AND id <> ?", models.KpiTargetStatusApproved, target.ID).First(&other).Error
			if err == nil {
				conflict = &other
				return errOriginalNotApproved
			} else if err != gorm.ErrRecordNotFound {
				return err
			}
			updates["approved_by_id"] = c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
			updates["approved_at"] = time.Now()
		}
		return tx.Model(&target).Updates(updates).Error
	})
	switch {
	case err == errNotSubmitted:
		return utils.ErrorResponse(c, fiber.StatusBadRequest,
			fmt.Sprintf("Only submitted targets can be %s — this target is %s", transition.past, target.TargetStatus))
	case conflict != nil:
		return utils.ErrorResponse(c, fiber.StatusConflict,
			fmt.Sprintf("Another approved target (version %d) already exists for this KPI, metric and period — submit a revision of it (Supersede) instead", conflict.Version))
	case err == errOriginalNotApproved:
		return utils.ErrorResponse(c, fiber.StatusConflict,
			"The target this revision supersedes is no longer approved, so the revision can't be approved")
	case services.IsUniqueViolation(err):
		return utils.ErrorResponse(c, fiber.StatusConflict, "Another approved target already exists for this KPI, metric and period")
	case err != nil:
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update"))
	}

	var reloaded models.KpiAnnualTarget
	db.Preload("Metric").Preload("ApprovedBy").First(&reloaded, id)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "transition",
		Module:      "kpi",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("%s target %s (status -> %s)", req.Action, reloaded.KpiCode, transition.status),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", reloaded.ToResponse())
}

// DeleteTarget removes a draft, returned or rejected target (a submitted one
// only through the admin override). Approved, locked and superseded targets
// are the audit trail of what was in force and are never deleted — deleting
// one would also orphan its revisions.
func (h *KpiPerformanceHandler) DeleteTarget(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	db := h.db.WithContext(c.UserContext())
	var target models.KpiAnnualTarget
	if err := db.First(&target, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	adminOverride := false
	switch {
	case models.IsKpiTargetImmutableStatus(target.TargetStatus):
		return utils.ErrorResponse(c, fiber.StatusForbidden,
			fmt.Sprintf("This target is %s and cannot be deleted — only draft, returned or rejected targets can be.", target.TargetStatus))
	case models.IsKpiTargetEditableStatus(target.TargetStatus):
	case isAdminOverride(c):
		adminOverride = true
	default:
		return utils.ErrorResponse(c, fiber.StatusForbidden,
			fmt.Sprintf("This target is %s and waiting for approval — it can't be deleted until it is returned or rejected.", target.TargetStatus))
	}

	// Guard the status in the predicate so a target approved in the
	// meantime is never deleted.
	allowed := []string{models.KpiTargetStatusDraft, models.KpiTargetStatusReturned, models.KpiTargetStatusRejected}
	if adminOverride {
		allowed = append(allowed, models.KpiTargetStatusSubmitted)
	}
	result := db.Where("target_status IN ?", allowed).Delete(&models.KpiAnnualTarget{}, id)
	if result.Error != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to delete target")
	}
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusConflict, "The target changed status meanwhile and can no longer be deleted — reload it")
	}

	description := fmt.Sprintf("Deleted %s target %s", target.TargetStatus, id)
	if adminOverride {
		description = "Admin override: " + description
	}
	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "delete",
		Module:      "kpi",
		ResourceID:  id.String(),
		Description: description,
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
	scope := kpiScope(c)
	q = scope.Apply(q, scope.CodeCondition("kpi_type", "kpi_code"))

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
	// "2026-Q2" and "q2" are the same period; its year must be the record's.
	periodCode, _, err := services.CanonicalTargetPeriodCode(periodKey, req.Year)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	if !periodWithinQuarter(periodCode, req.Quarter) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest,
			fmt.Sprintf("Period %s does not fall in quarter Q%d", services.FormatPeriodLabel(periodCode, req.Year), req.Quarter))
	}

	// REL-18: the record is measured against the approved target for this
	// KPI + period — its value, not one supplied by the client.
	target, err := approvedLegacyTarget(db, req.KpiCode, req.KpiType, req.Year, periodCode)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	// One live record per KPI + period (a rejected one can be re-entered).
	if dup, err := legacyPerformanceExists(db, req.KpiCode, req.KpiType, req.Year, periodCode); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	} else if dup != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest,
			fmt.Sprintf("A performance record (status %s) already exists for %s in %s — edit it instead",
				dup.Status, req.KpiCode, services.FormatPeriodLabel(periodCode, req.Year)))
	}

	item := &models.KpiPerformance{
		KpiCode:          req.KpiCode,
		KpiType:          req.KpiType,
		Year:             req.Year,
		Quarter:          req.Quarter,
		PeriodType:       periodType,
		PeriodKey:        periodKey,
		TargetID:         &target.ID,
		Target:           target.TargetValue,
		Actual:           req.Actual,
		AchievementPct:   services.CalculateAchievement(req.Actual, target.TargetValue, polarity),
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
	scope := kpiScope(c)
	q = scope.Apply(q, scope.CodeCondition("kpi_type", "kpi_code"))

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
		Municipality1:        req.Municipality1,
		Municipality1Label:   req.Municipality1Label,
		Municipality2:        req.Municipality2,
		Municipality2Label:   req.Municipality2Label,
		Municipality3:        req.Municipality3,
		Municipality3Label:   req.Municipality3Label,
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

func (h *KpiPerformanceHandler) UpdateBenchmark(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var existing models.KpiBenchmark
	if err := h.db.WithContext(c.UserContext()).First(&existing, id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "internal_error"))
	}

	var req models.KpiBenchmarkRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}

	existing.KpiCode = req.KpiCode
	existing.KpiType = req.KpiType
	existing.Year = req.Year
	existing.Quarter = req.Quarter
	existing.Zone = req.Zone
	existing.BenchmarkEntity = req.BenchmarkEntity
	existing.InternalAchievement = req.InternalAchievement
	existing.BenchmarkAchievement = req.BenchmarkAchievement
	existing.Municipality1 = req.Municipality1
	existing.Municipality1Label = req.Municipality1Label
	existing.Municipality2 = req.Municipality2
	existing.Municipality2Label = req.Municipality2Label
	existing.Municipality3 = req.Municipality3
	existing.Municipality3Label = req.Municipality3Label
	existing.Notes = req.Notes

	if req.DepartmentID != nil {
		if *req.DepartmentID == "" {
			existing.DepartmentID = nil
		} else {
			pid, err := uuid.Parse(*req.DepartmentID)
			if err != nil {
				return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_department_id"))
			}
			existing.DepartmentID = &pid
		}
	}

	if err := h.db.WithContext(c.UserContext()).Save(&existing).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update"))
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "update",
		Module:      "kpi",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("Updated benchmark for %s - %s", existing.KpiCode, existing.BenchmarkEntity),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", existing.ToBenchmarkResponse())
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
	scope := kpiScope(c)
	if err := scope.Apply(h.db.WithContext(c.UserContext()).Model(&models.KpiBenchmark{}), scope.CodeCondition("kpi_type", "kpi_code")).
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
	scope := kpiScope(c)
	q = scope.Apply(q, scope.CodeCondition("kpi_type", "kpi_code"))

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
	scope := kpiScope(c)
	if err := scope.Apply(h.db.WithContext(c.UserContext()).Model(&models.KpiSegmentation{}), scope.CodeCondition("kpi_type", "kpi_code")).
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

// hasTargetPermission checks a specific permission for the current request's
// user — used where the route-level RequirePermission only narrows down to
// "one of several" permissions (e.g. approve OR reject) and the handler
// needs to know exactly which one applies once it knows the actual action.
func hasTargetPermission(c *fiber.Ctx, permission string) bool {
	user, ok := c.Locals(constants.ContextKeys.User).(*models.User)
	if !ok || user == nil {
		return false
	}
	return user.IsSuperAdmin || user.HasPermission(permission)
}

// ─── Update / Delete (approval-lock enforced) ──────────────────────────────────

// legacyPerformanceEditable reports whether a legacy performance record may
// still be changed by its submitter: only while it is a draft or after it
// was sent back (returned / rejected). Submitted, under review, approved and
// published records are locked (the admin override still applies).
func legacyPerformanceEditable(status string) bool {
	switch status {
	case models.KPIPerfStatusDraft, models.KPIPerfStatusRejected, "returned":
		return true
	}
	return false
}

func legacyPerformanceLockedMessage(status, verb string) string {
	return fmt.Sprintf("This KPI Entry is %s and cannot be %s.", strings.ReplaceAll(status, "_", " "), verb)
}

// periodWithinQuarter reports whether a canonical period code and a quarter
// (1–4) agree: the period lies inside the quarter (a month or the quarter
// itself) or contains it (a half-year or the year). Custom ranges pass.
func periodWithinQuarter(periodCode string, quarter int) bool {
	start, end, ok := services.PeriodDateRange(2000, periodCode)
	if !ok || services.IsCustomPeriodCode(periodCode) {
		return true
	}
	qs, qe := (quarter-1)*3+1, quarter*3
	sm, em := int(start.Month()), int(end.Month())
	return (sm >= qs && em <= qe) || (sm <= qs && em >= qe)
}

// approvedLegacyTarget finds the approved target a legacy (KPI-level)
// performance record is measured against: the KPI-level target for the
// period (no metric, organization or segment), else the period's only
// approved target. Several metric-level targets and no KPI-level one is
// ambiguous.
func approvedLegacyTarget(db *gorm.DB, kpiCode, kpiType string, year int, periodCode string) (*models.KpiAnnualTarget, error) {
	var targets []models.KpiAnnualTarget
	if err := db.Where("kpi_code = ? AND kpi_type = ? AND target_year = ? AND period_code = ? AND target_status = ?",
		kpiCode, kpiType, year, periodCode, models.KpiTargetStatusApproved).
		Order("version DESC").Find(&targets).Error; err != nil {
		return nil, err
	}
	label := services.FormatPeriodLabel(periodCode, year)
	for i := range targets {
		t := &targets[i]
		if t.MetricID == nil && t.OrganizationID == nil && services.KpiTargetSegmentationKey(t.SegmentationValues) == "" {
			return t, nil
		}
	}
	switch len(targets) {
	case 0:
		return nil, fmt.Errorf("No approved target found for %s in %s", kpiCode, label)
	case 1:
		return &targets[0], nil
	}
	return nil, fmt.Errorf("%s has %d approved targets in %s (per metric, organization or segment) and no KPI-level one — set an approved KPI-level target for the period",
		kpiCode, len(targets), label)
}

// legacyPerformanceExists returns a non-rejected legacy performance record
// for the same KPI and period, matching "2026-Q2" and "q2" as one period.
func legacyPerformanceExists(db *gorm.DB, kpiCode, kpiType string, year int, periodCode string) (*models.KpiPerformance, error) {
	var rows []models.KpiPerformance
	if err := db.Where("kpi_code = ? AND kpi_type = ? AND year = ? AND status <> ?",
		kpiCode, kpiType, year, models.KPIPerfStatusRejected).Find(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		key := rows[i].PeriodKey
		if key == "" {
			key = fmt.Sprintf("%d-Q%d", rows[i].Year, rows[i].Quarter)
		}
		if code, _, err := services.CanonicalTargetPeriodCode(key, 0); err == nil && code == periodCode {
			return &rows[i], nil
		}
	}
	return nil, nil
}

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

	if !legacyPerformanceEditable(perf.Status) && !isAdminOverride(c) {
		if perf.Status == models.KPIPerfStatusApproved && req.Actual != perf.Actual {
			return utils.ErrorResponse(c, fiber.StatusForbidden, "Actual Value cannot be modified after approval.")
		}
		return utils.ErrorResponse(c, fiber.StatusForbidden, legacyPerformanceLockedMessage(perf.Status, "modified"))
	}

	// The target is the linked approved target's value, never the client's.
	targetValue := perf.Target
	if perf.TargetID != nil {
		var target models.KpiAnnualTarget
		if err := db.Select("target_value").First(&target, *perf.TargetID).Error; err == nil {
			targetValue = target.TargetValue
		}
	}
	polarity := services.GetKPIPolarity(db, perf.KpiCode, perf.KpiType)
	updates := map[string]interface{}{
		"target":            targetValue,
		"actual":            req.Actual,
		"achievement_pct":   services.CalculateAchievement(req.Actual, targetValue, polarity),
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

	if !legacyPerformanceEditable(perf.Status) && !isAdminOverride(c) {
		return utils.ErrorResponse(c, fiber.StatusForbidden, legacyPerformanceLockedMessage(perf.Status, "deleted"))
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

	if !legacyPerformanceEditable(perf.Status) && !isAdminOverride(c) {
		return utils.ErrorResponse(c, fiber.StatusForbidden,
			fmt.Sprintf("Evidence cannot be removed from a KPI Entry that is %s.", strings.ReplaceAll(perf.Status, "_", " ")))
	}

	result := h.db.WithContext(c.UserContext()).Where("kpi_performance_id = ?", id).Delete(&models.KpiPerformanceEvidence{}, evidenceID)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}
