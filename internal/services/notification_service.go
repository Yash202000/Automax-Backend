package services

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"text/template"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/storage"
	"github.com/automax/backend/internal/utils"
	"github.com/google/uuid"
)

type NotificationService struct {
	templateRepo repository.NotificationTemplateRepository
	logRepo      repository.NotificationLogRepository
	userRepo     repository.UserRepository

	storage *storage.MinIOStorage
}

type SendNotificationResult struct {
	SentLog     *models.NotificationLog
	InboxLogIDs []uuid.UUID // IDs of inbox copies created for internal recipients
}

func NewNotificationService(
	templateRepo repository.NotificationTemplateRepository,
	logRepo repository.NotificationLogRepository, userRepo repository.UserRepository, storage *storage.MinIOStorage,
) *NotificationService {
	return &NotificationService{
		templateRepo: templateRepo,
		logRepo:      logRepo,
		userRepo:     userRepo,
		storage:      storage,
	}
}

func (s *NotificationService) SendNotification(ctx context.Context, channel string, templateCode *string, language string, to []string, cc []string, bcc []string, subject string, body string, variables map[string]string, attachments []models.AttachmentData, sentBy *uuid.UUID, sessionID *uuid.UUID) (*SendNotificationResult, error) {
	// REQUIRED: at least one recipient
	if len(to) == 0 && len(cc) == 0 && len(bcc) == 0 {
		return nil, fmt.Errorf("at least one recipient (to, cc, or bcc) is required")
	}

	// REQUIRED: subject OR body
	if strings.TrimSpace(subject) == "" && strings.TrimSpace(body) == "" {
		return nil, fmt.Errorf("either subject or body must be provided")
	}

	//TEMPLATE LOGIC
	if templateCode != nil && *templateCode != "" {

		tpl, err := s.templateRepo.FindByCodeChannelLanguage(
			ctx, *templateCode, channel, language,
		)
		if err != nil {
			return nil, err
		}

		// Render only if variables exist
		if len(variables) > 0 {
			if tpl.Body != "" {
				body, _ = RenderTemplate(tpl.Body, variables)
			}
			if tpl.Subject != "" {
				subject, _ = RenderTemplate(tpl.Subject, variables)
			}
		}

	} else {
		// No template → render provided body/subject if variables exist
		if len(variables) > 0 {
			if body != "" {
				body, _ = RenderTemplate(body, variables)
			}
			if subject != "" {
				subject, _ = RenderTemplate(subject, variables)
			}
		}
	}

	status := "sent"
	provider := channel
	var recipientStatuses models.RecipientArray
	var attachmentInfo models.AttachmentArray

	allRecipients := append(append([]string{}, to...), cc...)
	allRecipients = append(allRecipients, bcc...)

	for _, att := range attachments {

		// Upload to MinIO using bytes
		objectName, err := storage.Storage.UploadBytes(
			ctx,
			att.Data,
			att.Filename,
			att.ContentType,
			"sla_breach-notifications",
		)
		if err != nil {
			return nil, fmt.Errorf("failed to upload attachment: %w", err)
		}

		attachmentInfo = append(attachmentInfo, models.AttachmentInfo{
			ID:          att.AttachmentID.String(),
			Filename:    att.Filename,
			ContentType: att.ContentType,
			Size:        int64(len(att.Data)),
			URL:         "/api/v1/attachments/" + att.AttachmentID.String(),
			PreviewURL:  "/api/v1/attachments/" + att.AttachmentID.String() + "/preview",
			StoragePath: objectName,
		})
	}

	switch channel {
	case "email":
		_, err := utils.SendSMTPWithCCBCC(to, cc, bcc, subject, body, attachments)
		if err != nil {

			status = "failed"

			for _, r := range allRecipients {
				recipientStatuses = append(recipientStatuses, models.RecipientInfo{
					Email:   r,
					Channel: r,
					Type:    utils.GetRecipientType(r, to, cc, bcc),
					Status:  "failed",
					Error:   err.Error(),
				})
			}

		} else {

			// Success case
			for _, r := range allRecipients {
				recipientStatuses = append(recipientStatuses, models.RecipientInfo{
					Email:   r,
					Channel: r,
					Type:    utils.GetRecipientType(r, to, cc, bcc),
					Status:  "success",
				})
			}

		}

		provider = "smtp"
	case "sms":
		for _, phone := range to {
			err := utils.SendSMS(phone, body)
			if err != nil {
				status = "failed"
				recipientStatuses = append(recipientStatuses, models.RecipientInfo{
					Email:   phone,
					Channel: phone,
					Type:    "to",
					Status:  "failed",
					Error:   err.Error(),
				})
			} else {
				recipientStatuses = append(recipientStatuses, models.RecipientInfo{
					Email:   phone,
					Channel: phone,
					Type:    "to",
					Status:  "success",
				})
			}
		}
		provider = "twilio"
	case "whatsapp":
		status = "sent"
		for _, phone := range to {
			err := utils.SendOTPWithMetaTemplate(phone, body)
			if err != nil {
				status = "failed"
				recipientStatuses = append(recipientStatuses, models.RecipientInfo{
					Channel:      phone,
					Type:         "to",
					Status:       "failed",
					Error:        err.Error(),
					ErrorMessage: err.Error(),
				})

			} else {
				recipientStatuses = append(recipientStatuses, models.RecipientInfo{
					Channel: phone,
					Type:    "to",
					Status:  "success",
				})
			}
		}

		provider = "meta"

	case "notification":
		// In-app only — no external delivery. Recipients must be internal user emails.
		status = "sent"
		provider = "in-app"
		for _, recipient := range to {
			recipientStatuses = append(recipientStatuses, models.RecipientInfo{
				Channel: recipient,
				Type:    "to",
				Status:  "success",
			})
		}

	default:
		return nil, fmt.Errorf("unsupported channel: %s", channel)
	}

	now := time.Now()
	log := &models.NotificationLog{
		ID:           uuid.New(),
		Channel:      channel,
		Direction:    models.DirectionOutbound,
		Category:     models.CategorySent,
		TemplateCode: deref(templateCode),
		Language:     language,
		Recipients:   recipientStatuses,
		CC:           cc,
		BCC:          bcc,
		Subject:      subject,
		Body:         body,
		OTPSessionID: sessionID,
		OTPVerified:  false,
		Status:       status,
		Provider:     provider,
		Attachments:  attachmentInfo,
		SentBy:       sentBy,
		SentAt:       &now,
		CreatedAt:    now,
		UpdatedAt:    &now,
	}

	if err := s.logRepo.Create(ctx, log); err != nil {
		return nil, err
	}

	// Always create inbox logs for internal recipients, even if delivery failed. This ensures escalation and system notifications always appear in the recipient's inbox.
	var inboxLogIDs []uuid.UUID
	for _, recipient := range to {

		// Look up the system user by email (email channel) or phone (sms whatsapp channel)
		var user *models.User
		var userErr error
		if channel == "sms" || channel == "whatsapp" {
			user, userErr = s.userRepo.FindByMobile(ctx, recipient)
		} else {
			user, userErr = s.userRepo.FindByEmail(ctx, recipient)
		}
		if userErr != nil || user == nil {
			continue // Skip external/unknown recipients
		}

		inboxLog := &models.NotificationLog{
			ID:           uuid.New(),
			Channel:      channel,
			Direction:    models.DirectionInbound,
			Category:     models.CategoryInbox,
			TemplateCode: deref(templateCode),
			Language:     language,
			Recipients:   recipientStatuses,
			Subject:      subject,
			Body:         body,
			Status:       status,
			Provider:     provider,
			Attachments:  attachmentInfo,
			SentBy:       sentBy,
			ReceivedBy:   &user.ID, // this makes it appear in inbox
			CreatedAt:    now,
			UpdatedAt:    &now,
		}

		if err := s.logRepo.Create(ctx, inboxLog); err == nil {
			inboxLogIDs = append(inboxLogIDs, inboxLog.ID)
		}
	}

	if status == "failed" {
		return nil, fmt.Errorf("notification delivery failed")
	}

	return &SendNotificationResult{
		SentLog:     log,
		InboxLogIDs: inboxLogIDs,
	}, nil
}

