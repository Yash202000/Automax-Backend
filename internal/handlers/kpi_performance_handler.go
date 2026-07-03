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

type KpiPerformanceHandler struct {
	db        *gorm.DB
	validator *validator.Validate
}

func NewKpiPerformanceHandler(db *gorm.DB) *KpiPerformanceHandler {
	return &KpiPerformanceHandler{
		db:        db,
		validator: validator.New(),
	}
}

// ─── Annual Targets ───────────────────────────────────────────────────────────

func (h *KpiPerformanceHandler) ListTargets(c *fiber.Ctx) error {
	q := h.db.WithContext(c.UserContext()).Model(&models.KpiAnnualTarget{})

	if kpiCode := c.Query("kpi_code"); kpiCode != "" {
		q = q.Where("kpi_code = ?", kpiCode)
	}
	if yearStr := c.Query("year"); yearStr != "" {
		year, err := strconv.Atoi(yearStr)
		if err == nil {
			q = q.Where("year = ?", year)
		}
	}

	var items []models.KpiAnnualTarget
	if err := q.Order("year DESC, kpi_code ASC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", items)
}

func (h *KpiPerformanceHandler) SetTarget(c *fiber.Ctx) error {
	var req models.KpiAnnualTargetRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}

	item := &models.KpiAnnualTarget{
		KpiCode:     req.KpiCode,
		KpiType:     req.KpiType,
		Year:        req.Year,
		TargetValue: req.TargetValue,
	}

	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "", item)
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

	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
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

	item := &models.KpiPerformance{
		KpiCode:          req.KpiCode,
		KpiType:          req.KpiType,
		Year:             req.Year,
		Quarter:          req.Quarter,
		Target:           req.Target,
		Actual:           req.Actual,
		TrendDescription: req.TrendDescription,
		Justification:    req.Justification,
		CorrectiveAction: req.CorrectiveAction,
		Status:           models.KPIPerfStatusDraft,
	}

	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
}

func (h *KpiPerformanceHandler) TransitionPerformance(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.KpiPerformanceTransitionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}

	var item models.KpiPerformance
	if err := h.db.WithContext(c.UserContext()).First(&item, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	nextStatus := map[string]string{
		"submit":  models.KPIPerfStatusSubmitted,
		"review":  models.KPIPerfStatusReview,
		"approve": models.KPIPerfStatusApproved,
		"reject":  models.KPIPerfStatusRejected,
		"publish": models.KPIPerfStatusPublished,
	}

	newStatus, ok := nextStatus[req.Action]
	if !ok {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_action"))
	}

	if err := h.db.WithContext(c.UserContext()).Model(&item).Update("status", newStatus).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update"))
	}

	item.Status = newStatus
	return utils.SuccessResponse(c, fiber.StatusOK, "", item.ToResponse())
}

// ─── Benchmarks ───────────────────────────────────────────────────────────────

func (h *KpiPerformanceHandler) ListBenchmarks(c *fiber.Ctx) error {
	q := h.db.WithContext(c.UserContext()).Model(&models.KpiBenchmark{})

	if kpiCode := c.Query("kpi_code"); kpiCode != "" {
		q = q.Where("kpi_code = ?", kpiCode)
	}

	var items []models.KpiBenchmark
	if err := q.Order("year DESC, kpi_code ASC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", items)
}

func (h *KpiPerformanceHandler) CreateBenchmark(c *fiber.Ctx) error {
	var req models.KpiBenchmarkRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}

	item := &models.KpiBenchmark{
		KpiCode:              req.KpiCode,
		KpiType:              req.KpiType,
		Year:                 req.Year,
		BenchmarkEntity:      req.BenchmarkEntity,
		InternalAchievement:  req.InternalAchievement,
		BenchmarkAchievement: req.BenchmarkAchievement,
		Notes:                req.Notes,
	}

	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "", item)
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

	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

// ─── Segmentation ─────────────────────────────────────────────────────────────

func (h *KpiPerformanceHandler) ListSegmentation(c *fiber.Ctx) error {
	q := h.db.WithContext(c.UserContext()).Model(&models.KpiSegmentation{})

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

	var items []models.KpiSegmentation
	if err := q.Order("year DESC, quarter DESC, dimension_name ASC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", items)
}

func (h *KpiPerformanceHandler) CreateSegmentation(c *fiber.Ctx) error {
	var req models.KpiSegmentationRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}

	item := &models.KpiSegmentation{
		KpiCode:       req.KpiCode,
		KpiType:       req.KpiType,
		Year:          req.Year,
		Quarter:       req.Quarter,
		DimensionName: req.DimensionName,
		SegmentName:   req.SegmentName,
		Achievement:   req.Achievement,
	}

	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "", item)
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

	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}
