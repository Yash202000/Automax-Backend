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
}

func NewKpiEntryHandler(db *gorm.DB, actionLogSvc services.ActionLogService) *KpiEntryHandler {
	return &KpiEntryHandler{
		db:           db,
		validator:    validator.New(),
		actionLogSvc: actionLogSvc,
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

	// REL-07: Check for duplicate entry (same KPI + metric + period)
	var dupCount int64
	db.Model(&models.KpiEntry{}).
		Where("kpi_id = ? AND kpi_type = ? AND metric_id = ? AND reporting_year = ? AND period_code = ?",
			kpiID, kpiType, mid, req.ReportingYear, req.PeriodCode).
		Count(&dupCount)
	if dupCount > 0 {
		return utils.ErrorResponse(c, fiber.StatusBadRequest,
			fmt.Sprintf("An entry already exists for this metric in period %s/%d", req.PeriodCode, req.ReportingYear))
	}

	// REL-07: Validate denominator is not zero for ratio types
	if (metric.CalculationType == "Percentage - Ratio" || metric.CalculationType == "Ratio") &&
		req.DenominatorValue != nil && *req.DenominatorValue == 0 {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Denominator cannot be zero for ratio-type metrics")
	}

	item := req.ToModel(db, kpiID, kpiType, userID)

	// REL-09: Look up matching approved target to populate TargetID and TargetValueSnapshot
	kpiCode, kpiErr := getKPICode(db, kpiType, kpiID)
	if kpiErr == nil && kpiCode != "" {
		var target models.KpiAnnualTarget
		if err := db.Where("kpi_code = ? AND kpi_type = ? AND target_year = ? AND period_code = ? AND target_status = 'approved'",
			kpiCode, kpiType, req.ReportingYear, req.PeriodCode,
		).First(&target).Error; err == nil {
			item.TargetID = &target.ID
			item.TargetValueSnapshot = &target.TargetValue
			item.ThresholdModeSnapshot = target.ThresholdMode
		}
	}

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
