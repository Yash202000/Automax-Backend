package utils

import (
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
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

// JWKS is the RFC 7517 JSON Web Key Set structure Automax consumes from LMA.
type JWKS struct {
	Keys []JWK `json:"keys"`
}

// JWK is a single RSA public key in JWKS form.
type JWK struct {
	Kty string `json:"kty"` // "RSA"
	Use string `json:"use"` // "sig"
	Alg string `json:"alg"` // "RS256"
	Kid string `json:"kid"`
	N   string `json:"n"` // base64url-encoded modulus (no padding)
	E   string `json:"e"` // base64url-encoded exponent (no padding)
}

// ValidateLicenseJWT verifies the JWT signature and parses claims using an RS256 public key.
//
// Deprecated: retained as a legacy/reference helper. New callers should use
// ValidateLicenseJWTWithJWKS which understands the `kid` header and JWKS rotation.
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

// ParseJWKS decodes a JWKS JSON blob into a map of kid → *rsa.PublicKey.
// Only RSA keys (kty=RSA) are accepted. Duplicate kids are rejected.
func ParseJWKS(jwksJSON string) (map[string]*rsa.PublicKey, error) {
	var set JWKS
	if err := json.Unmarshal([]byte(jwksJSON), &set); err != nil {
		return nil, fmt.Errorf("invalid JWKS JSON: %w", err)
	}
	if len(set.Keys) == 0 {
		return nil, fmt.Errorf("JWKS contains no keys")
	}

	out := make(map[string]*rsa.PublicKey, len(set.Keys))
	for i, k := range set.Keys {
		if !strings.EqualFold(k.Kty, "RSA") {
			// Skip non-RSA keys silently — JWKS may contain other key types.
			continue
		}
		if k.Kid == "" {
			return nil, fmt.Errorf("JWKS key at index %d is missing kid", i)
		}
		if _, exists := out[k.Kid]; exists {
			return nil, fmt.Errorf("duplicate kid %q in JWKS", k.Kid)
		}

		nBytes, err := base64.RawURLEncoding.DecodeString(k.N)
		if err != nil {
			return nil, fmt.Errorf("JWKS key %q: invalid modulus: %w", k.Kid, err)
		}
		eBytes, err := base64.RawURLEncoding.DecodeString(k.E)
		if err != nil {
			return nil, fmt.Errorf("JWKS key %q: invalid exponent: %w", k.Kid, err)
		}

		pub := &rsa.PublicKey{
			N: new(big.Int).SetBytes(nBytes),
			E: int(new(big.Int).SetBytes(eBytes).Int64()),
		}
		out[k.Kid] = pub
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("JWKS contained no usable RSA keys")
	}
	return out, nil
}

// ValidateLicenseJWTWithJWKS verifies a token by looking up its `kid` header
// in the provided JWKS set. Returns an error if the kid header is missing or
// unknown, the signature is invalid, or the token is expired / malformed.
func ValidateLicenseJWTWithJWKS(tokenString string, keysByKid map[string]*rsa.PublicKey) (*LicenseClaims, error) {
	if len(keysByKid) == 0 {
		return nil, fmt.Errorf("no JWKS keys available")
	}

	token, err := jwt.ParseWithClaims(tokenString, &LicenseClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		kidVal, ok := token.Header["kid"]
		if !ok {
			return nil, fmt.Errorf("token is missing kid header")
		}
		kid, ok := kidVal.(string)
		if !ok || kid == "" {
			return nil, fmt.Errorf("token kid header is not a string")
		}
		key, found := keysByKid[kid]
		if !found {
			return nil, fmt.Errorf("unknown kid %q", kid)
		}
		return key, nil
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

// JWKSFromPEM wraps a single RSA public PEM into a one-entry JWKS JSON blob.
// Used by the dev license issuer. The kid is a deterministic 16-char prefix of
// sha256(pem).
func JWKSFromPEM(publicPEM string) (jwksJSON string, kid string, err error) {
	pub, err := parseRSAPublicKey(publicPEM)
	if err != nil {
		return "", "", fmt.Errorf("parse public PEM: %w", err)
	}

	sum := sha256.Sum256([]byte(publicPEM))
	kid = hex.EncodeToString(sum[:])[:16]

	nBytes := pub.N.Bytes()
	eBytes := big.NewInt(int64(pub.E)).Bytes()

	jwk := JWK{
		Kty: "RSA",
		Use: "sig",
		Alg: "RS256",
		Kid: kid,
		N:   base64.RawURLEncoding.EncodeToString(nBytes),
		E:   base64.RawURLEncoding.EncodeToString(eBytes),
	}
	set := JWKS{Keys: []JWK{jwk}}

	b, err := json.Marshal(set)
	if err != nil {
		return "", "", fmt.Errorf("marshal JWKS: %w", err)
	}
	return string(b), kid, nil
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
