package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"time"

	"github.com/automax/backend/internal/repository"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type OTPService struct {
	redis               *redis.Client
	notificationService *NotificationService
	notificationLogRepo repository.NotificationLogRepository
}

func NewOTPService(
	redisClient *redis.Client,
	notificationService *NotificationService,
	notificationLogRepo repository.NotificationLogRepository,
) *OTPService {
	return &OTPService{
		redis:               redisClient,
		notificationService: notificationService,
		notificationLogRepo: notificationLogRepo,
	}
}

func GenerateOTP() (string, error) {
	max := big.NewInt(1000000)
	n, err := rand.Int(rand.Reader, max)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}

func HashOTP(otp string) string {
	hash := sha256.Sum256([]byte(otp))
	return hex.EncodeToString(hash[:])
}

func (s *OTPService) CheckRateLimit(ctx context.Context, phone string) error {

	key := "otp_req:" + phone

	count, _ := s.redis.Incr(ctx, key).Result()

	if count == 1 {
		s.redis.Expire(ctx, key, 5*time.Minute)
	}

	if count > 3 {
		return fmt.Errorf("too many otp requests")
	}

	return nil
}

func (s *OTPService) IsBlocked(ctx context.Context, phone string) bool {
	blockKey := "otp_block:" + phone
	exists, _ := s.redis.Exists(ctx, blockKey).Result()
	return exists == 1
}

type OTPData struct {
	Phone      string `json:"phone"`
	Hash       string `json:"hash"`
	SenderMode string `json:"senderMode"`
	Attempts   int    `json:"attempts"`
	Status     string `json:"status"`
	SessionID  string `json:"session_id"`

	SentAt     time.Time  `json:"sentAt"`
	VerifiedAt *time.Time `json:"verifiedAt,omitempty"`
	SentBy     *uuid.UUID `json:"sentBy"`
}

func (s *OTPService) SendOTP(
	ctx context.Context,
	phone string,
	senderMode string,
	sentBy *uuid.UUID,
) (string, error) {

	// - RATE LIMIT COUNTER
	counterKey := "otp_counter:" + phone

	count, err := s.redis.Incr(ctx, counterKey).Result()
	if err != nil {
		return "", fmt.Errorf("failed to increment otp counter: %w", err)
	}

	// Load env values
	maxAttemptsStr := os.Getenv("OTP_MAX_SEND_ATTEMPT")
	maxAttempts, _ := strconv.Atoi(maxAttemptsStr)
	if maxAttempts == 0 {
		maxAttempts = 6
	}

	counterExpStr := os.Getenv("OTP_COUNTER_EXPIRATION_TIME")
	counterExp, _ := strconv.Atoi(counterExpStr)
	if counterExp == 0 {
		counterExp = 5
	}

	// Set expiry only on first attempt
	if count == 1 {
		s.redis.Expire(ctx, counterKey,
			time.Duration(counterExp)*time.Minute)
	}

	// Check max send attempts
	if count > int64(maxAttempts) {
		return "", fmt.Errorf("max otp send attempts reached")
	}

	//GENERATE OTP
	otp, err := GenerateOTP()
	if err != nil {
		return "", fmt.Errorf("failed to generate otp: %w", err)
	}

	sessionID := uuid.New()
	key := "otp:" + phone + "--" + sessionID.String()

	otpExpStr := os.Getenv("OTP_DATA_EXPIRATION_TIME")
	otpExp, _ := strconv.Atoi(otpExpStr)
	if otpExp == 0 {
		otpExp = 15
	}

	err = s.sendOTPNotification(ctx, senderMode, phone, otp, &sessionID)

	fmt.Println("ERR INTO SERVICE", err)
	if err != nil {
		s.redis.Del(ctx, key)
		return "", fmt.Errorf("failed to send otp: %w", err)
	}

	otpData := OTPData{
		Phone:      phone,
		Hash:       HashOTP(otp),
		SenderMode: senderMode,
		Attempts:   0,
		SessionID:  sessionID.String(),
		Status:     "sent",
		SentAt:     time.Now(),
		SentBy:     sentBy,
	}

	jsonData, _ := json.Marshal(otpData)

	//STORE OTP --------
	err = s.redis.Set(
		ctx,
		key,
		jsonData,
		time.Duration(otpExp)*time.Minute,
	).Err()

	if err != nil {
		return "", fmt.Errorf("failed to store otp: %w", err)
	}

	return sessionID.String(), nil
}

func (s *OTPService) sendOTPNotification(ctx context.Context, channel, phone, otp string, sessionID *uuid.UUID) error {

	body := (otp)
	_, err := s.notificationService.SendNotification(ctx, channel, nil, "en", []string{phone}, nil, nil, "", body, nil, nil, nil, sessionID)
	if err != nil {
		return fmt.Errorf("ERR INTO WHATSAPP SERVICE: %w", err)
	}
	return err
}

func (s *OTPService) VerifyOTP(ctx context.Context, phone string, sessionID string, inputOTP string) error {
	key := "otp:" + phone + "--" + sessionID
	val, err := s.redis.Get(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("otp expired or invalid session")
	}

	var data struct {
		Phone      string `json:"phone"`
		Hash       string `json:"hash"`
		SenderMode string `json:"senderMode"`
		Attempts   int    `json:"attempts"`
	}

	err = json.Unmarshal([]byte(val), &data)
	if err != nil {
		return fmt.Errorf("invalid stored otp data")
	}

	// Load verify max attempt
	maxVerifyStr := os.Getenv("VERIFY_OTP_MAX_ATTEMPT")
	maxVerify, _ := strconv.Atoi(maxVerifyStr)
	if maxVerify == 0 {
		maxVerify = 3
	}

	// Check max verify attempts
	if data.Attempts >= maxVerify {
		s.redis.Del(ctx, key)
		return fmt.Errorf("max verify attempts exceeded")
	}

	// Check OTP match
	if data.Hash != HashOTP(inputOTP) {
		data.Attempts++
		updatedData, _ := json.Marshal(data)
		ttl, _ := s.redis.TTL(ctx, key).Result()

		// Update attempts but keep original TTL
		s.redis.Set(ctx, key, updatedData, ttl)

		return fmt.Errorf("invalid otp")
	}
	// UPDATE DB
	err = s.notificationLogRepo.MarkOTPVerified(ctx, sessionID, time.Now())
	if err != nil {
		return errors.New("failed to update otp status")
	}

	//SUCCESS  then DELETE OTP FROM REDIS
	s.redis.Del(ctx, key)

	return nil
}
