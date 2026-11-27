package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

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

type KpiDictionaryHandler struct {
	db           *gorm.DB
	validator    *validator.Validate
	actionLogSvc services.ActionLogService
	workflowSvc  *services.KpiWorkflowService
}

func NewKpiDictionaryHandler(db *gorm.DB, actionLogSvc services.ActionLogService, workflowSvc *services.KpiWorkflowService) *KpiDictionaryHandler {
	return &KpiDictionaryHandler{
		db:           db,
		validator:    validator.New(),
		actionLogSvc: actionLogSvc,
		workflowSvc:  workflowSvc,
	}
}

// replaceRelatedDepartments replaces the M2M RelatedDepartments association on a KPI model.
func (h *KpiDictionaryHandler) replaceRelatedDepartments(ctx context.Context, model interface{}, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return h.db.WithContext(ctx).Model(model).Association("RelatedDepartments").Clear()
	}
	var depts []models.Department
	if err := h.db.WithContext(ctx).Where("id IN ?", ids).Find(&depts).Error; err != nil {
		return err
	}
	return h.db.WithContext(ctx).Model(model).Association("RelatedDepartments").Replace(depts)
}

// kpiCode resolves a KPI dictionary row's code by (kpiType, id) — used to
// make activity-log Description strings read "KPI-XYZ" instead of a bare
// UUID (mirrors KpiEngagementHandler.kpiCodeAndName's dispatch pattern).
func (h *KpiDictionaryHandler) kpiCode(kpiType string, id uuid.UUID) string {
	var code string
	switch kpiType {
	case models.KPITypeOperational:
		h.db.Model(&models.OperationalKPI{}).Where("id = ?", id).Pluck("code", &code)
	case models.KPITypeAward:
		h.db.Model(&models.AwardKPI{}).Where("id = ?", id).Pluck("code", &code)
	default:
		h.db.Model(&models.StrategicKPI{}).Where("id = ?", id).Pluck("code", &code)
	}
	if code == "" {
		return id.String()
	}
	return code
}

// kpiListMaxLimit caps one page of a KPI dictionary list. A larger limit is
// clamped to it (not reset to the default), so a client asking for "all"
// gets the most a page can hold and pages through the rest.
const kpiListMaxLimit = 500

func kpiListLimit(raw int) int {
	if raw < 1 {
		return 10
	}
	return min(raw, kpiListMaxLimit)
}

// kpiDictCodeFormat is the KPI code format the KPI Dictionary import enforces
// (kpiCodeRe in services/kpi_dictionary_import.go), e.g. KPI-P1-01-01.
var kpiDictCodeFormat = regexp.MustCompile(`^[A-Z][A-Z0-9]*(-[A-Z0-9]+)+$`)

// kpiReqFields points at the fields every KPI request type shares, so one
// validator normalizes and checks all three.
type kpiReqFields struct {
	Code, NameEn, NameAr, OwnerType *string
	OperationalObjectiveID          uuid.UUID
	ProcessID                       uuid.UUID
	PillarID, DomainID              **uuid.UUID
	AwardKpiMeasureID               **uuid.UUID
	OwnerDeptID, OwnerOrgID         **uuid.UUID
	OwningAgencyID                  **uuid.UUID
	AwardSubCriterionID             **uuid.UUID
	RelatedDepartmentIDs            []uuid.UUID
}

// kpiValidationError is a request rejection with its HTTP status.
type kpiValidationError struct {
	status int
	msg    string
}

