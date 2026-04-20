package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"
)

func TestNotificationHandlerIntegration(t *testing.T) {
	t.Run("SendGridInboundWebhook - Invalid multipart", func(t *testing.T) {
		app := fiber.New()
		handler := NewNotificationHandler(nil, nil)
		app.Post("/webhooks/sendgrid/inbound", handler.SendGridInboundWebhook)

		req := httptest.NewRequest(http.MethodPost, "/webhooks/sendgrid/inbound", bytes.NewBuffer([]byte("not multipart")))
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
