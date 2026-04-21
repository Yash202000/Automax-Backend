// Package devlicense is a self-contained JWT issuer for development and CI.
//
// It uses the same embedded dev RSA keypair as the devseed package so that
// a token issued here activates cleanly against a backend running with the
// dev seeder enabled. Never used in production.
package devlicense

import (
	"embed"
	"fmt"
	"time"

	"github.com/automax/backend/internal/licensing"
	"github.com/automax/backend/pkg/utils"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

//go:embed license_keys/dev_private.pem license_keys/dev_public.pem
var devKeys embed.FS

// Issue signs a dev license JWT with the given features and expiry.
// If features is nil/empty, every feature in the catalog is granted.
// Returns the JWT string and the matching public key PEM, both suitable
// for POSTing to /api/v1/admin/license/activate.
func Issue(features []string, expiryDays int, maxUsers int) (licenseKey, publicKey string, err error) {
	if len(features) == 0 {
		features = licensing.AllCodes()
	}
	if expiryDays <= 0 {
		expiryDays = 90
	}
	if maxUsers <= 0 {
		maxUsers = 1000
	}

	privBytes, err := devKeys.ReadFile("license_keys/dev_private.pem")
	if err != nil {
		return "", "", fmt.Errorf("read embedded private key: %w", err)
	}
	pubBytes, err := devKeys.ReadFile("license_keys/dev_public.pem")
	if err != nil {
		return "", "", fmt.Errorf("read embedded public key: %w", err)
	}

	privKey, err := jwt.ParseRSAPrivateKeyFromPEM(privBytes)
	if err != nil {
		return "", "", fmt.Errorf("parse private key: %w", err)
	}

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
		Features:    features,
		MaxUsers:    maxUsers,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	signed, err := token.SignedString(privKey)
	if err != nil {
		return "", "", fmt.Errorf("sign token: %w", err)
	}
	return signed, string(pubBytes), nil
}
