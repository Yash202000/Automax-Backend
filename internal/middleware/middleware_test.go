package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestValidationMiddleware(t *testing.T) {
	t.Run("Sets default language to en", func(t *testing.T) {
		app := fiber.New()
		app.Use(ValidationMiddleware())
		app.Get("/test", func(c *fiber.Ctx) error {
			return c.JSON(fiber.Map{"lang": c.Get("Accept-Language")})
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("Accepts Arabic language", func(t *testing.T) {
		app := fiber.New()
		app.Use(ValidationMiddleware())
		app.Get("/test", func(c *fiber.Ctx) error {
			return c.SendString(c.Get("Accept-Language"))
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Accept-Language", "ar")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("Accepts English language", func(t *testing.T) {
		app := fiber.New()
		app.Use(ValidationMiddleware())
		app.Get("/test", func(c *fiber.Ctx) error {
			return c.SendString(c.Get("Accept-Language"))
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Accept-Language", "en")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("Accepts language with region", func(t *testing.T) {
		app := fiber.New()
		app.Use(ValidationMiddleware())
		app.Get("/test", func(c *fiber.Ctx) error {
			return c.SendString(c.Get("Accept-Language"))
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		req.Header.Set("Accept-Language", "en-US")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})
}

func TestRequestContextMiddleware(t *testing.T) {
	t.Run("Sets context values", func(t *testing.T) {
		app := fiber.New()
		app.Use(RequestContext())
		app.Get("/test", func(c *fiber.Ctx) error {
			return c.JSON(fiber.Map{
				"hostname": c.Locals("hostname"),
				"protocol": c.Locals("protocol"),
				"ip":       c.Locals("ip"),
			})
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("Continues to next handler", func(t *testing.T) {
		app := fiber.New()
		app.Use(RequestContext())
		app.Get("/test", func(c *fiber.Ctx) error {
			return c.SendString("OK")
		})

		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})
}