package handlers

import (
	"strings"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
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
// override if one exists, else the global default, else a hardcoded fallback
// — the same resolution performance status calculation uses
// (services.EffectiveBand).
func (h *KpiPerformanceBandHandler) GetEffectiveBand(c *fiber.Ctx) error {
	return utils.SuccessResponse(c, fiber.StatusOK, "", services.EffectiveBand(h.db.WithContext(c.UserContext()), c.Query("kpi_code")))
}

// maxBandThreshold is the highest green/amber minimum a band may use: band
// thresholds are compared to achievement %, which is displayed capped at 100.
const maxBandThreshold = 100

// UpsertBand creates or updates the band for the given kpi_code (nil = global default).
func (h *KpiPerformanceBandHandler) UpsertBand(c *fiber.Ctx) error {
	var req models.KpiPerformanceBandRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if req.GreenMin == nil || req.AmberMin == nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "green_min and amber_min are required")
	}
	green, amber := *req.GreenMin, *req.AmberMin
	if amber < 0 || green > maxBandThreshold {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Band thresholds must be between 0 and 100")
	}
	if green <= amber {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "green threshold must be greater than amber threshold")
	}

	db := h.db.WithContext(c.UserContext())
	if req.KpiCode != nil {
		code := strings.TrimSpace(*req.KpiCode)
		if code == "" {
			req.KpiCode = nil
		} else {
			// Bands are keyed by KPI code only (no kpi_type column), so the
			// code must exist in at least one of the three dictionaries.
			if !kpiExistsByCode(db, code, models.KPITypeStrategic) && !kpiExistsByCode(db, code, models.KPITypeOperational) &&
				!kpiExistsByCode(db, code, models.KPITypeAward) {
				return utils.ErrorResponse(c, fiber.StatusBadRequest, "KPI not found in dictionary")
			}
			req.KpiCode = &code
		}
	}

	// Unscoped: a previously deleted band for the same code still holds the
	// unique kpi_code, so it is restored and updated instead of re-created.
	var existing models.KpiPerformanceBand
	lookup := db.Unscoped().Where("kpi_code IS NULL")
	if req.KpiCode != nil {
		lookup = db.Unscoped().Where("kpi_code = ?", *req.KpiCode)
	}

	err := lookup.Order("deleted_at DESC NULLS FIRST").First(&existing).Error
	if err == gorm.ErrRecordNotFound {
		item := &models.KpiPerformanceBand{KpiCode: req.KpiCode, GreenMin: green, AmberMin: amber}
		if err := db.Create(item).Error; err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
		}
		return utils.SuccessResponse(c, fiber.StatusCreated, "", item.ToResponse())
	}
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	existing.GreenMin = green
	existing.AmberMin = amber
	existing.DeletedAt = gorm.DeletedAt{}
	if err := db.Unscoped().Save(&existing).Error; err != nil {
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
