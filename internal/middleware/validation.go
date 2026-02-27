package middleware

import (
	"context"
	"strings"

	"github.com/automax/backend/pkg/constants"
	"github.com/gofiber/fiber/v2"
)

// ValidationMiddleware adds validator and translator to the Fiber context
func ValidationMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Get language from header (default to English)
		lang := c.Get("Accept-Language", "en")
		if lang != "en" && lang != "ar" {
			lang = strings.Split(strings.Split(lang, ",")[0], "-")[0]
		}

		if lang == "" {
			lang = "en"
		}

		// Create a new context with validator and translator
		ctx := context.WithValue(c.UserContext(), constants.ContextKeys.ACCEPT_LANGUAGE, lang)

		// Store the updated context back in Fiber
		c.SetUserContext(ctx)

		return c.Next()
	}
}
