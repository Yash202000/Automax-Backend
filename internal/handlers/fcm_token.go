package handlers

import (
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/google/uuid"

	"github.com/gofiber/fiber/v2"
)

type FCMHandler struct {
	service *services.FCMService
}

func NewFCMHandler(service *services.FCMService) *FCMHandler {
	return &FCMHandler{service: service}
}

func (h *FCMHandler) RegisterToken(c *fiber.Ctx) error {

	var req models.RegisterTokenRequest

	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request body",
		})
	}

	if req.DeviceToken == "" || req.UserID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid Payload",
		})
	}

	// Call service
	err := h.service.RegisterDeviceToken(req.UserID, req.DeviceToken, req.DeviceType)
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

	userIDValue := c.Locals(constants.ContextKeys.UserID)
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

	userIDValue := c.Locals(constants.ContextKeys.UserID)
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

	var req models.PushRequest

	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "Invalid request body",
		})
	}

	// Validate required fields
	if req.UserID == uuid.Nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "user_id is required",
		})
	}

	if req.Title == "" || req.Body == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"error":   "title and body are required",
		})
	}

	// get sentBy from context
	if userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID); ok {
		req.SentBy = &userID
	}

	err := h.service.Push(c.UserContext(), &req)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"error":   err.Error(),
		})
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Push notification sent successfully",
	})
}