// validateKpiRequest trims and normalizes the shared KPI fields in place and
// checks them: code/name not blank, code in the import's format (only when it
// is new or changed, so legacy codes stay editable), code unique across all
// three KPI types (case-insensitive), every referenced master-data row
// exists and is not deleted, the process belongs to the objective, and the
// owner fields match the owner type. existingID is the KPI being updated
// (uuid.Nil on create) and currentCode its stored code.
func (h *KpiDictionaryHandler) validateKpiRequest(ctx context.Context, f kpiReqFields, existingID uuid.UUID, currentCode string) *kpiValidationError {
	bad := func(msg string, args ...interface{}) *kpiValidationError {
		return &kpiValidationError{status: fiber.StatusBadRequest, msg: fmt.Sprintf(msg, args...)}
	}
	db := h.db.WithContext(ctx)

	*f.Code = strings.ToUpper(strings.TrimSpace(*f.Code))
	*f.NameEn = strings.TrimSpace(*f.NameEn)
	*f.NameAr = strings.TrimSpace(*f.NameAr)
	if *f.Code == "" {
		return bad("KPI code is required")
	}
	if *f.NameEn == "" {
		return bad("KPI name (English) is required")
	}
	codeChanged := existingID == uuid.Nil || !strings.EqualFold(*f.Code, strings.TrimSpace(currentCode))
	if codeChanged && !kpiDictCodeFormat.MatchString(*f.Code) {
		return bad("KPI code %q is not in the expected format (e.g. KPI-P1-01-01, OP-P1-01-01)", *f.Code)
	}

	exists := func(model interface{}, id uuid.UUID) bool {
		var n int64
		db.Model(model).Where("id = ?", id).Count(&n)
		return n > 0
	}
	optional := func(p **uuid.UUID) (uuid.UUID, bool) {
		if p == nil || *p == nil || **p == uuid.Nil {
			if p != nil {
				*p = nil // treat the zero UUID as "not set"
			}
			return uuid.Nil, false
		}
		return **p, true
	}

	if !exists(&models.OperationalObjective{}, f.OperationalObjectiveID) {
		return bad("Objective %s was not found", f.OperationalObjectiveID)
	}
	var process models.Process
	if err := db.Select("id, operational_objective_id").First(&process, "id = ?", f.ProcessID).Error; err != nil {
		return bad("Process %s was not found", f.ProcessID)
	}
	if process.OperationalObjectiveID != f.OperationalObjectiveID {
		return bad("Process %s does not belong to objective %s", f.ProcessID, f.OperationalObjectiveID)
	}
	refs := []struct {
		ptr   **uuid.UUID
		model interface{}
		label string
	}{
		{f.PillarID, &models.Pillar{}, "Pillar"},
		{f.DomainID, &models.Domain{}, "Domain"},
		{f.AwardKpiMeasureID, &models.AwardKpiMeasure{}, "Award KPI measure"},
		{f.OwnerDeptID, &models.Department{}, "Owner department"},
		{f.OwnerOrgID, &models.KpiOrganization{}, "Owner organization"},
		{f.OwningAgencyID, &models.Department{}, "Owning agency"},
		{f.AwardSubCriterionID, &models.AwardSubCriterion{}, "Award sub-criterion"},
	}
	for _, r := range refs {
		if id, set := optional(r.ptr); set && !exists(r.model, id) {
			return bad("%s %s was not found", r.label, id)
		}
	}
	if len(f.RelatedDepartmentIDs) > 0 {
		unique := map[uuid.UUID]bool{}
		for _, id := range f.RelatedDepartmentIDs {
			unique[id] = true
		}
		ids := make([]uuid.UUID, 0, len(unique))
		for id := range unique {
			ids = append(ids, id)
		}
		var found []uuid.UUID
		db.Model(&models.Department{}).Where("id IN ?", ids).Pluck("id", &found)
		if len(found) != len(ids) {
			known := map[uuid.UUID]bool{}
			for _, id := range found {
				known[id] = true
			}
			var missing []string
			for _, id := range ids {
				if !known[id] {
					missing = append(missing, id.String())
				}
			}
			return bad("Related department(s) not found: %s", strings.Join(missing, ", "))
		}
	}

	if *f.OwnerType == "" {
		*f.OwnerType = models.KPIOwnerTypeInternal
	}
	if *f.OwnerType == models.KPIOwnerTypeExternal {
		if *f.OwnerOrgID == nil {
			return bad("An external KPI requires an owner organization (owner_org_id)")
		}
	} else {
		*f.OwnerOrgID = nil
	}

	var dup struct {
		Code    string
		KpiType string
	}
	db.Raw(`SELECT code, kpi_type FROM (
		SELECT id, code, 'strategic' AS kpi_type FROM strategic_kpis WHERE deleted_at IS NULL
		UNION ALL SELECT id, code, 'operational' FROM operational_kpis WHERE deleted_at IS NULL
		UNION ALL SELECT id, code, 'award' FROM award_kpis WHERE deleted_at IS NULL
	) k WHERE upper(trim(k.code)) = ? AND k.id <> ? LIMIT 1`, *f.Code, existingID).Scan(&dup)
	if dup.Code != "" {
		return &kpiValidationError{status: fiber.StatusConflict, msg: fmt.Sprintf("A KPI with code '%s' already exists (%s)", dup.Code, dup.KpiType)}
	}
	return nil
}

// kpiDictUniqueViolation reports whether err is a Postgres unique violation.
func kpiDictUniqueViolation(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "duplicate key"))
}

// currentKpiCode returns the stored code of a live KPI, or ok=false when it
// does not exist.
func (h *KpiDictionaryHandler) currentKpiCode(ctx context.Context, kpiType string, id uuid.UUID) (string, bool) {
	var row struct{ Code string }
	err := h.db.WithContext(ctx).Table(kpiDictTable(kpiType)).Select("code").
		Where("id = ? AND deleted_at IS NULL", id).Limit(1).Scan(&row).Error
	if err != nil {
		return "", false
	}
	var n int64
	h.db.WithContext(ctx).Table(kpiDictTable(kpiType)).Where("id = ? AND deleted_at IS NULL", id).Count(&n)
	return row.Code, n > 0
}

func kpiDictTable(kpiType string) string {
	switch kpiType {
	case models.KPITypeOperational:
		return "operational_kpis"
	case models.KPITypeAward:
		return "award_kpis"
	default:
		return "strategic_kpis"
	}
}

// checkKpiEditable answers 403 unless the user may edit the KPI's definition
// in its current KPI Workflow state (state Editable Roles; Closed and other
// terminal states are admin-only). Returns a non-nil error response when the
// caller must stop.
func (h *KpiDictionaryHandler) checkKpiEditable(c *fiber.Ctx, kpiType string, id uuid.UUID) (bool, error) {
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ok, state, err := h.workflowSvc.CanEditKpiDefinition(c.UserContext(), kpiType, id, userID)
	if errors.Is(err, services.ErrKpiDictionaryNotFound) {
		return false, utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	if err != nil {
		return false, utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "internal_server_error"))
	}
	if !ok {
		return false, utils.ErrorResponse(c, fiber.StatusForbidden,
			fmt.Sprintf("This KPI is %s; your role cannot change its definition in this workflow state", state))
	}
	return true, nil
}

