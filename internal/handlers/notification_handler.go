package handlers

import (
	"fmt"
	"io"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/internal/storage"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type NotificationHandler struct {
	service *services.NotificationService
	storage *storage.MinIOStorage
}

func NewNotificationHandler(service *services.NotificationService, storage *storage.MinIOStorage) *NotificationHandler {
	return &NotificationHandler{
		service: service,
		storage: storage,
	}
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
				log.Printf("Failed to open attachment %s: %v", fileHeader.Filename, err)
				continue // Skip failed attachments
			}
			defer file.Close()

			data, err := io.ReadAll(file)
			if err != nil {
				log.Printf("Failed to read attachment %s: %v", fileHeader.Filename, err)
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

	// Save attachments to MinIO and create attachment info
	var attachmentInfo models.AttachmentArray
	for _, att := range attachments {
		// Save attachment to MinIO storage
		objectName := ""
		if h.storage != nil && len(att.Data) > 0 {
			// Create a unique folder for this notification's attachments
			folder := fmt.Sprintf("notifications/%s", uuid.New().String())

			// Upload to MinIO
			uploadedPath, err := h.storage.UploadBytes(
				c.Context(),
				att.Data,
				att.Filename,
				att.ContentType,
				folder,
			)
			if err != nil {
				log.Printf("Failed to upload attachment %s to MinIO: %v", att.Filename, err)
				// Continue without URL if upload fails
			} else {
				objectName = uploadedPath
			}
		}

		attachmentInfo = append(attachmentInfo, models.AttachmentInfo{
			ID:          uuid.New().String(),
			Filename:    att.Filename,
			ContentType: att.ContentType,
			Size:        int64(len(att.Data)),
			StoragePath: objectName, // Store MinIO object path
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
		log, err := h.service.SendNotification(c.Context(), req.Channel, req.TemplateCode, req.Language, req.To, req.CC, req.BCC, req.Subject, req.Body, req.Variables, nil, sentBy, nil)
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
	var attachmentURLs []models.AttachmentInfo // Track uploaded attachments with URLs
	if files, ok := form.File["attachments"]; ok {
		// Create a unique folder for this notification's attachments
		folder := fmt.Sprintf("notifications/%s", uuid.New().String())

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

			contentType := fileHeader.Header.Get("Content-Type")
			if contentType == "" {
				contentType = "application/octet-stream"
			}

			// Upload to MinIO
			objectName := ""
			if h.storage != nil {
				uploadedPath, err := h.storage.UploadBytes(
					c.Context(),
					data,
					fileHeader.Filename,
					contentType,
					folder,
				)
				if err != nil {
					log.Printf("Failed to upload attachment %s to MinIO: %v", fileHeader.Filename, err)
				} else {
					objectName = uploadedPath
				}
			}

			attachments = append(attachments, models.AttachmentData{
				Filename:    fileHeader.Filename,
				ContentType: contentType,
				Data:        data,
			})

			attachmentURLs = append(attachmentURLs, models.AttachmentInfo{
				ID:          uuid.New().String(),
				Filename:    fileHeader.Filename,
				ContentType: contentType,
				Size:        int64(len(data)),
				StoragePath: objectName,
			})
		}
	}

	var templateCodePtr *string
	if templateCode != "" {
		templateCodePtr = &templateCode
	}

	// Send notification with attachments
	notificationLog, err := h.service.SendNotification(c.Context(), channel, templateCodePtr, language, to, cc, bcc, subject, body, variables, attachments, sentBy, nil)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Update attachment URLs if attachments were uploaded
	if len(attachmentURLs) > 0 {
		if err := h.service.UpdateAttachmentURLs(c.Context(), notificationLog.ID, attachmentURLs); err != nil {
			log.Printf("Warning: Failed to update attachment URLs for notification %s: %v", notificationLog.ID, err)
			// Don't fail the request, just log the warning
		}
	}

	res := SendNotificationResponse{
		ID:       notificationLog.ID.String(),
		Status:   notificationLog.Status,
		Provider: notificationLog.Provider,
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

// Reply handles POST /api/v1/notifications/:id/reply
func (h *NotificationHandler) Reply(c *fiber.Ctx) error {
	// Get the original email ID
	idStr := c.Params("id")
	originalID, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid notification ID")
	}

	// Get user ID from context
	var sentBy *uuid.UUID
	if userID, ok := c.Locals("user_id").(uuid.UUID); ok {
		sentBy = &userID
	}

	type ReplyRequest struct {
		To       []string `json:"to"`
		CC       []string `json:"cc,omitempty"`
		BCC      []string `json:"bcc,omitempty"`
		Subject  string   `json:"subject"`
		Body     string   `json:"body"`
		BodyHTML string   `json:"body_html,omitempty"`
	}

	var req ReplyRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	// Send reply with threading
	reply, err := h.service.ReplyToNotification(c.Context(), originalID, req.To, req.CC, req.BCC, req.Subject, req.Body, req.BodyHTML, sentBy)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"success": true,
		"data":    reply,
		"message": "Reply sent successfully",
	})
}

// GetThread handles GET /api/v1/notifications/threads/:thread_id
func (h *NotificationHandler) GetThread(c *fiber.Ctx) error {
	threadIDStr := c.Params("thread_id")
	threadID, err := uuid.Parse(threadIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid thread ID")
	}

	// Get all emails in this thread
	filter := &models.NotificationLogFilter{
		ThreadID: &threadID,
		Page:     1,
		Limit:    100, // Get all messages in thread
	}

	notifications, total, err := h.service.ListNotifications(c.Context(), filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return c.JSON(fiber.Map{
		"success":     true,
		"data":        notifications,
		"total_items": total,
		"thread_id":   threadID,
	})
}

// DownloadAttachment handles GET /api/v1/notifications/:id/attachments/:filename
// Downloads a specific attachment from a notification
func (h *NotificationHandler) DownloadAttachment(c *fiber.Ctx) error {
	// Get notification ID
	notificationIDStr := c.Params("id")
	notificationID, err := uuid.Parse(notificationIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid notification ID")
	}

	// Get filename from URL
	filename := c.Params("filename")
	if filename == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Filename is required")
	}

	// Fetch the notification to verify it exists and get attachment info
	notification, err := h.service.GetNotification(c.Context(), notificationID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Notification not found")
	}

	// Find the attachment with matching filename
	var storagePath string
	var contentType string
	for _, att := range notification.Attachments {
		if att.Filename == filename {
			storagePath = att.StoragePath
			contentType = att.ContentType
			break
		}
	}

	if storagePath == "" {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Attachment not found")
	}

	// Download from MinIO
	if h.storage == nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Storage not configured")
	}

	fileReader, err := h.storage.GetFile(c.Context(), storagePath)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Failed to retrieve attachment: "+err.Error())
	}
	defer fileReader.Close()

	// Read the file content
	fileData, err := io.ReadAll(fileReader)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to read attachment")
	}

	// Set appropriate headers
	c.Set("Content-Type", contentType)
	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", filename))
	c.Set("Content-Length", fmt.Sprintf("%d", len(fileData)))

	return c.Send(fileData)
}

