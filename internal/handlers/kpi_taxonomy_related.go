package handlers

import (
	"sort"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// "Related Details" tabs (Related KPIs / Collaborators / Evidence) for the
// taxonomy entities: Operational Objective, Process (Child Objective), Award
// Criterion and Award Sub-Criterion.
//
// Objective, Process and Award Sub-Criterion are optional tags on EVERY KPI
// type (Strategic, Operational and Award all carry operational_objective_id,
// process_id and award_sub_criterion_id), so each lookup here must span all
// three KPI tables — querying only the "home" table for the entity silently
// drops KPIs of the other types (e.g. a Strategic KPI filed under an
// Objective showed "Related KPIs: 0").

// relatedKpiIDs holds the IDs of KPIs linked to a taxonomy entity, keyed by
// kpi_type (models.KPIType*).
type relatedKpiIDs map[string][]uuid.UUID

func (ids relatedKpiIDs) empty() bool {
	for _, v := range ids {
		if len(v) > 0 {
			return false
		}
	}
	return true
}

// scope narrows a kpi_type/kpi_id-keyed table (collaborator assignments,
// evidence) to rows belonging to any of the linked KPIs. Callers must check
// empty() first.
func (ids relatedKpiIDs) scope(db *gorm.DB) *gorm.DB {
	cond := db.Session(&gorm.Session{NewDB: true})
	for _, kpiType := range []string{models.KPITypeStrategic, models.KPITypeOperational, models.KPITypeAward} {
		if len(ids[kpiType]) > 0 {
			cond = cond.Or("kpi_type = ? AND kpi_id IN ?", kpiType, ids[kpiType])
		}
	}
	return db.Where(cond)
}

// relatedKpiResponse is the common summary of a KPI of any type, as rendered
// by the frontend RelatedKpisTable. KpiType lets the row link to the right
// dictionary detail page.
type relatedKpiResponse struct {
	KpiType            string                          `json:"kpi_type"`
	ID                 uuid.UUID                       `json:"id"`
	Code               string                          `json:"code"`
	NameEn             string                          `json:"name_en"`
	DescriptionEn      string                          `json:"description_en"`
	ActivationStatus   string                          `json:"activation_status"`
	ReportingFrequency string                          `json:"reporting_frequency"`
	OwnerDept          *models.DepartmentBriefResponse `json:"owner_dept,omitempty"`
	OwnerOrg           *models.KpiOrganizationResponse `json:"owner_org,omitempty"`
	OwningAgency       *models.DepartmentBriefResponse `json:"owning_agency,omitempty"`
	CreatedBy          *models.UserBrief               `json:"created_by,omitempty"`
	UpdatedAt          time.Time                       `json:"updated_at"`
}

// findRelatedKpiIDs returns every KPI, of any type, matching the given
// condition on the shared taxonomy FK columns.
func (h *KpiMasterDataHandler) findRelatedKpiIDs(c *fiber.Ctx, query string, args ...interface{}) (relatedKpiIDs, error) {
	tables := map[string]interface{}{
		models.KPITypeStrategic:   &models.StrategicKPI{},
		models.KPITypeOperational: &models.OperationalKPI{},
		models.KPITypeAward:       &models.AwardKPI{},
	}
	ids := relatedKpiIDs{}
	for kpiType, model := range tables {
		var found []uuid.UUID
		if err := h.db.WithContext(c.UserContext()).Model(model).
			Where(query, args...).Pluck("id", &found).Error; err != nil {
			return nil, err
		}
		ids[kpiType] = found
	}
	return ids, nil
}

// kpiIDsForObjective returns every KPI linked to the given Objective —
// directly (operational_objective_id) OR indirectly, via any of the
// Objective's child Processes (process_id). A KPI's operational_objective_id
// is expected to match its Process's own parent, but the create/edit form
// lets the two be picked independently, so both are checked to avoid
// silently missing KPIs only reachable via their Process.
func (h *KpiMasterDataHandler) kpiIDsForObjective(c *fiber.Ctx, objectiveID uuid.UUID) (relatedKpiIDs, error) {
	var childProcessIDs []uuid.UUID
	if err := h.db.WithContext(c.UserContext()).Model(&models.Process{}).
		Where("operational_objective_id = ?", objectiveID).Pluck("id", &childProcessIDs).Error; err != nil {
		return nil, err
	}
	if len(childProcessIDs) == 0 {
		return h.findRelatedKpiIDs(c, "operational_objective_id = ?", objectiveID)
	}
	return h.findRelatedKpiIDs(c, "operational_objective_id = ? OR process_id IN ?", objectiveID, childProcessIDs)
}

func (h *KpiMasterDataHandler) kpiIDsForProcess(c *fiber.Ctx, processID uuid.UUID) (relatedKpiIDs, error) {
	return h.findRelatedKpiIDs(c, "process_id = ?", processID)
}

// kpiIDsForAwardCriterion reaches KPIs via the criterion's child
// Sub-Criteria — KPIs only store award_sub_criterion_id.
func (h *KpiMasterDataHandler) kpiIDsForAwardCriterion(c *fiber.Ctx, criterionID uuid.UUID) (relatedKpiIDs, error) {
	var subCriteriaIDs []uuid.UUID
	if err := h.db.WithContext(c.UserContext()).Model(&models.AwardSubCriterion{}).
		Where("award_criterion_id = ?", criterionID).Pluck("id", &subCriteriaIDs).Error; err != nil {
		return nil, err
	}
	if len(subCriteriaIDs) == 0 {
		return relatedKpiIDs{}, nil
	}
	return h.findRelatedKpiIDs(c, "award_sub_criterion_id IN ?", subCriteriaIDs)
}

func (h *KpiMasterDataHandler) kpiIDsForAwardSubCriterion(c *fiber.Ctx, subCriterionID uuid.UUID) (relatedKpiIDs, error) {
	return h.findRelatedKpiIDs(c, "award_sub_criterion_id = ?", subCriterionID)
}

// loadRelatedKpis loads the linked KPIs of every type as common summaries,
// ordered by code.
func (h *KpiMasterDataHandler) loadRelatedKpis(c *fiber.Ctx, ids relatedKpiIDs) ([]relatedKpiResponse, error) {
	resp := []relatedKpiResponse{}
	base := func() *gorm.DB {
		return h.db.WithContext(c.UserContext()).
			Preload("OwnerDept").Preload("OwnerOrg").Preload("OwningAgency")
	}

	if len(ids[models.KPITypeStrategic]) > 0 {
		var kpis []models.StrategicKPI
		if err := base().Where("id IN ?", ids[models.KPITypeStrategic]).Find(&kpis).Error; err != nil {
			return nil, err
		}
		for i := range kpis {
			r := kpis[i].ToResponse()
			resp = append(resp, relatedKpiResponse{
				KpiType: models.KPITypeStrategic, ID: r.ID, Code: r.Code, NameEn: r.NameEn,
				DescriptionEn: r.DescriptionEn, ActivationStatus: r.ActivationStatus,
				ReportingFrequency: r.ReportingFrequency, OwnerDept: r.OwnerDept,
				OwnerOrg: r.OwnerOrg, OwningAgency: r.OwningAgency, UpdatedAt: r.UpdatedAt,
			})
		}
	}
	if len(ids[models.KPITypeOperational]) > 0 {
		var kpis []models.OperationalKPI
		if err := base().Preload("WorkflowInstance.InitiatedBy").
			Where("id IN ?", ids[models.KPITypeOperational]).Find(&kpis).Error; err != nil {
			return nil, err
		}
		for i := range kpis {
			r := kpis[i].ToResponse()
			resp = append(resp, relatedKpiResponse{
				KpiType: models.KPITypeOperational, ID: r.ID, Code: r.Code, NameEn: r.NameEn,
				DescriptionEn: r.DescriptionEn, ActivationStatus: r.ActivationStatus,
				ReportingFrequency: r.ReportingFrequency, OwnerDept: r.OwnerDept,
				OwnerOrg: r.OwnerOrg, OwningAgency: r.OwningAgency, CreatedBy: r.CreatedBy,
				UpdatedAt: r.UpdatedAt,
			})
		}
	}
	if len(ids[models.KPITypeAward]) > 0 {
		var kpis []models.AwardKPI
		if err := base().Preload("WorkflowInstance.InitiatedBy").
			Where("id IN ?", ids[models.KPITypeAward]).Find(&kpis).Error; err != nil {
			return nil, err
		}
		for i := range kpis {
			r := kpis[i].ToResponse()
			resp = append(resp, relatedKpiResponse{
				KpiType: models.KPITypeAward, ID: r.ID, Code: r.Code, NameEn: r.NameEn,
				DescriptionEn: r.DescriptionEn, ActivationStatus: r.ActivationStatus,
				ReportingFrequency: r.ReportingFrequency, OwnerDept: r.OwnerDept,
				OwnerOrg: r.OwnerOrg, OwningAgency: r.OwningAgency, CreatedBy: r.CreatedBy,
				UpdatedAt: r.UpdatedAt,
			})
		}
	}

	sort.Slice(resp, func(i, j int) bool { return resp[i].Code < resp[j].Code })
	return resp, nil
}

// respondRelated resolves the entity ID from the route, finds its linked KPIs
// and renders the requested relationship (kpis / collaborators / evidence).
func (h *KpiMasterDataHandler) respondRelated(
	c *fiber.Ctx,
	resolve func(*fiber.Ctx, uuid.UUID) (relatedKpiIDs, error),
	render func(*fiber.Ctx, relatedKpiIDs) (interface{}, error),
) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	ids, err := resolve(c, id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	data, err := render(c, ids)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", data)
}

func (h *KpiMasterDataHandler) renderRelatedKpis(c *fiber.Ctx, ids relatedKpiIDs) (interface{}, error) {
	return h.loadRelatedKpis(c, ids)
}

func (h *KpiMasterDataHandler) renderRelatedCollaborators(c *fiber.Ctx, ids relatedKpiIDs) (interface{}, error) {
	resp := []models.KpiCollaboratorAssignmentResponse{}
	if ids.empty() {
		return resp, nil
	}
	var items []models.KpiCollaboratorAssignment
	if err := ids.scope(h.db.WithContext(c.UserContext()).Preload("User.Department")).
		Order("created_at ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	for i := range items {
		resp = append(resp, toAssignmentResponse(&items[i]))
	}
	return resp, nil
}

func (h *KpiMasterDataHandler) renderRelatedEvidence(c *fiber.Ctx, ids relatedKpiIDs) (interface{}, error) {
	items := []models.KpiEvidence{}
	if ids.empty() {
		return items, nil
	}
	if err := ids.scope(h.db.WithContext(c.UserContext()).Preload("UploadedBy").Preload("Metric")).
		Order("created_at DESC").Find(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

// ─── Operational Objective ────────────────────────────────────────────────────

func (h *KpiMasterDataHandler) ListKpisForOperationalObjective(c *fiber.Ctx) error {
	return h.respondRelated(c, h.kpiIDsForObjective, h.renderRelatedKpis)
}

func (h *KpiMasterDataHandler) ListCollaboratorsForOperationalObjective(c *fiber.Ctx) error {
	return h.respondRelated(c, h.kpiIDsForObjective, h.renderRelatedCollaborators)
}

func (h *KpiMasterDataHandler) ListEvidenceForOperationalObjective(c *fiber.Ctx) error {
	return h.respondRelated(c, h.kpiIDsForObjective, h.renderRelatedEvidence)
}

// ─── Process (Child Objective) ────────────────────────────────────────────────

func (h *KpiMasterDataHandler) ListKpisForProcess(c *fiber.Ctx) error {
	return h.respondRelated(c, h.kpiIDsForProcess, h.renderRelatedKpis)
}

func (h *KpiMasterDataHandler) ListCollaboratorsForProcess(c *fiber.Ctx) error {
	return h.respondRelated(c, h.kpiIDsForProcess, h.renderRelatedCollaborators)
}

func (h *KpiMasterDataHandler) ListEvidenceForProcess(c *fiber.Ctx) error {
	return h.respondRelated(c, h.kpiIDsForProcess, h.renderRelatedEvidence)
}

// ─── Award Criterion ──────────────────────────────────────────────────────────

func (h *KpiMasterDataHandler) ListKpisForAwardCriterion(c *fiber.Ctx) error {
	return h.respondRelated(c, h.kpiIDsForAwardCriterion, h.renderRelatedKpis)
}

func (h *KpiMasterDataHandler) ListCollaboratorsForAwardCriterion(c *fiber.Ctx) error {
	return h.respondRelated(c, h.kpiIDsForAwardCriterion, h.renderRelatedCollaborators)
}

func (h *KpiMasterDataHandler) ListEvidenceForAwardCriterion(c *fiber.Ctx) error {
	return h.respondRelated(c, h.kpiIDsForAwardCriterion, h.renderRelatedEvidence)
}

// ─── Award Sub-Criterion ──────────────────────────────────────────────────────

func (h *KpiMasterDataHandler) ListKpisForAwardSubCriterion(c *fiber.Ctx) error {
	return h.respondRelated(c, h.kpiIDsForAwardSubCriterion, h.renderRelatedKpis)
}

func (h *KpiMasterDataHandler) ListCollaboratorsForAwardSubCriterion(c *fiber.Ctx) error {
	return h.respondRelated(c, h.kpiIDsForAwardSubCriterion, h.renderRelatedCollaborators)
}

func (h *KpiMasterDataHandler) ListEvidenceForAwardSubCriterion(c *fiber.Ctx) error {
	return h.respondRelated(c, h.kpiIDsForAwardSubCriterion, h.renderRelatedEvidence)
}
