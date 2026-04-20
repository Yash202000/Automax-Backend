package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestEscalationPolicyHandlerIntegration(t *testing.T) {
	t.Run("Create - Invalid JSON", func(t *testing.T) {
		app := fiber.New()
		handler := NewEscalationPolicyHandler(nil, nil, nil)
		app.Post("/escalation-policies", handler.Create)

		req := httptest.NewRequest(http.MethodPost, "/escalation-policies", bytes.NewBuffer([]byte("invalid{")))
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
		handler := NewEscalationPolicyHandler(nil, nil, nil)
		app.Post("/escalation-policies", handler.Create)

		payload := map[string]string{}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/escalation-policies", bytes.NewBuffer(body))
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
		handler := NewEscalationPolicyHandler(nil, nil, nil)
		app.Get("/escalation-policies/:id", handler.GetByID)

		req := httptest.NewRequest(http.MethodGet, "/escalation-policies/not-a-uuid", nil)
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
		handler := NewEscalationPolicyHandler(nil, nil, nil)
		app.Put("/escalation-policies/:id", handler.Update)

		req := httptest.NewRequest(http.MethodPut, "/escalation-policies/550e8400-e29b-41d4-a716-446655440000", bytes.NewBuffer([]byte("invalid{")))
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
		handler := NewEscalationPolicyHandler(nil, nil, nil)
		app.Put("/escalation-policies/:id", handler.Update)

		req := httptest.NewRequest(http.MethodPut, "/escalation-policies/not-a-uuid", nil)
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
		handler := NewEscalationPolicyHandler(nil, nil, nil)
		app.Delete("/escalation-policies/:id", handler.Delete)

		req := httptest.NewRequest(http.MethodDelete, "/escalation-policies/not-a-uuid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("ResolveTargetUsers - Invalid JSON", func(t *testing.T) {
		app := fiber.New()
		handler := NewEscalationPolicyHandler(nil, nil, nil)
		app.Post("/escalation-policies/resolve-users", handler.ResolveTargetUsers)

		req := httptest.NewRequest(http.MethodPost, "/escalation-policies/resolve-users", bytes.NewBuffer([]byte("invalid{")))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("ResolveTargetUsers - Invalid department_id", func(t *testing.T) {
		app := fiber.New()
		handler := NewEscalationPolicyHandler(nil, nil, nil)
		app.Post("/escalation-policies/resolve-users", handler.ResolveTargetUsers)

		payload := map[string]interface{}{
			"department_id": "not-a-uuid",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/escalation-policies/resolve-users", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("ResolveTargetUsers - Invalid role_id", func(t *testing.T) {
		app := fiber.New()
		handler := NewEscalationPolicyHandler(nil, nil, nil)
		app.Post("/escalation-policies/resolve-users", handler.ResolveTargetUsers)

		payload := map[string]interface{}{
			"role_id": "not-a-uuid",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/escalation-policies/resolve-users", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})
}