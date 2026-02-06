package handlers

import (
	"github.com/automax/backend/internal/services"
	"github.com/gofiber/fiber/v2"
)

type NotificationHandler struct {
	service *services.NotificationService
}

func NewNotificationHandler(service *services.NotificationService) *NotificationHandler {
	return &NotificationHandler{service: service}
}

type SendNotificationRequest struct {
	Channel      string  `json:"channel"`
	TemplateCode *string `json:"templateCode,omitempty"`
	Language     string  `json:"language"`
	To           string  `json:"to"`

	Subject   string            `json:"subject,omitempty"`
	Body      string            `json:"body,omitempty"`
	Variables map[string]string `json:"variables,omitempty"`
}

type SendNotificationResponse struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Provider string `json:"provider"`
}

func (h *NotificationHandler) Send(c *fiber.Ctx) error {
	var req SendNotificationRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": err.Error()})
	}

	// send notification
	log, err := h.service.SendNotification(
		c.Context(),
		req.Channel,
		req.TemplateCode, // now *string
		req.Language,
		req.To,
		req.Subject,
		req.Body,
		req.Variables,
	)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": err.Error()})
	}

	// return more info in response
	res := SendNotificationResponse{
		ID:       log.ID.String(),
		Status:   log.Status,
		Provider: log.Provider,
	}

	return c.Status(200).JSON(res)
}
