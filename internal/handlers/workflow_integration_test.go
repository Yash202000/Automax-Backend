package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestWorkflowHandlerIntegration(t *testing.T) {
	t.Run("CreateWorkflow - Invalid JSON", func(t *testing.T) {
		app := fiber.New()
		handler := NewWorkflowHandler(nil, nil)
		app.Post("/workflows", handler.CreateWorkflow)

		req := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewBuffer([]byte("invalid{")))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("CreateWorkflow - Missing required fields", func(t *testing.T) {
		app := fiber.New()
		handler := NewWorkflowHandler(nil, nil)
		app.Post("/workflows", handler.CreateWorkflow)

		payload := map[string]string{}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("UpdateWorkflow - Invalid UUID", func(t *testing.T) {
		app := fiber.New()
		handler := NewWorkflowHandler(nil, nil)
		app.Put("/workflows/:id", handler.UpdateWorkflow)

		req := httptest.NewRequest(http.MethodPut, "/workflows/not-a-uuid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("DeleteWorkflow - Invalid UUID", func(t *testing.T) {
		app := fiber.New()
		handler := NewWorkflowHandler(nil, nil)
		app.Delete("/workflows/:id", handler.DeleteWorkflow)

		req := httptest.NewRequest(http.MethodDelete, "/workflows/not-a-uuid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("GetWorkflow - Invalid UUID", func(t *testing.T) {
		app := fiber.New()
		handler := NewWorkflowHandler(nil, nil)
		app.Get("/workflows/:id", handler.GetWorkflow)

		req := httptest.NewRequest(http.MethodGet, "/workflows/not-a-uuid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})
}