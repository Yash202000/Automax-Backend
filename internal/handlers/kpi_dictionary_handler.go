package handlers

import (
	"strconv"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type KpiDictionaryHandler struct {
	db        *gorm.DB
	validator *validator.Validate
}

func NewKpiDictionaryHandler(db *gorm.DB) *KpiDictionaryHandler {
	return &KpiDictionaryHandler{
		db:        db,
		validator: validator.New(),
	}
}

// ─── Strategic KPI ────────────────────────────────────────────────────────────

func (h *KpiDictionaryHandler) ListStrategic(c *fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))
	search := c.Query("search")
	goalIDStr := c.Query("strategic_goal_id")

	page = max(page, 1)
	if limit < 1 || limit > 100 {
		limit = 10
	}

	q := h.db.WithContext(c.UserContext()).Model(&models.StrategicKPI{}).
		Preload("StrategicGoal").Preload("Pillar").Preload("Domain").Preload("OwnerDept").Preload("Goal")

	if search != "" {
		q = q.Where("name_en ILIKE ? OR name_ar ILIKE ? OR code ILIKE ?", "%"+search+"%", "%"+search+"%", "%"+search+"%")
	}
	if goalIDStr != "" {
		goalID, err := uuid.Parse(goalIDStr)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
		}
		q = q.Where("strategic_goal_id = ?", goalID)
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
		Preload("StrategicGoal").Preload("Pillar").Preload("Domain").Preload("OwnerDept").Preload("Goal").
		First(&item, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiDictionaryHandler) CreateStrategic(c *fiber.Ctx) error {
	var req models.StrategicKPIRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}

	item := &models.StrategicKPI{
		Code:               req.Code,
		NameEn:             req.NameEn,
		NameAr:             req.NameAr,
		StrategicGoalID:    req.StrategicGoalID,
		PillarID:           req.PillarID,
		DomainID:           req.DomainID,
		OwnerDeptID:        req.OwnerDeptID,
		Polarity:           req.Polarity,
		ActivationStatus:   req.ActivationStatus,
		DescriptionEn:      req.DescriptionEn,
		DescriptionAr:      req.DescriptionAr,
		Formula:            req.Formula,
		Baseline:           req.Baseline,
		ReportingFrequency: req.ReportingFrequency,
		Lifecycle:          req.Lifecycle,
		DataSource:         req.DataSource,
		SegmentationAxes:   req.SegmentationAxes,
		RelatedUnits:       req.RelatedUnits,
		Notes:              req.Notes,
	}
	if item.Polarity == "" {
		item.Polarity = models.KPIPolarityAscending
	}
	if item.ActivationStatus == "" {
		item.ActivationStatus = models.KPIStatusDraft
	}

	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}

	h.db.WithContext(c.UserContext()).
		Preload("StrategicGoal").Preload("Pillar").Preload("Domain").Preload("OwnerDept").
		First(item, item.ID)

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

	updates := map[string]interface{}{
		"code":                req.Code,
		"name_en":             req.NameEn,
		"name_ar":             req.NameAr,
		"strategic_goal_id":   req.StrategicGoalID,
		"pillar_id":           req.PillarID,
		"domain_id":           req.DomainID,
		"owner_dept_id":       req.OwnerDeptID,
		"polarity":            req.Polarity,
		"activation_status":   req.ActivationStatus,
		"description_en":      req.DescriptionEn,
		"description_ar":      req.DescriptionAr,
		"formula":             req.Formula,
		"baseline":            req.Baseline,
		"reporting_frequency": req.ReportingFrequency,
		"lifecycle":           req.Lifecycle,
		"data_source":         req.DataSource,
		"segmentation_axes":   req.SegmentationAxes,
		"related_units":       req.RelatedUnits,
		"notes":               req.Notes,
	}

	result := h.db.WithContext(c.UserContext()).Model(&models.StrategicKPI{ID: id}).Updates(updates)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	if result.Error != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update"))
	}

	var item models.StrategicKPI
	h.db.WithContext(c.UserContext()).
		Preload("StrategicGoal").Preload("Pillar").Preload("Domain").Preload("OwnerDept").
		First(&item, id)

	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiDictionaryHandler) DeleteStrategic(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	result := h.db.WithContext(c.UserContext()).Delete(&models.StrategicKPI{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

// ─── Operational KPI ──────────────────────────────────────────────────────────

func (h *KpiDictionaryHandler) ListOperational(c *fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))
	search := c.Query("search")

	page = max(page, 1)
	if limit < 1 || limit > 100 {
		limit = 10
	}

	q := h.db.WithContext(c.UserContext()).Model(&models.OperationalKPI{}).
		Preload("StrategicGoal").Preload("OperationalObjective").Preload("Process").Preload("OwnerDept")

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

	item := &models.OperationalKPI{
		Code:                   req.Code,
		NameEn:                 req.NameEn,
		NameAr:                 req.NameAr,
		StrategicGoalID:        req.StrategicGoalID,
		OperationalObjectiveID: req.OperationalObjectiveID,
		ProcessID:              req.ProcessID,
		OwnerDeptID:            req.OwnerDeptID,
		Polarity:               req.Polarity,
		ActivationStatus:       req.ActivationStatus,
		DescriptionEn:          req.DescriptionEn,
		DescriptionAr:          req.DescriptionAr,
		Formula:                req.Formula,
		Baseline:               req.Baseline,
		ReportingFrequency:     req.ReportingFrequency,
		DataSource:             req.DataSource,
		Notes:                  req.Notes,
	}
	if item.Polarity == "" {
		item.Polarity = models.KPIPolarityAscending
	}
	if item.ActivationStatus == "" {
		item.ActivationStatus = models.KPIStatusDraft
	}

	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}

	h.db.WithContext(c.UserContext()).
		Preload("StrategicGoal").Preload("OperationalObjective").Preload("Process").Preload("OwnerDept").
		First(item, item.ID)

	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiDictionaryHandler) GetOperational(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var item models.OperationalKPI
	if err := h.db.WithContext(c.UserContext()).
		Preload("StrategicGoal").Preload("OperationalObjective").Preload("Process").Preload("OwnerDept").
		First(&item, id).Error; err != nil {
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

	updates := map[string]interface{}{
		"code":                     req.Code,
		"name_en":                  req.NameEn,
		"name_ar":                  req.NameAr,
		"strategic_goal_id":        req.StrategicGoalID,
		"operational_objective_id": req.OperationalObjectiveID,
		"process_id":               req.ProcessID,
		"owner_dept_id":            req.OwnerDeptID,
		"polarity":                 req.Polarity,
		"activation_status":        req.ActivationStatus,
		"description_en":           req.DescriptionEn,
		"description_ar":           req.DescriptionAr,
		"formula":                  req.Formula,
		"baseline":                 req.Baseline,
		"reporting_frequency":      req.ReportingFrequency,
		"data_source":              req.DataSource,
		"notes":                    req.Notes,
	}

	result := h.db.WithContext(c.UserContext()).Model(&models.OperationalKPI{ID: id}).Updates(updates)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	if result.Error != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update"))
	}

	var item models.OperationalKPI
	h.db.WithContext(c.UserContext()).
		Preload("StrategicGoal").Preload("OperationalObjective").Preload("Process").Preload("OwnerDept").
		First(&item, id)

	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

func (h *KpiDictionaryHandler) DeleteOperational(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	result := h.db.WithContext(c.UserContext()).Delete(&models.OperationalKPI{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

// ─── Award KPI ────────────────────────────────────────────────────────────────

func (h *KpiDictionaryHandler) ListAward(c *fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))

	page = max(page, 1)
	if limit < 1 || limit > 100 {
		limit = 10
	}

	q := h.db.WithContext(c.UserContext()).Model(&models.AwardKPI{}).
		Preload("AwardSubCriterion.AwardCriterion").Preload("OwnerDept")

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

	item := &models.AwardKPI{
		Code:                req.Code,
		NameEn:              req.NameEn,
		NameAr:              req.NameAr,
		AwardSubCriterionID: req.AwardSubCriterionID,
		OwnerDeptID:         req.OwnerDeptID,
		Polarity:            req.Polarity,
		ActivationStatus:    req.ActivationStatus,
		DescriptionEn:       req.DescriptionEn,
		DescriptionAr:       req.DescriptionAr,
		Formula:             req.Formula,
		Baseline:            req.Baseline,
		ReportingFrequency:  req.ReportingFrequency,
		DataSource:          req.DataSource,
		Notes:               req.Notes,
	}
	if item.Polarity == "" {
		item.Polarity = models.KPIPolarityAscending
	}
	if item.ActivationStatus == "" {
		item.ActivationStatus = models.KPIStatusDraft
	}

	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}

	h.db.WithContext(c.UserContext()).
		Preload("AwardSubCriterion.AwardCriterion").Preload("OwnerDept").
		First(item, item.ID)

	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiDictionaryHandler) DeleteAward(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	result := h.db.WithContext(c.UserContext()).Delete(&models.AwardKPI{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

func (h *KpiDictionaryHandler) GetAward(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var item models.AwardKPI
	if err := h.db.WithContext(c.UserContext()).
		Preload("AwardSubCriterion.AwardCriterion").Preload("OwnerDept").
		First(&item, id).Error; err != nil {
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

	updates := map[string]interface{}{
		"code":                   req.Code,
		"name_en":                req.NameEn,
		"name_ar":                req.NameAr,
		"award_sub_criterion_id": req.AwardSubCriterionID,
		"owner_dept_id":          req.OwnerDeptID,
		"polarity":               req.Polarity,
		"activation_status":      req.ActivationStatus,
		"description_en":        req.DescriptionEn,
		"description_ar":        req.DescriptionAr,
		"formula":                req.Formula,
		"baseline":               req.Baseline,
		"reporting_frequency":    req.ReportingFrequency,
		"data_source":            req.DataSource,
		"notes":                  req.Notes,
	}

	result := h.db.WithContext(c.UserContext()).Model(&models.AwardKPI{ID: id}).Updates(updates)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	if result.Error != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update"))
	}

	var item models.AwardKPI
	h.db.WithContext(c.UserContext()).
		Preload("AwardSubCriterion.AwardCriterion").Preload("OwnerDept").
		First(&item, id)

	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

// ─── KPI Status Transition ────────────────────────────────────────────────────

type kpiTransitionRequest struct {
	Action  string `json:"action" validate:"required,oneof=activate deactivate reactivate"`
	Comment string `json:"comment"`
}

func (h *KpiDictionaryHandler) TransitionKpiStatus(c *fiber.Ctx) error {
	kpiType := c.Params("type")
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req kpiTransitionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	var newStatus string
	switch req.Action {
	case "activate":
		newStatus = models.KPIStatusActive
	case "deactivate":
		newStatus = models.KPIStatusInactive
	case "reactivate":
		newStatus = models.KPIStatusActive
	}

	allowedTypes := map[string]bool{"strategic": true, "operational": true, "award": true}
	if !allowedTypes[kpiType] {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	db := h.db.WithContext(c.UserContext())
	var result *gorm.DB

	switch kpiType {
	case "strategic":
		var item models.StrategicKPI
		if err := db.First(&item, id).Error; err != nil {
			return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
		}
		if !isValidTransition(item.ActivationStatus, req.Action) {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid transition for current status")
		}
		result = db.Model(&models.StrategicKPI{ID: id}).Update("activation_status", newStatus)
	case "operational":
		var item models.OperationalKPI
		if err := db.First(&item, id).Error; err != nil {
			return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
		}
		if !isValidTransition(item.ActivationStatus, req.Action) {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid transition for current status")
		}
		result = db.Model(&models.OperationalKPI{ID: id}).Update("activation_status", newStatus)
	case "award":
		var item models.AwardKPI
		if err := db.First(&item, id).Error; err != nil {
			return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
		}
		if !isValidTransition(item.ActivationStatus, req.Action) {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid transition for current status")
		}
		result = db.Model(&models.AwardKPI{ID: id}).Update("activation_status", newStatus)
	}

	if result.Error != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update"))
	}
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Status updated successfully", nil)
}

func isValidTransition(currentStatus, action string) bool {
	switch action {
	case "activate":
		return currentStatus == models.KPIStatusDraft
	case "deactivate":
		return currentStatus == models.KPIStatusActive
	case "reactivate":
		return currentStatus == models.KPIStatusInactive
	}
	return false
}
