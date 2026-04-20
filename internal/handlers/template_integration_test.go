package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestTemplateHandlerIntegration(t *testing.T) {
	t.Run("Create - Invalid JSON", func(t *testing.T) {
		app := fiber.New()
		handler := NewNotificationTemplateHandler(nil)
		app.Post("/templates", handler.Create)

		req := httptest.NewRequest(http.MethodPost, "/templates", bytes.NewBuffer([]byte("invalid{")))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("Create - Invalid UUID in body", func(t *testing.T) {
		app := fiber.New()
		handler := NewNotificationTemplateHandler(nil)
		app.Post("/templates", handler.Create)

		payload := map[string]interface{}{
			"id": "not-a-uuid",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/templates", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("Update - Invalid UUID", func(t *testing.T) {
		app := fiber.New()
		handler := NewNotificationTemplateHandler(nil)
		app.Put("/templates/:id", handler.Update)

		req := httptest.NewRequest(http.MethodPut, "/templates/not-a-uuid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("Delete - Invalid UUID", func(t *testing.T) {
		app := fiber.New()
		handler := NewNotificationTemplateHandler(nil)
		app.Delete("/templates/:id", handler.Delete)

		req := httptest.NewRequest(http.MethodDelete, "/templates/not-a-uuid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("GetByID - Invalid UUID", func(t *testing.T) {
		app := fiber.New()
		handler := NewNotificationTemplateHandler(nil)
		app.Get("/templates/:id", handler.GetByID)

		req := httptest.NewRequest(http.MethodGet, "/templates/not-a-uuid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})
}