func deref(s *string) string {
	if s != nil {
		return *s
	}
	return ""
}

// ListNotifications retrieves notifications with filtering and search
func (s *NotificationService) ListNotifications(ctx context.Context, filter *models.NotificationLogFilter) ([]models.NotificationLogResponse, int64, error) {
	logs, total, err := s.logRepo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]models.NotificationLogResponse, len(logs))
	for i, log := range logs {
		responses[i] = models.ToNotificationLogResponse(&log)
	}

	return responses, total, nil
}

// GetNotification retrieves a single notification by ID
func (s *NotificationService) GetNotification(ctx context.Context, id uuid.UUID) (*models.NotificationLogResponse, error) {
	log, err := s.logRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	response := models.ToNotificationLogResponse(log)
	return &response, nil
}

// FindNotificationByAttachmentID finds a notification and attachment by attachment ID
func (s *NotificationService) FindNotificationByAttachmentID(ctx context.Context, attachmentID string) (*models.NotificationLog, *models.AttachmentInfo, error) {
	// Get all notifications that have attachments (this is a simple implementation)
	// For better performance, consider adding an index on attachments JSONB field
	notifications, err := s.logRepo.FindByAttachmentID(ctx, attachmentID)
	if err != nil {
		return nil, nil, fmt.Errorf("attachment not found: %v", err)
	}

	// Find the specific attachment within the notification
	for _, notification := range notifications {
		for _, att := range notification.Attachments {
			if att.ID == attachmentID {
				return notification, &att, nil
			}
		}
	}

	return nil, nil, fmt.Errorf("attachment not found")
}

