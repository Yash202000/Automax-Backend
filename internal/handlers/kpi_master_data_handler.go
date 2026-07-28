package handlers

import (
	"github.com/automax/backend/internal/models"
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

func (h *KpiMasterDataHandler) CreatePillar(c *fiber.Ctx) error {
	var req models.PillarRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	item := &models.Pillar{NameEn: req.NameEn, NameAr: req.NameAr, OwnerID: req.OwnerID}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdatePillar(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var req models.PillarRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.Pillar{ID: id}).Updates(map[string]interface{}{
		"name_en":  req.NameEn,
		"name_ar":  req.NameAr,
		"owner_id": req.OwnerID,
	})
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	if result.Error != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update"))
	}
	var item models.Pillar
	h.db.WithContext(c.UserContext()).Preload("Owner").First(&item, id)
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeletePillar(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	result := h.db.WithContext(c.UserContext()).Delete(&models.Pillar{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
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
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	item := &models.Enabler{NameEn: req.NameEn, NameAr: req.NameAr, OwnerID: req.OwnerID}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdateEnabler(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var req models.EnablerRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.Enabler{ID: id}).Updates(map[string]interface{}{
		"name_en":  req.NameEn,
		"name_ar":  req.NameAr,
		"owner_id": req.OwnerID,
	})
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var item models.Enabler
	h.db.WithContext(c.UserContext()).Preload("Owner").First(&item, id)
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteEnabler(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	result := h.db.WithContext(c.UserContext()).Delete(&models.Enabler{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

// ─── Operational Objectives ───────────────────────────────────────────────────

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

func (h *KpiMasterDataHandler) CreateOperationalObjective(c *fiber.Ctx) error {
	var req models.OperationalObjectiveRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	item := &models.OperationalObjective{
		NameEn:    req.NameEn,
		NameAr:    req.NameAr,
		GoalID:    &req.GoalID,
		PillarID:  req.PillarID,
		EnablerID: req.EnablerID,
	}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdateOperationalObjective(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var req models.OperationalObjectiveRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.OperationalObjective{ID: id}).Updates(map[string]interface{}{
		"name_en":    req.NameEn,
		"name_ar":    req.NameAr,
		"goal_id":    req.GoalID,
		"pillar_id":  req.PillarID,
		"enabler_id": req.EnablerID,
	})
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var item models.OperationalObjective
	h.db.WithContext(c.UserContext()).Preload("Goal").Preload("Pillar").Preload("Enabler").First(&item, id)
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteOperationalObjective(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	result := h.db.WithContext(c.UserContext()).Delete(&models.OperationalObjective{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

// ─── Processes ────────────────────────────────────────────────────────────────

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

func (h *KpiMasterDataHandler) CreateProcess(c *fiber.Ctx) error {
	var req models.ProcessRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
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
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdateProcess(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var req models.ProcessRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
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
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var item models.Process
	h.db.WithContext(c.UserContext()).Preload("OperationalObjective").Preload("Goal").Preload("Pillar").Preload("Enabler").Preload("Department").First(&item, id)
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteProcess(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	result := h.db.WithContext(c.UserContext()).Delete(&models.Process{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
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

func (h *KpiMasterDataHandler) CreateInitiative(c *fiber.Ctx) error {
	var req models.InitiativeRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
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
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdateInitiative(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var req models.InitiativeRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
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
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var item models.Initiative
	h.db.WithContext(c.UserContext()).Preload("Goal").Preload("Objective").Preload("Owner").First(&item, id)
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteInitiative(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	result := h.db.WithContext(c.UserContext()).Delete(&models.Initiative{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
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
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	var existing models.Domain
	if err := h.db.WithContext(c.UserContext()).Where("name_en = ?", req.NameEn).First(&existing).Error; err == nil {
		return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "domain_already_exists"))
	}
	item := &models.Domain{NameEn: req.NameEn, NameAr: req.NameAr, Type: req.Type}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdateDomain(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var req models.DomainRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	var existing models.Domain
	if err := h.db.WithContext(c.UserContext()).Where("name_en = ? AND id != ?", req.NameEn, id).First(&existing).Error; err == nil {
		return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "domain_already_exists"))
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.Domain{ID: id}).Updates(map[string]interface{}{
		"name_en": req.NameEn,
		"name_ar": req.NameAr,
		"type":    req.Type,
	})
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var domain models.Domain
	if err := h.db.WithContext(c.UserContext()).First(&domain, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", domain.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteDomain(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	result := h.db.WithContext(c.UserContext()).Delete(&models.Domain{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
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
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	item := &models.AwardCriterion{CriterionNo: req.CriterionNo, NameEn: req.NameEn, NameAr: req.NameAr}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdateAwardCriterion(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var req models.AwardCriterionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.AwardCriterion{ID: id}).Updates(map[string]interface{}{
		"criterion_no": req.CriterionNo,
		"name_en":      req.NameEn,
		"name_ar":      req.NameAr,
	})
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var item models.AwardCriterion
	h.db.WithContext(c.UserContext()).First(&item, id)
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteAwardCriterion(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	result := h.db.WithContext(c.UserContext()).Delete(&models.AwardCriterion{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
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

func (h *KpiMasterDataHandler) CreateAwardSubCriterion(c *fiber.Ctx) error {
	var req models.AwardSubCriterionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	item := &models.AwardSubCriterion{
		AwardCriterionID: req.AwardCriterionID,
		SubNo:            req.SubNo,
		NameEn:           req.NameEn,
		NameAr:           req.NameAr,
	}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdateAwardSubCriterion(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var req models.AwardSubCriterionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.AwardSubCriterion{ID: id}).Updates(map[string]interface{}{
		"award_criterion_id": req.AwardCriterionID,
		"sub_no":             req.SubNo,
		"name_en":            req.NameEn,
		"name_ar":            req.NameAr,
	})
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var item models.AwardSubCriterion
	h.db.WithContext(c.UserContext()).Preload("AwardCriterion").First(&item, id)
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteAwardSubCriterion(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	result := h.db.WithContext(c.UserContext()).Delete(&models.AwardSubCriterion{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
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
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	var existing models.KpiDataSource
	if err := h.db.WithContext(c.UserContext()).Where("name_en = ?", req.NameEn).First(&existing).Error; err == nil {
		return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "data_source_already_exists"))
	}
	item := &models.KpiDataSource{NameEn: req.NameEn, NameAr: req.NameAr}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdateDataSource(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var req models.KpiDataSourceRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	var existing models.KpiDataSource
	if err := h.db.WithContext(c.UserContext()).Where("name_en = ? AND id != ?", req.NameEn, id).First(&existing).Error; err == nil {
		return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "data_source_already_exists"))
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.KpiDataSource{ID: id}).Updates(map[string]interface{}{
		"name_en": req.NameEn,
		"name_ar": req.NameAr,
	})
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var item models.KpiDataSource
	if err := h.db.WithContext(c.UserContext()).First(&item, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteDataSource(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	result := h.db.WithContext(c.UserContext()).Delete(&models.KpiDataSource{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
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

func (h *KpiMasterDataHandler) CreateSegmentationDimension(c *fiber.Ctx) error {
	var req models.KpiSegmentationDimensionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	item := &models.KpiSegmentationDimension{NameEn: req.NameEn, NameAr: req.NameAr}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdateSegmentationDimension(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var req models.KpiSegmentationDimensionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.KpiSegmentationDimension{ID: id}).Updates(map[string]interface{}{
		"name_en": req.NameEn,
		"name_ar": req.NameAr,
	})
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var item models.KpiSegmentationDimension
	if err := h.db.WithContext(c.UserContext()).First(&item, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteSegmentationDimension(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	result := h.db.WithContext(c.UserContext()).Delete(&models.KpiSegmentationDimension{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
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

func (h *KpiMasterDataHandler) CreateOrganization(c *fiber.Ctx) error {
	var req models.KpiOrganizationRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	item := &models.KpiOrganization{NameEn: req.NameEn, NameAr: req.NameAr, ContactInfo: req.ContactInfo}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdateOrganization(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var req models.KpiOrganizationRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.KpiOrganization{ID: id}).Updates(map[string]interface{}{
		"name_en":      req.NameEn,
		"name_ar":      req.NameAr,
		"contact_info": req.ContactInfo,
	})
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var item models.KpiOrganization
	if err := h.db.WithContext(c.UserContext()).First(&item, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteOrganization(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	result := h.db.WithContext(c.UserContext()).Delete(&models.KpiOrganization{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

// ─── Segmentation Axes (structured, per-KPI) ─────────────────────────────────
// Links a KPI dictionary row to one or more governed segmentation dimensions.
// GET/POST /kpi/:type/:id/segmentation-axes, DELETE /kpi/segmentation-axes/:id

func (h *KpiMasterDataHandler) ListSegmentationAxes(c *fiber.Ctx) error {
	kpiType := c.Params("type")
	if !isValidKpiType(kpiType) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
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
	kpiType := c.Params("type")
	if !isValidKpiType(kpiType) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	kpiID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var req models.KpiSegmentationAxisRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	item := &models.KpiSegmentationAxis{KpiID: kpiID, KpiType: kpiType, DimensionID: req.DimensionID}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
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
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

// ─── Administrative Units (structured, per-KPI) ──────────────────────────────
// Links a KPI dictionary row to one or more related Departments.
// GET/POST /kpi/:type/:id/administrative-units, DELETE /kpi/administrative-units/:id

func (h *KpiMasterDataHandler) ListAdministrativeUnits(c *fiber.Ctx) error {
	kpiType := c.Params("type")
	if !isValidKpiType(kpiType) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
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
	kpiType := c.Params("type")
	if !isValidKpiType(kpiType) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	kpiID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var req models.KpiAdministrativeUnitRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	item := &models.KpiAdministrativeUnit{KpiID: kpiID, KpiType: kpiType, DepartmentID: req.DepartmentID}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
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
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}
