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

type KpiPerformanceBandHandler struct {
	db        *gorm.DB
	validator *validator.Validate
}

func NewKpiPerformanceBandHandler(db *gorm.DB) *KpiPerformanceBandHandler {
	return &KpiPerformanceBandHandler{db: db, validator: validator.New()}
}

func (h *KpiPerformanceBandHandler) ListBands(c *fiber.Ctx) error {
	var items []models.KpiPerformanceBand
	if err := h.db.WithContext(c.UserContext()).Order("kpi_code ASC NULLS FIRST").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	resp := make([]models.KpiPerformanceBandResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

// GetEffectiveBand resolves the band that actually applies to a KPI: its own
// override if one exists, else the global default, else a hardcoded fallback.
func (h *KpiPerformanceBandHandler) GetEffectiveBand(c *fiber.Ctx) error {
	kpiCode := c.Query("kpi_code")

	if kpiCode != "" {
		var override models.KpiPerformanceBand
		if err := h.db.WithContext(c.UserContext()).Where("kpi_code = ?", kpiCode).First(&override).Error; err == nil {
			return utils.SuccessResponse(c, fiber.StatusOK, "", override.ToResponse())
		}
	}

	var global models.KpiPerformanceBand
	if err := h.db.WithContext(c.UserContext()).Where("kpi_code IS NULL").Order("created_at ASC").First(&global).Error; err == nil {
		return utils.SuccessResponse(c, fiber.StatusOK, "", global.ToResponse())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "", models.DefaultKpiPerformanceBand)
}

// UpsertBand creates or updates the band for the given kpi_code (nil = global default).
func (h *KpiPerformanceBandHandler) UpsertBand(c *fiber.Ctx) error {
	var req models.KpiPerformanceBandRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	if req.GreenMin <= req.AmberMin {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "green threshold must be greater than amber threshold")
	}

	q := h.db.WithContext(c.UserContext())
	var existing models.KpiPerformanceBand
	lookup := q.Where("kpi_code IS NULL")
	if req.KpiCode != nil && *req.KpiCode != "" {
		lookup = q.Where("kpi_code = ?", *req.KpiCode)
	}

	err := lookup.First(&existing).Error
	if err == gorm.ErrRecordNotFound {
		item := &models.KpiPerformanceBand{KpiCode: req.KpiCode, GreenMin: req.GreenMin, AmberMin: req.AmberMin}
		if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
		}
		return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
	}
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	existing.GreenMin = req.GreenMin
	existing.AmberMin = req.AmberMin
	if err := h.db.WithContext(c.UserContext()).Save(&existing).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", existing.ToResponse())
}

func (h *KpiPerformanceBandHandler) DeleteBand(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	result := h.db.WithContext(c.UserContext()).Delete(&models.KpiPerformanceBand{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}