// DeleteNotification soft deletes a notification (moves to trash)
func (s *NotificationService) DeleteNotification(ctx context.Context, id uuid.UUID) error {
	// Move to trash instead of hard delete
	return s.logRepo.MoveToCategory(ctx, id, models.CategoryTrash)
}

// PermanentDelete permanently deletes a notification
func (s *NotificationService) PermanentDelete(ctx context.Context, id uuid.UUID) error {
	return s.logRepo.Delete(ctx, id)
}

// CreateDraft creates a draft email
func (s *NotificationService) CreateDraft(ctx context.Context, req *models.CreateDraftRequest, createdBy uuid.UUID) (*models.NotificationLogResponse, error) {
	// Build recipients array for draft
	var recipients models.RecipientArray
	for _, email := range req.To {
		recipients = append(recipients, models.RecipientInfo{
			Channel: email,
			Type:    "to",
			Status:  "draft",
		})
	}

	// Convert attachments
	var attachmentInfo models.AttachmentArray
	for _, att := range req.Attachments {
		attachmentInfo = append(attachmentInfo, models.AttachmentInfo{
			Filename:    att.Filename,
			ContentType: att.ContentType,
			Size:        int64(len(att.Data)),
		})
	}

	now := time.Now()
	draft := &models.NotificationLog{
		ID:          uuid.New(),
		Channel:     req.Channel,
		Direction:   models.DirectionOutbound,
		Category:    models.CategoryDraft,
		Recipients:  recipients,
		CC:          req.CC,
		BCC:         req.BCC,
		Subject:     req.Subject,
		Body:        req.Body,
		BodyHTML:    req.BodyHTML,
		Status:      models.StatusDraft,
		Attachments: attachmentInfo,
		SentBy:      &createdBy,
		ScheduledAt: req.ScheduledAt,
		CreatedAt:   now,
		UpdatedAt:   &now,
	}

	if err := s.logRepo.Create(ctx, draft); err != nil {
		return nil, err
	}

	response := models.ToNotificationLogResponse(draft)
	return &response, nil
}

