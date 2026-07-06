package handlers

import (
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type KpiCorrectiveActionHandler struct {
	db        *gorm.DB
	validator *validator.Validate
}

func NewKpiCorrectiveActionHandler(db *gorm.DB) *KpiCorrectiveActionHandler {
	return &KpiCorrectiveActionHandler{db: db, validator: validator.New()}
}

func (h *KpiCorrectiveActionHandler) ListByPerformance(c *fiber.Ctx) error {
	performanceID, err := uuid.Parse(c.Query("performance_id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var items []models.KpiCorrectiveAction
	if err := h.db.WithContext(c.UserContext()).
		Preload("Owner").Preload("CreatedBy").
		Where("kpi_performance_id = ?", performanceID).
		Order("created_at DESC").
		Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	resp := make([]models.KpiCorrectiveActionResponse, len(items))
	for i, item := range items {
		resp[i] = item.ToResponse()
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

func (h *KpiCorrectiveActionHandler) Create(c *fiber.Ctx) error {
	var req models.KpiCorrectiveActionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}

	var perf models.KpiPerformance
	if err := h.db.WithContext(c.UserContext()).First(&perf, req.KpiPerformanceID).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "the referenced performance record does not exist")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	item := &models.KpiCorrectiveAction{
		KpiPerformanceID: req.KpiPerformanceID,
		Description:      req.Description,
		OwnerID:          req.OwnerID,
		DueDate:          req.DueDate,
		Status:           models.CorrectiveActionStatusOpen,
		CreatedByID:      userID,
	}

	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}

	var reloaded models.KpiCorrectiveAction
	h.db.WithContext(c.UserContext()).Preload("Owner").Preload("CreatedBy").First(&reloaded, item.ID)

	return utils.SuccessResponse(c, fiber.StatusCreated, "", reloaded.ToResponse())
}

func (h *KpiCorrectiveActionHandler) UpdateStatus(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.KpiCorrectiveActionStatusRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	if req.Status == models.CorrectiveActionStatusClosed && strings.TrimSpace(req.ClosureNote) == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "a closure note is required to close a corrective action")
	}

	var item models.KpiCorrectiveAction
	if err := h.db.WithContext(c.UserContext()).First(&item, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	item.Status = req.Status
	item.ClosureNote = req.ClosureNote
	item.ClosureEvidenceURL = req.ClosureEvidenceURL
	if req.Status == models.CorrectiveActionStatusEscalated && item.EscalatedAt == nil {
		now := time.Now()
		item.EscalatedAt = &now
	}

	if err := h.db.WithContext(c.UserContext()).Save(&item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update"))
	}

	var reloaded models.KpiCorrectiveAction
	h.db.WithContext(c.UserContext()).Preload("Owner").Preload("CreatedBy").First(&reloaded, item.ID)

	return utils.SuccessResponse(c, fiber.StatusOK, "", reloaded.ToResponse())
}
