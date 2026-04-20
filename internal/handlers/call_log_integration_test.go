package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestCallLogHandlerIntegration(t *testing.T) {
	t.Run("CreateCallLog - Invalid JSON", func(t *testing.T) {
		app := fiber.New()
		handler := NewCallLogHandler(nil, nil, nil)
		app.Post("/call-logs", handler.CreateCallLog)

		req := httptest.NewRequest(http.MethodPost, "/call-logs", bytes.NewBuffer([]byte("invalid{")))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("CreateCallLog - Missing required fields", func(t *testing.T) {
		app := fiber.New()
		handler := NewCallLogHandler(nil, nil, nil)
		app.Post("/call-logs", handler.CreateCallLog)

		payload := map[string]string{}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/call-logs", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("GetCallLog - Invalid UUID", func(t *testing.T) {
		app := fiber.New()
		handler := NewCallLogHandler(nil, nil, nil)
		app.Get("/call-logs/:id", handler.GetCallLog)

		req := httptest.NewRequest(http.MethodGet, "/call-logs/not-a-uuid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("UpdateCallLog - Invalid JSON", func(t *testing.T) {
		app := fiber.New()
		handler := NewCallLogHandler(nil, nil, nil)
		app.Put("/call-logs/:id", handler.UpdateCallLog)

		req := httptest.NewRequest(http.MethodPut, "/call-logs/550e8400-e29b-41d4-a716-446655440000", bytes.NewBuffer([]byte("invalid{")))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("DeleteCallLog - Invalid UUID", func(t *testing.T) {
		app := fiber.New()
		handler := NewCallLogHandler(nil, nil, nil)
		app.Delete("/call-logs/:id", handler.DeleteCallLog)

		req := httptest.NewRequest(http.MethodDelete, "/call-logs/not-a-uuid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})
}