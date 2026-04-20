package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestGoalHandlerIntegration(t *testing.T) {
	t.Run("CreateGoal - Invalid JSON", func(t *testing.T) {
		app := fiber.New()
		handler := NewGoalHandler(nil, nil)
		app.Post("/goals", handler.CreateGoal)

		req := httptest.NewRequest(http.MethodPost, "/goals", bytes.NewBuffer([]byte("invalid{")))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("CreateGoal - Missing required fields", func(t *testing.T) {
		app := fiber.New()
		handler := NewGoalHandler(nil, nil)
		app.Post("/goals", handler.CreateGoal)

		payload := map[string]string{}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/goals", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("UpdateGoal - Invalid UUID", func(t *testing.T) {
		app := fiber.New()
		handler := NewGoalHandler(nil, nil)
		app.Put("/goals/:id", handler.UpdateGoal)

		req := httptest.NewRequest(http.MethodPut, "/goals/not-a-uuid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("DeleteGoal - Invalid UUID", func(t *testing.T) {
		app := fiber.New()
		handler := NewGoalHandler(nil, nil)
		app.Delete("/goals/:id", handler.DeleteGoal)

		req := httptest.NewRequest(http.MethodDelete, "/goals/not-a-uuid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})
}