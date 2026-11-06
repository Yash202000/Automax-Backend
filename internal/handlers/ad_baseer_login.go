package handlers

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net/mail"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

// AD login through the client's AD-Baseer APIs (enabled by AD_BASEER_ENABLED):
//
//	POST /api/v1/ldap/login             username+password -> registered-user check
//	                                    -> GetToken + ValidateCredentialsOTP
//	                                    -> {otp_required, challenge_id}
//	POST /api/v1/ldap/login/verify-otp  challenge_id+otp -> IsValidOTP -> verified email
//	                                    -> find the admin-registered AD user -> normal Automax tokens
//
// The AD-Baseer access token and requestID stay server-side in Redis under the
// random challenge_id. Login never creates a local user: AD users are
// registered by an admin. An unregistered username is refused with 403
// ad_user_not_registered before AD-Baseer is called, so no OTP is sent; the
// verified AD email is checked again after the OTP. Passwords, OTPs and tokens are never logged.

// Error codes returned in error_code so the client can react without parsing
// translated messages.
const (
	adErrRateLimited   = "rate_limited"
	adErrInvalidOTP    = "ad_otp_invalid"
	adErrRestartLogin  = "ad_otp_restart"
	adErrNotRegistered = "ad_user_not_registered"
)

const (
	adChallengeKeyPrefix  = "ad_baseer_challenge:"
	adChallengeAttemptsFx = ":attempts"
	// Per-IP limits are looser than per-username so one office behind NAT
	// isn't locked out by a few colleagues' typos.
	adIPAttemptMultiplier = 4
)

var adChallengeIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)

// adOTPPattern accepts 4-8 digits: the contract does not state the OTP length.
var adOTPPattern = regexp.MustCompile(`^[0-9]{4,8}$`)

type adBaseerChallengeState struct {
	Username    string `json:"username"`
	AccessToken string `json:"access_token"`
	RequestID   string `json:"request_id"`
}

// ADOTPChallengeResponse is the first-step response when AD-Baseer is enabled.
type ADOTPChallengeResponse struct {
	OTPRequired bool   `json:"otp_required"`
	ChallengeID string `json:"challenge_id"`
	ExpiresIn   int    `json:"expires_in"`
	Source      string `json:"source"`
}

// ADVerifyOTPRequest is the second-step request body.
type ADVerifyOTPRequest struct {
	ChallengeID string `json:"challenge_id" validate:"required"`
	OTP         string `json:"otp" validate:"required"`
}

func adErrorResponse(c *fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(utils.ErrorCodeResponse{Success: false, ErrorCode: code, Error: message})
}

// startADBaseerLogin is step 1: validate credentials and trigger the OTP.
func (h *LDAPHandler) startADBaseerLogin(c *fiber.Ctx, req LDAPLoginRequest) error {
	ctx := c.UserContext()
	username := strings.TrimSpace(req.Username)
	userKey, ipKey := adUserLimitKey(username), adIPLimitKey(c.IP())

	if h.adLimitBlocked(ctx, userKey, ipKey) {
		log.Printf("[AD-BASEER] login rate-limited user=%q ip=%s", username, c.IP())
		return adErrorResponse(c, fiber.StatusTooManyRequests, adErrRateLimited, i18n.T(ctx, "too_many_login_attempts"))
	}

	// Only admin-registered AD users get an OTP. Counted as a failed attempt
	// so the check can't be used to probe usernames quickly.
	registered, err := h.findRegisteredADLogin(ctx, username)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		h.adLimitFail(ctx, userKey, ipKey)
		log.Printf("[AD-BASEER] login refused before OTP: user=%q not registered in Automax", username)
		return adErrorResponse(c, fiber.StatusForbidden, adErrNotRegistered, i18n.T(ctx, "ad_login_not_registered"))
	}
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(ctx, "failed_to_look_up_user"))
	}
	if !registered.IsActive {
		log.Printf("[AD-BASEER] login refused before OTP: user=%q id=%s deactivated", username, registered.ID)
		return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(ctx, "account_deactivated"))
	}

	challenge, err := h.adBaseer.StartLogin(ctx, username, req.Password)
	if err != nil {
		if errors.Is(err, services.ErrADBaseerInvalidCredentials) {
			h.adLimitFail(ctx, userKey, ipKey)
			log.Printf("[AD-BASEER] credential check failed user=%q ip=%s: %v", username, c.IP(), err)
			return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(ctx, "ad_invalid_credentials"))
		}
		log.Printf("[AD-BASEER] credential step unavailable user=%q: %v", username, err)
		return utils.ErrorResponse(c, fiber.StatusServiceUnavailable, i18n.T(ctx, "ad_baseer_unavailable"))
	}

	challengeID, err := randomHex(16)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(ctx, "internal_server_error"))
	}
	ttl := time.Duration(h.config.ADBaseer.OTPTTLSeconds) * time.Second
	state := adBaseerChallengeState{Username: username, AccessToken: challenge.AccessToken, RequestID: challenge.RequestID}
	if err := h.sessionStore.Set(ctx, adChallengeKeyPrefix+challengeID, state, ttl); err != nil {
		return utils.InternalErrorResponse(c, fmt.Errorf("store AD-Baseer challenge: %w", err), i18n.T(ctx, "internal_server_error"))
	}

	log.Printf("[AD-BASEER] OTP sent user=%q challenge=%s…", username, challengeID[:8])
	return c.JSON(fiber.Map{
		"success": true,
		"data": ADOTPChallengeResponse{
			OTPRequired: true,
			ChallengeID: challengeID,
			ExpiresIn:   h.config.ADBaseer.OTPTTLSeconds,
			Source:      "ad_baseer",
		},
	})
}

