package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"time"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"google.golang.org/api/option"
)

type FCMService struct {
	deviceTokenRepo  *repository.DeviceTokenRepository
	notificationRepo repository.NotificationLogRepository
}

func NewFCMService(deviceTokenRepo *repository.DeviceTokenRepository, notificationRepo repository.NotificationLogRepository) *FCMService {
	return &FCMService{
		deviceTokenRepo:  deviceTokenRepo,
		notificationRepo: notificationRepo,
	}
}

func (s *FCMService) RegisterDeviceToken(userID *uuid.UUID, token, deviceType string) error {
	existing, err := s.deviceTokenRepo.GetByToken(token)
	if err != nil {
		return err
	}

	// If token already exists → update
	if existing != nil {
		existing.UserID = userID
		existing.DeviceType = deviceType

		return s.deviceTokenRepo.Update(existing)
	}

	// If token does not exist → create
	newToken := &models.DeviceToken{
		ID:          uuid.New(),
		UserID:      userID,
		DeviceToken: token,
		DeviceType:  deviceType,
	}

	return s.deviceTokenRepo.Create(newToken)
}

func (s *FCMService) GetUserDeviceTokens(userID uuid.UUID) ([]models.DeviceToken, error) {
	return s.deviceTokenRepo.GetByUserID(userID)
}

func (s *FCMService) RemoveDeviceToken(userID uuid.UUID, token string) error {
	return s.deviceTokenRepo.DeleteByUserAndToken(userID, token)
}

func (svc *FCMService) Push(ctx context.Context, userID uuid.UUID, title string, body string, user_type string, sentBy *uuid.UUID) error {

	// Fetch tokens
	tokens, err := svc.deviceTokenRepo.GetByUserID(userID)
	if err != nil {
		return fmt.Errorf("failed to fetch tokens: %w", err)
	}

	if len(tokens) == 0 {
		return fmt.Errorf("no FCM tokens found for user %s", userID)
	}

	var pushErr error

	for _, t := range tokens {

		err := svc.SendPushNotification(ctx, t.DeviceToken, title, body)
		if err != nil {
			log.Printf("Push failed: %v", err)
			pushErr = err
		}
	}

	// Save Notification Log
	now := time.Now()

	status := models.StatusSent
	if pushErr != nil {
		status = models.StatusFailed
	}
	logEntry := &models.NotificationLog{
		Channel:      "push-notification",
		Direction:    models.DirectionOutbound,
		Category:     models.CategorySent,
		Status:       status,
		Subject:      title,
		Body:         body,
		Provider:     "fcm",
		TemplateCode: user_type,
		SentAt:       &now,
		SentBy:       sentBy,
		ReceivedBy:   &userID,
		Recipients: models.RecipientArray{
			{
				Email:   userID.String(),
				Channel: "fcm",
				Type:    user_type,
				Status: func() string {
					if pushErr != nil {
						return "failed"
					}
					return "success"
				}(),
			},
		},
	}

	if pushErr != nil {
		logEntry.ErrorMessage = pushErr.Error()
	}

	if err := svc.notificationRepo.Create(ctx, logEntry); err != nil {
		log.Printf("Failed to save notification log: %v", err)
	}

	return pushErr
}

func (svc *FCMService) SendPushNotification(ctx context.Context, token, title, body string) error {

	// Load environment variables
	_ = godotenv.Load()

	// Read from .env
	credsPath := os.Getenv("FIREBASE_CREDENTIAL_PATH")

	if credsPath == "" {
		return fmt.Errorf("FIREBASE_CREDENTIAL_PATH not set in environment")
	}

	// Read and parse JSON to get project_id
	credsBytes, err := ioutil.ReadFile(credsPath)
	if err != nil {
		return fmt.Errorf("failed to read Firebase credentials: %w", err)
	}

	var creds struct {
		ProjectID string `json:"project_id"`
	}
	if err := json.Unmarshal(credsBytes, &creds); err != nil {
		return fmt.Errorf("failed to parse Firebase credentials: %w", err)
	}

	if creds.ProjectID == "" {
		return fmt.Errorf("project_id not found in credentials file")
	}

	// Initialize Firebase
	opt := option.WithCredentialsFile(credsPath)
	app, err := firebase.NewApp(ctx, &firebase.Config{
		ProjectID: creds.ProjectID,
	}, opt)
	if err != nil {
		return fmt.Errorf("failed to initialize Firebase app: %w", err)
	}

	client, err := app.Messaging(ctx)
	if err != nil {
		return fmt.Errorf("failed to get FCM client: %w", err)
	}
	message := &messaging.Message{
		Token: token,
		Notification: &messaging.Notification{
			Title: title,
			Body:  body,
		},
	}
	resp, err := client.Send(ctx, message)
	if err != nil {
		if strings.Contains(err.Error(), "registration-token-not-registered") {
			return fmt.Errorf("the notification could not be delivered (device unreachable)")
		}
		return fmt.Errorf("failed to send FCM message: %w", err)
	}
	log.Println("Time For push notification:", time.Now(), " push response:", resp)
	return nil
}
