package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func setupApp() *fiber.App {
	app := fiber.New()
	handler := NewHealthHandler()

	app.Get("/health", handler.Health)
	app.Get("/ready", handler.Ready)

	return app
}

func TestHealthHandler(t *testing.T) {
	app := setupApp()

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	resp, err := app.Test(req)

	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&body)

	if body["status"] != "healthy" {
		t.Errorf("expected healthy, got %v", body["status"])
	}
}

func TestReadyHandler(t *testing.T) {
	app := setupApp()

	req := httptest.NewRequest(http.MethodGet, "/ready", nil)
	resp, err := app.Test(req)

	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var body map[string]interface{}
	_ = json.NewDecoder(resp.Body).Decode(&body)

	if body["status"] != "ready" {
		t.Errorf("expected ready, got %v", body["status"])
	}
}