// VerifyADOTP is step 2: verify the OTP, then sign the AD user in.
// POST /api/v1/ldap/login/verify-otp
func (h *LDAPHandler) VerifyADOTP(c *fiber.Ctx) error {
	ctx := c.UserContext()
	if !h.config.ADBaseer.Enabled || h.adBaseer == nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(ctx, "ldap_auth_not_enabled"))
	}

	var req ADVerifyOTPRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(ctx, "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(ctx, &req); len(validationErrors) != 0 {
		errMsg := ""
		for field, err := range validationErrors {
			errMsg += field + ": " + err + "; "
		}
		return utils.ErrorResponse(c, fiber.StatusBadRequest, errMsg)
	}
	req.ChallengeID = strings.TrimSpace(req.ChallengeID)
	req.OTP = strings.TrimSpace(req.OTP)
	if !adChallengeIDPattern.MatchString(req.ChallengeID) {
		return adErrorResponse(c, fiber.StatusUnauthorized, adErrRestartLogin, i18n.T(ctx, "ad_otp_session_expired"))
	}
	if !adOTPPattern.MatchString(req.OTP) {
		return adErrorResponse(c, fiber.StatusBadRequest, adErrInvalidOTP, i18n.T(ctx, "invalid_or_expired_otp"))
	}

	ipKey := adIPLimitKey(c.IP())
	if h.adLimitBlocked(ctx, ipKey) {
		log.Printf("[AD-BASEER] OTP rate-limited ip=%s", c.IP())
		return adErrorResponse(c, fiber.StatusTooManyRequests, adErrRateLimited, i18n.T(ctx, "too_many_login_attempts"))
	}

	challengeKey := adChallengeKeyPrefix + req.ChallengeID
	var state adBaseerChallengeState
	if err := h.sessionStore.Get(ctx, challengeKey, &state); err != nil {
		return adErrorResponse(c, fiber.StatusUnauthorized, adErrRestartLogin, i18n.T(ctx, "ad_otp_session_expired"))
	}

	// Count the attempt before calling AD-Baseer so parallel guesses can't
	// exceed the limit.
	maxAttempts := h.config.ADBaseer.OTPMaxAttempts
	attempts, err := h.incrChallengeAttempts(ctx, challengeKey)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(ctx, "internal_server_error"))
	}
	if attempts > maxAttempts {
		h.deleteChallenge(ctx, challengeKey)
		return adErrorResponse(c, fiber.StatusUnauthorized, adErrRestartLogin, i18n.T(ctx, "ad_otp_attempts_exceeded"))
	}

	employee, err := h.adBaseer.VerifyOTP(ctx, &services.ADBaseerChallenge{AccessToken: state.AccessToken, RequestID: state.RequestID}, req.OTP)
	if err != nil {
		switch {
		case errors.Is(err, services.ErrADBaseerInvalidOTP):
			h.adLimitFail(ctx, ipKey)
			log.Printf("[AD-BASEER] OTP rejected user=%q attempt %d/%d: %v", state.Username, attempts, maxAttempts, err)
			if attempts >= maxAttempts {
				h.deleteChallenge(ctx, challengeKey)
				return adErrorResponse(c, fiber.StatusUnauthorized, adErrRestartLogin, i18n.T(ctx, "ad_otp_attempts_exceeded"))
			}
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"success":            false,
				"error_code":         adErrInvalidOTP,
				"error":              i18n.T(ctx, "invalid_or_expired_otp"),
				"attempts_remaining": maxAttempts - attempts,
			})
		case errors.Is(err, services.ErrADBaseerTokenRejected):
			h.deleteChallenge(ctx, challengeKey)
			log.Printf("[AD-BASEER] OTP step token rejected user=%q: %v", state.Username, err)
			return adErrorResponse(c, fiber.StatusUnauthorized, adErrRestartLogin, i18n.T(ctx, "ad_otp_session_expired"))
		default:
			log.Printf("[AD-BASEER] OTP step unavailable user=%q: %v", state.Username, err)
			return utils.ErrorResponse(c, fiber.StatusServiceUnavailable, i18n.T(ctx, "ad_baseer_unavailable"))
		}
	}

	// Single use: only the request that deletes the challenge may continue.
	if !h.deleteChallenge(ctx, challengeKey) {
		return adErrorResponse(c, fiber.StatusUnauthorized, adErrRestartLogin, i18n.T(ctx, "ad_otp_session_expired"))
	}

	email, ok := normalizeADEmail(string(employee.Email))
	if !ok {
		log.Printf("[AD-BASEER] verified user=%q has no usable email (%q) — login refused", state.Username, employee.Email)
		return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(ctx, "ad_account_no_email"))
	}
	if !h.adStatusAllowed(string(employee.Status)) {
		log.Printf("[AD-BASEER] verified user=%q status %q (%s) not in AD_BASEER_ACTIVE_STATUSES — login refused", state.Username, employee.Status, employee.StatusDesc)
		return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(ctx, "account_disabled_ad"))
	}

	user, status, msgKey, err := h.resolveADBaseerUser(ctx, email, employee)
	if err != nil {
		if status >= 500 {
			return utils.InternalErrorResponse(c, err, i18n.T(ctx, msgKey))
		}
		log.Printf("[AD-BASEER] user resolution refused user=%q email=%q: %v", state.Username, email, err)
		if msgKey == adErrNotRegistered {
			return adErrorResponse(c, status, adErrNotRegistered, i18n.T(ctx, msgKey))
		}
		return utils.ErrorResponse(c, status, i18n.T(ctx, msgKey))
	}
	if !user.IsActive {
		return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(ctx, "account_deactivated"))
	}

	h.adLimitReset(ctx, adUserLimitKey(state.Username))
	log.Printf("[AD-BASEER] login success user=%q email=%q id=%s", state.Username, email, user.ID)
	return h.issueADLoginTokens(c, user)
}

