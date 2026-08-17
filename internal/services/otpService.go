package services

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"os"
	"strconv"
	"time"

	"github.com/automax/backend/internal/database"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type OTPService struct {
	redis               *redis.Client
	notificationService *NotificationService
	notificationLogRepo repository.NotificationLogRepository
	userRepo            repository.UserRepository
	userService         UserService
	sessionStore        *database.SessionStore
	jwtManager          *utils.JWTManager
	// [Citizen Auto-Register] roleRepo is used to find the "citizen" role when auto-creating citizen users
	roleRepo repository.RoleRepository
}

func NewOTPService(
	redisClient *redis.Client,
	notificationService *NotificationService,
	notificationLogRepo repository.NotificationLogRepository,
	userRepo repository.UserRepository,
	userService UserService,
	sessionStore *database.SessionStore,
	jwtManager *utils.JWTManager,
	roleRepo repository.RoleRepository,
) *OTPService {
	return &OTPService{
		redis:               redisClient,
		notificationService: notificationService,
		notificationLogRepo: notificationLogRepo,
		userRepo:            userRepo,
		userService:         userService,
		sessionStore:        sessionStore,
		jwtManager:          jwtManager,
		roleRepo:            roleRepo,
	}
}

func (s *OTPService) GenerateOTP() (string, error) {
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
	// [Citizen Auto-Register] Citizen name stored during SendOTP, used for auto-creating user on VerifyOTP
	Name string `json:"name,omitempty"`
}

// [Citizen Auto-Register] Added citizenName parameter — stored in Redis alongside OTP
// so it can be used to auto-create the citizen user upon successful OTP verification.
// Pass empty string for non-citizen flows (e.g. authenticated OTP sends).
//
// userType is "citizen" or "employee":
//   - "citizen" or blank → OTP_DATA_EXPIRATION_TIME (existing behavior, unchanged)
//   - "employee"         → LOGIN_OTP_EXPIRY_SECONDS (default 60s)
//
// Super Admins skip OTP entirely regardless of userType: bypassResp is non-nil and
// sessionID is empty in that case, since tokens are issued immediately with no code sent.
func (s *OTPService) SendOTP(ctx context.Context, phone string, senderMode string, userType string, sentBy *uuid.UUID, citizenName ...string) (sessionID string, bypassResp *models.LoginResponse, err error) {

	if user, lookupErr := s.userRepo.FindByMobile(ctx, phone); lookupErr == nil && user != nil && user.IsSuperAdmin {
		authResp, tokenErr := s.userService.GenerateTokenViaUserID(ctx, user.ID)
		if tokenErr != nil {
			return "", nil, fmt.Errorf("failed to generate token: %w", tokenErr)
		}
		return "", &models.LoginResponse{
			Token:        authResp.Token,
			RefreshToken: authResp.RefreshToken,
			ExpiresIn:    authResp.ExpiresIn,
			User:         user,
		}, nil
	}

	// - RATE LIMIT COUNTER
	counterKey := "otp_counter:" + phone

	count, err := s.redis.Incr(ctx, counterKey).Result()
	if err != nil {
		return "", nil, fmt.Errorf("failed to increment otp counter: %w", err)
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
		return "", nil, fmt.Errorf("max otp send attempts reached")
	}

	//GENERATE OTP
	otp, err := s.GenerateOTP()
	fmt.Println("Generated OTP:", otp)
	if err != nil {
		return "", nil, fmt.Errorf("failed to generate otp: %w", err)
	}

	otpSessionID := uuid.New()
	key := "otp:" + phone + "--" + otpSessionID.String()

	// Expiry: "employee" gets a short, .env-configurable window (LOGIN_OTP_EXPIRY_SECONDS,
	// default 60s); "citizen" or blank keeps the existing default (OTP_DATA_EXPIRATION_TIME).
	var ttl time.Duration
	if userType == "employee" {
		loginOtpExpStr := os.Getenv("LOGIN_OTP_EXPIRY_SECONDS")
		loginOtpExp, _ := strconv.Atoi(loginOtpExpStr)
		if loginOtpExp == 0 {
			loginOtpExp = 60
		}
		ttl = time.Duration(loginOtpExp) * time.Second
	} else {
		otpExpStr := os.Getenv("OTP_DATA_EXPIRATION_TIME")
		otpExpMin, _ := strconv.Atoi(otpExpStr)
		if otpExpMin == 0 {
			otpExpMin = 15
		}
		ttl = time.Duration(otpExpMin) * time.Minute
	}

	err = s.sendOTPNotification(ctx, senderMode, phone, otp, &otpSessionID)
	if err != nil {
		s.redis.Del(ctx, key)
		return "", nil, fmt.Errorf("failed to send otp: %w", err)
	}

	// [Citizen Auto-Register] Store citizen name in OTP data for auto-registration on verify
	name := ""
	if len(citizenName) > 0 {
		name = citizenName[0]
	}

	otpData := OTPData{
		Phone:      phone,
		Hash:       HashOTP(otp),
		SenderMode: senderMode,
		Attempts:   0,
		SessionID:  otpSessionID.String(),
		Status:     "sent",
		SentAt:     time.Now(),
		SentBy:     sentBy,
		Name:       name,
	}

	jsonData, _ := json.Marshal(otpData)

	//STORE OTP --------
	err = s.redis.Set(
		ctx,
		key,
		jsonData,
		ttl,
	).Err()

	if err != nil {
		return "", nil, fmt.Errorf("failed to store otp: %w", err)
	}

	// Tombstone: outlives the main OTP key so VerifyOTP can tell "this session existed
	// and timed out" apart from "this session_id was never valid" once the main key
	// (and thus data.Hash/data.Attempts) is gone. Value is unused — only presence matters.
	tombstoneKey := "otp_seen:" + phone + "--" + otpSessionID.String()
	s.redis.Set(ctx, tombstoneKey, "1", ttl+10*time.Minute)

	return otpSessionID.String(), nil, nil
}

