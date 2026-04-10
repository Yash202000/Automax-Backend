package utils

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	ErrExpired        = errors.New("token has expired")
	ErrInvalid        = errors.New("invalid token signature")
	ErrMalformed      = errors.New("malformed token")
	ErrIDMismatch     = errors.New("token id mismatch")
	ErrWrongTokenType = errors.New("wrong token type")
)

const ivrPrefix = "ivr_signed"

// GenerateIncidentToken creates a signed token valid for 24 hours.
// Format: incidentID|expiresAt|hmac
func GenerateIncidentToken(incidentID string, t time.Duration) string {
	expiresAt := time.Now().Add(t).Unix()
	payload := fmt.Sprintf("%s|%d", incidentID, expiresAt)
	sig := sign(payload)
	return fmt.Sprintf("%s|%s", payload, sig)
}

// // ValidateIncidentToken verifies the token signature, expiry, and that
// // the id inside the token matches the id from the URL param.
func ValidateIncidentToken(token, expectedID string) error {
	// Decode URL-encoding if token was passed as a query param
	decoded, err := url.QueryUnescape(token)
	if err != nil {
		log.Println("Error decoding token:", err)
	} else {
		token = decoded
	}

	parts := strings.Split(token, "|")
	if len(parts) != 3 {
		return ErrMalformed
	}

	incidentID, expiresAtStr, sig := parts[0], parts[1], parts[2]

	// 1. Verify HMAC signature — prevents tampering
	payload := fmt.Sprintf("%s|%s", incidentID, expiresAtStr)
	if !hmac.Equal([]byte(sign(payload)), []byte(sig)) {
		return ErrInvalid
	}

	// 2. Check expiry
	expiresAt, err := strconv.ParseInt(expiresAtStr, 10, 64)
	if err != nil {
		return ErrMalformed
	}
	if time.Now().Unix() > expiresAt {
		return ErrExpired
	}

	// 3. Ensure token belongs to the requested incident
	if incidentID != expectedID {
		return ErrIDMismatch
	}

	return nil
}

// GenerateIvrSessionToken creates a short-lived signed token valid for 15 minutes.
// Format: ivr_session|incidentID|expiresAt|hmac
func GenerateIvrSessionToken(incidentID string, duration time.Duration) string {
	expiresAt := time.Now().Add(duration).Unix()
	payload := fmt.Sprintf("%s|%s|%d", ivrPrefix, incidentID, expiresAt)
	sig := sign(payload)
	return fmt.Sprintf("%s|%s", payload, sig)
}

// ValidateIvrSessionToken verifies the 15-minute IVR session token.
func ValidateIvrSessionToken(token, expectedID string) error {
	parts := strings.Split(token, "|")
	if len(parts) != 4 {
		return ErrMalformed
	}

	prefix, incidentID, expiresAtStr, sig := parts[0], parts[1], parts[2], parts[3]

	// 1. Ensure this is the correct token type
	if prefix != ivrPrefix {
		return ErrWrongTokenType
	}

	// 2. Verify HMAC signature
	payload := fmt.Sprintf("%s|%s|%s", prefix, incidentID, expiresAtStr)
	if !hmac.Equal([]byte(sign(payload)), []byte(sig)) {
		return ErrInvalid
	}

	// 3. Check expiry
	expiresAt, err := strconv.ParseInt(expiresAtStr, 10, 64)
	if err != nil {
		return ErrMalformed
	}
	if time.Now().Unix() > expiresAt {
		return ErrExpired
	}

	// 4. Ensure token belongs to the requested incident
	if incidentID != expectedID {
		return ErrIDMismatch
	}

	return nil
}

func sign(payload string) string {
	secret := os.Getenv("SIGNED_URL_SECRET") // e.g. "my-secret-key-32chars"
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}
