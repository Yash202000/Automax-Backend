package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestReviewHandlerIntegration(t *testing.T) {
	t.Run("CreateCycle - Invalid JSON", func(t *testing.T) {
		app := fiber.New()
		handler := NewReviewHandler(nil)
		app.Post("/reviews/cycles", handler.CreateCycle)

		req := httptest.NewRequest(http.MethodPost, "/reviews/cycles", bytes.NewBuffer([]byte("invalid{")))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("CreateCycle - Missing required fields", func(t *testing.T) {
		app := fiber.New()
		handler := NewReviewHandler(nil)
		app.Post("/reviews/cycles", handler.CreateCycle)

		payload := map[string]string{}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/reviews/cycles", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("CreateCycle - Without user", func(t *testing.T) {
		app := fiber.New()
		handler := NewReviewHandler(nil)
		app.Post("/reviews/cycles", handler.CreateCycle)

		payload := map[string]interface{}{
			"name":       "Test Cycle",
			"start_date": "2024-01-01",
			"end_date":   "2024-12-31",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/reviews/cycles", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("UpdateCycle - Invalid UUID", func(t *testing.T) {
		app := fiber.New()
		handler := NewReviewHandler(nil)
		app.Put("/reviews/cycles/:id", handler.UpdateCycle)

		req := httptest.NewRequest(http.MethodPut, "/reviews/cycles/not-a-uuid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("DeleteCycle - Invalid UUID", func(t *testing.T) {
		app := fiber.New()
		handler := NewReviewHandler(nil)
		app.Delete("/reviews/cycles/:id", handler.DeleteCycle)

		req := httptest.NewRequest(http.MethodDelete, "/reviews/cycles/not-a-uuid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("GetCycle - Invalid UUID", func(t *testing.T) {
		app := fiber.New()
		handler := NewReviewHandler(nil)
		app.Get("/reviews/cycles/:id", handler.GetCycle)

		req := httptest.NewRequest(http.MethodGet, "/reviews/cycles/not-a-uuid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})
}