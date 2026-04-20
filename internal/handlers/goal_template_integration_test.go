package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestGoalTemplateHandlerIntegration(t *testing.T) {
	t.Run("Create - Invalid JSON", func(t *testing.T) {
		app := fiber.New()
		handler := NewGoalTemplateHandler(nil)
		app.Post("/goal-templates", handler.Create)

		req := httptest.NewRequest(http.MethodPost, "/goal-templates", bytes.NewBuffer([]byte("invalid{")))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("Create - Missing required fields", func(t *testing.T) {
		app := fiber.New()
		handler := NewGoalTemplateHandler(nil)
		app.Post("/goal-templates", handler.Create)

		payload := map[string]string{}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/goal-templates", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
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
		handler := NewGoalTemplateHandler(nil)
		app.Get("/goal-templates/:id", handler.GetByID)

		req := httptest.NewRequest(http.MethodGet, "/goal-templates/not-a-uuid", nil)
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
		handler := NewGoalTemplateHandler(nil)
		app.Put("/goal-templates/:id", handler.Update)

		req := httptest.NewRequest(http.MethodPut, "/goal-templates/not-a-uuid", nil)
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
		handler := NewGoalTemplateHandler(nil)
		app.Delete("/goal-templates/:id", handler.Delete)

		req := httptest.NewRequest(http.MethodDelete, "/goal-templates/not-a-uuid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})
}