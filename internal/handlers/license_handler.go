package handlers

import (
	"errors"
	"strings"

	"github.com/automax/backend/internal/licensing"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type LicenseHandler struct {
	licenseService services.LicenseService
}

func NewLicenseHandler(licenseService services.LicenseService) *LicenseHandler {
	return &LicenseHandler{licenseService: licenseService}
}

// Activate activates a license key.
// POST /api/v1/admin/license/activate
func (h *LicenseHandler) Activate(c *fiber.Ctx) error {
	var req models.LicenseActivateRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "invalid_request",
			"message": "Invalid request body",
		})
	}

	if strings.TrimSpace(req.LicenseKey) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "invalid_request",
			"message": "License key is required",
		})
	}

	if strings.TrimSpace(req.JWKS) == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "invalid_request",
			"message": "JWKS is required",
		})
	}

	userID, _ := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	status, err := h.licenseService.Activate(c.UserContext(), req.LicenseKey, req.JWKS, userID)
	if err != nil {
		statusCode := fiber.StatusBadRequest

		errCode := "activation_failed"
		if errors.Is(err, services.ErrUserLimitReached) {
			statusCode = fiber.StatusConflict
			errCode = "user_limit_exceeded"
		} else if errors.Is(err, services.ErrProductMismatch) {
			errCode = "product_mismatch"
		} else if errors.Is(err, services.ErrEncryptionKey) {
			statusCode = fiber.StatusInternalServerError
			errCode = "server_config_error"
		}

		return c.Status(statusCode).JSON(fiber.Map{
			"success": false,
			"error":   errCode,
			"message": err.Error(),
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"message": "License activated successfully",
		"data":    status,
	})
}

// GetStatus returns the full license status (admin only).
// GET /api/v1/admin/license/status
func (h *LicenseHandler) GetStatus(c *fiber.Ctx) error {
	status, err := h.licenseService.GetStatus(c.UserContext())
	if err != nil {
		if errors.Is(err, services.ErrNoLicense) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"success": false,
				"error":   "no_license",
				"message": "No active license found",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   "internal_error",
			"message": "Failed to retrieve license status",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"data":    status,
	})
}

// Deactivate removes the active license.
// DELETE /api/v1/admin/license
func (h *LicenseHandler) Deactivate(c *fiber.Ctx) error {
	if err := h.licenseService.Deactivate(c.UserContext()); err != nil {
		if errors.Is(err, services.ErrNoLicense) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"success": false,
				"error":   "no_license",
				"message": "No active license to deactivate",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   "internal_error",
			"message": "Failed to deactivate license",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"message": "License deactivated successfully",
	})
}

// GetPublicInfo returns license info for all authenticated users.
// GET /api/v1/license/info
func (h *LicenseHandler) GetPublicInfo(c *fiber.Ctx) error {
	info, err := h.licenseService.GetInfo(c.UserContext())
	if err != nil {
		if errors.Is(err, services.ErrNoLicense) {
			return c.Status(fiber.StatusNotFound).JSON(fiber.Map{
				"success": false,
				"error":   "no_license",
				"message": "No active license",
			})
		}
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   "internal_error",
			"message": "Failed to retrieve license info",
		})
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"data":    info,
	})
}

// GetCatalog returns the canonical list of licensable features and their metadata.
// It is public (no auth) because it describes product shape, not tenant state.
// Frontend uses this to render the license page and to gate navigation.
// GET /api/v1/license/catalog
func (h *LicenseHandler) GetCatalog(c *fiber.Ctx) error {
	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"features": licensing.Catalog,
		},
	})
}
