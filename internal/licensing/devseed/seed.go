// Package devseed auto-activates a development license at server startup.
// It only runs when APP_ENV=development (or LICENSE_DEV_SEED=true), so
// production deployments are unaffected.
package devseed

import (
	"context"
	"fmt"
	"log"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/licensing"
	"github.com/automax/backend/internal/licensing/devlicense"
	"github.com/automax/backend/internal/services"
	"github.com/google/uuid"
)

// devSeederSystemUserID is a placeholder "activated by" for the dev seeder.
// It is not a real user — it identifies bootstrap-time activations.
const devSeederSystemUserID = "00000000-0000-0000-0000-000000000000"

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

	expiryDays := cfg.License.DevSeedExpiryDays
	token, pubKey, err := devlicense.Issue(nil, expiryDays, 1000)
	if err != nil {
		return fmt.Errorf("issue dev license: %w", err)
	}

	systemUUID, _ := uuid.Parse(devSeederSystemUserID)
	if _, err := licenseSvc.Activate(ctx, token, pubKey, systemUUID); err != nil {
		return fmt.Errorf("activate dev license: %w", err)
	}

	if expiryDays <= 0 {
		expiryDays = 90
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