// UpdateDraft updates an existing draft
func (s *NotificationService) UpdateDraft(ctx context.Context, id uuid.UUID, req *models.UpdateDraftRequest) (*models.NotificationLogResponse, error) {
	draft, err := s.logRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if draft.Category != models.CategoryDraft {
		return nil, fmt.Errorf("can only update drafts")
	}

	// Update fields if provided
	if len(req.To) > 0 {
		var recipients models.RecipientArray
		for _, email := range req.To {
			recipients = append(recipients, models.RecipientInfo{
				Channel: email,
				Type:    "to",
				Status:  "draft",
			})
		}
		draft.Recipients = recipients
	}

	if len(req.CC) > 0 {
		draft.CC = req.CC
	}
	if len(req.BCC) > 0 {
		draft.BCC = req.BCC
	}
	if req.Subject != "" {
		draft.Subject = req.Subject
	}
	if req.Body != "" {
		draft.Body = req.Body
	}
	if req.BodyHTML != "" {
		draft.BodyHTML = req.BodyHTML
	}
	if req.ScheduledAt != nil {
		draft.ScheduledAt = req.ScheduledAt
	}

	// Update attachments if provided
	if len(req.Attachments) > 0 {
		var attachmentInfo models.AttachmentArray
		for _, att := range req.Attachments {
			attachmentInfo = append(attachmentInfo, models.AttachmentInfo{
				Filename:    att.Filename,
				ContentType: att.ContentType,
				Size:        int64(len(att.Data)),
			})
		}
		draft.Attachments = attachmentInfo
	}

	now := time.Now()
	draft.UpdatedAt = &now

	if err := s.logRepo.Update(ctx, draft); err != nil {
		return nil, err
	}

	response := models.ToNotificationLogResponse(draft)
	return &response, nil
}

// SendDraft sends a draft email
func (s *NotificationService) SendDraft(ctx context.Context, id uuid.UUID) (*models.NotificationLogResponse, error) {
	draft, err := s.logRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if draft.Category != models.CategoryDraft {
		return nil, fmt.Errorf("can only send drafts")
	}

	// Extract TO recipients from Recipients array
	var to []string
	for _, r := range draft.Recipients {
		if r.Type == "to" {
			to = append(to, r.Channel)
		}
	}

	// Convert AttachmentInfo to AttachmentData for sending
	var attachments []models.AttachmentData
	for _, att := range draft.Attachments {
		// Note: We don't have the actual file data stored, so we can't send attachments from drafts
		// This is a limitation - in a real system, you'd store attachment files separately
		attachments = append(attachments, models.AttachmentData{
			Filename:    att.Filename,
			ContentType: att.ContentType,
			Data:        []byte{}, // Empty data - need to store files separately
		})
	}

	// Send the email using SendNotification
	result, err := s.SendNotification(
		ctx,
		draft.Channel,
		nil,
		draft.Language,
		to,
		draft.CC,
		draft.BCC,
		draft.Subject,
		draft.Body,
		nil,
		attachments,
		draft.SentBy,
		nil,
	)
	if err != nil {
		return nil, err
	}

	// Delete the draft after successful send
	_ = s.logRepo.Delete(ctx, id)

	// Convert NotificationLog to NotificationLogResponse
	response := models.ToNotificationLogResponse(result.SentLog)
	return &response, nil
}

// MarkAsRead marks an email as read/unread
func (s *NotificationService) MarkAsRead(ctx context.Context, id uuid.UUID, isRead bool) error {
	return s.logRepo.MarkAsRead(ctx, id, isRead)
}

// ToggleStar toggles star/unstar on an email
func (s *NotificationService) ToggleStar(ctx context.Context, id uuid.UUID, isStarred bool) error {
	return s.logRepo.ToggleStar(ctx, id, isStarred)
}

