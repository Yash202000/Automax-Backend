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

// DeleteNotification soft deletes a notification
func (s *NotificationService) DeleteNotification(ctx context.Context, id uuid.UUID) error {
	return s.logRepo.Delete(ctx, id)
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
