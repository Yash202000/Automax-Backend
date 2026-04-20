package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestReportHandlerIntegration(t *testing.T) {
	t.Run("CreateReport - Invalid JSON", func(t *testing.T) {
		app := fiber.New()
		handler := NewReportHandler(nil)
		app.Post("/reports", handler.CreateReport)

		req := httptest.NewRequest(http.MethodPost, "/reports", bytes.NewBuffer([]byte("invalid{")))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("CreateReport - Missing required fields", func(t *testing.T) {
		app := fiber.New()
		handler := NewReportHandler(nil)
		app.Post("/reports", handler.CreateReport)

		payload := map[string]string{}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/reports", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("UpdateReport - Invalid UUID", func(t *testing.T) {
		app := fiber.New()
		handler := NewReportHandler(nil)
		app.Put("/reports/:id", handler.UpdateReport)

		req := httptest.NewRequest(http.MethodPut, "/reports/not-a-uuid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("DeleteReport - Invalid UUID", func(t *testing.T) {
		app := fiber.New()
		handler := NewReportHandler(nil)
		app.Delete("/reports/:id", handler.DeleteReport)

		req := httptest.NewRequest(http.MethodDelete, "/reports/not-a-uuid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("GetReport - Invalid UUID", func(t *testing.T) {
		app := fiber.New()
		handler := NewReportHandler(nil)
		app.Get("/reports/:id", handler.GetReport)

		req := httptest.NewRequest(http.MethodGet, "/reports/not-a-uuid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("ExecuteReport - Invalid UUID", func(t *testing.T) {
		app := fiber.New()
		handler := NewReportHandler(nil)
		app.Post("/reports/:id/execute", handler.ExecuteReport)

		req := httptest.NewRequest(http.MethodPost, "/reports/not-a-uuid/execute", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})
}