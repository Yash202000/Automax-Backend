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
	"github.com/automax/backend/internal/utils"
	"github.com/google/uuid"
)

type NotificationService struct {
	templateRepo repository.NotificationTemplateRepository
	logRepo      repository.NotificationLogRepository
}

func NewNotificationService(
	templateRepo repository.NotificationTemplateRepository,
	logRepo repository.NotificationLogRepository,
) *NotificationService {
	return &NotificationService{
		templateRepo: templateRepo,
		logRepo:      logRepo,
	}
}

func (s *NotificationService) SendNotification(ctx context.Context, channel string, templateCode *string, language string, to []string, cc []string, bcc []string, subject string, body string, variables map[string]string, attachments []models.AttachmentData, sentBy *uuid.UUID) (*models.NotificationLog, error) {

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
		attachmentInfo = append(attachmentInfo, models.AttachmentInfo{
			Filename:    att.Filename,
			ContentType: att.ContentType,
			Size:        int64(len(att.Data)),
		})
	}

	switch channel {

	case "email":
		statuses, err := utils.SendSMTPWithCCBCC(to, cc, bcc, subject, body, attachments)
		if err != nil {
			status = "failed"
			for _, r := range allRecipients {
				recipientStatuses = append(recipientStatuses, models.RecipientInfo{
					Email:  r,
					Type:   utils.GetRecipientType(r, to, cc, bcc),
					Status: "failed",
					Error:  err.Error(),
				})
			}
		} else {
			recipientStatuses = statuses
		}
		provider = "smtp"

	case "sms":
		for _, phone := range to {
			err := utils.SendSMS(phone, body)
			if err != nil {
				status = "failed"
				recipientStatuses = append(recipientStatuses, models.RecipientInfo{
					Email:  phone,
					Type:   "to",
					Status: "failed",
					Error:  err.Error(),
				})
			} else {
				recipientStatuses = append(recipientStatuses, models.RecipientInfo{
					Email:  phone,
					Type:   "to",
					Status: "success",
				})
			}
		}
		provider = "twilio"

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

	return log, nil
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
			Email:  email,
			Type:   "to",
			Status: "draft",
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
				Email:  email,
				Type:   "to",
				Status: "draft",
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
			to = append(to, r.Email)
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
	sentLog, err := s.SendNotification(
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
	)
	if err != nil {
		return nil, err
	}

	// Delete the draft after successful send
	_ = s.logRepo.Delete(ctx, id)

	// Convert NotificationLog to NotificationLogResponse
	response := models.ToNotificationLogResponse(sentLog)
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

func RenderTemplate(tpl string, vars map[string]string) (string, error) {
	t, err := template.New("tpl").Option("missingkey=zero").Parse(tpl)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	err = t.Execute(&buf, vars)
	return buf.String(), err
}