// GetAttachmentURL handles GET /api/v1/notifications/:id/attachments/:filename/url
// Returns a presigned URL for downloading the attachment
func (h *NotificationHandler) GetAttachmentURL(c *fiber.Ctx) error {
	// Get notification ID
	notificationIDStr := c.Params("id")
	notificationID, err := uuid.Parse(notificationIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid notification ID")
	}

	// Get filename from URL
	filename := c.Params("filename")
	if filename == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Filename is required")
	}

	// Fetch the notification to verify it exists and get attachment info
	notification, err := h.service.GetNotification(c.Context(), notificationID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Notification not found")
	}

	// Find the attachment with matching filename
	var storagePath string
	for _, att := range notification.Attachments {
		if att.Filename == filename {
			storagePath = att.StoragePath
			break
		}
	}

	if storagePath == "" {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Attachment not found")
	}

	// Generate presigned URL from MinIO
	if h.storage == nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Storage not configured")
	}

	presignedURL, err := h.storage.GetFileURL(c.Context(), storagePath)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to generate download URL: "+err.Error())
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"url":      presignedURL,
			"filename": filename,
		},
	})
}

// DownloadNotificationAttachmentByID handles GET /api/v1/attachments/:attachment_id
// Downloads a notification attachment by its ID
func (h *NotificationHandler) DownloadNotificationAttachmentByID(c *fiber.Ctx) error {
	attachmentID := c.Params("attachment_id")
	if attachmentID == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Attachment ID is required")
	}

	// Find the notification containing this attachment
	_, attachment, err := h.service.FindNotificationByAttachmentID(c.Context(), attachmentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Attachment not found")
	}

	if attachment.StoragePath == "" {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Attachment storage path not found")
	}

	// Download from MinIO
	if h.storage == nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Storage not configured")
	}

	fileReader, err := h.storage.GetFile(c.Context(), attachment.StoragePath)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Failed to retrieve attachment: "+err.Error())
	}
	defer fileReader.Close()

	// Read the file content
	fileData, err := io.ReadAll(fileReader)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to read attachment")
	}

	// Set appropriate headers for download
	c.Set("Content-Type", attachment.ContentType)
	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", attachment.Filename))
	c.Set("Content-Length", fmt.Sprintf("%d", len(fileData)))

	return c.Send(fileData)
}

// PreviewNotificationAttachmentByID handles GET /api/v1/attachments/:attachment_id/preview
// Previews a notification attachment by its ID (inline display)
func (h *NotificationHandler) PreviewNotificationAttachmentByID(c *fiber.Ctx) error {
	attachmentID := c.Params("attachment_id")
	if attachmentID == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Attachment ID is required")
	}

	// Find the notification containing this attachment
	_, attachment, err := h.service.FindNotificationByAttachmentID(c.Context(), attachmentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Attachment not found")
	}

	if attachment.StoragePath == "" {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Attachment storage path not found")
	}

	// Download from MinIO
	if h.storage == nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Storage not configured")
	}

	fileReader, err := h.storage.GetFile(c.Context(), attachment.StoragePath)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Failed to retrieve attachment: "+err.Error())
	}
	defer fileReader.Close()

	// Read the file content
	fileData, err := io.ReadAll(fileReader)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to read attachment")
	}

	// Set appropriate headers for inline preview
	c.Set("Content-Type", attachment.ContentType)
	c.Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", attachment.Filename))
	c.Set("Content-Length", fmt.Sprintf("%d", len(fileData)))

	return c.Send(fileData)
}
