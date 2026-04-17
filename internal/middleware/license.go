package middleware

import (
	"strings"

	"github.com/automax/backend/internal/services"
	"github.com/gofiber/fiber/v2"
)

type LicenseMiddleware struct {
	licenseService services.LicenseService
}

func NewLicenseMiddleware(licenseService services.LicenseService) *LicenseMiddleware {
	return &LicenseMiddleware{licenseService: licenseService}
}

// LicenseGate is a global middleware applied after authentication.
// It checks whether a valid license exists and blocks access if not.
// Exempt paths (health, auth, license management, settings) are always allowed.
func (m *LicenseMiddleware) LicenseGate() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// If licensing is disabled (dev mode), pass through
		if !m.licenseService.IsEnabled() {
			return c.Next()
		}

		path := c.Path()

		// Always allow exempt paths
		if isLicenseExemptPath(path) {
			return c.Next()
		}

		// If no license is loaded, block with informative message
		if !m.licenseService.IsValid() {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"success": false,
				"error":   "license_required",
				"message": "No valid license. Please activate a license in the admin panel.",
			})
		}

		// If in grace period, set flag in locals for downstream middleware
		if m.licenseService.IsGracePeriod() {
			c.Locals("license_grace", true)
		}

		return c.Next()
	}
}

// RequireLicensedFeature returns middleware that gates access to a specific feature.
// It checks whether the feature is included in the active license.
// During grace period, only read methods (GET, HEAD, OPTIONS) are allowed.
func (m *LicenseMiddleware) RequireLicensedFeature(featureCode string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		// If licensing is disabled, pass through
		if !m.licenseService.IsEnabled() {
			return c.Next()
		}

		// Check feature is licensed
		if !m.licenseService.IsFeatureLicensed(featureCode) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"success": false,
				"error":   "feature_not_licensed",
				"feature": featureCode,
				"message": "This feature is not included in your current license. Please upgrade.",
			})
		}

		// During grace period, block write operations
		if m.licenseService.IsGracePeriod() && isWriteMethod(c.Method()) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"success": false,
				"error":   "license_grace_read_only",
				"message": "Your license has expired. Read-only access is available during the grace period. Please renew your license.",
			})
		}

		return c.Next()
	}
}

// isLicenseExemptPath returns true for paths that should never be blocked by license checks.
func isLicenseExemptPath(path string) bool {
	exemptPrefixes := []string{
		"/api/v1/health",
		"/api/v1/ready",
		"/api/v1/auth/",
		"/api/v1/ldap/",
		"/api/v1/admin/license",
		"/api/v1/license/info",
		"/api/v1/settings",
		"/api/v1/users/me",
		"/api/v1/ws",
		"/.well-known/",
		"/sso/",
	}
	for _, prefix := range exemptPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func isWriteMethod(method string) bool {
	switch method {
	case "POST", "PUT", "PATCH", "DELETE":
		return true
	default:
		return false
	}
}
