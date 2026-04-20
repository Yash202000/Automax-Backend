// Package devseed auto-activates a development license at server startup.
// It only runs when APP_ENV=development (or LICENSE_DEV_SEED=true), so
// production deployments are unaffected.
package devseed

import (
	"context"
	"embed"
	"fmt"
	"log"
	"time"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/licensing"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/utils"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

//go:embed license_keys/dev_private.pem license_keys/dev_public.pem
var devLicenseKeys embed.FS

const (
	devPrivateKeyPath = "license_keys/dev_private.pem"
	devPublicKeyPath  = "license_keys/dev_public.pem"
	// systemUserID is a placeholder "activated by" for the dev seeder.
	// It is not a real user — it identifies bootstrap-time activations.
	devSeederSystemUserID = "00000000-0000-0000-0000-000000000000"
)

// SeedDevLicenseIfNeeded auto-activates a dev license at startup when:
//  1. The license service is enabled AND
//  2. No valid license is currently loaded AND
//  3. APP_ENV is "development" OR LICENSE_DEV_SEED=true
//
// The dev license is signed with a repo-committed RSA keypair that is
// watermarked "DEV ONLY — DO NOT USE IN PRODUCTION". It grants every
// feature in licensing.Catalog so developers can run any flow without
// juggling JWTs.
//
// Production deployments set APP_ENV=production (the default), so this
// function is a no-op there.
func SeedDevLicenseIfNeeded(ctx context.Context, cfg *config.Config, licenseSvc services.LicenseService) error {
	if !licenseSvc.IsEnabled() {
		return nil // license enforcement disabled globally
	}
	if !shouldSeedDev(cfg) {
		return nil
	}
	if licenseSvc.IsValid() {
		log.Println("[license] dev seeder: license already active, skipping")
		return nil
	}

	if cfg.License.EncryptionKey == "" {
		return fmt.Errorf("dev license seeder requires LICENSE_ENCRYPTION_KEY to be set")
	}

	privBytes, err := devLicenseKeys.ReadFile(devPrivateKeyPath)
	if err != nil {
		return fmt.Errorf("read embedded dev private key: %w", err)
	}
	pubBytes, err := devLicenseKeys.ReadFile(devPublicKeyPath)
	if err != nil {
		return fmt.Errorf("read embedded dev public key: %w", err)
	}

	expiryDays := cfg.License.DevSeedExpiryDays
	if expiryDays <= 0 {
		expiryDays = 90
	}
	token, err := signDevLicense(string(privBytes), expiryDays)
	if err != nil {
		return fmt.Errorf("sign dev license: %w", err)
	}

	systemUUID, _ := uuid.Parse(devSeederSystemUserID)
	_, err = licenseSvc.Activate(ctx, token, string(pubBytes), systemUUID)
	if err != nil {
		return fmt.Errorf("activate dev license: %w", err)
	}

	log.Printf("[license] DEV LICENSE SEEDED — NOT FOR PRODUCTION USE (features=%d, valid for %d days)",
		len(licensing.Catalog), expiryDays)
	return nil
}

func shouldSeedDev(cfg *config.Config) bool {
	if cfg.License.DevSeedEnabled {
		return true
	}
	return cfg.Env == "development"
}

// signDevLicense creates an RS256-signed JWT with every feature from the catalog,
// a configurable expiry (default 90 days), and a 1000-user cap. Uses utils.LicenseClaims
// so the format is byte-for-byte identical to a real Licensify-issued token.
func signDevLicense(privateKeyPEM string, expiryDays int) (string, error) {
	now := time.Now().UTC()
	expiry := now.Add(time.Duration(expiryDays) * 24 * time.Hour)

	claims := utils.LicenseClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiry),
			Issuer:    "automax-dev-seeder",
			Subject:   "license",
		},
		LicenseID:   uuid.New().String(),
		ClientName:  "Automax Development",
		ClientEmail: "devops@automax.local",
		CompanyName: "Automax Dev",
		Product:     "automax",
		LicenseType: "development",
		Features:    licensing.AllCodes(),
		MaxUsers:    1000,
	}

	// Parse the PEM private key (comments before the BEGIN marker are ignored by jwt.ParseRSAPrivateKeyFromPEM).
	privKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(privateKeyPEM))
	if err != nil {
		return "", fmt.Errorf("parse private key: %w", err)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	return token.SignedString(privKey)
}
