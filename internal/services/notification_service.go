package services

import (
	"bytes"
	"context"
	"fmt"
	"net/smtp"
	"os"
	"text/template"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/google/uuid"

	twilio "github.com/twilio/twilio-go"
	openapi "github.com/twilio/twilio-go/rest/api/v2010"
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

// func (s *NotificationService) SendNotification(ctx context.Context, channel, templateCode, language, recipient string, variables map[string]string) (*models.NotificationLog, error) {

func (s *NotificationService) SendNotification(
	ctx context.Context,
	channel string,
	templateCode *string,
	language string,
	recipient string,
	subject string,
	body string,
	variables map[string]string,
) (*models.NotificationLog, error) {

	// CASE 1: templateCode provided → existing behavior
	if templateCode != nil && *templateCode != "" {
		tpl, err := s.templateRepo.FindByCodeChannelLanguage(
			ctx, *templateCode, channel, language,
		)
		if err != nil {
			return nil, err
		}

		body, err = RenderTemplate(tpl.Body, variables)
		if err != nil {
			return nil, err
		}

		if tpl.Subject != "" {
			subject, _ = RenderTemplate(tpl.Subject, variables)
		}
	} else {
		// CASE 2: no template → body must be provided
		// if body == "" {
		// 	return nil, fmt.Errorf("body is required when templateCode is not provided")
		// }

		// subject optional (especially for SMS)
		if variables != nil {
			body, _ = RenderTemplate(body, variables)
			if subject != "" {
				subject, _ = RenderTemplate(subject, variables)
			}
		}
	}

	status := "sent"
	provider := channel

	if os.Getenv("ENV") != "local" {
		switch channel {
		case "email":
			if err := SendSMTP(recipient, subject, body); err != nil {
				return nil, err
			}
			provider = "smtp"

		case "sms":
			if err := SendSMS(recipient, body); err != nil {
				return nil, err
			}
			provider = "twilio"

		default:
			return nil, fmt.Errorf("unsupported channel: %s", channel)
		}
	} else {
		status = "mock-sent"
		provider = "mock"
	}

	log := &models.NotificationLog{
		ID:      uuid.New(),
		Channel: channel,
		TemplateCode: func() string {
			if templateCode != nil {
				return *templateCode
			}
			return ""
		}(),
		Language:  language,
		Recipient: recipient,
		Subject:   subject,
		Body:      body,
		Status:    status,
		Provider:  provider,
	}

	if err := s.logRepo.Create(ctx, log); err != nil {
		return nil, err
	}

	return log, nil
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

func SendSMTP(to, subject, body string) error {
	host := os.Getenv("SMTP_HOST")
	port := os.Getenv("SMTP_PORT")
	user := os.Getenv("SMTP_USER")
	pass := os.Getenv("SMTP_PASS")
	from := os.Getenv("SMTP_FROM")

	addr := fmt.Sprintf("%s:%s", host, port)
	auth := smtp.PlainAuth("", user, pass, host)

	msg := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=\"UTF-8\"\r\n\r\n%s",
		from, to, subject, body))

	return smtp.SendMail(addr, auth, from, []string{to}, msg)
}

func SendSMS(to, message string) error {
	// Load Twilio credentials
	accountSID := os.Getenv("TWILIO_ACCOUNT_SID")
	authToken := os.Getenv("TWILIO_AUTH_TOKEN")

	client := twilio.NewRestClientWithParams(twilio.ClientParams{
		Username: accountSID,
		Password: authToken,
	})

	params := &openapi.CreateMessageParams{}
	params.SetTo(to)                                 // recipient
	params.SetFrom(os.Getenv("TWILIO_PHONE_NUMBER")) // Twilio from number
	params.SetBody(message)                          // SMS body

	_, err := client.Api.CreateMessage(params) // correct method
	if err != nil {
		return fmt.Errorf("twilio send sms error: %w", err)
	}

	return nil
}
