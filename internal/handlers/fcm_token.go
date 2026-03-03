package handlers

import (
	"github.com/automax/backend/internal/services"
	"github.com/google/uuid"

	"github.com/gofiber/fiber/v2"
)

type FCMHandler struct {
	service *services.FCMService
}

func NewFCMHandler(service *services.FCMService) *FCMHandler {
	return &FCMHandler{service: service}
}

type RegisterTokenRequest struct {
	DeviceToken string `json:"device_token"`
	DeviceType  string `json:"device_type"`
}

func (h *FCMHandler) RegisterToken(c *fiber.Ctx) error {

	var req RegisterTokenRequest

	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	if req.DeviceToken == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "device_token is required",
		})
	}

	// Get UUID string from JWT middleware
	var userId *uuid.UUID
	if userID, ok := c.Locals("user_id").(uuid.UUID); ok {
		userId = &userID
	}

	// Call service
	err := h.service.RegisterDeviceToken(userId, req.DeviceToken, req.DeviceType)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Failed to register device token",
		})
	}

	return c.JSON(fiber.Map{
		"message": "Device token registered successfully",
	})
}

func (h *FCMHandler) GetUserDeviceTokens(c *fiber.Ctx) error {

	userIDValue := c.Locals("user_id")
	if userIDValue == nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"success": false,
			"error":   "Unauthorized",
		})
	}

	userUUID := userIDValue.(uuid.UUID)

	tokens, err := h.service.GetUserDeviceTokens(userUUID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   "Failed to fetch device tokens",
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    tokens,
	})
}

func (h *FCMHandler) RemoveDevice(c *fiber.Ctx) error {

	userIDValue := c.Locals("user_id")
	if userIDValue == nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"success": false,
			"error":   "Unauthorized",
		})
	}

	userUUID := userIDValue.(uuid.UUID)

	var req struct {
		DeviceToken string `json:"device_token"`
	}

	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "Invalid request body",
		})
	}

	if req.DeviceToken == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "Device token required",
		})
	}

	err := h.service.RemoveDeviceToken(userUUID, req.DeviceToken)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   "Failed to remove device",
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Device removed successfully",
	})
}

func (h *FCMHandler) PushNotification(c *fiber.Ctx) error {

	var req struct {
		UserID   uuid.UUID `json:"user_id"`
		Title    string    `json:"title"`
		Body     string    `json:"body"`
		UserType string    `json:"user_type"`
	}

	var sentBy *uuid.UUID
	if userID, ok := c.Locals("user_id").(uuid.UUID); ok {
		sentBy = &userID
	}
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "Invalid request body",
		})
	}

	err := h.service.Push(c.Context(), req.UserID, req.Title, req.Body, req.UserType, sentBy)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Push notification sent",
	})
}
