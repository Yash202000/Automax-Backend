package handlers

import (
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"

	"github.com/gofiber/fiber/v2"
)

type SettingsHandler struct {
	settingsService services.SettingsService
}

func NewSettingsHandler(settingsService services.SettingsService) *SettingsHandler {
	return &SettingsHandler{
		settingsService: settingsService,
	}
}

// GetSettings retrieves the application settings (public endpoint)
// @Summary Get application settings
// @Description Get application-wide settings and branding configuration
// @Tags Settings
// @Accept json
// @Produce json
// @Success 200 {object} models.SettingsResponse
// @Failure 500 {object} fiber.Map
// @Router /api/v1/settings [get]
func (h *SettingsHandler) GetSettings(c *fiber.Ctx) error {
	settings, err := h.settingsService.GetSettings(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to retrieve settings",
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    settings,
	})
}

// UpdateSettings updates the application settings (admin only)
// @Summary Update application settings
// @Description Update application-wide settings and branding (requires settings:update permission)
// @Tags Settings
// @Accept json
// @Produce json
// @Param request body models.SettingsUpdateRequest true "Settings update data"
// @Success 200 {object} models.SettingsResponse
// @Failure 400 {object} fiber.Map
// @Failure 403 {object} fiber.Map
// @Failure 500 {object} fiber.Map
// @Router /api/v1/admin/settings [put]
func (h *SettingsHandler) UpdateSettings(c *fiber.Ctx) error {
	var req models.SettingsUpdateRequest

	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	settings, err := h.settingsService.UpdateSettings(c.UserContext(), &req)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to update settings",
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Settings updated successfully",
		"data":    settings,
	})
}