func (s *OTPService) sendOTPNotification(ctx context.Context, channel, phone, otp string, sessionID *uuid.UUID) error {
	var body string
	if channel == "sms" {
		body = fmt.Sprintf("Your OTP is %s", otp)
	} else {
		body = (otp)
	}
	_, err := s.notificationService.SendNotification(ctx, channel, nil, "en", []string{phone}, nil, nil, "", body, nil, nil, nil, sessionID)
	if err != nil {
		return fmt.Errorf("Err Into Send Notification Service: %w", err)
	}
	return err
}

func (s *OTPService) VerifyOTP(ctx context.Context, phone string, sessionID string, inputOTP string) (*models.LoginResponse, error) {

	key := "otp:" + phone + "--" + sessionID
	tombstoneKey := "otp_seen:" + phone + "--" + sessionID

	val, err := s.redis.Get(ctx, key).Result()
	if err != nil {
		// Main key is gone — check the tombstone to tell "this OTP existed and timed
		// out" (clear "expired" message) apart from "this session_id was never valid"
		// (clear "invalid session" message), instead of one combined message for both.
		if seen, _ := s.redis.Exists(ctx, tombstoneKey).Result(); seen == 1 {
			return nil, fmt.Errorf("%s", i18n.T(ctx, "otp_expired"))
		}
		return nil, fmt.Errorf("%s", i18n.T(ctx, "invalid_otp_session"))
	}

	var data models.OTPData
	err = json.Unmarshal([]byte(val), &data)
	if err != nil {
		return nil, fmt.Errorf("%s", i18n.T(ctx, "invalid_stored_otp"))
	}

	maxVerifyStr := os.Getenv("VERIFY_OTP_MAX_ATTEMPT")
	maxVerify, _ := strconv.Atoi(maxVerifyStr)
	if maxVerify == 0 {
		maxVerify = 3
	}

	if data.Attempts >= maxVerify {
		s.redis.Del(ctx, key, tombstoneKey)
		return nil, fmt.Errorf("%s", i18n.T(ctx, "max_verify_attempts_exceeded"))
	}

	if data.Hash != HashOTP(inputOTP) {

		data.Attempts++
		updatedData, _ := json.Marshal(data)
		ttl, _ := s.redis.TTL(ctx, key).Result()

		s.redis.Set(ctx, key, updatedData, ttl)

		return nil, fmt.Errorf("%s", i18n.T(ctx, "invalid_otp"))
	}

	// Fetch user by phone — if not found, auto-create as citizen
	user, err := s.userRepo.FindByMobile(ctx, phone)
	if err != nil {
		// [Citizen Auto-Register] Auto-create citizen user on first OTP-verified login.
		// The citizen name was stored in Redis during SendOTP. A unique email/username
		// is generated from the phone number since these fields are required (DB unique constraints).
		// The "citizen" role is assigned automatically. To revert this feature, restore the
		// original line: return nil, fmt.Errorf("%s", i18n.T(ctx, "user_not_found"))
		user, err = s.autoCreateCitizenUser(ctx, phone, data.Name)
		if err != nil {
			return nil, fmt.Errorf("failed to register citizen: %w", err)
		}
	}

	// GenerateTokenViaUserID creates its own session internally; no separate session needed here.
	authResp, err := s.userService.GenerateTokenViaUserID(ctx, user.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}
	// Response
	resp := &models.LoginResponse{
		Token:        authResp.Token,
		RefreshToken: authResp.RefreshToken,
		ExpiresIn:    authResp.ExpiresIn,
		User:         user,
	}

	// update notification log
	err = s.notificationLogRepo.MarkOTPVerified(ctx, sessionID, time.Now())
	if err != nil {
		return nil, fmt.Errorf("failed to update otp status: %w", err)
	}
	//SUCCESS  then DELETE OTP FROM REDIS
	s.redis.Del(ctx, key, tombstoneKey)

	return resp, nil
}

