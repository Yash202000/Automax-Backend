package utils

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// SSOClaims holds the claims for an SSO JWT token
type SSOClaims struct {
	Email     string `json:"email"`
	Name      string `json:"name"`
	TargetApp string `json:"target_app"`
	Role      string `json:"role"`
	JTI       string `json:"jti"`
	jwt.RegisteredClaims
}

// SSOJWTManager manages RSA key pair for SSO token operations
type SSOJWTManager struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	keyID      string
	issuerURL  string // set via SSO_ISSUER_URL; embedded in "iss" claim so receivers can fetch JWKS dynamically
}

// NewSSOJWTManager creates a new SSOJWTManager.
//   - privatePEM: PEM-encoded RSA private key. Auto-generates 2048-bit if empty (dev only).
//   - issuerURL:  Base URL of this Automax instance, e.g. "https://automax.example.com".
//     Embedded in the "iss" JWT claim so receivers can dynamically fetch
//     the public key from {iss}/.well-known/jwks.json.
//     Falls back to the plain string "automax" if empty (same-deployment use).
func NewSSOJWTManager(privatePEM, issuerURL string) *SSOJWTManager {
	mgr := &SSOJWTManager{
		keyID:     "automax-sso-1",
		issuerURL: issuerURL,
	}

	if privatePEM == "" {
		log.Println("WARNING: SSO_RSA_PRIVATE_KEY not set — auto-generating ephemeral RSA-2048 key (dev only, tokens won't survive restart)")
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			log.Fatalf("Failed to generate RSA key: %v", err)
		}
		mgr.privateKey = key
		mgr.publicKey = &key.PublicKey
		return mgr
	}

	// Support "\n" literals that appear when the PEM is set via environment variable
	normalized := strings.ReplaceAll(privatePEM, `\n`, "\n")
	block, _ := pem.Decode([]byte(normalized))
	if block == nil {
		log.Fatalf("Failed to decode SSO_RSA_PRIVATE_KEY: not a valid PEM block")
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		// Try PKCS8 (e.g. keys generated with openssl genpkey)
		keyAny, err2 := x509.ParsePKCS8PrivateKey(block.Bytes)
		if err2 != nil {
			log.Fatalf("Failed to parse RSA private key (tried PKCS1 and PKCS8): %v / %v", err, err2)
		}
		rsaKey, ok := keyAny.(*rsa.PrivateKey)
		if !ok {
			log.Fatalf("SSO_RSA_PRIVATE_KEY is not an RSA private key")
		}
		key = rsaKey
	}

	mgr.privateKey = key
	mgr.publicKey = &key.PublicKey
	return mgr
}

// issuer returns the value used for the "iss" JWT claim.
// Uses SSO_ISSUER_URL when set (cross-deployment); falls back to "automax" (same-deployment).
func (m *SSOJWTManager) issuer() string {
	if m.issuerURL != "" {
		return m.issuerURL
	}
	return "automax"
}

// GenerateSSOToken creates a signed RS256 JWT for SSO relay with a 60-second TTL.
// When SSO_ISSUER_URL is configured, the "iss" claim is set to that URL so that
// receiver deployments can fetch the public key from {iss}/.well-known/jwks.json.
func (m *SSOJWTManager) GenerateSSOToken(sub, email, name, targetApp, role string, jti uuid.UUID) (string, error) {
	now := time.Now()
	claims := SSOClaims{
		Email:     email,
		Name:      name,
		TargetApp: targetApp,
		Role:      role,
		JTI:       jti.String(),
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   sub,
			Issuer:    m.issuer(),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(60 * time.Second)),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	token.Header["kid"] = m.keyID
	return token.SignedString(m.privateKey)
}

// ValidateSSOToken verifies an RS256 token using the local instance's own public key.
// Use this when the token was issued by the same Automax deployment (iss = "automax").
func (m *SSOJWTManager) ValidateSSOToken(tokenString string) (*SSOClaims, error) {
	return m.validateWithKey(tokenString, m.publicKey)
}

// ValidateSSOTokenAuto verifies an RS256 token with automatic key resolution:
//   - If iss is a URL (starts with "http"), the public key is fetched dynamically
//     from {iss}/.well-known/jwks.json — cross-deployment path.
//   - Otherwise the local public key is used — same-deployment path.
//
// This is the method used in /sso/callback so both same-deployment and
// cross-deployment tokens are handled transparently.
func (m *SSOJWTManager) ValidateSSOTokenAuto(ctx context.Context, tokenString string) (*SSOClaims, error) {
	// Decode without verifying to read the "iss" claim
	unverified, _, err := jwt.NewParser().ParseUnverified(tokenString, &SSOClaims{})
	if err != nil {
		return nil, fmt.Errorf("failed to decode token: %w", err)
	}
	claims, ok := unverified.Claims.(*SSOClaims)
	if !ok {
		return nil, errors.New("failed to read token claims")
	}

	issuer := claims.Issuer
	if strings.HasPrefix(issuer, "http://") || strings.HasPrefix(issuer, "https://") {
		// Cross-deployment: fetch the provider's public key dynamically
		jwksURL := strings.TrimRight(issuer, "/") + "/.well-known/jwks.json"
		pubKey, err := fetchRSAPublicKeyFromJWKS(ctx, jwksURL)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch provider JWKS from %s: %w", jwksURL, err)
		}
		return m.validateWithKey(tokenString, pubKey)
	}

	// Same-deployment: validate with local key
	return m.ValidateSSOToken(tokenString)
}

// validateWithKey verifies signature + expiry using the supplied RSA public key.
func (m *SSOJWTManager) validateWithKey(tokenString string, pubKey *rsa.PublicKey) (*SSOClaims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &SSOClaims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, errors.New("unexpected signing method: expected RS256")
		}
		return pubKey, nil
	})
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(*SSOClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid SSO token")
	}
	return claims, nil
}

// GetJWKS returns the local RSA public key formatted as a JSON Web Key Set.
func (m *SSOJWTManager) GetJWKS() map[string]interface{} {
	pub := m.publicKey

	nEncoded := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	eEncoded := base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes())

	return map[string]interface{}{
		"keys": []interface{}{
			map[string]interface{}{
				"kty": "RSA",
				"use": "sig",
				"alg": "RS256",
				"kid": m.keyID,
				"n":   nEncoded,
				"e":   eEncoded,
			},
		},
	}
}

// fetchRSAPublicKeyFromJWKS fetches a JWKS URL and reconstructs the first RS256 public key found.
func fetchRSAPublicKeyFromJWKS(ctx context.Context, jwksURL string) (*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	if err != nil {
		return nil, err
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("JWKS endpoint returned HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var jwks struct {
		Keys []struct {
			Kty string `json:"kty"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &jwks); err != nil {
		return nil, fmt.Errorf("failed to parse JWKS JSON: %w", err)
	}

	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || k.N == "" || k.E == "" {
			continue
		}
		nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			continue
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			continue
		}
		return &rsa.PublicKey{
			N: new(big.Int).SetBytes(nBytes),
			E: int(new(big.Int).SetBytes(eBytes).Int64()),
		}, nil
	}

	return nil, errors.New("no RSA key found in JWKS response")
}
