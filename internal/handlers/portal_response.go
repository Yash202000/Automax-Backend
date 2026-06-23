package handlers

import (
	"os"
	"strings"

	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

// isPortalContext returns true when the request originates from the EPM portal.
// Checks both the query string (?source=epmportal) and an optional body-level source value.
func isPortalContext(c *fiber.Ctx, bodySource ...string) bool {
	clientCode := strings.TrimSpace(os.Getenv("CLIENT_CODE"))
	if !strings.EqualFold(clientCode, constants.CLIENT_CODE.EPM940) {
		return false
	}
	if strings.EqualFold(c.Query("source"), constants.INCIDENT_SOURCE.EPMPORTAL) {
		return true
	}
	for _, s := range bodySource {
		if strings.EqualFold(s, constants.INCIDENT_SOURCE.EPMPORTAL) {
			return true
		}
	}
	return false
}

// ErrorResponseWithKey sends an error response using the i18n key for the message.
// For portal requests, it also includes an error_code field (uppercased i18n key).
// For non-portal requests, it returns the standard error response.
func ErrorResponseWithKey(c *fiber.Ctx, statusCode int, i18nKey string, bodySource ...string) error {
	message := i18n.T(c.UserContext(), i18nKey)
	if isPortalContext(c, bodySource...) {
		code := strings.ToUpper(i18nKey)
		return c.Status(statusCode).JSON(utils.ErrorCodeResponse{
			Success:   false,
			ErrorCode: code,
			Error:     message,
		})
	}
	return c.Status(statusCode).JSON(utils.Response{
		Success: false,
		Error:   message,
	})
}