// [Citizen Auto-Register] autoCreateCitizenUser creates a new user record for an unregistered citizen
// who has successfully verified their phone via OTP. The user gets:
//   - Phone = the verified mobile number
//   - FirstName = citizen name provided during OTP send
//   - Email = citizen_<phone>@automax.local (synthetic, to satisfy DB unique constraint)
//   - Username = citizen_<phone> (synthetic, to satisfy DB unique constraint)
//   - MobileVerified = true (phone was just verified via OTP)
//   - Role = "citizen" (looked up by code from roles table)
//
// To revert this feature: remove this method and restore "user not found" error in VerifyOTP.
func (s *OTPService) autoCreateCitizenUser(ctx context.Context, phone string, name string) (*models.User, error) {
	if name == "" {
		name = "Citizen"
	}

	// Find the "citizen" role
	citizenRole, err := s.roleRepo.FindByCode(ctx, "citizen")
	if err != nil {
		return nil, fmt.Errorf("citizen role not found in database — please create a role with code 'citizen'")
	}

	newUser := &models.User{
		FirstName:      name,
		Phone:          phone,
		Email:          fmt.Sprintf("citizen_%s@automax.local", phone),
		Username:       fmt.Sprintf("citizen_%s", phone),
		Password:       uuid.New().String(), // random password — citizen logs in via OTP only
		MobileVerified: true,
		IsActive:       true,
		Roles:          []models.Role{*citizenRole},
	}

	if err := s.userRepo.Create(ctx, newUser); err != nil {
		return nil, fmt.Errorf("failed to create citizen user: %w", err)
	}

	return newUser, nil
}

// func (s *OTPService) VerifyOTP(ctx context.Context, phone string, sessionID string, inputOTP string) error {
// 	key := "otp:" + phone + "--" + sessionID
// 	val, err := s.redis.Get(ctx, key).Result()
// 	if err != nil {
// 		return fmt.Errorf("%s", i18n.T(ctx, "otp_expired_invalid_session"))
// 	}

// 	var data struct {
// 		Phone      string `json:"phone"`
// 		Hash       string `json:"hash"`
// 		SenderMode string `json:"senderMode"`
// 		Attempts   int    `json:"attempts"`
// 	}

// 	err = json.Unmarshal([]byte(val), &data)
// 	if err != nil {
// 		return fmt.Errorf("%s", i18n.T(ctx, "invalid_stored_otp"))
// 	}

// 	// Load verify max attempt
// 	maxVerifyStr := os.Getenv("VERIFY_OTP_MAX_ATTEMPT")
// 	maxVerify, _ := strconv.Atoi(maxVerifyStr)
// 	if maxVerify == 0 {
// 		maxVerify = 3
// 	}

// 	// Check max verify attempts
// 	if data.Attempts >= maxVerify {
// 		s.redis.Del(ctx, key)
// 		return fmt.Errorf("%s", i18n.T(ctx, "max_verify_attempts_exceeded"))
// 	}

// 	// Check OTP match
// 	if data.Hash != HashOTP(inputOTP) {
// 		data.Attempts++
// 		updatedData, _ := json.Marshal(data)
// 		ttl, _ := s.redis.TTL(ctx, key).Result()

// 		// Update attempts but keep original TTL
// 		s.redis.Set(ctx, key, updatedData, ttl)

// 		return fmt.Errorf("%s", i18n.T(ctx, "invalid_otp"))
// 	}
// 	// UPDATE DB
// 	err = s.notificationLogRepo.MarkOTPVerified(ctx, sessionID, time.Now())
// 	if err != nil {
// 		return errors.New("failed to update otp status")
// 	}

// 	//SUCCESS  then DELETE OTP FROM REDIS
// 	s.redis.Del(ctx, key)

// 	return nil
// }
