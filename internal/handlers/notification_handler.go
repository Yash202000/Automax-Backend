package handlers

import (
	"io"
	"strconv"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type NotificationHandler struct {
	service *services.NotificationService
}

func NewNotificationHandler(service *services.NotificationService) *NotificationHandler {
	return &NotificationHandler{service: service}
}

type SendNotificationRequest struct {
	Channel      string   `json:"channel"`
	TemplateCode *string  `json:"templateCode,omitempty"`
	Language     string   `json:"language"`
	To           []string `json:"to"`
	CC           []string `json:"cc,omitempty"`
	BCC          []string `json:"bcc,omitempty"`

	Subject   string            `json:"subject,omitempty"`
	Body      string            `json:"body,omitempty"`
	Variables map[string]string `json:"variables,omitempty"`
}

type SendNotificationResponse struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Provider string `json:"provider"`
}

// Send handles POST /api/v1/notifications/send with multipart/form-data support
func (h *NotificationHandler) Send(c *fiber.Ctx) error {
	// Get user ID from context
	var sentBy *uuid.UUID
	if userID, ok := c.Locals("user_id").(uuid.UUID); ok {
		sentBy = &userID
	}

	// Parse multipart form
	form, err := c.MultipartForm()
	if err != nil {
		// Try JSON fallback
		var req SendNotificationRequest
		if jsonErr := c.BodyParser(&req); jsonErr != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request format")
		}

		// Send without attachments
		log, err := h.service.SendNotification(c.Context(), req.Channel, req.TemplateCode, req.Language, req.To, req.CC, req.BCC, req.Subject, req.Body, req.Variables, nil, sentBy)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
		}

		res := SendNotificationResponse{
			ID:       log.ID.String(),
			Status:   log.Status,
			Provider: log.Provider,
		}

		return c.Status(fiber.StatusOK).JSON(fiber.Map{
			"success": true,
			"data":    res,
		})
	}

	// Extract form fields
	channel := c.FormValue("channel")
	templateCode := c.FormValue("templateCode")
	language := c.FormValue("language", "en")
	to := form.Value["to"]
	cc := form.Value["cc"]
	bcc := form.Value["bcc"]
	subject := c.FormValue("subject")
	body := c.FormValue("body")

	// Parse variables if provided
	variables := make(map[string]string)
	if varsStr := c.FormValue("variables"); varsStr != "" {
		// Simple key=value parsing (can be enhanced to JSON)
		// For now, expect comma-separated key=value pairs
	}

	// Handle attachments
	var attachments []models.AttachmentData
	if files, ok := form.File["attachments"]; ok {
		for _, fileHeader := range files {
			file, err := fileHeader.Open()
			if err != nil {
				return utils.ErrorResponse(c, fiber.StatusBadRequest, "Failed to read attachment: "+fileHeader.Filename)
			}
			defer file.Close()

			data, err := io.ReadAll(file)
			if err != nil {
				return utils.ErrorResponse(c, fiber.StatusBadRequest, "Failed to read attachment data: "+fileHeader.Filename)
			}

			attachments = append(attachments, models.AttachmentData{
				Filename:    fileHeader.Filename,
				ContentType: fileHeader.Header.Get("Content-Type"),
				Data:        data,
			})
		}
	}

	var templateCodePtr *string
	if templateCode != "" {
		templateCodePtr = &templateCode
	}

	// Send notification with attachments
	log, err := h.service.SendNotification(c.Context(), channel, templateCodePtr, language, to, cc, bcc, subject, body, variables, attachments, sentBy)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	res := SendNotificationResponse{
		ID:       log.ID.String(),
		Status:   log.Status,
		Provider: log.Provider,
	}

	return c.Status(fiber.StatusOK).JSON(fiber.Map{
		"success": true,
		"data":    res,
	})
}

// List handles GET /api/v1/notifications with search and filters
func (h *NotificationHandler) List(c *fiber.Ctx) error {
	filter := &models.NotificationLogFilter{
		Page:  1,
		Limit: 20,
	}

	// Parse query parameters
	if page := c.Query("page"); page != "" {
		if p, err := strconv.Atoi(page); err == nil {
			filter.Page = p
		}
	}
	if limit := c.Query("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil {
			filter.Limit = l
		}
	}
	if channel := c.Query("channel"); channel != "" {
		filter.Channel = channel
	}
	if status := c.Query("status"); status != "" {
		filter.Status = status
	}
	if search := c.Query("search"); search != "" {
		filter.Search = search
	}
	if sentBy := c.Query("sent_by"); sentBy != "" {
		if id, err := uuid.Parse(sentBy); err == nil {
			filter.SentBy = &id
		}
	}
	if templateCode := c.Query("template_code"); templateCode != "" {
		filter.TemplateCode = templateCode
	}
	if startDate := c.Query("start_date"); startDate != "" {
		if t, err := time.Parse("2006-01-02", startDate); err == nil {
			filter.StartDate = &t
		}
	}
	if endDate := c.Query("end_date"); endDate != "" {
		if t, err := time.Parse("2006-01-02", endDate); err == nil {
			// Set to end of day
			t = t.Add(24*time.Hour - time.Second)
			filter.EndDate = &t
		}
	}

	notifications, total, err := h.service.ListNotifications(c.Context(), filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	totalPages := (int(total) + filter.Limit - 1) / filter.Limit

	return c.JSON(fiber.Map{
		"success":     true,
		"data":        notifications,
		"total_items": total,
		"total_pages": totalPages,
		"page":        filter.Page,
		"limit":       filter.Limit,
	})
}

// Get handles GET /api/v1/notifications/:id
func (h *NotificationHandler) Get(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid notification ID")
	}

	notification, err := h.service.GetNotification(c.Context(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Notification not found")
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    notification,
	})
}

// Delete handles DELETE /api/v1/notifications/:id
func (h *NotificationHandler) Delete(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid notification ID")
	}

	if err := h.service.DeleteNotification(c.Context(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Notification deleted successfully",
	})
}
