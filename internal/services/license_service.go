package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/utils"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Sentinel errors
var (
	ErrNoLicense        = errors.New("no active license")
	ErrLicenseExpired   = errors.New("license has expired")
	ErrLicenseInvalid   = errors.New("license key is invalid")
	ErrProductMismatch  = errors.New("license is not for this product")
	ErrUserLimitReached = errors.New("active user count exceeds license limit")
	ErrEncryptionKey    = errors.New("LICENSE_ENCRYPTION_KEY is not configured")
)

type LicenseService interface {
	Activate(ctx context.Context, licenseKey string, jwksJSON string, activatedBy uuid.UUID) (*models.LicenseStatusResponse, error)
	Deactivate(ctx context.Context) error
	GetStatus(ctx context.Context) (*models.LicenseStatusResponse, error)
	GetInfo(ctx context.Context) (*models.LicenseInfoResponse, error)
	IsFeatureLicensed(featureCode string) bool
	GetMaxUsers() int
	IsValid() bool
	IsGracePeriod() bool
	IsEnabled() bool
	LoadFromDB(ctx context.Context) error
}

// licenseCache holds decoded license info in memory to avoid DB queries on every request.
type licenseCache struct {
	mu               sync.RWMutex
	loaded           bool
	valid            bool
	gracePeriod      bool
	licenseType      string
	features         map[string]bool
	maxUsers         int
	expiresAt        *time.Time
	validationStatus string
}

type licenseService struct {
	repo    repository.LicenseRepository
	cfg     config.LicenseConfig
	cache   licenseCache
}

func NewLicenseService(repo repository.LicenseRepository, cfg config.LicenseConfig) LicenseService {
	return &licenseService{
		repo: repo,
		cfg:  cfg,
	}
}

func (s *licenseService) IsEnabled() bool {
	return s.cfg.Enabled
}

func (s *licenseService) Activate(ctx context.Context, licenseKey string, jwksJSON string, activatedBy uuid.UUID) (*models.LicenseStatusResponse, error) {
	if s.cfg.EncryptionKey == "" {
		return nil, ErrEncryptionKey
	}

	// 1. Decode without verification to inspect product
	unsafeClaims, err := utils.DecodeLicenseJWTUnsafe(licenseKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decode license key: %w", err)
	}
	if unsafeClaims.Product != "automax" {
		return nil, ErrProductMismatch
	}

	// 2. Parse the JWKS and verify the signature by looking up the token's kid.
	keysByKid, err := utils.ParseJWKS(jwksJSON)
	if err != nil {
		return nil, fmt.Errorf("invalid JWKS: %w", err)
	}
	claims, err := utils.ValidateLicenseJWTWithJWKS(licenseKey, keysByKid)
	if err != nil {
		return nil, fmt.Errorf("license key validation failed: %w", err)
	}

	// 3. Check active user count against max_users
	activeCount, err := s.repo.CountActiveUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to count active users: %w", err)
	}
	if activeCount > int64(claims.MaxUsers) {
		return nil, fmt.Errorf("%w: %d active users exceeds license limit of %d — deactivate users first", ErrUserLimitReached, activeCount, claims.MaxUsers)
	}

	// 4. Encrypt the license key
	encryptedKey, nonce, err := utils.EncryptLicenseKey(licenseKey, s.cfg.EncryptionKey)
	if err != nil {
		return nil, fmt.Errorf("failed to encrypt license key: %w", err)
	}

	// 5. Marshal features to JSON
	featuresJSON, err := json.Marshal(claims.Features)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal features: %w", err)
	}

	// 6. Determine expiry and validation status
	var expiresAt *time.Time
	validationStatus := "valid"
	if claims.ExpiresAt != nil {
		t := claims.ExpiresAt.Time
		expiresAt = &t
		if time.Now().After(t) {
			graceEnd := t.Add(time.Duration(s.cfg.GracePeriodDays) * 24 * time.Hour)
			if time.Now().After(graceEnd) {
				validationStatus = "expired"
			}
			// Within grace period — still "valid" for activation, middleware handles grace mode
		}
	}

	now := time.Now()
	license := &models.License{
		EncryptedKey:     encryptedKey,
		KeyNonce:         nonce,
		LicenseID:        claims.LicenseID,
		ClientName:       claims.ClientName,
		ClientEmail:      claims.ClientEmail,
		CompanyName:      claims.CompanyName,
		Product:          claims.Product,
		LicenseType:      claims.LicenseType,
		Features:         featuresJSON,
		MaxUsers:         claims.MaxUsers,
		ExpiresAt:        expiresAt,
		ActivatedAt:      &now,
		ActivatedBy:      &activatedBy,
		ValidationStatus: validationStatus,
		JWKS:             jwksJSON,
	}

	// 7. Upsert (replace any existing license)
	if err := s.repo.Upsert(ctx, license); err != nil {
		return nil, fmt.Errorf("failed to store license: %w", err)
	}

	// 8. Update in-memory cache
	s.updateCache(claims.Features, claims.MaxUsers, expiresAt, claims.LicenseType, validationStatus)

	return s.buildStatusResponse(license, activeCount)
}

