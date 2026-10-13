package utils

import (
	"regexp"
	"strings"
)

var (
	secretPairRe = regexp.MustCompile(`(?i)("?(?:password|passwd|pwd|token|secret|authorization|api[_-]?key|access_token|refresh_token|otp)"?\s*[:=]\s*)("[^"]*"|[^\s,;&]+)`)
	bearerRe     = regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9\-._~+/]+=*`)
	// Phone numbers and national / ID numbers: 9+ digits, optionally with spaces, dashes or a leading +.
	longNumberRe = regexp.MustCompile(`\+?\d[\d\s\-]{7,}\d`)
)

// MaskPhone masks the middle of a phone number, e.g. +971501234567 -> +971 50 *** 4567.
func MaskPhone(phone string) string {
	digits := strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, phone)
	if len(digits) < 8 {
		return phone
	}
	prefix := ""
	if strings.HasPrefix(strings.TrimSpace(phone), "+") {
		prefix = "+"
	}
	return prefix + digits[:3] + " " + digits[3:5] + " *** " + digits[len(digits)-4:]
}

// SanitizeErrorText removes secrets (passwords, tokens) and masks phone / ID numbers
// so the text is safe to store in and show from the Error Log.
func SanitizeErrorText(s string) string {
	if s == "" {
		return s
	}
	s = bearerRe.ReplaceAllString(s, "Bearer [REDACTED]")
	s = secretPairRe.ReplaceAllString(s, "${1}[REDACTED]")
	s = longNumberRe.ReplaceAllStringFunc(s, MaskPhone)
	return s
}
