package handlers

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type KpiMasterDataHandler struct {
	db        *gorm.DB
	validator *validator.Validate
}

func NewKpiMasterDataHandler(db *gorm.DB) *KpiMasterDataHandler {
	return &KpiMasterDataHandler{
		db:        db,
		validator: validator.New(),
	}
}

// ─── Shared validation helpers ────────────────────────────────────────────────
//
// The forms enforce the same rules as the master-data Excel import
// (services/kpi_master_data_import.go): names are trimmed, blank names are
// rejected, duplicates are detected case-insensitively among live
// (non-deleted) rows within the same scope as the import's identity key, and
// every reference must point at an existing, non-deleted record.

const mdMaxNameLen = 255

// mdTables are the physical table names used by the raw existence /
// duplicate / dependant queries below.
const (
	mdTablePillars           = "pillars"
	mdTableEnablers          = "enablers"
	mdTableObjectives        = "operational_objectives"
	mdTableProcesses         = "processes"
	mdTableInitiatives       = "initiatives"
	mdTableDomains           = "domains"
	mdTableAwardCriteria     = "award_criterions"
	mdTableAwardSubCriteria  = "award_sub_criterions"
	mdTableAwardKpiMeasures  = "award_kpi_measures"
	mdTableDataSources       = "kpi_data_sources"
	mdTableSegmentationDims  = "kpi_segmentation_dimensions"
	mdTableOrganizations     = "kpi_organizations"
	mdTableDepartments       = "departments"
	mdTableGoals             = "goals"
	mdTableStrategicKpis     = "strategic_kpis"
	mdTableOperationalKpis   = "operational_kpis"
	mdTableAwardKpis         = "award_kpis"
	mdTableSegmentationAxes  = "kpi_segmentation_axis"
	mdTableKpiAnnualTargets  = "kpi_annual_targets"
	mdTableKpiEntries        = "kpi_entries"
	mdTableStrategicGoalsAlt = "strategic_goals"
)

func mdBadRequest(c *fiber.Ctx, msg string) error {
	return utils.ErrorResponse(c, fiber.StatusBadRequest, msg)
}

func mdNotFound(c *fiber.Ctx) error {
	return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
}

// mdIsUniqueViolation reports whether err is a Postgres unique-constraint
// violation (SQLSTATE 23505).
func mdIsUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "23505") || strings.Contains(s, "duplicate key") || strings.Contains(s, "unique constraint")
}

// mdCleanNames trims both names and validates them the way the import does.
// It returns a user-facing message when the names are invalid.
func mdCleanNames(nameEn, nameAr *string) string {
	*nameEn = strings.TrimSpace(*nameEn)
	*nameAr = strings.TrimSpace(*nameAr)
	if *nameEn == "" {
		return "Name (English) is required."
	}
	if utf8.RuneCountInString(*nameEn) > mdMaxNameLen {
		return "Name (English) must be at most 255 characters."
	}
	if utf8.RuneCountInString(*nameAr) > mdMaxNameLen {
		return "Name (Arabic) must be at most 255 characters."
	}
	return ""
}

// mdExists reports whether a live (non-deleted) row with the given id exists.
func (h *KpiMasterDataHandler) mdExists(c *fiber.Ctx, table string, id uuid.UUID) bool {
	var n int64
	h.db.WithContext(c.UserContext()).Table(table).Where("id = ? AND deleted_at IS NULL", id).Count(&n)
	return n > 0
}

// mdRefCheck validates an optional reference. Returns a message when the
// referenced record does not exist.
func (h *KpiMasterDataHandler) mdRefCheck(c *fiber.Ctx, table, label string, id *uuid.UUID) string {
	if id == nil || *id == uuid.Nil {
		return ""
	}
	if !h.mdExists(c, table, *id) {
		return fmt.Sprintf("%s %s was not found.", label, id.String())
	}
	return ""
}

// mdNameTaken reports whether another live row in table already uses name
// (trimmed, case-insensitive) within the optional scope column. A nil scope
// value matches rows whose scope column IS NULL.
func (h *KpiMasterDataHandler) mdNameTaken(c *fiber.Ctx, table, column, name string, exclude uuid.UUID, scopeCol string, scopeVal *uuid.UUID) bool {
	q := h.db.WithContext(c.UserContext()).Table(table).
		Where("deleted_at IS NULL").
		Where(fmt.Sprintf("LOWER(TRIM(%s)) = LOWER(?)", column), strings.TrimSpace(name))
	if exclude != uuid.Nil {
		q = q.Where("id <> ?", exclude)
	}
	if scopeCol != "" {
		if scopeVal == nil {
			q = q.Where(scopeCol + " IS NULL")
		} else {
			q = q.Where(scopeCol+" = ?", *scopeVal)
		}
	}
	var n int64
	q.Count(&n)
	return n > 0
}

func mdDuplicateMessage(entity, name string) string {
	return fmt.Sprintf("%s %q already exists.", entity, name)
}

// mdDependant describes a table/column that references a master-data record.
type mdDependant struct {
	table      string
	column     string
	label      string
	softDelete bool
}

// mdDependantsMessage counts live rows referencing id and returns a message
// listing them, or "" when nothing references the record. Tables that do not
// exist in this deployment are skipped.
func (h *KpiMasterDataHandler) mdDependantsMessage(c *fiber.Ctx, entity string, id uuid.UUID, deps []mdDependant) string {
	var parts []string
	for _, d := range deps {
		q := h.db.WithContext(c.UserContext()).Table(d.table).Where(d.column+" = ?", id)
		if d.softDelete {
			q = q.Where("deleted_at IS NULL")
		}
		var n int64
		if err := q.Count(&n).Error; err != nil {
			continue
		}
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, d.label))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return fmt.Sprintf("This %s cannot be deleted because it is still used by: %s. Reassign or delete those records first.", entity, strings.Join(parts, ", "))
}

func kpiTaxonomyDependants(column string) []mdDependant {
	return []mdDependant{
		{mdTableStrategicKpis, column, "strategic KPI(s)", true},
		{mdTableOperationalKpis, column, "operational KPI(s)", true},
		{mdTableAwardKpis, column, "award KPI(s)", true},
	}
}

// mdDelete runs the shared delete flow: 404 when missing, 409 when still
// referenced, otherwise soft delete.
func (h *KpiMasterDataHandler) mdDelete(c *fiber.Ctx, model interface{}, table, entity string, deps []mdDependant) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	if !h.mdExists(c, table, id) {
		return mdNotFound(c)
	}
	if msg := h.mdDependantsMessage(c, entity, id, deps); msg != "" {
		return utils.ErrorResponse(c, fiber.StatusConflict, msg)
	}
	result := h.db.WithContext(c.UserContext()).Delete(model, "id = ?", id)
	if result.Error != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_delete"))
	}
	if result.RowsAffected == 0 {
		return mdNotFound(c)
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

// mdParseBody parses + struct-validates the request body. It returns a
// non-nil error response (already written) when invalid.
func mdParseBody(c *fiber.Ctx, req interface{}) (bool, error) {
	if err := c.BodyParser(req); err != nil {
		return false, utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), req); len(validationErrors) != 0 {
		return false, c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	return true, nil
}

// mdParseID parses :id. ok=false means a response was already written.
func mdParseID(c *fiber.Ctx) (uuid.UUID, bool, error) {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return uuid.Nil, false, utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	return id, true, nil
}

// mdWriteError maps a create/update DB error to a response.
func mdWriteError(c *fiber.Ctx, err error, entity, name, failKey string) error {
	if mdIsUniqueViolation(err) {
		return utils.ErrorResponse(c, fiber.StatusConflict, mdDuplicateMessage(entity, name))
	}
	return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), failKey))
}

