package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestSSOHandlerIntegration(t *testing.T) {
	t.Run("Launch - Without auth", func(t *testing.T) {
		app := fiber.New()
		handler := NewSSOHandler(nil, nil, nil, nil, nil, "")
		app.Post("/sso/launch", handler.Launch)

		payload := map[string]string{
			"app_link_id": "test-id",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/sso/launch", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("Callback - Missing token", func(t *testing.T) {
		app := fiber.New()
		handler := NewSSOHandler(nil, nil, nil, nil, nil, "")
		app.Get("/sso/callback", handler.Callback)

		req := httptest.NewRequest(http.MethodGet, "/sso/callback", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("Callback - Invalid token", func(t *testing.T) {
		app := fiber.New()
		handler := NewSSOHandler(nil, nil, nil, nil, nil, "")
		app.Get("/sso/callback", handler.Callback)

		req := httptest.NewRequest(http.MethodGet, "/sso/callback?token=invalid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusUnauthorized && resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400 or 401, got %d", resp.StatusCode)
		}
	})
}