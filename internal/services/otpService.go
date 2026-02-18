package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/redis/go-redis/v9"
)

type OTPService struct {
	redis               *redis.Client
	notificationService *NotificationService
}

func NewOTPService(
	redisClient *redis.Client,
	notificationService *NotificationService,
) *OTPService {
	return &OTPService{
		redis:               redisClient,
		notificationService: notificationService,
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

func (s *OTPService) SendLoginOTP(ctx context.Context, phone string) error {

	// Check if user is blocked
	if s.IsBlocked(ctx, phone) {
		return fmt.Errorf("user temporarily blocked")
	}

	//  Rate limit check
	if err := s.CheckRateLimit(ctx, phone); err != nil {
		return err
	}

	//  Generate secure OTP
	otp, err := GenerateOTP()
	fmt.Println("OTP", otp)
	if err != nil {
		return fmt.Errorf("failed to generate otp: %w", err)
	}

	// Prepare Redis payload
	data := map[string]interface{}{
		"hash":     HashOTP(otp),
		"attempts": 0,
	}
	fmt.Println("data", data)
	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("failed to marshal otp data: %w", err)
	}

	key := "otp:login:" + phone

	fmt.Println("key", key)

	// Store OTP in Redis with TTL
	if err := s.redis.Set(ctx, key, jsonData, 5*time.Minute).Err(); err != nil {
		return fmt.Errorf("failed to store otp: %w", err)
	}

	// Try WhatsApp first
	if err := s.sendOTPNotification(ctx, "whatsapp", phone, otp); err == nil {
		return nil
	} else {
		// Log WhatsApp failure
		log.Printf("WhatsApp OTP failed for %s: %v", phone, err)
	}

	// Fallback to SMS
	if err := s.sendOTPNotification(ctx, "sms", phone, otp); err == nil {
		return nil
	} else {
		log.Printf("SMS OTP failed for %s: %v", phone, err)
	}

	// If both failed → delete OTP (important!)
	if err := s.redis.Del(ctx, key).Err(); err != nil {
		log.Printf("Failed to cleanup OTP after delivery failure: %v", err)
	}

	return fmt.Errorf("failed to send otp via all channels")
}

func (s *OTPService) sendOTPNotification(ctx context.Context, channel, phone, otp string) error {

	body := fmt.Sprintf("Your OTP is %s", otp)

	fmt.Println("body", body)

	_, err := s.notificationService.SendNotification(
		ctx,
		channel,
		nil, // no template
		"en",
		[]string{phone},
		nil,
		nil,
		"",
		body,
		nil,
		nil,
		nil,
	)

	return err
}

// func (s *OTPService) sendOTPNotification(ctx context.Context, channel, phone, otp string) error {

// 	template := "OTP_TEMPLATE"

// 	_, err := s.notificationService.SendNotification(
// 		ctx,
// 		channel,
// 		&template,
// 		"en",
// 		[]string{phone},
// 		nil,
// 		nil,
// 		"",
// 		"",
// 		map[string]string{
// 			"otp": otp,
// 		},
// 		nil,
// 		nil,
// 	)

// 	return err
// }

func (s *OTPService) VerifyLoginOTP(ctx context.Context, phone, input string) error {

	key := "otp:login:" + phone

	val, err := s.redis.Get(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("otp expired")
	}

	var data struct {
		Hash     string `json:"hash"`
		Attempts int    `json:"attempts"`
	}

	json.Unmarshal([]byte(val), &data)

	if data.Attempts >= 3 {
		s.redis.Set(ctx, "otp_block:"+phone, "1", 15*time.Minute)
		return fmt.Errorf("too many attempts")
	}

	if data.Hash != HashOTP(input) {
		data.Attempts++
		updated, _ := json.Marshal(data)
		s.redis.Set(ctx, key, updated, 5*time.Minute)
		return fmt.Errorf("invalid otp")
	}

	s.redis.Del(ctx, key)

	return nil
}