func (s *licenseService) Deactivate(ctx context.Context) error {
	license, err := s.repo.GetActive(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNoLicense
		}
		return err
	}

	if err := s.repo.Delete(ctx, license.ID); err != nil {
		return err
	}

	s.clearCache()
	return nil
}

func (s *licenseService) GetStatus(ctx context.Context) (*models.LicenseStatusResponse, error) {
	license, err := s.repo.GetActive(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrNoLicense
		}
		return nil, err
	}

	activeCount, _ := s.repo.CountActiveUsers(ctx)
	return s.buildStatusResponse(license, activeCount)
}

func (s *licenseService) GetInfo(ctx context.Context) (*models.LicenseInfoResponse, error) {
	s.cache.mu.RLock()
	loaded := s.cache.loaded
	s.cache.mu.RUnlock()

	if !loaded {
		return nil, ErrNoLicense
	}

	activeCount, _ := s.repo.CountActiveUsers(ctx)

	s.cache.mu.RLock()
	defer s.cache.mu.RUnlock()

	features := make([]string, 0, len(s.cache.features))
	for f := range s.cache.features {
		features = append(features, f)
	}

	resp := &models.LicenseInfoResponse{
		LicenseType:      s.cache.licenseType,
		Features:         features,
		MaxUsers:         s.cache.maxUsers,
		ActiveUserCount:  activeCount,
		IsGracePeriod:    s.cache.gracePeriod,
		ValidationStatus: s.cache.validationStatus,
	}

	if s.cache.expiresAt != nil {
		t := s.cache.expiresAt.Format(time.RFC3339)
		resp.ExpiresAt = &t
		days := int(math.Ceil(time.Until(*s.cache.expiresAt).Hours() / 24))
		resp.DaysRemaining = &days
	}

	return resp, nil
}

func (s *licenseService) IsFeatureLicensed(featureCode string) bool {
	s.cache.mu.RLock()
	defer s.cache.mu.RUnlock()
	if !s.cache.loaded || !s.cache.valid {
		return false
	}
	return s.cache.features[featureCode]
}

func (s *licenseService) GetMaxUsers() int {
	s.cache.mu.RLock()
	defer s.cache.mu.RUnlock()
	return s.cache.maxUsers
}

func (s *licenseService) IsValid() bool {
	s.cache.mu.RLock()
	defer s.cache.mu.RUnlock()
	return s.cache.loaded && s.cache.valid
}

func (s *licenseService) IsGracePeriod() bool {
	s.cache.mu.RLock()
	defer s.cache.mu.RUnlock()
	return s.cache.gracePeriod
}