// MoveToCategory moves email to a different category/folder
func (s *NotificationService) MoveToCategory(ctx context.Context, id uuid.UUID, category string) error {
	// Validate category
	validCategories := []string{
		models.CategoryInbox,
		models.CategorySent,
		models.CategoryDraft,
		models.CategoryOutbox,
		models.CategoryTrash,
		models.CategorySpam,
	}

	valid := false
	for _, c := range validCategories {
		if category == c {
			valid = true
			break
		}
	}

	if !valid {
		return fmt.Errorf("invalid category: %s", category)
	}

	return s.logRepo.MoveToCategory(ctx, id, category)
}

// BulkMoveToCategory moves multiple emails to a category
func (s *NotificationService) BulkMoveToCategory(ctx context.Context, ids []uuid.UUID, category string) error {
	return s.logRepo.BulkMoveToCategory(ctx, ids, category)
}

// BulkDelete deletes multiple emails
func (s *NotificationService) BulkDelete(ctx context.Context, ids []uuid.UUID) error {
	return s.logRepo.BulkDelete(ctx, ids)
}

// SaveInboundNotification saves an inbound notification (received email/SMS)
func (s *NotificationService) SaveInboundNotification(ctx context.Context, log *models.NotificationLog) error {
	return s.logRepo.Create(ctx, log)
}

// ReplyToNotification sends a reply to an existing email and maintains threading
func (s *NotificationService) ReplyToNotification(ctx context.Context, originalID uuid.UUID, to []string, cc []string, bcc []string, subject string, body string, bodyHTML string, sentBy *uuid.UUID) (*models.NotificationLogResponse, error) {
	// Get the original email
	original, err := s.logRepo.FindByID(ctx, originalID)
	if err != nil {
		return nil, fmt.Errorf("original email not found: %w", err)
	}

	// Determine thread ID
	threadID := original.ThreadID
	if threadID == nil {
		// If original email has no thread, use its ID as the thread ID
		threadID = &original.ID
	}

	// Auto-prefix subject with "Re:" if not already present
	if subject != "" && !strings.HasPrefix(subject, "Re:") {
		subject = "Re: " + subject
	} else if subject == "" && original.Subject != "" {
		subject = "Re: " + original.Subject
	}

	// Send the reply
	result, err := s.SendNotification(
		ctx,
		original.Channel,
		nil,
		original.Language,
		to,
		cc,
		bcc,
		subject,
		body,
		nil,
		nil,
		sentBy,
		nil,
	)
	if err != nil {
		return nil, err
	}

	// Update the sent email with threading info
	result.SentLog.ThreadID = threadID
	result.SentLog.InReplyTo = &originalID
	if err := s.logRepo.Update(ctx, result.SentLog); err != nil {
		return nil, err
	}

	// Also update original email's thread_id if it didn't have one
	if original.ThreadID == nil {
		original.ThreadID = threadID
		_ = s.logRepo.Update(ctx, original)
	}

	response := models.ToNotificationLogResponse(result.SentLog)
	return &response, nil
}

// UpdateAttachmentURLs updates the attachment URLs in an existing notification
func (s *NotificationService) UpdateAttachmentURLs(ctx context.Context, id uuid.UUID, attachments models.AttachmentArray) error {
	log, err := s.logRepo.FindByID(ctx, id)
	if err != nil {
		return err
	}

	log.Attachments = attachments
	now := time.Now()
	log.UpdatedAt = &now

	return s.logRepo.Update(ctx, log)
}

func (s *NotificationService) SetMetaOnLogs(ctx context.Context, ids []uuid.UUID, meta *models.NotificationMeta) error {
	return s.logRepo.SetMeta(ctx, ids, meta)
}

func RenderTemplate(tpl string, vars map[string]string) (string, error) {
	t, err := template.New("tpl").Option("missingkey=zero").Parse(tpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	err = t.Execute(&buf, vars)
	return buf.String(), err
}