// deleteKpi soft-deletes a KPI and cancels its workflow instance in one
// transaction, after the workflow-state edit check.
func (h *KpiDictionaryHandler) deleteKpi(c *fiber.Ctx, kpiType string, model interface{}) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	if ok, resp := h.checkKpiEditable(c, kpiType, id); !ok {
		return resp
	}
	code := h.kpiCode(kpiType, id)
	var affected int64
	err = h.db.WithContext(c.UserContext()).Transaction(func(tx *gorm.DB) error {
		result := tx.Delete(model, id)
		if result.Error != nil {
			return result.Error
		}
		affected = result.RowsAffected
		if affected == 0 {
			return nil
		}
		return services.CancelKpiDictionaryWorkflow(tx, id)
	})
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "internal_server_error"))
	}
	if affected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "delete",
		Module:      "kpi",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("Deleted %s KPI %s", kpiType, code),
		Status:      "success",
	})
	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

// prepareKpiUpdate runs the shared update checks: KPI exists (404),
// definition editable in its workflow state (403), request fields valid
// (400/409). Returns ok=false with the response already written otherwise.
func (h *KpiDictionaryHandler) prepareKpiUpdate(c *fiber.Ctx, kpiType string, id uuid.UUID, f kpiReqFields) (bool, error) {
	currentCode, found := h.currentKpiCode(c.UserContext(), kpiType, id)
	if !found {
		return false, utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	if ok, resp := h.checkKpiEditable(c, kpiType, id); !ok {
		return false, resp
	}
	if verr := h.validateKpiRequest(c.UserContext(), f, id, currentCode); verr != nil {
		return false, utils.ErrorResponse(c, verr.status, verr.msg)
	}
	return true, nil
}

// applyKpiUpdate writes the update map, answering 409 for a code taken
// concurrently, 500 for other errors and 404 when nothing matched.
func (h *KpiDictionaryHandler) applyKpiUpdate(c *fiber.Ctx, model interface{}, updates map[string]interface{}) (bool, error) {
	result := h.db.WithContext(c.UserContext()).Model(model).Updates(updates)
	if result.Error != nil {
		if kpiDictUniqueViolation(result.Error) {
			return false, utils.ErrorResponse(c, fiber.StatusConflict, fmt.Sprintf("A KPI with code '%v' already exists", updates["code"]))
		}
		return false, utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update"))
	}
	if result.RowsAffected == 0 {
		return false, utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	return true, nil
}

// ─── Strategic KPI ────────────────────────────────────────────────────────────

func (h *KpiDictionaryHandler) ListStrategic(c *fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))
	search := c.Query("search")
	goalIDStr := c.Query("goal_id")

	page = max(page, 1)
	limit = kpiListLimit(limit)

	q := h.db.WithContext(c.UserContext()).Model(&models.StrategicKPI{}).
		Preload("Pillar").Preload("Domain").Preload("AwardKpiMeasure").Preload("OwnerDept").Preload("OwnerOrg").Preload("OwningAgency").Preload("RelatedDepartments").Preload("Goal").Preload("Process")
	// Only the Super Admin and the KPI's assigned collaborators see it.
	scope := kpiScope(c)
	q = scope.Apply(q, scope.IDCondition("strategic", "strategic_kpis.id"))

	if search != "" {
		q = q.Where("name_en ILIKE ? OR name_ar ILIKE ? OR code ILIKE ?", "%"+search+"%", "%"+search+"%", "%"+search+"%")
	}
	if goalIDStr != "" {
		goalID, err := uuid.Parse(goalIDStr)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
		}
		q = q.Where("goal_id = ?", goalID)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	var items []models.StrategicKPI
	if err := q.Order("created_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	resp := make([]models.StrategicKPIResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}

	return utils.PaginatedSuccessResponse(c, resp, page, limit, total)
}

func (h *KpiDictionaryHandler) GetStrategic(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var item models.StrategicKPI
	if err := h.db.WithContext(c.UserContext()).
		Preload("Pillar").Preload("Domain").Preload("OwnerDept").Preload("OwnerOrg").Preload("OwningAgency").Preload("RelatedDepartments").Preload("Goal").Preload("Process").
		Preload("OperationalObjective").Preload("AwardSubCriterion.AwardCriterion").First(&item, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

// deriveGoalID resolves the Goal a KPI should be filed under from its
// selected Objective, rather than trusting a client-supplied goal_id — Goal
// is a read-only value on the KPI create/edit form, always shown as the
// selected Objective's own Goal.
func (h *KpiDictionaryHandler) deriveGoalID(ctx context.Context, objectiveID uuid.UUID) *uuid.UUID {
	var objective models.OperationalObjective
	if err := h.db.WithContext(ctx).Select("goal_id").Where("id = ?", objectiveID).First(&objective).Error; err != nil {
		return nil
	}
	return objective.GoalID
}

func (h *KpiDictionaryHandler) CreateStrategic(c *fiber.Ctx) error {
	var req models.StrategicKPIRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	if verr := h.validateKpiRequest(c.UserContext(), kpiReqFields{
		Code: &req.Code, NameEn: &req.NameEn, NameAr: &req.NameAr, OwnerType: &req.OwnerType,
		OperationalObjectiveID: req.OperationalObjectiveID, ProcessID: req.ProcessID,
		PillarID: &req.PillarID, DomainID: &req.DomainID, AwardKpiMeasureID: &req.AwardKpiMeasureID,
		OwnerDeptID: &req.OwnerDeptID, OwnerOrgID: &req.OwnerOrgID, OwningAgencyID: &req.OwningAgencyID,
		AwardSubCriterionID: &req.AwardSubCriterionID, RelatedDepartmentIDs: req.RelatedDepartmentIDs,
	}, uuid.Nil, ""); verr != nil {
		return utils.ErrorResponse(c, verr.status, verr.msg)
	}

	item := &models.StrategicKPI{
		Code:                   req.Code,
		NameEn:                 req.NameEn,
		NameAr:                 req.NameAr,
		GoalID:                 h.deriveGoalID(c.UserContext(), req.OperationalObjectiveID),
		OperationalObjectiveID: &req.OperationalObjectiveID,
		ProcessID:              &req.ProcessID,
		AwardSubCriterionID:    req.AwardSubCriterionID,
		Classification:         req.Classification,
		PillarID:               req.PillarID,
		DomainID:               req.DomainID,
		AwardKpiMeasureID:      req.AwardKpiMeasureID,
		OwnerType:              req.OwnerType,
		OwnerDeptID:            req.OwnerDeptID,
		OwnerOrgID:             req.OwnerOrgID,
		OwningAgencyID:         req.OwningAgencyID,
		Polarity:               req.Polarity,
		TargetRequirement:      req.TargetRequirement,
		DescriptionEn:          req.DescriptionEn,
		DescriptionAr:          req.DescriptionAr,
		Formula:                req.Formula,
		Baseline:               req.Baseline,
		UnitOfMeasure:          req.UnitOfMeasure,
		ReportingFrequency:     req.ReportingFrequency,
		Lifecycle:              req.Lifecycle,
		DataSource:             req.DataSource,
		SegmentationAxes:       req.SegmentationAxes,
		Notes:                  req.Notes,
		DocumentaFolderID:      req.DocumentaFolderID,
	}
	if item.Polarity == "" {
		item.Polarity = models.KPIPolarityAscending
	}
	if item.ActivationStatus == "" {
		item.ActivationStatus = models.KPIStatusDraft
	}
	if item.OwnerType == "" {
		item.OwnerType = models.KPIOwnerTypeInternal
	}
	if item.TargetRequirement == "" {
		item.TargetRequirement = "mandatory"
	}

	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		if kpiDictUniqueViolation(err) {
			return utils.ErrorResponse(c, fiber.StatusConflict, fmt.Sprintf("A KPI with code '%s' already exists", req.Code))
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create KPI")
	}
	if err := h.replaceRelatedDepartments(c.UserContext(), item, req.RelatedDepartmentIDs); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to set related departments")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	if err := h.workflowSvc.InitiateKpiDictionaryWorkflow(c.UserContext(), models.KPITypeStrategic, item.ID, userID); err != nil {
		log.Printf("[kpi_dictionary] CreateStrategic: InitiateKpiDictionaryWorkflow failed for %s: %v", item.ID, err)
	}
	if err := assignKpiCreator(c, models.KPITypeStrategic, item.ID); err != nil {
		log.Printf("[kpi_dictionary] CreateStrategic: assigning creator as KPI Owner failed for %s: %v", item.ID, err)
	}

	h.db.WithContext(c.UserContext()).
		Preload("Goal").Preload("Pillar").Preload("Domain").Preload("OwnerDept").Preload("OwnerOrg").Preload("OwningAgency").Preload("RelatedDepartments").
		Preload("OperationalObjective").Preload("Process").Preload("AwardSubCriterion.AwardCriterion").First(item, item.ID)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "create",
		Module:      "kpi",
		ResourceID:  item.ID.String(),
		Description: fmt.Sprintf("Created strategic KPI %s", item.Code),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiDictionaryHandler) UpdateStrategic(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.StrategicKPIRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	if ok, resp := h.prepareKpiUpdate(c, models.KPITypeStrategic, id, kpiReqFields{
		Code: &req.Code, NameEn: &req.NameEn, NameAr: &req.NameAr, OwnerType: &req.OwnerType,
		OperationalObjectiveID: req.OperationalObjectiveID, ProcessID: req.ProcessID,
		PillarID: &req.PillarID, DomainID: &req.DomainID, AwardKpiMeasureID: &req.AwardKpiMeasureID,
		OwnerDeptID: &req.OwnerDeptID, OwnerOrgID: &req.OwnerOrgID, OwningAgencyID: &req.OwningAgencyID,
		AwardSubCriterionID: &req.AwardSubCriterionID, RelatedDepartmentIDs: req.RelatedDepartmentIDs,
	}); !ok {
		return resp
	}

	updates := map[string]interface{}{
		"code":                     req.Code,
		"name_en":                  req.NameEn,
		"name_ar":                  req.NameAr,
		"goal_id":                  h.deriveGoalID(c.UserContext(), req.OperationalObjectiveID),
		"operational_objective_id": req.OperationalObjectiveID,
		"process_id":               req.ProcessID,
		"award_sub_criterion_id":   req.AwardSubCriterionID,
		"classification":           req.Classification,
		"pillar_id":                req.PillarID,
		"domain_id":                req.DomainID,
		"award_kpi_measure_id":     req.AwardKpiMeasureID,
		"owner_type":               req.OwnerType,
		"owner_dept_id":            req.OwnerDeptID,
		"owner_org_id":             req.OwnerOrgID,
		"owning_agency_id":         req.OwningAgencyID,
		"polarity":                 req.Polarity,
		"target_requirement":       req.TargetRequirement,
		"description_en":           req.DescriptionEn,
		"description_ar":           req.DescriptionAr,
		"formula":                  req.Formula,
		"baseline":                 req.Baseline,
		"unit_of_measure":          req.UnitOfMeasure,
		"reporting_frequency":      req.ReportingFrequency,
		"lifecycle":                req.Lifecycle,
		"data_source":              req.DataSource,
		"segmentation_axes":        req.SegmentationAxes,
		"notes":                    req.Notes,
		"documenta_folder_id":      req.DocumentaFolderID,
	}
	if req.OwnerType == "" {
		updates["owner_type"] = models.KPIOwnerTypeInternal
	}
	if req.TargetRequirement == "" {
		delete(updates, "target_requirement")
	}

	if ok, resp := h.applyKpiUpdate(c, &models.StrategicKPI{ID: id}, updates); !ok {
		return resp
	}
	if err := h.replaceRelatedDepartments(c.UserContext(), &models.StrategicKPI{ID: id}, req.RelatedDepartmentIDs); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to set related departments")
	}

	var item models.StrategicKPI
	h.db.WithContext(c.UserContext()).
		Preload("Goal").Preload("Pillar").Preload("Domain").Preload("OwnerDept").Preload("OwnerOrg").Preload("OwningAgency").Preload("RelatedDepartments").
		Preload("OperationalObjective").Preload("Process").Preload("AwardSubCriterion.AwardCriterion").First(&item, id)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "update",
		Module:      "kpi",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("Updated strategic KPI %s", item.Code),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiDictionaryHandler) DeleteStrategic(c *fiber.Ctx) error {
	return h.deleteKpi(c, models.KPITypeStrategic, &models.StrategicKPI{})
}

// ─── Operational KPI ──────────────────────────────────────────────────────────

func (h *KpiDictionaryHandler) ListOperational(c *fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))
	search := c.Query("search")

	page = max(page, 1)
	limit = kpiListLimit(limit)

	q := h.db.WithContext(c.UserContext()).Model(&models.OperationalKPI{}).
		Preload("Goal").Preload("OperationalObjective").Preload("Process").Preload("Domain").Preload("AwardKpiMeasure").
		Preload("OwnerDept").Preload("OwnerOrg").Preload("OwningAgency").Preload("RelatedDepartments")
	// Only the Super Admin and the KPI's assigned collaborators see it.
	scope := kpiScope(c)
	q = scope.Apply(q, scope.IDCondition("operational", "operational_kpis.id"))

	if search != "" {
		q = q.Where("name_en ILIKE ? OR name_ar ILIKE ? OR code ILIKE ?", "%"+search+"%", "%"+search+"%", "%"+search+"%")
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	var items []models.OperationalKPI
	if err := q.Order("created_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	resp := make([]models.OperationalKPIResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}

	return utils.PaginatedSuccessResponse(c, resp, page, limit, total)
}

func (h *KpiDictionaryHandler) CreateOperational(c *fiber.Ctx) error {
	var req models.OperationalKPIRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	if verr := h.validateKpiRequest(c.UserContext(), kpiReqFields{
		Code: &req.Code, NameEn: &req.NameEn, NameAr: &req.NameAr, OwnerType: &req.OwnerType,
		OperationalObjectiveID: req.OperationalObjectiveID, ProcessID: req.ProcessID,
		PillarID: &req.PillarID, DomainID: &req.DomainID, AwardKpiMeasureID: &req.AwardKpiMeasureID,
		OwnerDeptID: &req.OwnerDeptID, OwnerOrgID: &req.OwnerOrgID, OwningAgencyID: &req.OwningAgencyID,
		AwardSubCriterionID: &req.AwardSubCriterionID, RelatedDepartmentIDs: req.RelatedDepartmentIDs,
	}, uuid.Nil, ""); verr != nil {
		return utils.ErrorResponse(c, verr.status, verr.msg)
	}

	item := &models.OperationalKPI{
		Code:                   req.Code,
		NameEn:                 req.NameEn,
		NameAr:                 req.NameAr,
		GoalID:                 h.deriveGoalID(c.UserContext(), req.OperationalObjectiveID),
		OperationalObjectiveID: req.OperationalObjectiveID,
		ProcessID:              req.ProcessID,
		PillarID:               req.PillarID,
		AwardSubCriterionID:    req.AwardSubCriterionID,
		Classification:         req.Classification,
		DomainID:               req.DomainID,
		AwardKpiMeasureID:      req.AwardKpiMeasureID,
		OwnerType:              req.OwnerType,
		OwnerDeptID:            req.OwnerDeptID,
		OwnerOrgID:             req.OwnerOrgID,
		OwningAgencyID:         req.OwningAgencyID,
		Polarity:               req.Polarity,
		TargetRequirement:      req.TargetRequirement,
		DescriptionEn:          req.DescriptionEn,
		DescriptionAr:          req.DescriptionAr,
		Formula:                req.Formula,
		Baseline:               req.Baseline,
		UnitOfMeasure:          req.UnitOfMeasure,
		ReportingFrequency:     req.ReportingFrequency,
		Lifecycle:              req.Lifecycle,
		DataSource:             req.DataSource,
		Notes:                  req.Notes,
		DocumentaFolderID:      req.DocumentaFolderID,
	}
	if item.Polarity == "" {
		item.Polarity = models.KPIPolarityAscending
	}
	if item.ActivationStatus == "" {
		item.ActivationStatus = models.KPIStatusDraft
	}
	if item.OwnerType == "" {
		item.OwnerType = models.KPIOwnerTypeInternal
	}
	if item.TargetRequirement == "" {
		item.TargetRequirement = "mandatory"
	}

	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		if kpiDictUniqueViolation(err) {
			return utils.ErrorResponse(c, fiber.StatusConflict, fmt.Sprintf("A KPI with code '%s' already exists", req.Code))
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create KPI")
	}
	if err := h.replaceRelatedDepartments(c.UserContext(), item, req.RelatedDepartmentIDs); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to set related departments")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	if err := h.workflowSvc.InitiateKpiDictionaryWorkflow(c.UserContext(), models.KPITypeOperational, item.ID, userID); err != nil {
		log.Printf("[kpi_dictionary] CreateOperational: InitiateKpiDictionaryWorkflow failed for %s: %v", item.ID, err)
	}
	if err := assignKpiCreator(c, models.KPITypeOperational, item.ID); err != nil {
		log.Printf("[kpi_dictionary] CreateOperational: assigning creator as KPI Owner failed for %s: %v", item.ID, err)
	}

	h.db.WithContext(c.UserContext()).
		Preload("Goal").Preload("OperationalObjective").Preload("Process").Preload("Domain").Preload("AwardKpiMeasure").
		Preload("OwnerDept").Preload("OwnerOrg").Preload("OwningAgency").Preload("RelatedDepartments").
		Preload("Pillar").Preload("AwardSubCriterion.AwardCriterion").First(item, item.ID)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "create",
		Module:      "kpi",
		ResourceID:  item.ID.String(),
		Description: fmt.Sprintf("Created operational KPI %s", item.Code),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiDictionaryHandler) GetOperational(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var item models.OperationalKPI
	if err := h.db.WithContext(c.UserContext()).
		Preload("Goal").Preload("OperationalObjective").Preload("Process").Preload("Domain").Preload("AwardKpiMeasure").
		Preload("OwnerDept").Preload("OwnerOrg").Preload("OwningAgency").Preload("RelatedDepartments").
		Preload("Pillar").Preload("AwardSubCriterion.AwardCriterion").First(&item, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiDictionaryHandler) UpdateOperational(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.OperationalKPIRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	if ok, resp := h.prepareKpiUpdate(c, models.KPITypeOperational, id, kpiReqFields{
		Code: &req.Code, NameEn: &req.NameEn, NameAr: &req.NameAr, OwnerType: &req.OwnerType,
		OperationalObjectiveID: req.OperationalObjectiveID, ProcessID: req.ProcessID,
		PillarID: &req.PillarID, DomainID: &req.DomainID, AwardKpiMeasureID: &req.AwardKpiMeasureID,
		OwnerDeptID: &req.OwnerDeptID, OwnerOrgID: &req.OwnerOrgID, OwningAgencyID: &req.OwningAgencyID,
		AwardSubCriterionID: &req.AwardSubCriterionID, RelatedDepartmentIDs: req.RelatedDepartmentIDs,
	}); !ok {
		return resp
	}

	updates := map[string]interface{}{
		"code":                     req.Code,
		"name_en":                  req.NameEn,
		"name_ar":                  req.NameAr,
		"goal_id":                  h.deriveGoalID(c.UserContext(), req.OperationalObjectiveID),
		"operational_objective_id": req.OperationalObjectiveID,
		"process_id":               req.ProcessID,
		"pillar_id":                req.PillarID,
		"award_sub_criterion_id":   req.AwardSubCriterionID,
		"classification":           req.Classification,
		"domain_id":                req.DomainID,
		"award_kpi_measure_id":     req.AwardKpiMeasureID,
		"owner_type":               req.OwnerType,
		"owner_dept_id":            req.OwnerDeptID,
		"owner_org_id":             req.OwnerOrgID,
		"owning_agency_id":         req.OwningAgencyID,
		"polarity":                 req.Polarity,
		"target_requirement":       req.TargetRequirement,
		"description_en":           req.DescriptionEn,
		"description_ar":           req.DescriptionAr,
		"formula":                  req.Formula,
		"baseline":                 req.Baseline,
		"unit_of_measure":          req.UnitOfMeasure,
		"reporting_frequency":      req.ReportingFrequency,
		"lifecycle":                req.Lifecycle,
		"data_source":              req.DataSource,
		"notes":                    req.Notes,
		"documenta_folder_id":      req.DocumentaFolderID,
	}
	if req.OwnerType == "" {
		updates["owner_type"] = models.KPIOwnerTypeInternal
	}
	if req.TargetRequirement == "" {
		delete(updates, "target_requirement")
	}

	if ok, resp := h.applyKpiUpdate(c, &models.OperationalKPI{ID: id}, updates); !ok {
		return resp
	}
	if err := h.replaceRelatedDepartments(c.UserContext(), &models.OperationalKPI{ID: id}, req.RelatedDepartmentIDs); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to set related departments")
	}

	var item models.OperationalKPI
	h.db.WithContext(c.UserContext()).
		Preload("Goal").Preload("OperationalObjective").Preload("Process").Preload("Domain").Preload("AwardKpiMeasure").
		Preload("OwnerDept").Preload("OwnerOrg").Preload("OwningAgency").Preload("RelatedDepartments").
		Preload("Pillar").Preload("AwardSubCriterion.AwardCriterion").First(&item, id)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "update",
		Module:      "kpi",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("Updated operational KPI %s", item.Code),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiDictionaryHandler) DeleteOperational(c *fiber.Ctx) error {
	return h.deleteKpi(c, models.KPITypeOperational, &models.OperationalKPI{})
}

