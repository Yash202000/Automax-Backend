package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestEscalationHandlerIntegration(t *testing.T) {
	t.Run("ListByIncident - Missing incident_id", func(t *testing.T) {
		app := fiber.New()
		handler := NewEscalationHandler(nil)
		app.Get("/escalation/incident/:incident_id", handler.ListByIncident)

		req := httptest.NewRequest(http.MethodGet, "/escalation/incident/", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("ListByIncident - Invalid UUID", func(t *testing.T) {
		app := fiber.New()
		handler := NewEscalationHandler(nil)
		app.Get("/escalation/incident/:incident_id", handler.ListByIncident)

		req := httptest.NewRequest(http.MethodGet, "/escalation/incident/not-a-uuid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})
}