// LoadFromDB loads the active license from the database and populates the in-memory cache.
// Called once at server startup.
func (s *licenseService) LoadFromDB(ctx context.Context) error {
	license, err := s.repo.GetActive(ctx)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			log.Println("License: No active license found")
			return nil // No license — not an error
		}
		return fmt.Errorf("failed to load license: %w", err)
	}

	// Decrypt and re-validate if encryption key is available
	if s.cfg.EncryptionKey != "" && license.JWKS != "" {
		plainKey, err := utils.DecryptLicenseKey(license.EncryptedKey, license.KeyNonce, s.cfg.EncryptionKey)
		if err != nil {
			log.Printf("License: Failed to decrypt stored key: %v", err)
			s.updateCacheFromLicense(license)
			return nil
		}

		keysByKid, err := utils.ParseJWKS(license.JWKS)
		if err != nil {
			log.Printf("License: Failed to parse stored JWKS: %v", err)
			s.updateCacheFromLicense(license)
			return nil
		}

		claims, err := utils.ValidateLicenseJWTWithJWKS(plainKey, keysByKid)
		if err != nil {
			log.Printf("License: Stored key failed validation: %v", err)
			// Key might be expired — still load from DB for grace period handling
			s.updateCacheFromLicense(license)
			return nil
		}

		// Re-validate succeeded — update cache from fresh claims
		var expiresAt *time.Time
		if claims.ExpiresAt != nil {
			t := claims.ExpiresAt.Time
			expiresAt = &t
		}
		s.updateCache(claims.Features, claims.MaxUsers, expiresAt, claims.LicenseType, license.ValidationStatus)
	} else {
		s.updateCacheFromLicense(license)
	}

	log.Printf("License: Loaded — type=%s, max_users=%d, features=%d, status=%s",
		license.LicenseType, license.MaxUsers, len(s.cache.features), license.ValidationStatus)
	return nil
}

// updateCache refreshes the in-memory cache from decoded claims.
func (s *licenseService) updateCache(features []string, maxUsers int, expiresAt *time.Time, licenseType, validationStatus string) {
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()

	s.cache.loaded = true
	s.cache.licenseType = licenseType
	s.cache.maxUsers = maxUsers
	s.cache.expiresAt = expiresAt
	s.cache.validationStatus = validationStatus

	s.cache.features = make(map[string]bool, len(features))
	for _, f := range features {
		s.cache.features[f] = true
	}

	// Determine validity and grace period
	s.cache.valid = true
	s.cache.gracePeriod = false

	if validationStatus == "expired" || validationStatus == "invalid" {
		s.cache.valid = false
		return
	}

	if expiresAt != nil && time.Now().After(*expiresAt) {
		graceEnd := expiresAt.Add(time.Duration(s.cfg.GracePeriodDays) * 24 * time.Hour)
		if time.Now().After(graceEnd) {
			s.cache.valid = false
			s.cache.validationStatus = "expired"
		} else {
			s.cache.gracePeriod = true
		}
	}
}

// updateCacheFromLicense loads cache from the stored License model (without re-validating JWT).
func (s *licenseService) updateCacheFromLicense(license *models.License) {
	var features []string
	if license.Features != nil {
		_ = json.Unmarshal(license.Features, &features)
	}
	s.updateCache(features, license.MaxUsers, license.ExpiresAt, license.LicenseType, license.ValidationStatus)
}

func (s *licenseService) clearCache() {
	s.cache.mu.Lock()
	defer s.cache.mu.Unlock()
	s.cache.loaded = false
	s.cache.valid = false
	s.cache.gracePeriod = false
	s.cache.features = nil
	s.cache.maxUsers = 0
	s.cache.expiresAt = nil
	s.cache.licenseType = ""
	s.cache.validationStatus = ""
}

func (s *licenseService) buildStatusResponse(license *models.License, activeCount int64) (*models.LicenseStatusResponse, error) {
	var features []string
	if license.Features != nil {
		_ = json.Unmarshal(license.Features, &features)
	}

	resp := &models.LicenseStatusResponse{
		LicenseID:        license.LicenseID,
		ClientName:       license.ClientName,
		ClientEmail:      license.ClientEmail,
		CompanyName:      license.CompanyName,
		Product:          license.Product,
		LicenseType:      license.LicenseType,
		Features:         features,
		MaxUsers:         license.MaxUsers,
		ActiveUserCount:  activeCount,
		ValidationStatus: license.ValidationStatus,
	}

	s.cache.mu.RLock()
	resp.IsGracePeriod = s.cache.gracePeriod
	s.cache.mu.RUnlock()

	if license.ExpiresAt != nil {
		t := license.ExpiresAt.Format(time.RFC3339)
		resp.ExpiresAt = &t
		days := int(math.Ceil(time.Until(*license.ExpiresAt).Hours() / 24))
		resp.DaysRemaining = &days
	}

	if license.ActivatedAt != nil {
		t := license.ActivatedAt.Format(time.RFC3339)
		resp.ActivatedAt = &t
	}

	if license.ActivatedBy != nil {
		s := license.ActivatedBy.String()
		resp.ActivatedBy = &s
	}

	return resp, nil
}