// ─── Award KPI ────────────────────────────────────────────────────────────────

func (h *KpiDictionaryHandler) ListAward(c *fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))
	search := c.Query("search")

	page = max(page, 1)
	limit = kpiListLimit(limit)

	q := h.db.WithContext(c.UserContext()).Model(&models.AwardKPI{}).
		Preload("AwardSubCriterion.AwardCriterion").Preload("Domain").Preload("AwardKpiMeasure").
		Preload("OwnerDept").Preload("OwnerOrg").Preload("OwningAgency").Preload("RelatedDepartments")
	// Only the Super Admin and the KPI's assigned collaborators see it.
	scope := kpiScope(c)
	q = scope.Apply(q, scope.IDCondition("award", "award_kpis.id"))
	if search != "" {
		q = q.Where("name_en ILIKE ? OR name_ar ILIKE ? OR code ILIKE ?", "%"+search+"%", "%"+search+"%", "%"+search+"%")
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	var items []models.AwardKPI
	if err := q.Order("created_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	resp := make([]models.AwardKPIResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}

	return utils.PaginatedSuccessResponse(c, resp, page, limit, total)
}

func (h *KpiDictionaryHandler) CreateAward(c *fiber.Ctx) error {
	var req models.AwardKPIRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	if verr := h.validateKpiRequest(c.UserContext(), kpiReqFields{
		Code: &req.Code, NameEn: &req.NameEn, NameAr: &req.NameAr, OwnerType: &req.OwnerType,
		OperationalObjectiveID: req.OperationalObjectiveID, ProcessID: req.ProcessID,
		PillarID: &req.PillarID, DomainID: &req.DomainID, AwardKpiMeasureID: &req.AwardKpiMeasureID,
		OwnerDeptID: &req.OwnerDeptID, OwnerOrgID: &req.OwnerOrgID, OwningAgencyID: &req.OwningAgencyID,
		AwardSubCriterionID: &req.AwardSubCriterionID, RelatedDepartmentIDs: req.RelatedDepartmentIDs,
	}, uuid.Nil, ""); verr != nil {
		return utils.ErrorResponse(c, verr.status, verr.msg)
	}

	item := &models.AwardKPI{
		Code:                   req.Code,
		NameEn:                 req.NameEn,
		NameAr:                 req.NameAr,
		GoalID:                 h.deriveGoalID(c.UserContext(), req.OperationalObjectiveID),
		OperationalObjectiveID: &req.OperationalObjectiveID,
		ProcessID:              &req.ProcessID,
		AwardSubCriterionID:    req.AwardSubCriterionID,
		Classification:         req.Classification,
		PillarID:               req.PillarID,
		DomainID:               req.DomainID,
		AwardKpiMeasureID:      req.AwardKpiMeasureID,
		OwnerType:              req.OwnerType,
		OwnerDeptID:            req.OwnerDeptID,
		OwnerOrgID:             req.OwnerOrgID,
		OwningAgencyID:         req.OwningAgencyID,
		Polarity:               req.Polarity,
		TargetRequirement:      req.TargetRequirement,
		DescriptionEn:          req.DescriptionEn,
		DescriptionAr:          req.DescriptionAr,
		Formula:                req.Formula,
		Baseline:               req.Baseline,
		UnitOfMeasure:          req.UnitOfMeasure,
		ReportingFrequency:     req.ReportingFrequency,
		Lifecycle:              req.Lifecycle,
		DataSource:             req.DataSource,
		Notes:                  req.Notes,
		DocumentaFolderID:      req.DocumentaFolderID,
	}
	if item.Polarity == "" {
		item.Polarity = models.KPIPolarityAscending
	}
	if item.ActivationStatus == "" {
		item.ActivationStatus = models.KPIStatusDraft
	}
	if item.OwnerType == "" {
		item.OwnerType = models.KPIOwnerTypeInternal
	}
	if item.TargetRequirement == "" {
		item.TargetRequirement = "mandatory"
	}

	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		if kpiDictUniqueViolation(err) {
			return utils.ErrorResponse(c, fiber.StatusConflict, fmt.Sprintf("A KPI with code '%s' already exists", req.Code))
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create KPI")
	}
	if err := h.replaceRelatedDepartments(c.UserContext(), item, req.RelatedDepartmentIDs); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to set related departments")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	if err := h.workflowSvc.InitiateKpiDictionaryWorkflow(c.UserContext(), models.KPITypeAward, item.ID, userID); err != nil {
		log.Printf("[kpi_dictionary] CreateAward: InitiateKpiDictionaryWorkflow failed for %s: %v", item.ID, err)
	}
	if err := assignKpiCreator(c, models.KPITypeAward, item.ID); err != nil {
		log.Printf("[kpi_dictionary] CreateAward: assigning creator as KPI Owner failed for %s: %v", item.ID, err)
	}

	h.db.WithContext(c.UserContext()).
		Preload("AwardSubCriterion.AwardCriterion").Preload("Domain").Preload("AwardKpiMeasure").
		Preload("OwnerDept").Preload("OwnerOrg").Preload("OwningAgency").Preload("RelatedDepartments").
		Preload("Goal").Preload("OperationalObjective").Preload("Process").Preload("Pillar").First(item, item.ID)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "create",
		Module:      "kpi",
		ResourceID:  item.ID.String(),
		Description: fmt.Sprintf("Created award KPI %s", item.Code),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiDictionaryHandler) DeleteAward(c *fiber.Ctx) error {
	return h.deleteKpi(c, models.KPITypeAward, &models.AwardKPI{})
}

