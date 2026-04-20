package handlers

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/automax/backend/pkg/constants"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

func TestFCMHandlerIntegration(t *testing.T) {
	app := fiber.New()

	t.Run("RegisterToken - Invalid JSON", func(t *testing.T) {
		handler := NewFCMHandler(nil)
		app.Post("/fcm/register", handler.RegisterToken)

		req := httptest.NewRequest(http.MethodPost, "/fcm/register", bytes.NewBuffer([]byte("invalid json{")))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("RegisterToken - Missing required fields", func(t *testing.T) {
		handler := NewFCMHandler(nil)
		app.Post("/fcm/register", handler.RegisterToken)

		payload := map[string]string{}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/fcm/register", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("GetUserDeviceTokens - Unauthorized", func(t *testing.T) {
		handler := NewFCMHandler(nil)
		app.Get("/fcm/tokens", handler.GetUserDeviceTokens)

		req := httptest.NewRequest(http.MethodGet, "/fcm/tokens", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("GetUserDeviceTokens - Unauthorized without user", func(t *testing.T) {
		handler := NewFCMHandler(nil)
		app.Get("/fcm/tokens", handler.GetUserDeviceTokens)

		req := httptest.NewRequest(http.MethodGet, "/fcm/tokens", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("RemoveDevice - Unauthorized", func(t *testing.T) {
		handler := NewFCMHandler(nil)
		app.Delete("/fcm/device", handler.RemoveDevice)

		req := httptest.NewRequest(http.MethodDelete, "/fcm/device", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Errorf("expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("RemoveDevice - Missing token (with auth)", func(t *testing.T) {
		handler := NewFCMHandler(nil)
		testApp := fiber.New()
		testApp.Use(func(c *fiber.Ctx) error {
			c.Locals(constants.ContextKeys.UserID, uuid.New())
			return c.Next()
		})
		testApp.Delete("/fcm/device", handler.RemoveDevice)

		payload := map[string]string{}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodDelete, "/fcm/device", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := testApp.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("PushNotification - Invalid JSON", func(t *testing.T) {
		handler := NewFCMHandler(nil)
		app.Post("/fcm/push", handler.PushNotification)

		req := httptest.NewRequest(http.MethodPost, "/fcm/push", bytes.NewBuffer([]byte("invalid")))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("PushNotification - Missing user_id", func(t *testing.T) {
		handler := NewFCMHandler(nil)
		app.Post("/fcm/push", handler.PushNotification)

		payload := map[string]string{
			"title": "Test",
			"body":  "Test body",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/fcm/push", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("PushNotification - Missing title", func(t *testing.T) {
		handler := NewFCMHandler(nil)
		app.Post("/fcm/push", handler.PushNotification)

		payload := map[string]interface{}{
			"user_id": uuid.New().String(),
			"body":    "Test body",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/fcm/push", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("PushNotification - Missing body", func(t *testing.T) {
		handler := NewFCMHandler(nil)
		app.Post("/fcm/push", handler.PushNotification)

		payload := map[string]interface{}{
			"user_id": uuid.New().String(),
			"title":   "Test Title",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/fcm/push", bytes.NewBuffer(body))
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