// resolveADBaseerUser finds the local user for a verified AD-Baseer email.
// Users are never created here: AD users must be registered beforehand by an
// admin from the admin area. An unregistered person (whose AD credentials and
// OTP are already verified) is told so explicitly. It never converts a local (non-AD) account. On
// error it returns the HTTP status and i18n message key to answer with.
func (h *LDAPHandler) resolveADBaseerUser(ctx context.Context, email string, employee *services.ADBaseerEmployee) (*models.User, int, string, error) {
	user, err := h.findADUserByEmail(ctx, email, string(employee.Email))
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fiber.StatusForbidden, adErrNotRegistered,
			fmt.Errorf("no registered Automax user for this email; not creating one")
	}
	if err != nil {
		return nil, fiber.StatusInternalServerError, "failed_to_look_up_user", err
	}
	if !user.IsADUser {
		return nil, fiber.StatusForbidden, "ad_account_conflict",
			fmt.Errorf("email belongs to local (non-AD) account %s; not converting it", user.ID)
	}
	return user, 0, "", nil
}

// findRegisteredADLogin finds the admin-registered AD user for the name typed
// on the login form: by username, or by email when an email was typed. A
// match that is not an AD user counts as not registered.
func (h *LDAPHandler) findRegisteredADLogin(ctx context.Context, login string) (*models.User, error) {
	var (
		user *models.User
		err  error
	)
	if strings.Contains(login, "@") {
		user, err = h.findADUserByEmail(ctx, strings.ToLower(login), login)
	} else {
		user, err = h.userRepo.FindByUsername(ctx, strings.ToLower(login))
		if errors.Is(err, gorm.ErrRecordNotFound) && strings.ToLower(login) != login {
			user, err = h.userRepo.FindByUsername(ctx, login)
		}
	}
	if err != nil {
		return nil, err
	}
	if !user.IsADUser {
		return nil, gorm.ErrRecordNotFound
	}
	return user, nil
}