// ─── Pillars ──────────────────────────────────────────────────────────────────

func (h *KpiMasterDataHandler) ListPillars(c *fiber.Ctx) error {
	var items []models.Pillar
	if err := h.db.WithContext(c.UserContext()).Preload("Owner").Order("created_at DESC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	resp := make([]models.PillarResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

// validatePillarLike validates pillar/enabler requests (same shape).
func (h *KpiMasterDataHandler) validatePillarLike(c *fiber.Ctx, table, entity string, nameEn, nameAr *string, ownerID *uuid.UUID, self uuid.UUID) string {
	if msg := mdCleanNames(nameEn, nameAr); msg != "" {
		return msg
	}
	if msg := h.mdRefCheck(c, mdTableDepartments, "Owner department", ownerID); msg != "" {
		return msg
	}
	if h.mdNameTaken(c, table, "name_en", *nameEn, self, "", nil) {
		return "dup:" + mdDuplicateMessage(entity, *nameEn)
	}
	return ""
}

// mdValidationResponse writes a 409 for "dup:"-prefixed messages and 400
// otherwise.
func mdValidationResponse(c *fiber.Ctx, msg string) error {
	if strings.HasPrefix(msg, "dup:") {
		return utils.ErrorResponse(c, fiber.StatusConflict, strings.TrimPrefix(msg, "dup:"))
	}
	return mdBadRequest(c, msg)
}

func (h *KpiMasterDataHandler) CreatePillar(c *fiber.Ctx) error {
	var req models.PillarRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if msg := h.validatePillarLike(c, mdTablePillars, "Pillar", &req.NameEn, &req.NameAr, req.OwnerID, uuid.Nil); msg != "" {
		return mdValidationResponse(c, msg)
	}
	item := &models.Pillar{NameEn: req.NameEn, NameAr: req.NameAr, OwnerID: req.OwnerID}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return mdWriteError(c, err, "Pillar", req.NameEn, "failed_to_create")
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdatePillar(c *fiber.Ctx) error {
	id, ok, resp := mdParseID(c)
	if !ok {
		return resp
	}
	var req models.PillarRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if !h.mdExists(c, mdTablePillars, id) {
		return mdNotFound(c)
	}
	if msg := h.validatePillarLike(c, mdTablePillars, "Pillar", &req.NameEn, &req.NameAr, req.OwnerID, id); msg != "" {
		return mdValidationResponse(c, msg)
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.Pillar{ID: id}).Updates(map[string]interface{}{
		"name_en":  req.NameEn,
		"name_ar":  req.NameAr,
		"owner_id": req.OwnerID,
	})
	if result.Error != nil {
		return mdWriteError(c, result.Error, "Pillar", req.NameEn, "failed_to_update")
	}
	if result.RowsAffected == 0 {
		return mdNotFound(c)
	}
	var item models.Pillar
	h.db.WithContext(c.UserContext()).Preload("Owner").First(&item, id)
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeletePillar(c *fiber.Ctx) error {
	deps := append([]mdDependant{
		{mdTableObjectives, "pillar_id", "parent objective(s)", true},
		{mdTableProcesses, "pillar_id", "operational objective(s)", true},
		{mdTableInitiatives, "pillar_id", "initiative(s)", true},
		{mdTableStrategicGoalsAlt, "pillar_id", "strategic goal(s)", true},
	}, kpiTaxonomyDependants("pillar_id")...)
	return h.mdDelete(c, &models.Pillar{}, mdTablePillars, "pillar", deps)
}

// ─── Enablers ─────────────────────────────────────────────────────────────────

func (h *KpiMasterDataHandler) ListEnablers(c *fiber.Ctx) error {
	var items []models.Enabler
	if err := h.db.WithContext(c.UserContext()).Preload("Owner").Order("created_at DESC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	resp := make([]models.EnablerResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

func (h *KpiMasterDataHandler) CreateEnabler(c *fiber.Ctx) error {
	var req models.EnablerRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if msg := h.validatePillarLike(c, mdTableEnablers, "Enabler", &req.NameEn, &req.NameAr, req.OwnerID, uuid.Nil); msg != "" {
		return mdValidationResponse(c, msg)
	}
	item := &models.Enabler{NameEn: req.NameEn, NameAr: req.NameAr, OwnerID: req.OwnerID}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return mdWriteError(c, err, "Enabler", req.NameEn, "failed_to_create")
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdateEnabler(c *fiber.Ctx) error {
	id, ok, resp := mdParseID(c)
	if !ok {
		return resp
	}
	var req models.EnablerRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if !h.mdExists(c, mdTableEnablers, id) {
		return mdNotFound(c)
	}
	if msg := h.validatePillarLike(c, mdTableEnablers, "Enabler", &req.NameEn, &req.NameAr, req.OwnerID, id); msg != "" {
		return mdValidationResponse(c, msg)
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.Enabler{ID: id}).Updates(map[string]interface{}{
		"name_en":  req.NameEn,
		"name_ar":  req.NameAr,
		"owner_id": req.OwnerID,
	})
	if result.Error != nil {
		return mdWriteError(c, result.Error, "Enabler", req.NameEn, "failed_to_update")
	}
	if result.RowsAffected == 0 {
		return mdNotFound(c)
	}
	var item models.Enabler
	h.db.WithContext(c.UserContext()).Preload("Owner").First(&item, id)
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteEnabler(c *fiber.Ctx) error {
	deps := []mdDependant{
		{mdTableObjectives, "enabler_id", "parent objective(s)", true},
		{mdTableProcesses, "enabler_id", "operational objective(s)", true},
		{mdTableInitiatives, "enabler_id", "initiative(s)", true},
		{mdTableStrategicGoalsAlt, "enabler_id", "strategic goal(s)", true},
	}
	return h.mdDelete(c, &models.Enabler{}, mdTableEnablers, "enabler", deps)
}

// ─── Operational Objectives (UI: "Parent Objectives") ────────────────────────

func (h *KpiMasterDataHandler) ListOperationalObjectives(c *fiber.Ctx) error {
	var items []models.OperationalObjective
	if err := h.db.WithContext(c.UserContext()).Preload("Goal").Preload("Pillar").Preload("Enabler").Order("created_at DESC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	resp := make([]models.OperationalObjectiveResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

// validateObjective applies the import's rules: trimmed names, goal required
// and existing, pillar/enabler existing, name unique per goal.
func (h *KpiMasterDataHandler) validateObjective(c *fiber.Ctx, req *models.OperationalObjectiveRequest, self uuid.UUID) string {
	if msg := mdCleanNames(&req.NameEn, &req.NameAr); msg != "" {
		return msg
	}
	goalID := req.GoalID
	if goalID == uuid.Nil {
		return "Strategic Goal is required."
	}
	if msg := h.mdRefCheck(c, mdTableGoals, "Strategic Goal", &goalID); msg != "" {
		return msg
	}
	if msg := h.mdRefCheck(c, mdTablePillars, "Pillar", req.PillarID); msg != "" {
		return msg
	}
	if msg := h.mdRefCheck(c, mdTableEnablers, "Enabler", req.EnablerID); msg != "" {
		return msg
	}
	if h.mdNameTaken(c, mdTableObjectives, "name_en", req.NameEn, self, "goal_id", &goalID) {
		return "dup:" + fmt.Sprintf("Parent objective %q already exists for this strategic goal.", req.NameEn)
	}
	return ""
}

func (h *KpiMasterDataHandler) CreateOperationalObjective(c *fiber.Ctx) error {
	var req models.OperationalObjectiveRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if msg := h.validateObjective(c, &req, uuid.Nil); msg != "" {
		return mdValidationResponse(c, msg)
	}
	item := &models.OperationalObjective{
		NameEn:    req.NameEn,
		NameAr:    req.NameAr,
		GoalID:    &req.GoalID,
		PillarID:  req.PillarID,
		EnablerID: req.EnablerID,
	}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return mdWriteError(c, err, "Parent objective", req.NameEn, "failed_to_create")
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdateOperationalObjective(c *fiber.Ctx) error {
	id, ok, resp := mdParseID(c)
	if !ok {
		return resp
	}
	var req models.OperationalObjectiveRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	var current models.OperationalObjective
	if err := h.db.WithContext(c.UserContext()).First(&current, "id = ?", id).Error; err != nil {
		return mdNotFound(c)
	}
	if msg := h.validateObjective(c, &req, id); msg != "" {
		return mdValidationResponse(c, msg)
	}
	goalChanged := current.GoalID == nil || *current.GoalID != req.GoalID
	if goalChanged {
		// Initiatives tie their objective to their own goal; moving the
		// objective to another goal would silently break that link.
		var n int64
		h.db.WithContext(c.UserContext()).Table(mdTableInitiatives).
			Where("objective_id = ? AND deleted_at IS NULL AND (goal_id IS NULL OR goal_id <> ?)", id, req.GoalID).Count(&n)
		if n > 0 {
			return utils.ErrorResponse(c, fiber.StatusConflict,
				fmt.Sprintf("The strategic goal cannot be changed: %d initiative(s) linked to this parent objective belong to the current goal. Update or unlink them first.", n))
		}
	}
	err := h.db.WithContext(c.UserContext()).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&models.OperationalObjective{ID: id}).Updates(map[string]interface{}{
			"name_en":    req.NameEn,
			"name_ar":    req.NameAr,
			"goal_id":    req.GoalID,
			"pillar_id":  req.PillarID,
			"enabler_id": req.EnablerID,
		})
		if res.Error != nil {
			return res.Error
		}
		if goalChanged {
			// Child processes inherit the strategic goal from their parent.
			return tx.Model(&models.Process{}).Where("operational_objective_id = ?", id).
				Update("goal_id", req.GoalID).Error
		}
		return nil
	})
	if err != nil {
		return mdWriteError(c, err, "Parent objective", req.NameEn, "failed_to_update")
	}
	var item models.OperationalObjective
	h.db.WithContext(c.UserContext()).Preload("Goal").Preload("Pillar").Preload("Enabler").First(&item, id)
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteOperationalObjective(c *fiber.Ctx) error {
	deps := append([]mdDependant{
		{mdTableProcesses, "operational_objective_id", "operational objective(s)", true},
		{mdTableInitiatives, "objective_id", "initiative(s)", true},
	}, kpiTaxonomyDependants("operational_objective_id")...)
	return h.mdDelete(c, &models.OperationalObjective{}, mdTableObjectives, "parent objective", deps)
}

// ─── Processes (UI: "Operational Objectives") ────────────────────────────────

func (h *KpiMasterDataHandler) ListProcesses(c *fiber.Ctx) error {
	var items []models.Process
	if err := h.db.WithContext(c.UserContext()).Preload("OperationalObjective").Preload("Goal").Preload("Pillar").Preload("Enabler").Preload("Department").Order("created_at DESC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	resp := make([]models.ProcessResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

// validateProcess applies the import's rules. The strategic goal always comes
// from the parent objective: a goal_id that differs from the parent's goal is
// rejected, and the request's GoalID / PillarID / EnablerID are filled from
// the parent where missing.
func (h *KpiMasterDataHandler) validateProcess(c *fiber.Ctx, req *models.ProcessRequest, self uuid.UUID) string {
	if msg := mdCleanNames(&req.NameEn, &req.NameAr); msg != "" {
		return msg
	}
	req.Unit = strings.TrimSpace(req.Unit)
	if utf8.RuneCountInString(req.Unit) > mdMaxNameLen {
		return "Unit must be at most 255 characters."
	}
	if req.OperationalObjectiveID == uuid.Nil {
		return "Parent Objective is required."
	}
	var parent models.OperationalObjective
	if err := h.db.WithContext(c.UserContext()).First(&parent, "id = ?", req.OperationalObjectiveID).Error; err != nil {
		return fmt.Sprintf("Parent Objective %s was not found.", req.OperationalObjectiveID)
	}
	if parent.GoalID != nil {
		if req.GoalID != uuid.Nil && req.GoalID != *parent.GoalID {
			return "Strategic Goal must match the parent objective's strategic goal."
		}
		req.GoalID = *parent.GoalID
	}
	if req.GoalID == uuid.Nil {
		return "Strategic Goal is required."
	}
	goalID := req.GoalID
	if msg := h.mdRefCheck(c, mdTableGoals, "Strategic Goal", &goalID); msg != "" {
		return msg
	}
	if req.PillarID == nil {
		req.PillarID = parent.PillarID
	}
	if req.EnablerID == nil {
		req.EnablerID = parent.EnablerID
	}
	if msg := h.mdRefCheck(c, mdTablePillars, "Pillar", req.PillarID); msg != "" {
		return msg
	}
	if msg := h.mdRefCheck(c, mdTableEnablers, "Enabler", req.EnablerID); msg != "" {
		return msg
	}
	if msg := h.mdRefCheck(c, mdTableDepartments, "Department", req.DepartmentID); msg != "" {
		return msg
	}
	parentID := req.OperationalObjectiveID
	if h.mdNameTaken(c, mdTableProcesses, "name_en", req.NameEn, self, "operational_objective_id", &parentID) {
		return "dup:" + fmt.Sprintf("Operational objective %q already exists under this parent objective.", req.NameEn)
	}
	return ""
}

func (h *KpiMasterDataHandler) CreateProcess(c *fiber.Ctx) error {
	var req models.ProcessRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if msg := h.validateProcess(c, &req, uuid.Nil); msg != "" {
		return mdValidationResponse(c, msg)
	}
	item := &models.Process{
		NameEn:                 req.NameEn,
		NameAr:                 req.NameAr,
		OperationalObjectiveID: req.OperationalObjectiveID,
		GoalID:                 &req.GoalID,
		PillarID:               req.PillarID,
		EnablerID:              req.EnablerID,
		DepartmentID:           req.DepartmentID,
		Unit:                   req.Unit,
	}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return mdWriteError(c, err, "Operational objective", req.NameEn, "failed_to_create")
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdateProcess(c *fiber.Ctx) error {
	id, ok, resp := mdParseID(c)
	if !ok {
		return resp
	}
	var req models.ProcessRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if !h.mdExists(c, mdTableProcesses, id) {
		return mdNotFound(c)
	}
	if msg := h.validateProcess(c, &req, id); msg != "" {
		return mdValidationResponse(c, msg)
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.Process{ID: id}).Updates(map[string]interface{}{
		"name_en":                  req.NameEn,
		"name_ar":                  req.NameAr,
		"operational_objective_id": req.OperationalObjectiveID,
		"goal_id":                  req.GoalID,
		"pillar_id":                req.PillarID,
		"enabler_id":               req.EnablerID,
		"department_id":            req.DepartmentID,
		"unit":                     req.Unit,
	})
	if result.Error != nil {
		return mdWriteError(c, result.Error, "Operational objective", req.NameEn, "failed_to_update")
	}
	if result.RowsAffected == 0 {
		return mdNotFound(c)
	}
	var item models.Process
	h.db.WithContext(c.UserContext()).Preload("OperationalObjective").Preload("Goal").Preload("Pillar").Preload("Enabler").Preload("Department").First(&item, id)
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteProcess(c *fiber.Ctx) error {
	return h.mdDelete(c, &models.Process{}, mdTableProcesses, "operational objective", kpiTaxonomyDependants("process_id"))
}

// ─── Initiatives ──────────────────────────────────────────────────────────────

func (h *KpiMasterDataHandler) ListInitiatives(c *fiber.Ctx) error {
	var items []models.Initiative
	if err := h.db.WithContext(c.UserContext()).Preload("Goal").Preload("Objective").Preload("Owner").Order("created_at DESC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	resp := make([]models.InitiativeResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

func (h *KpiMasterDataHandler) validateInitiative(c *fiber.Ctx, req *models.InitiativeRequest, self uuid.UUID) string {
	if msg := mdCleanNames(&req.NameEn, &req.NameAr); msg != "" {
		return msg
	}
	if req.GoalID == uuid.Nil {
		return "Strategic Goal is required."
	}
	goalID := req.GoalID
	if msg := h.mdRefCheck(c, mdTableGoals, "Strategic Goal", &goalID); msg != "" {
		return msg
	}
	if req.ObjectiveID != nil && *req.ObjectiveID != uuid.Nil {
		var obj models.OperationalObjective
		if err := h.db.WithContext(c.UserContext()).First(&obj, "id = ?", *req.ObjectiveID).Error; err != nil {
			return fmt.Sprintf("Parent Objective %s was not found.", req.ObjectiveID)
		}
		if obj.GoalID == nil || *obj.GoalID != req.GoalID {
			return "Parent Objective must belong to the selected strategic goal."
		}
	}
	if msg := h.mdRefCheck(c, mdTablePillars, "Pillar", req.PillarID); msg != "" {
		return msg
	}
	if msg := h.mdRefCheck(c, mdTableEnablers, "Enabler", req.EnablerID); msg != "" {
		return msg
	}
	if msg := h.mdRefCheck(c, mdTableDepartments, "Owner department", req.OwnerID); msg != "" {
		return msg
	}
	if h.mdNameTaken(c, mdTableInitiatives, "name_en", req.NameEn, self, "goal_id", &goalID) {
		return "dup:" + fmt.Sprintf("Initiative %q already exists for this strategic goal.", req.NameEn)
	}
	return ""
}

func (h *KpiMasterDataHandler) CreateInitiative(c *fiber.Ctx) error {
	var req models.InitiativeRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if msg := h.validateInitiative(c, &req, uuid.Nil); msg != "" {
		return mdValidationResponse(c, msg)
	}
	status := req.Status
	if status == "" {
		status = models.InitiativeStatusDraft
	}
	item := &models.Initiative{
		NameEn:      req.NameEn,
		NameAr:      req.NameAr,
		GoalID:      &req.GoalID,
		ObjectiveID: req.ObjectiveID,
		PillarID:    req.PillarID,
		EnablerID:   req.EnablerID,
		OwnerID:     req.OwnerID,
		Status:      status,
	}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return mdWriteError(c, err, "Initiative", req.NameEn, "failed_to_create")
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdateInitiative(c *fiber.Ctx) error {
	id, ok, resp := mdParseID(c)
	if !ok {
		return resp
	}
	var req models.InitiativeRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if !h.mdExists(c, mdTableInitiatives, id) {
		return mdNotFound(c)
	}
	if msg := h.validateInitiative(c, &req, id); msg != "" {
		return mdValidationResponse(c, msg)
	}
	updates := map[string]interface{}{
		"name_en":      req.NameEn,
		"name_ar":      req.NameAr,
		"goal_id":      req.GoalID,
		"objective_id": req.ObjectiveID,
		"pillar_id":    req.PillarID,
		"enabler_id":   req.EnablerID,
		"owner_id":     req.OwnerID,
	}
	if req.Status != "" {
		updates["status"] = req.Status
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.Initiative{ID: id}).Updates(updates)
	if result.Error != nil {
		return mdWriteError(c, result.Error, "Initiative", req.NameEn, "failed_to_update")
	}
	if result.RowsAffected == 0 {
		return mdNotFound(c)
	}
	var item models.Initiative
	h.db.WithContext(c.UserContext()).Preload("Goal").Preload("Objective").Preload("Owner").First(&item, id)
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteInitiative(c *fiber.Ctx) error {
	return h.mdDelete(c, &models.Initiative{}, mdTableInitiatives, "initiative", nil)
}

// ─── Domains ──────────────────────────────────────────────────────────────────

func (h *KpiMasterDataHandler) ListDomains(c *fiber.Ctx) error {
	var items []models.Domain
	if err := h.db.WithContext(c.UserContext()).Order("created_at DESC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	resp := make([]models.DomainResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

func (h *KpiMasterDataHandler) CreateDomain(c *fiber.Ctx) error {
	var req models.DomainRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if msg := mdCleanNames(&req.NameEn, &req.NameAr); msg != "" {
		return mdBadRequest(c, msg)
	}
	if h.mdNameTaken(c, mdTableDomains, "name_en", req.NameEn, uuid.Nil, "", nil) {
		return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "domain_already_exists"))
	}
	item := &models.Domain{NameEn: req.NameEn, NameAr: req.NameAr, Type: req.Type}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return mdWriteError(c, err, "Domain", req.NameEn, "failed_to_create")
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdateDomain(c *fiber.Ctx) error {
	id, ok, resp := mdParseID(c)
	if !ok {
		return resp
	}
	var req models.DomainRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if !h.mdExists(c, mdTableDomains, id) {
		return mdNotFound(c)
	}
	if msg := mdCleanNames(&req.NameEn, &req.NameAr); msg != "" {
		return mdBadRequest(c, msg)
	}
	if h.mdNameTaken(c, mdTableDomains, "name_en", req.NameEn, id, "", nil) {
		return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "domain_already_exists"))
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.Domain{ID: id}).Updates(map[string]interface{}{
		"name_en": req.NameEn,
		"name_ar": req.NameAr,
		"type":    req.Type,
	})
	if result.Error != nil {
		return mdWriteError(c, result.Error, "Domain", req.NameEn, "failed_to_update")
	}
	if result.RowsAffected == 0 {
		return mdNotFound(c)
	}
	var domain models.Domain
	if err := h.db.WithContext(c.UserContext()).First(&domain, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", domain.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteDomain(c *fiber.Ctx) error {
	return h.mdDelete(c, &models.Domain{}, mdTableDomains, "domain", kpiTaxonomyDependants("domain_id"))
}

// ─── Award Criteria ───────────────────────────────────────────────────────────

func (h *KpiMasterDataHandler) ListAwardCriteria(c *fiber.Ctx) error {
	var items []models.AwardCriterion
	if err := h.db.WithContext(c.UserContext()).Order("criterion_no ASC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	resp := make([]models.AwardCriterionResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

func (h *KpiMasterDataHandler) CreateAwardCriterion(c *fiber.Ctx) error {
	var req models.AwardCriterionRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if msg := mdCleanNames(&req.NameEn, &req.NameAr); msg != "" {
		return mdBadRequest(c, msg)
	}
	if req.CriterionNo <= 0 {
		return mdBadRequest(c, "Criterion No. must be a positive whole number.")
	}
	held, err := h.awardCriterionByNo(c, req.CriterionNo, uuid.Nil)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}
	if held != nil && !held.DeletedAt.Valid {
		return utils.ErrorResponse(c, fiber.StatusConflict, fmt.Sprintf("Criterion No. %d already exists (%q)", req.CriterionNo, held.NameEn))
	}
	if held != nil {
		// criterion_no has a plain (non-partial) unique index, so a
		// soft-deleted criterion keeps holding its number. Re-creating that
		// number restores the deleted row (same ID) with the new names.
		res := h.db.WithContext(c.UserContext()).Unscoped().Model(&models.AwardCriterion{}).Where("id = ?", held.ID).
			Updates(map[string]interface{}{"name_en": req.NameEn, "name_ar": req.NameAr, "is_active": true, "deleted_at": nil})
		if res.Error != nil {
			return mdWriteError(c, res.Error, "Award criterion", strconv.Itoa(req.CriterionNo), "failed_to_create")
		}
		var item models.AwardCriterion
		h.db.WithContext(c.UserContext()).First(&item, "id = ?", held.ID)
		return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
	}
	item := &models.AwardCriterion{CriterionNo: req.CriterionNo, NameEn: req.NameEn, NameAr: req.NameAr}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return mdWriteError(c, err, "Award criterion", strconv.Itoa(req.CriterionNo), "failed_to_create")
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdateAwardCriterion(c *fiber.Ctx) error {
	id, ok, resp := mdParseID(c)
	if !ok {
		return resp
	}
	var req models.AwardCriterionRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	var current models.AwardCriterion
	if err := h.db.WithContext(c.UserContext()).First(&current, "id = ?", id).Error; err != nil {
		return mdNotFound(c)
	}
	if msg := mdCleanNames(&req.NameEn, &req.NameAr); msg != "" {
		return mdBadRequest(c, msg)
	}
	if req.CriterionNo <= 0 {
		return mdBadRequest(c, "Criterion No. must be a positive whole number.")
	}
	if msg := h.awardCriterionNoConflict(c, req.CriterionNo, id); msg != "" {
		return utils.ErrorResponse(c, fiber.StatusConflict, msg)
	}
	if req.CriterionNo != current.CriterionNo {
		// Sub-criteria numbers are prefixed with the criterion number.
		var n int64
		h.db.WithContext(c.UserContext()).Table(mdTableAwardSubCriteria).
			Where("award_criterion_id = ? AND deleted_at IS NULL", id).Count(&n)
		if n > 0 {
			return utils.ErrorResponse(c, fiber.StatusConflict,
				fmt.Sprintf("Criterion No. cannot be changed while %d sub-criteria are numbered under %d.", n, current.CriterionNo))
		}
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.AwardCriterion{ID: id}).Updates(map[string]interface{}{
		"criterion_no": req.CriterionNo,
		"name_en":      req.NameEn,
		"name_ar":      req.NameAr,
	})
	if result.Error != nil {
		return mdWriteError(c, result.Error, "Award criterion", strconv.Itoa(req.CriterionNo), "failed_to_update")
	}
	if result.RowsAffected == 0 {
		return mdNotFound(c)
	}
	var item models.AwardCriterion
	h.db.WithContext(c.UserContext()).First(&item, id)
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

// awardCriterionByNo returns any criterion (including soft-deleted) other
// than selfID that holds criterionNo, or nil.
func (h *KpiMasterDataHandler) awardCriterionByNo(c *fiber.Ctx, criterionNo int, selfID uuid.UUID) (*models.AwardCriterion, error) {
	var existing models.AwardCriterion
	err := h.db.WithContext(c.UserContext()).Unscoped().
		Where("criterion_no = ? AND id <> ?", criterionNo, selfID).
		Order("deleted_at IS NOT NULL").First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &existing, nil
}

// awardCriterionNoConflict reports a human-readable conflict when another
// award criterion already uses criterionNo. criterion_no is unique across
// soft-deleted rows too (plain unique index), so those are checked as well;
// re-creating a deleted number via POST restores that criterion instead.
func (h *KpiMasterDataHandler) awardCriterionNoConflict(c *fiber.Ctx, criterionNo int, selfID uuid.UUID) string {
	existing, err := h.awardCriterionByNo(c, criterionNo, selfID)
	if err != nil || existing == nil {
		return ""
	}
	if existing.DeletedAt.Valid {
		return fmt.Sprintf("Criterion No. %d is still reserved by a deleted award criterion (%q). Create a new criterion with this number to restore it.", criterionNo, existing.NameEn)
	}
	return fmt.Sprintf("Criterion No. %d already exists (%q)", criterionNo, existing.NameEn)
}

func (h *KpiMasterDataHandler) DeleteAwardCriterion(c *fiber.Ctx) error {
	deps := []mdDependant{
		{mdTableAwardSubCriteria, "award_criterion_id", "sub-criteria", true},
	}
	return h.mdDelete(c, &models.AwardCriterion{}, mdTableAwardCriteria, "award criterion", deps)
}

// ─── Award Sub Criteria ───────────────────────────────────────────────────────

func (h *KpiMasterDataHandler) ListAwardSubCriteria(c *fiber.Ctx) error {
	criterionIDStr := c.Params("criterionId")
	var items []models.AwardSubCriterion
	q := h.db.WithContext(c.UserContext()).Preload("AwardCriterion").Order("sub_no ASC")
	if criterionIDStr != "" {
		criterionID, err := uuid.Parse(criterionIDStr)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
		}
		q = q.Where("award_criterion_id = ?", criterionID)
	} else {
		// Hide sub-criteria whose parent criterion was deleted.
		q = q.Where("award_criterion_id IN (SELECT id FROM award_criterions WHERE deleted_at IS NULL)")
	}
	if err := q.Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	resp := make([]models.AwardSubCriterionResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

// validateSubCriterion checks the parent criterion exists, sub_no is
// "<criterion_no>.<n>" and unique (case-insensitive) within the criterion.
func (h *KpiMasterDataHandler) validateSubCriterion(c *fiber.Ctx, req *models.AwardSubCriterionRequest, self uuid.UUID) (string, int) {
	if msg := mdCleanNames(&req.NameEn, &req.NameAr); msg != "" {
		return msg, fiber.StatusBadRequest
	}
	req.SubNo = strings.TrimSpace(req.SubNo)
	if req.SubNo == "" {
		return "Sub No. is required.", fiber.StatusBadRequest
	}
	var crit models.AwardCriterion
	if err := h.db.WithContext(c.UserContext()).First(&crit, "id = ?", req.AwardCriterionID).Error; err != nil {
		return fmt.Sprintf("Award criterion %s was not found.", req.AwardCriterionID), fiber.StatusNotFound
	}
	if msg := services.KpiSubNoPrefixError(crit.CriterionNo, req.SubNo); msg != "" {
		return msg, fiber.StatusBadRequest
	}
	criterionID := req.AwardCriterionID
	if h.mdNameTaken(c, mdTableAwardSubCriteria, "sub_no", req.SubNo, self, "award_criterion_id", &criterionID) {
		return fmt.Sprintf("Sub No. %q already exists under criterion %d.", req.SubNo, crit.CriterionNo), fiber.StatusConflict
	}
	return "", 0
}

func (h *KpiMasterDataHandler) CreateAwardSubCriterion(c *fiber.Ctx) error {
	var req models.AwardSubCriterionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	// The route is /award-criteria/:criterionId/sub-criteria — the path is
	// authoritative; a conflicting body value is rejected.
	if pathID := c.Params("criterionId"); pathID != "" {
		criterionID, err := uuid.Parse(pathID)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
		}
		if req.AwardCriterionID != uuid.Nil && req.AwardCriterionID != criterionID {
			return mdBadRequest(c, "award_criterion_id in the body does not match the criterion in the URL.")
		}
		req.AwardCriterionID = criterionID
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	if msg, status := h.validateSubCriterion(c, &req, uuid.Nil); msg != "" {
		return utils.ErrorResponse(c, status, msg)
	}
	item := &models.AwardSubCriterion{
		AwardCriterionID: req.AwardCriterionID,
		SubNo:            req.SubNo,
		NameEn:           req.NameEn,
		NameAr:           req.NameAr,
	}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return mdWriteError(c, err, "Award sub-criterion", req.SubNo, "failed_to_create")
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdateAwardSubCriterion(c *fiber.Ctx) error {
	id, ok, resp := mdParseID(c)
	if !ok {
		return resp
	}
	var req models.AwardSubCriterionRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if !h.mdExists(c, mdTableAwardSubCriteria, id) {
		return mdNotFound(c)
	}
	if msg, status := h.validateSubCriterion(c, &req, id); msg != "" {
		return utils.ErrorResponse(c, status, msg)
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.AwardSubCriterion{ID: id}).Updates(map[string]interface{}{
		"award_criterion_id": req.AwardCriterionID,
		"sub_no":             req.SubNo,
		"name_en":            req.NameEn,
		"name_ar":            req.NameAr,
	})
	if result.Error != nil {
		return mdWriteError(c, result.Error, "Award sub-criterion", req.SubNo, "failed_to_update")
	}
	if result.RowsAffected == 0 {
		return mdNotFound(c)
	}
	var item models.AwardSubCriterion
	h.db.WithContext(c.UserContext()).Preload("AwardCriterion").First(&item, id)
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteAwardSubCriterion(c *fiber.Ctx) error {
	return h.mdDelete(c, &models.AwardSubCriterion{}, mdTableAwardSubCriteria, "award sub-criterion", kpiTaxonomyDependants("award_sub_criterion_id"))
}

// ─── Data Sources ─────────────────────────────────────────────────────────────

func (h *KpiMasterDataHandler) ListDataSources(c *fiber.Ctx) error {
	var items []models.KpiDataSource
	if err := h.db.WithContext(c.UserContext()).Order("name_en ASC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	resp := make([]models.KpiDataSourceResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

func (h *KpiMasterDataHandler) CreateDataSource(c *fiber.Ctx) error {
	var req models.KpiDataSourceRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if msg := mdCleanNames(&req.NameEn, &req.NameAr); msg != "" {
		return mdBadRequest(c, msg)
	}
	if h.mdNameTaken(c, mdTableDataSources, "name_en", req.NameEn, uuid.Nil, "", nil) {
		return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "data_source_already_exists"))
	}
	item := &models.KpiDataSource{NameEn: req.NameEn, NameAr: req.NameAr}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return mdWriteError(c, err, "Data source", req.NameEn, "failed_to_create")
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdateDataSource(c *fiber.Ctx) error {
	id, ok, resp := mdParseID(c)
	if !ok {
		return resp
	}
	var req models.KpiDataSourceRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if !h.mdExists(c, mdTableDataSources, id) {
		return mdNotFound(c)
	}
	if msg := mdCleanNames(&req.NameEn, &req.NameAr); msg != "" {
		return mdBadRequest(c, msg)
	}
	if h.mdNameTaken(c, mdTableDataSources, "name_en", req.NameEn, id, "", nil) {
		return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "data_source_already_exists"))
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.KpiDataSource{ID: id}).Updates(map[string]interface{}{
		"name_en": req.NameEn,
		"name_ar": req.NameAr,
	})
	if result.Error != nil {
		return mdWriteError(c, result.Error, "Data source", req.NameEn, "failed_to_update")
	}
	if result.RowsAffected == 0 {
		return mdNotFound(c)
	}
	var item models.KpiDataSource
	if err := h.db.WithContext(c.UserContext()).First(&item, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteDataSource(c *fiber.Ctx) error {
	return h.mdDelete(c, &models.KpiDataSource{}, mdTableDataSources, "data source", nil)
}

// ─── Segmentation Dimensions ──────────────────────────────────────────────────

func (h *KpiMasterDataHandler) ListSegmentationDimensions(c *fiber.Ctx) error {
	var items []models.KpiSegmentationDimension
	if err := h.db.WithContext(c.UserContext()).Order("name_en ASC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	resp := make([]models.KpiSegmentationDimensionResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

// deletedDimensionByName returns a soft-deleted dimension whose name_en equals
// name exactly. kpi_segmentation_dimensions.name_en has a plain unique index
// (it ignores deleted_at), so such a row blocks re-using the name.
func (h *KpiMasterDataHandler) deletedDimensionByName(c *fiber.Ctx, name string) *models.KpiSegmentationDimension {
	var d models.KpiSegmentationDimension
	err := h.db.WithContext(c.UserContext()).Unscoped().
		Where("name_en = ? AND deleted_at IS NOT NULL", name).First(&d).Error
	if err != nil {
		return nil
	}
	return &d
}

func (h *KpiMasterDataHandler) CreateSegmentationDimension(c *fiber.Ctx) error {
	var req models.KpiSegmentationDimensionRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if msg := mdCleanNames(&req.NameEn, &req.NameAr); msg != "" {
		return mdBadRequest(c, msg)
	}
	if h.mdNameTaken(c, mdTableSegmentationDims, "name_en", req.NameEn, uuid.Nil, "", nil) {
		return utils.ErrorResponse(c, fiber.StatusConflict, mdDuplicateMessage("Segmentation dimension", req.NameEn))
	}
	if deleted := h.deletedDimensionByName(c, req.NameEn); deleted != nil {
		// Restore the soft-deleted dimension instead of failing on the
		// non-partial unique index.
		res := h.db.WithContext(c.UserContext()).Unscoped().Model(&models.KpiSegmentationDimension{}).Where("id = ?", deleted.ID).
			Updates(map[string]interface{}{"name_ar": req.NameAr, "is_active": true, "deleted_at": nil})
		if res.Error != nil {
			return mdWriteError(c, res.Error, "Segmentation dimension", req.NameEn, "failed_to_create")
		}
		var item models.KpiSegmentationDimension
		h.db.WithContext(c.UserContext()).First(&item, "id = ?", deleted.ID)
		return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
	}
	item := &models.KpiSegmentationDimension{NameEn: req.NameEn, NameAr: req.NameAr}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return mdWriteError(c, err, "Segmentation dimension", req.NameEn, "failed_to_create")
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdateSegmentationDimension(c *fiber.Ctx) error {
	id, ok, resp := mdParseID(c)
	if !ok {
		return resp
	}
	var req models.KpiSegmentationDimensionRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if !h.mdExists(c, mdTableSegmentationDims, id) {
		return mdNotFound(c)
	}
	if msg := mdCleanNames(&req.NameEn, &req.NameAr); msg != "" {
		return mdBadRequest(c, msg)
	}
	if h.mdNameTaken(c, mdTableSegmentationDims, "name_en", req.NameEn, id, "", nil) {
		return utils.ErrorResponse(c, fiber.StatusConflict, mdDuplicateMessage("Segmentation dimension", req.NameEn))
	}
	if deleted := h.deletedDimensionByName(c, req.NameEn); deleted != nil && deleted.ID != id {
		return utils.ErrorResponse(c, fiber.StatusConflict,
			fmt.Sprintf("Segmentation dimension name %q is reserved by a deleted dimension. Create it again to restore that dimension.", req.NameEn))
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.KpiSegmentationDimension{ID: id}).Updates(map[string]interface{}{
		"name_en": req.NameEn,
		"name_ar": req.NameAr,
	})
	if result.Error != nil {
		return mdWriteError(c, result.Error, "Segmentation dimension", req.NameEn, "failed_to_update")
	}
	if result.RowsAffected == 0 {
		return mdNotFound(c)
	}
	var item models.KpiSegmentationDimension
	if err := h.db.WithContext(c.UserContext()).First(&item, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteSegmentationDimension(c *fiber.Ctx) error {
	deps := []mdDependant{{mdTableSegmentationAxes, "dimension_id", "KPI segmentation axis link(s)", false}}
	return h.mdDelete(c, &models.KpiSegmentationDimension{}, mdTableSegmentationDims, "segmentation dimension", deps)
}

// ─── Organizations (external KPI owners) ─────────────────────────────────────

func (h *KpiMasterDataHandler) ListOrganizations(c *fiber.Ctx) error {
	var items []models.KpiOrganization
	if err := h.db.WithContext(c.UserContext()).Order("name_en ASC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	resp := make([]models.KpiOrganizationResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

func (h *KpiMasterDataHandler) validateOrganization(c *fiber.Ctx, req *models.KpiOrganizationRequest, self uuid.UUID) string {
	if msg := mdCleanNames(&req.NameEn, &req.NameAr); msg != "" {
		return msg
	}
	req.ContactInfo = strings.TrimSpace(req.ContactInfo)
	if utf8.RuneCountInString(req.ContactInfo) > mdMaxNameLen {
		return "Contact Info must be at most 255 characters."
	}
	if h.mdNameTaken(c, mdTableOrganizations, "name_en", req.NameEn, self, "", nil) {
		return "dup:" + mdDuplicateMessage("Organization", req.NameEn)
	}
	return ""
}

func (h *KpiMasterDataHandler) CreateOrganization(c *fiber.Ctx) error {
	var req models.KpiOrganizationRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if msg := h.validateOrganization(c, &req, uuid.Nil); msg != "" {
		return mdValidationResponse(c, msg)
	}
	item := &models.KpiOrganization{NameEn: req.NameEn, NameAr: req.NameAr, ContactInfo: req.ContactInfo}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return mdWriteError(c, err, "Organization", req.NameEn, "failed_to_create")
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdateOrganization(c *fiber.Ctx) error {
	id, ok, resp := mdParseID(c)
	if !ok {
		return resp
	}
	var req models.KpiOrganizationRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if !h.mdExists(c, mdTableOrganizations, id) {
		return mdNotFound(c)
	}
	if msg := h.validateOrganization(c, &req, id); msg != "" {
		return mdValidationResponse(c, msg)
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.KpiOrganization{ID: id}).Updates(map[string]interface{}{
		"name_en":      req.NameEn,
		"name_ar":      req.NameAr,
		"contact_info": req.ContactInfo,
	})
	if result.Error != nil {
		return mdWriteError(c, result.Error, "Organization", req.NameEn, "failed_to_update")
	}
	if result.RowsAffected == 0 {
		return mdNotFound(c)
	}
	var item models.KpiOrganization
	if err := h.db.WithContext(c.UserContext()).First(&item, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteOrganization(c *fiber.Ctx) error {
	deps := []mdDependant{
		{mdTableKpiAnnualTargets, "organization_id", "KPI annual target(s)", true},
		{mdTableKpiEntries, "organization_id", "KPI entry/entries", true},
	}
	return h.mdDelete(c, &models.KpiOrganization{}, mdTableOrganizations, "organization", deps)
}

// ─── Per-KPI link helpers ────────────────────────────────────────────────────

// mdKpiTable maps a KPI dictionary type to its table.
func mdKpiTable(kpiType string) string {
	switch kpiType {
	case models.KPITypeOperational:
		return mdTableOperationalKpis
	case models.KPITypeAward:
		return mdTableAwardKpis
	default:
		return mdTableStrategicKpis
	}
}

// mdKpiPath parses and validates :type and :id. ok=false means a response was
// already written (400 for a bad type/id, 404 when the KPI does not exist).
func (h *KpiMasterDataHandler) mdKpiPath(c *fiber.Ctx) (string, uuid.UUID, bool, error) {
	kpiType := c.Params("type")
	if !isValidKpiType(kpiType) {
		return "", uuid.Nil, false, mdBadRequest(c, fmt.Sprintf("Unknown KPI type %q (expected strategic, operational or award).", kpiType))
	}
	kpiID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return "", uuid.Nil, false, utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	if !h.mdExists(c, mdKpiTable(kpiType), kpiID) {
		return "", uuid.Nil, false, utils.ErrorResponse(c, fiber.StatusNotFound, fmt.Sprintf("%s KPI %s was not found.", kpiType, kpiID))
	}
	return kpiType, kpiID, true, nil
}

// ─── Segmentation Axes (structured, per-KPI) ─────────────────────────────────
// Links a KPI dictionary row to one or more governed segmentation dimensions.
// GET/POST /kpi/:type/:id/segmentation-axes, DELETE /kpi/segmentation-axes/:id

func (h *KpiMasterDataHandler) ListSegmentationAxes(c *fiber.Ctx) error {
	kpiType := c.Params("type")
	if !isValidKpiType(kpiType) {
		return mdBadRequest(c, fmt.Sprintf("Unknown KPI type %q (expected strategic, operational or award).", kpiType))
	}
	kpiID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var items []models.KpiSegmentationAxis
	if err := h.db.WithContext(c.UserContext()).Preload("Dimension").
		Where("kpi_id = ? AND kpi_type = ?", kpiID, kpiType).
		Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	resp := make([]models.KpiSegmentationAxisResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

func (h *KpiMasterDataHandler) AddSegmentationAxis(c *fiber.Ctx) error {
	kpiType, kpiID, ok, resp := h.mdKpiPath(c)
	if !ok {
		return resp
	}
	var req models.KpiSegmentationAxisRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if !h.mdExists(c, mdTableSegmentationDims, req.DimensionID) {
		return mdBadRequest(c, fmt.Sprintf("Segmentation dimension %s was not found.", req.DimensionID))
	}
	var n int64
	h.db.WithContext(c.UserContext()).Model(&models.KpiSegmentationAxis{}).
		Where("kpi_id = ? AND kpi_type = ? AND dimension_id = ?", kpiID, kpiType, req.DimensionID).Count(&n)
	if n > 0 {
		return utils.ErrorResponse(c, fiber.StatusConflict, "This segmentation dimension is already linked to the KPI.")
	}
	item := &models.KpiSegmentationAxis{KpiID: kpiID, KpiType: kpiType, DimensionID: req.DimensionID}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		if mdIsUniqueViolation(err) {
			return utils.ErrorResponse(c, fiber.StatusConflict, "This segmentation dimension is already linked to the KPI.")
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}
	h.db.WithContext(c.UserContext()).Preload("Dimension").First(item, item.ID)
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteSegmentationAxis(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	result := h.db.WithContext(c.UserContext()).Delete(&models.KpiSegmentationAxis{}, id)
	if result.Error != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_delete"))
	}
	if result.RowsAffected == 0 {
		return mdNotFound(c)
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

// ─── Administrative Units (structured, per-KPI) ──────────────────────────────
// Links a KPI dictionary row to one or more related Departments.
// GET/POST /kpi/:type/:id/administrative-units, DELETE /kpi/administrative-units/:id

func (h *KpiMasterDataHandler) ListAdministrativeUnits(c *fiber.Ctx) error {
	kpiType := c.Params("type")
	if !isValidKpiType(kpiType) {
		return mdBadRequest(c, fmt.Sprintf("Unknown KPI type %q (expected strategic, operational or award).", kpiType))
	}
	kpiID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var items []models.KpiAdministrativeUnit
	if err := h.db.WithContext(c.UserContext()).Preload("Department").
		Where("kpi_id = ? AND kpi_type = ?", kpiID, kpiType).
		Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	resp := make([]models.KpiAdministrativeUnitResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

func (h *KpiMasterDataHandler) AddAdministrativeUnit(c *fiber.Ctx) error {
	kpiType, kpiID, ok, resp := h.mdKpiPath(c)
	if !ok {
		return resp
	}
	var req models.KpiAdministrativeUnitRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if !h.mdExists(c, mdTableDepartments, req.DepartmentID) {
		return mdBadRequest(c, fmt.Sprintf("Department %s was not found.", req.DepartmentID))
	}
	var n int64
	h.db.WithContext(c.UserContext()).Model(&models.KpiAdministrativeUnit{}).
		Where("kpi_id = ? AND kpi_type = ? AND department_id = ?", kpiID, kpiType, req.DepartmentID).Count(&n)
	if n > 0 {
		return utils.ErrorResponse(c, fiber.StatusConflict, "This department is already linked to the KPI.")
	}
	item := &models.KpiAdministrativeUnit{KpiID: kpiID, KpiType: kpiType, DepartmentID: req.DepartmentID}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		if mdIsUniqueViolation(err) {
			return utils.ErrorResponse(c, fiber.StatusConflict, "This department is already linked to the KPI.")
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}
	h.db.WithContext(c.UserContext()).Preload("Department").First(item, item.ID)
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteAdministrativeUnit(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	result := h.db.WithContext(c.UserContext()).Delete(&models.KpiAdministrativeUnit{}, id)
	if result.Error != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_delete"))
	}
	if result.RowsAffected == 0 {
		return mdNotFound(c)
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

// ─── Award KPIs and Measures ──────────────────────────────────────────────────

func (h *KpiMasterDataHandler) ListAwardKpiMeasures(c *fiber.Ctx) error {
	var items []models.AwardKpiMeasure
	if err := h.db.WithContext(c.UserContext()).Select("id", "code", "name_en", "name_ar", "version").Order("code ASC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	resp := make([]models.AwardKpiMeasureResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

func (h *KpiMasterDataHandler) GetAwardKpiMeasure(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var item models.AwardKpiMeasure
	if err := h.db.WithContext(c.UserContext()).Select("id", "code", "name_en", "name_ar", "version").First(&item, "id = ?", id).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) CreateAwardKpiMeasure(c *fiber.Ctx) error {
	var req models.AwardKpiMeasureRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if msg := mdCleanNames(&req.NameEn, &req.NameAr); msg != "" {
		return mdBadRequest(c, msg)
	}
	if h.mdNameTaken(c, mdTableAwardKpiMeasures, "name_en", req.NameEn, uuid.Nil, "", nil) {
		return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "award_kpi_measure_already_exists"))
	}
	// code is left empty so Postgres assigns it from award_kpi_measure_code_seq.
	item := &models.AwardKpiMeasure{NameEn: req.NameEn, NameAr: req.NameAr}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return mdWriteError(c, err, "Award KPI measure", req.NameEn, "failed_to_create")
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdateAwardKpiMeasure(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var req models.AwardKpiMeasureUpdateRequest
	if ok, resp := mdParseBody(c, &req); !ok {
		return resp
	}
	if !h.mdExists(c, mdTableAwardKpiMeasures, id) {
		return mdNotFound(c)
	}
	if msg := mdCleanNames(&req.NameEn, &req.NameAr); msg != "" {
		return mdBadRequest(c, msg)
	}
	if h.mdNameTaken(c, mdTableAwardKpiMeasures, "name_en", req.NameEn, id, "", nil) {
		return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "award_kpi_measure_already_exists"))
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.AwardKpiMeasure{}).
		Where("id = ? AND version = ?", id, req.Version).
		Updates(map[string]interface{}{
			"name_en": req.NameEn,
			"name_ar": req.NameAr,
			"version": gorm.Expr("version + 1"),
		})
	if result.Error != nil {
		return mdWriteError(c, result.Error, "Award KPI measure", req.NameEn, "failed_to_update")
	}
	if result.RowsAffected == 0 {
		// Zero rows: either the record is gone (404) or the version is stale (409).
		var n int64
		h.db.WithContext(c.UserContext()).Model(&models.AwardKpiMeasure{}).Where("id = ?", id).Count(&n)
		if n > 0 {
			return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "award_kpi_measure_version_conflict"))
		}
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var item models.AwardKpiMeasure
	if err := h.db.WithContext(c.UserContext()).First(&item, "id = ?", id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteAwardKpiMeasure(c *fiber.Ctx) error {
	return h.mdDelete(c, &models.AwardKpiMeasure{}, mdTableAwardKpiMeasures, "award KPI measure", kpiTaxonomyDependants("award_kpi_measure_id"))
}