func (h *KpiDictionaryHandler) GetAward(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var item models.AwardKPI
	if err := h.db.WithContext(c.UserContext()).
		Preload("AwardSubCriterion.AwardCriterion").Preload("Domain").Preload("AwardKpiMeasure").
		Preload("OwnerDept").Preload("OwnerOrg").Preload("OwningAgency").Preload("RelatedDepartments").
		Preload("Goal").Preload("OperationalObjective").Preload("Process").Preload("Pillar").First(&item, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiDictionaryHandler) UpdateAward(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.AwardKPIRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	if ok, resp := h.prepareKpiUpdate(c, models.KPITypeAward, id, kpiReqFields{
		Code: &req.Code, NameEn: &req.NameEn, NameAr: &req.NameAr, OwnerType: &req.OwnerType,
		OperationalObjectiveID: req.OperationalObjectiveID, ProcessID: req.ProcessID,
		PillarID: &req.PillarID, DomainID: &req.DomainID, AwardKpiMeasureID: &req.AwardKpiMeasureID,
		OwnerDeptID: &req.OwnerDeptID, OwnerOrgID: &req.OwnerOrgID, OwningAgencyID: &req.OwningAgencyID,
		AwardSubCriterionID: &req.AwardSubCriterionID, RelatedDepartmentIDs: req.RelatedDepartmentIDs,
	}); !ok {
		return resp
	}

	updates := map[string]interface{}{
		"code":                     req.Code,
		"name_en":                  req.NameEn,
		"name_ar":                  req.NameAr,
		"goal_id":                  h.deriveGoalID(c.UserContext(), req.OperationalObjectiveID),
		"operational_objective_id": req.OperationalObjectiveID,
		"process_id":               req.ProcessID,
		"award_sub_criterion_id":   req.AwardSubCriterionID,
		"classification":           req.Classification,
		"pillar_id":                req.PillarID,
		"domain_id":                req.DomainID,
		"award_kpi_measure_id":     req.AwardKpiMeasureID,
		"owner_type":               req.OwnerType,
		"owner_dept_id":            req.OwnerDeptID,
		"owner_org_id":             req.OwnerOrgID,
		"owning_agency_id":         req.OwningAgencyID,
		"polarity":                 req.Polarity,
		"target_requirement":       req.TargetRequirement,
		"description_en":           req.DescriptionEn,
		"description_ar":           req.DescriptionAr,
		"formula":                  req.Formula,
		"baseline":                 req.Baseline,
		"unit_of_measure":          req.UnitOfMeasure,
		"reporting_frequency":      req.ReportingFrequency,
		"lifecycle":                req.Lifecycle,
		"data_source":              req.DataSource,
		"notes":                    req.Notes,
		"documenta_folder_id":      req.DocumentaFolderID,
	}
	if req.OwnerType == "" {
		updates["owner_type"] = models.KPIOwnerTypeInternal
	}
	if req.TargetRequirement == "" {
		delete(updates, "target_requirement")
	}

	if ok, resp := h.applyKpiUpdate(c, &models.AwardKPI{ID: id}, updates); !ok {
		return resp
	}
	if err := h.replaceRelatedDepartments(c.UserContext(), &models.AwardKPI{ID: id}, req.RelatedDepartmentIDs); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to set related departments")
	}

	var item models.AwardKPI
	h.db.WithContext(c.UserContext()).
		Preload("AwardSubCriterion.AwardCriterion").Preload("Domain").Preload("AwardKpiMeasure").
		Preload("OwnerDept").Preload("OwnerOrg").Preload("OwningAgency").Preload("RelatedDepartments").
		Preload("Goal").Preload("OperationalObjective").Preload("Process").Preload("Pillar").First(&item, id)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "update",
		Module:      "kpi",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("Updated award KPI %s", item.Code),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

// ─── KPI Workflow Transitions ──────────────────────────────────────────────
// Replaces the old hardcoded activate/deactivate/reactivate mechanism with
// the generic Workflow/WorkflowState/WorkflowTransition engine's "KPI
// Workflow" (Draft→Reviewed→Approved→Active→Closed), auto-assigned to every
// KPI at creation (see CreateStrategic/CreateOperational/CreateAward) — see
// services.KpiDictionaryWorkflowService for the state machine, role
// (AllowedRoles) gating, and BR-07 weight-sum check.

// GetAvailableTransitions lists the transitions the calling user may
// currently execute on this KPI (already role-filtered server-side).
func (h *KpiDictionaryHandler) GetAvailableTransitions(c *fiber.Ctx) error {
	kpiType := c.Params("type")
	id, err := uuid.Parse(c.Params("id"))
	if err != nil || !isValidKpiType(kpiType) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	transitions, err := h.workflowSvc.GetAvailableKpiDictionaryTransitions(c.UserContext(), kpiType, id, userID)
	if errors.Is(err, services.ErrKpiDictionaryNotFound) {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	if transitions, err = allowedTransitions(c, kpiType, id, transitions); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", transitions)
}

// TransitionKpi executes a workflow transition on this KPI's own lifecycle
// (as opposed to /entries or /performance, which transition nested records).
func (h *KpiDictionaryHandler) TransitionKpi(c *fiber.Ctx) error {
	kpiType := c.Params("type")
	id, err := uuid.Parse(c.Params("id"))
	if err != nil || !isValidKpiType(kpiType) {
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
	newStatus, err := h.workflowSvc.TransitionKpiDictionary(c.UserContext(), kpiType, id, &req, userID)
	switch {
	case err == nil:
	case errors.Is(err, services.ErrKpiDictionaryNotFound):
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	case errors.Is(err, services.ErrKpiTransitionForbidden):
		return utils.ErrorResponse(c, fiber.StatusForbidden, err.Error())
	case errors.Is(err, services.ErrKpiTransitionInvalid):
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	default:
		log.Printf("[kpi_dictionary] TransitionKpi %s %s: %v", kpiType, id, err)
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "internal_server_error"))
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "transition",
		Module:      "kpi",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("Transitioned %s KPI %s (status -> %s)", kpiType, h.kpiCode(kpiType, id), newStatus),
		Status:      "success",
	})

	if req.Comment != "" {
		h.db.WithContext(c.UserContext()).Create(&models.KpiComment{
			KpiID:    id,
			KpiType:  kpiType,
			AuthorID: userID,
			Content:  req.Comment,
		})
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Status updated successfully", fiber.Map{"activation_status": newStatus})
}