// findADUserByEmail looks up by the normalised (lowercase) email, then by the
// email exactly as AD-Baseer returned it — older AD users may have been stored
// with their original casing.
func (h *LDAPHandler) findADUserByEmail(ctx context.Context, normalized, original string) (*models.User, error) {
	user, err := h.userRepo.FindByEmailForLogin(ctx, normalized)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if trimmed := strings.TrimSpace(original); trimmed != "" && trimmed != normalized {
			return h.userRepo.FindByEmailForLogin(ctx, trimmed)
		}
	}
	return user, err
}

func (h *LDAPHandler) adStatusAllowed(status string) bool {
	allowed := h.config.ADBaseer.ActiveStatuses
	return slices.Contains(allowed, "*") || (status != "" && slices.Contains(allowed, strings.TrimSpace(status)))
}

// normalizeADEmail lowercases and validates an email returned by AD-Baseer.
// Display-name forms ("Name <a@b>") are rejected.
func normalizeADEmail(raw string) (string, bool) {
	email := strings.ToLower(strings.TrimSpace(raw))
	if email == "" || len(email) > 254 {
		return "", false
	}
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || addr.Name != "" {
		return "", false
	}
	return email, true
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// ─── Challenge store helpers ──────────────────────────────────────────────────

func (h *LDAPHandler) incrChallengeAttempts(ctx context.Context, challengeKey string) (int, error) {
	if h.redis == nil {
		return 0, errors.New("redis not configured")
	}
	key := challengeKey + adChallengeAttemptsFx
	n, err := h.redis.Incr(ctx, key).Result()
	if err != nil {
		return 0, err
	}
	if n == 1 {
		h.redis.Expire(ctx, key, time.Duration(h.config.ADBaseer.OTPTTLSeconds)*time.Second)
	}
	return int(n), nil
}

// deleteChallenge removes the challenge and reports whether this call was the
// one that removed it.
func (h *LDAPHandler) deleteChallenge(ctx context.Context, challengeKey string) bool {
	if h.redis == nil {
		return false
	}
	n, err := h.redis.Del(ctx, challengeKey).Result()
	h.redis.Del(ctx, challengeKey+adChallengeAttemptsFx)
	return err == nil && n == 1
}

// ─── Rate limiting ────────────────────────────────────────────────────────────
// Failed attempts are counted per username and per IP in Redis using the
// LOGIN_RATE_LIMIT_* settings (MAX_LOGIN_ATTEMPTS per RATE_LIMIT_WINDOW
// minutes, then blocked for BLOCK_DURATION minutes). The IP limit is
// adIPAttemptMultiplier times the username limit. Redis errors fail open,
// like the regular login rate limiter.

func adUserLimitKey(username string) string {
	return "user:" + strings.ToLower(strings.TrimSpace(username))
}
func adIPLimitKey(ip string) string { return "ip:" + ip }

func (h *LDAPHandler) adLimitMax(key string) int {
	if strings.HasPrefix(key, "ip:") {
		return h.config.LoginRateLimit.MaxAttempts * adIPAttemptMultiplier
	}
	return h.config.LoginRateLimit.MaxAttempts
}

func (h *LDAPHandler) adLimitBlocked(ctx context.Context, keys ...string) bool {
	if h.redis == nil || !h.config.LoginRateLimit.Enabled {
		return false
	}
	for _, k := range keys {
		if n, _ := h.redis.Exists(ctx, "ad_login_block:"+k).Result(); n > 0 {
			return true
		}
	}
	return false
}

func (h *LDAPHandler) adLimitFail(ctx context.Context, keys ...string) {
	if h.redis == nil || !h.config.LoginRateLimit.Enabled {
		return
	}
	window := time.Duration(h.config.LoginRateLimit.RateLimitWindow) * time.Minute
	block := time.Duration(h.config.LoginRateLimit.BlockDuration) * time.Minute
	for _, k := range keys {
		attemptsKey := "ad_login_attempts:" + k
		n, err := h.redis.Incr(ctx, attemptsKey).Result()
		if err != nil {
			continue
		}
		if n == 1 {
			h.redis.Expire(ctx, attemptsKey, window)
		}
		if int(n) >= h.adLimitMax(k) {
			h.redis.Set(ctx, "ad_login_block:"+k, "1", block)
			h.redis.Del(ctx, attemptsKey)
			log.Printf("[AD-BASEER] blocking %s for %s after %d failed attempts", k, block, n)
		}
	}
}

func (h *LDAPHandler) adLimitReset(ctx context.Context, keys ...string) {
	if h.redis == nil {
		return
	}
	for _, k := range keys {
		h.redis.Del(ctx, "ad_login_attempts:"+k, "ad_login_block:"+k)
	}
}
