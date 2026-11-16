package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/automax/backend/internal/models"
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
		c.Locals(constants.ContextKeys.ACCEPT_LANGUAGE, lang)

		// Store the updated context back in Fiber
		c.SetUserContext(ctx)

		if err := c.Next(); err != nil {
			return err
		}
		if lang == "ar" && c.Method() == fiber.MethodGet &&
			bytes.HasPrefix(c.Response().Header.ContentType(), []byte(fiber.MIMEApplicationJSON)) {
			// Cheap pre-check so the regex only runs on responses that contain duration options.
			// Content-Length is recomputed by fasthttp when the response is written.
			if body := c.Response().Body(); bytes.Contains(body, []byte(`"duration_options"`)) {
				c.Response().SetBodyRaw(durationOptionsRe.ReplaceAllFunc(body, localizeDurationOptions))
			}
		}
		return nil
	}
}

var durationOptionsRe = regexp.MustCompile(`"duration_options":\[(?:[^\]"]|"(?:[^"\\]|\\.)*")*\]`)

// localizeDurationOptions rewrites a "duration_options":[...] JSON fragment into Arabic labels.
func localizeDurationOptions(frag []byte) []byte {
	var opts []string
	if json.Unmarshal(frag[len(`"duration_options":`):], &opts) != nil {
		return frag
	}
	for i, o := range opts {
		opts[i] = models.LocalizeDurationLabelAr(o)
	}
	b, _ := json.Marshal(opts)
	return append([]byte(`"duration_options":`), b...)
}
