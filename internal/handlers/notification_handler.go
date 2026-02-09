package handlers

import (
	"io"
	"log"
	"strconv"
	"strings"
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

// SendGridInboundWebhook handles incoming emails from SendGrid Inbound Parse
// POST /api/v1/webhooks/sendgrid/inbound
func (h *NotificationHandler) SendGridInboundWebhook(c *fiber.Ctx) error {
	// Parse multipart form data
	form, err := c.MultipartForm()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Failed to parse multipart form")
	}

	// Extract email fields from SendGrid webhook
	from := c.FormValue("from")
	to := c.FormValue("to")
	subject := c.FormValue("subject")
	textBody := c.FormValue("text")
	htmlBody := c.FormValue("html")
	cc := c.FormValue("cc")

	// Parse headers for additional info (optional)
	headersJSON := c.FormValue("headers")

	if from == "" || to == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Missing required fields: from or to")
	}

	// Parse recipients (TO field can be comma-separated)
	toRecipients := parseEmailAddresses(to)
	ccRecipients := parseEmailAddresses(cc)

	// Handle attachments
	var attachments []models.AttachmentData
	if files, ok := form.File["attachment"]; ok {
		for _, fileHeader := range files {
			file, err := fileHeader.Open()
			if err != nil {
				continue // Skip failed attachments
			}
			defer file.Close()

			data, err := io.ReadAll(file)
			if err != nil {
				continue
			}

			attachments = append(attachments, models.AttachmentData{
				Filename:    fileHeader.Filename,
				ContentType: fileHeader.Header.Get("Content-Type"),
				Data:        data,
			})
		}
	}

	// Try to find the recipient user in the system
	// You'll need to implement user lookup by email
	var receivedBy *uuid.UUID
	// TODO: Look up user by email address
	// receivedBy = findUserByEmail(toRecipients[0])

	// Create recipient info array
	var recipients models.RecipientArray
	for _, email := range toRecipients {
		recipients = append(recipients, models.RecipientInfo{
			Email:  email,
			Type:   "to",
			Status: "received",
		})
	}

	// Convert attachments to attachment info
	var attachmentInfo models.AttachmentArray
	for _, att := range attachments {
		attachmentInfo = append(attachmentInfo, models.AttachmentInfo{
			Filename:    att.Filename,
			ContentType: att.ContentType,
			Size:        int64(len(att.Data)),
		})
	}

	// Create inbound notification log
	now := time.Now()
	inboundLog := &models.NotificationLog{
		ID:          uuid.New(),
		Channel:     "email",
		Direction:   models.DirectionInbound,
		Category:    models.CategoryInbox,
		From:        from,
		Recipients:  recipients,
		CC:          ccRecipients,
		Subject:     subject,
		Body:        textBody,
		BodyHTML:    htmlBody,
		Status:      "received",
		Provider:    "sendgrid",
		IsRead:      false,
		IsStarred:   false,
		Attachments: attachmentInfo,
		ReceivedBy:  receivedBy,
		SentAt:      &now,
		CreatedAt:   now,
		UpdatedAt:   &now,
	}

	// Save to database using the service
	if err := h.service.SaveInboundNotification(c.Context(), inboundLog); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to save inbound email: "+err.Error())
	}

	// Log for debugging (optional)
	log.Printf("Received inbound email from %s to %s with subject: %s (Headers: %s)", from, to, subject, headersJSON)

	// Return 200 OK to SendGrid
	return c.JSON(fiber.Map{
		"success": true,
		"message": "Email received successfully",
		"id":      inboundLog.ID.String(),
	})
}

