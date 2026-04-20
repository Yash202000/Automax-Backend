package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestEscalationGroupHandlerIntegration(t *testing.T) {
	t.Run("Create - Invalid JSON", func(t *testing.T) {
		app := fiber.New()
		handler := NewEscalationGroupHandler(nil)
		app.Post("/escalation-groups", handler.Create)

		req := httptest.NewRequest(http.MethodPost, "/escalation-groups", bytes.NewBuffer([]byte("invalid{")))
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
		handler := NewEscalationGroupHandler(nil)
		app.Post("/escalation-groups", handler.Create)

		payload := map[string]string{}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/escalation-groups", bytes.NewBuffer(body))
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
		handler := NewEscalationGroupHandler(nil)
		app.Get("/escalation-groups/:id", handler.GetByID)

		req := httptest.NewRequest(http.MethodGet, "/escalation-groups/not-a-uuid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("Update - Invalid JSON", func(t *testing.T) {
		app := fiber.New()
		handler := NewEscalationGroupHandler(nil)
		app.Put("/escalation-groups/:id", handler.Update)

		req := httptest.NewRequest(http.MethodPut, "/escalation-groups/550e8400-e29b-41d4-a716-446655440000", bytes.NewBuffer([]byte("invalid{")))
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
		handler := NewEscalationGroupHandler(nil)
		app.Put("/escalation-groups/:id", handler.Update)

		req := httptest.NewRequest(http.MethodPut, "/escalation-groups/not-a-uuid", nil)
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
		handler := NewEscalationGroupHandler(nil)
		app.Delete("/escalation-groups/:id", handler.Delete)

		req := httptest.NewRequest(http.MethodDelete, "/escalation-groups/not-a-uuid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})
}