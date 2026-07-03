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

// ─── Strategic Goals ──────────────────────────────────────────────────────────

func (h *KpiMasterDataHandler) ListGoals(c *fiber.Ctx) error {
	var items []models.StrategicGoal
	if err := h.db.WithContext(c.UserContext()).Preload("Pillar").Preload("Enabler").Order("created_at DESC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	resp := make([]models.StrategicGoalResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

func (h *KpiMasterDataHandler) CreateGoal(c *fiber.Ctx) error {
	var req models.StrategicGoalRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	item := &models.StrategicGoal{NameEn: req.NameEn, NameAr: req.NameAr, TitleEn: req.NameEn, TitleAr: req.NameAr, PillarID: req.PillarID, EnablerID: req.EnablerID}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) UpdateGoal(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var req models.StrategicGoalRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.StrategicGoal{ID: id}).Updates(map[string]interface{}{
		"name_en":    req.NameEn,
		"name_ar":    req.NameAr,
		"title_en":   req.NameEn,
		"title_ar":   req.NameAr,
		"pillar_id":  req.PillarID,
		"enabler_id": req.EnablerID,
	})
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var item models.StrategicGoal
	h.db.WithContext(c.UserContext()).Preload("Pillar").Preload("Enabler").First(&item, id)
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiMasterDataHandler) DeleteGoal(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	result := h.db.WithContext(c.UserContext()).Delete(&models.StrategicGoal{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

// ─── Operational Objectives ───────────────────────────────────────────────────

func (h *KpiMasterDataHandler) ListOperationalObjectives(c *fiber.Ctx) error {
	var items []models.OperationalObjective
	if err := h.db.WithContext(c.UserContext()).Preload("StrategicGoal").Order("created_at DESC").Find(&items).Error; err != nil {
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
	item := &models.OperationalObjective{NameEn: req.NameEn, NameAr: req.NameAr, StrategicGoalID: req.StrategicGoalID}
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
		"name_en":           req.NameEn,
		"name_ar":           req.NameAr,
		"strategic_goal_id": req.StrategicGoalID,
	})
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var item models.OperationalObjective
	h.db.WithContext(c.UserContext()).Preload("StrategicGoal").First(&item, id)
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
	if err := h.db.WithContext(c.UserContext()).Preload("OperationalObjective").Preload("StrategicGoal").Preload("Department").Order("created_at DESC").Find(&items).Error; err != nil {
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
		StrategicGoalID:        req.StrategicGoalID,
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
		"name_en":                   req.NameEn,
		"name_ar":                   req.NameAr,
		"operational_objective_id":  req.OperationalObjectiveID,
		"strategic_goal_id":         req.StrategicGoalID,
		"department_id":             req.DepartmentID,
		"unit":                      req.Unit,
	})
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var item models.Process
	h.db.WithContext(c.UserContext()).Preload("OperationalObjective").Preload("StrategicGoal").Preload("Department").First(&item, id)
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
	if err := h.db.WithContext(c.UserContext()).Preload("StrategicGoal").Preload("Owner").Order("created_at DESC").Find(&items).Error; err != nil {
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
		NameEn:          req.NameEn,
		NameAr:          req.NameAr,
		StrategicGoalID: req.StrategicGoalID,
		PillarID:        req.PillarID,
		EnablerID:       req.EnablerID,
		OwnerID:         req.OwnerID,
		Status:          status,
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
		"name_en":           req.NameEn,
		"name_ar":           req.NameAr,
		"strategic_goal_id": req.StrategicGoalID,
		"pillar_id":         req.PillarID,
		"enabler_id":        req.EnablerID,
		"owner_id":          req.OwnerID,
	}
	if req.Status != "" {
		updates["status"] = req.Status
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.Initiative{ID: id}).Updates(updates)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var item models.Initiative
	h.db.WithContext(c.UserContext()).Preload("StrategicGoal").Preload("Owner").First(&item, id)
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
	result := h.db.WithContext(c.UserContext()).Model(&models.Domain{ID: id}).Updates(map[string]interface{}{
		"name_en": req.NameEn,
		"name_ar": req.NameAr,
		"type":    req.Type,
	})
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", models.DomainResponse{ID: id, NameEn: req.NameEn, NameAr: req.NameAr, Type: req.Type})
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
