package services

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// type EscalationService struct {
// 	repo repository.EscalationRepository
// }

type EscalationService struct {
	repo         repository.EscalationRepository
	incidentRepo repository.IncidentRepository
	// smsService   repository.NotificationLogRepository
	// emailService repository.NotificationLogRepository
	userRepo            repository.UserRepository
	notificationService *NotificationService
}

func NewEscalationService(
	repo repository.EscalationRepository,
	incidentRepo repository.IncidentRepository,
	userRepo repository.UserRepository,
	notificationService *NotificationService,
) *EscalationService {
	return &EscalationService{
		repo:                repo,
		incidentRepo:        incidentRepo,
		userRepo:            userRepo,
		notificationService: notificationService,
	}
}

func (s *EscalationService) CreateConfig(ctx context.Context, e *models.EscalationSLA) error {
	return s.repo.Create(ctx, e)
}

func (s *EscalationService) GetConfigs(ctx context.Context) ([]models.EscalationSLA, error) {
	return s.repo.GetAll(ctx)
}

func (s *EscalationService) ProcessSLAEscalations(ctx context.Context) error {
	fmt.Println("Escalation job running at:", time.Now())
	// Step 1: Mark breached
	_, err := s.incidentRepo.MarkSLABreached(ctx)
	if err != nil {
		return err
	}

	// Step 2: Get all escalation configs
	configs, err := s.repo.GetAll(ctx)
	if err != nil {
		return err
	}

	for _, config := range configs {

		// Frequency Check
		if config.ReportFrequency == "WEEKLY" {
			if time.Now().Weekday() != time.Sunday {
				continue
			}
		}

		// Fetch breached incidents
		incidents, err := s.incidentRepo.GetBreachedByFilter(
			ctx,
			config.LocationID,
			config.ClassificationID,
		)
		if err != nil {
			continue
		}

		if len(incidents) == 0 {
			continue
		}

		// Get user email & phone
		user, err := s.userRepo.FindByID(ctx, config.UserID)
		if err != nil {
			continue
		}

		count := len(incidents)

		url, err := s.generateSignedURL(config.UserID)
		if err != nil {
			continue
		}

		report, err := s.generateCSVReport(incidents)
		if err != nil {
			continue
		}

		err = s.sendEscalation(ctx, config, user, count, url, report)
		if err != nil {
			continue
		}
	}

	return nil
}

func (s *EscalationService) generateCSVReport(
	incidents []models.Incident,
) ([]byte, error) {

	var buffer bytes.Buffer
	writer := csv.NewWriter(&buffer)

	writer.Write([]string{
		"Incident Number",
		"Location",
		"Classification",
		"SLA Deadline",
	})

	for _, inc := range incidents {
		writer.Write([]string{
			inc.IncidentNumber,
			inc.Location.Name,
			inc.Classification.Name,
			inc.SLADeadline.Format(time.RFC3339),
		})
	}

	writer.Flush()
	return buffer.Bytes(), nil
}

func (s *EscalationService) generateSignedURL(userID uuid.UUID) (string, error) {

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"user_id": userID.String(),
		"exp":     time.Now().Add(24 * time.Hour).Unix(),
	})

	tokenString, err := token.SignedString([]byte("your-secret"))
	if err != nil {
		return "", err
	}

	return fmt.Sprintf("https://yourdomain.com/api/sla-report?token=%s", tokenString), nil
}

func (s *EscalationService) sendEscalation(
	ctx context.Context,
	config models.EscalationSLA,
	user *models.User,
	count int,
	url string,
	report []byte,
) error {

	body := fmt.Sprintf(
		"You have %d incidents that have exceeded the SLA. For more details, please click on this link: %s",
		count,
		url,
	)

	var attachments []models.AttachmentData

	// Only attach for email
	attachments = append(attachments, models.AttachmentData{
		Filename:    "sla_breached_report.csv",
		ContentType: "text/csv",
		Data:        report,
	})

	for _, action := range config.Actions {

		switch action {

		case "EMAIL":
			_, err := s.notificationService.SendNotification(
				ctx,
				"email",
				nil,
				"en",
				[]string{user.Email},
				nil,
				nil,
				"SLA Escalation Alert",
				body,
				nil,
				attachments,
				nil,
				nil,
			)
			if err != nil {
				return err
			}

		case "SMS":
			_, err := s.notificationService.SendNotification(
				ctx,
				"sms",
				nil,
				"en",
				[]string{user.Phone},
				nil,
				nil,
				"",
				body,
				nil,
				nil,
				nil,
				nil,
			)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
