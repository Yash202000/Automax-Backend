package utils

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// LicenseClaims represents the claims embedded in a Licensify JWT token.
type LicenseClaims struct {
	jwt.RegisteredClaims
	LicenseID   string   `json:"license_id"`
	ClientName  string   `json:"client_name"`
	ClientEmail string   `json:"client_email"`
	CompanyName string   `json:"company_name"`
	Product     string   `json:"product"`
	LicenseType string   `json:"license_type"`
	Features    []string `json:"features"`
	MaxUsers    int      `json:"max_users"`
	HardwareID  string   `json:"hardware_id,omitempty"`
}

// ValidateLicenseJWT verifies the JWT signature and parses claims using an RS256 public key.
func ValidateLicenseJWT(tokenString string, publicKeyPEM string) (*LicenseClaims, error) {
	pubKey, err := parseRSAPublicKey(publicKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("invalid public key: %w", err)
	}

	token, err := jwt.ParseWithClaims(tokenString, &LicenseClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		return pubKey, nil
	})
	if err != nil {
		return nil, fmt.Errorf("token validation failed: %w", err)
	}

	claims, ok := token.Claims.(*LicenseClaims)
	if !ok || !token.Valid {
		return nil, fmt.Errorf("invalid token claims")
	}

	return claims, nil
}

// DecodeLicenseJWTUnsafe decodes the JWT payload without verifying the signature.
// WARNING: Only use this for inspection — never for authorization decisions.
func DecodeLicenseJWTUnsafe(tokenString string) (*LicenseClaims, error) {
	parts := strings.Split(tokenString, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format: expected 3 parts, got %d", len(parts))
	}

	// Decode the payload (second part)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode JWT payload: %w", err)
	}

	var claims LicenseClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse JWT claims: %w", err)
	}

	return &claims, nil
}

func parseRSAPublicKey(pemStr string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemStr))
	if block == nil {
		return nil, fmt.Errorf("failed to decode PEM block")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key: %w", err)
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("key is not an RSA public key")
	}

	return rsaPub, nil
}