// parseEmailAddresses splits comma-separated email addresses
func parseEmailAddresses(emails string) []string {
	if emails == "" {
		return []string{}
	}

	result := []string{}
	parts := strings.Split(emails, ",")
	for _, part := range parts {
		email := strings.TrimSpace(part)
		if email != "" {
			result = append(result, email)
		}
	}
	return result
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
	if direction := c.Query("direction"); direction != "" {
		filter.Direction = direction
	}
	if category := c.Query("category"); category != "" {
		filter.Category = category
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
	if receivedBy := c.Query("received_by"); receivedBy != "" {
		if id, err := uuid.Parse(receivedBy); err == nil {
			filter.ReceivedBy = &id
		}
	}
	if isRead := c.Query("is_read"); isRead != "" {
		if b, err := strconv.ParseBool(isRead); err == nil {
			filter.IsRead = &b
		}
	}
	if isStarred := c.Query("is_starred"); isStarred != "" {
		if b, err := strconv.ParseBool(isStarred); err == nil {
			filter.IsStarred = &b
		}
	}
	if threadID := c.Query("thread_id"); threadID != "" {
		if id, err := uuid.Parse(threadID); err == nil {
			filter.ThreadID = &id
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

// Delete handles DELETE /api/v1/notifications/:id (moves to trash)
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
		"message": "Notification moved to trash",
	})
}

// PermanentDelete handles DELETE /api/v1/notifications/:id/permanent
func (h *NotificationHandler) PermanentDelete(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid notification ID")
	}

	if err := h.service.PermanentDelete(c.Context(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Notification permanently deleted",
	})
}

// CreateDraft handles POST /api/v1/notifications/drafts
func (h *NotificationHandler) CreateDraft(c *fiber.Ctx) error {
	var req models.CreateDraftRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	// Get user ID from context
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, "User not authenticated")
	}

	draft, err := h.service.CreateDraft(c.Context(), &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"success": true,
		"data":    draft,
	})
}

// UpdateDraft handles PUT /api/v1/notifications/drafts/:id
func (h *NotificationHandler) UpdateDraft(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid draft ID")
	}

	var req models.UpdateDraftRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	draft, err := h.service.UpdateDraft(c.Context(), id, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    draft,
	})
}

// SendDraft handles POST /api/v1/notifications/drafts/:id/send
func (h *NotificationHandler) SendDraft(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid draft ID")
	}

	result, err := h.service.SendDraft(c.Context(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    result,
		"message": "Draft sent successfully",
	})
}

// MarkAsRead handles PATCH /api/v1/notifications/:id/read
func (h *NotificationHandler) MarkAsRead(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid notification ID")
	}

	type ReadRequest struct {
		IsRead bool `json:"is_read"`
	}

	var req ReadRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.service.MarkAsRead(c.Context(), id, req.IsRead); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Notification updated",
	})
}

// ToggleStar handles PATCH /api/v1/notifications/:id/star
func (h *NotificationHandler) ToggleStar(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid notification ID")
	}

	type StarRequest struct {
		IsStarred bool `json:"is_starred"`
	}

	var req StarRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.service.ToggleStar(c.Context(), id, req.IsStarred); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Notification updated",
	})
}

// MoveToCategory handles PATCH /api/v1/notifications/:id/move
func (h *NotificationHandler) MoveToCategory(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid notification ID")
	}

	type MoveRequest struct {
		Category string `json:"category"`
	}

	var req MoveRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.service.MoveToCategory(c.Context(), id, req.Category); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Notification moved successfully",
	})
}

// BulkMoveToCategory handles POST /api/v1/notifications/bulk/move
func (h *NotificationHandler) BulkMoveToCategory(c *fiber.Ctx) error {
	type BulkMoveRequest struct {
		IDs      []string `json:"ids"`
		Category string   `json:"category"`
	}

	var req BulkMoveRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	// Parse UUIDs
	ids := make([]uuid.UUID, len(req.IDs))
	for i, idStr := range req.IDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid notification ID: "+idStr)
		}
		ids[i] = id
	}

	if err := h.service.BulkMoveToCategory(c.Context(), ids, req.Category); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Notifications moved successfully",
	})
}

// BulkDelete handles POST /api/v1/notifications/bulk/delete
func (h *NotificationHandler) BulkDelete(c *fiber.Ctx) error {
	type BulkDeleteRequest struct {
		IDs []string `json:"ids"`
	}

	var req BulkDeleteRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	// Parse UUIDs
	ids := make([]uuid.UUID, len(req.IDs))
	for i, idStr := range req.IDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid notification ID: "+idStr)
		}
		ids[i] = id
	}

	if err := h.service.BulkDelete(c.Context(), ids); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "Notifications deleted successfully",
	})
}
