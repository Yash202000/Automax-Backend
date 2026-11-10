package handlers

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/database"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// National ID login through Nafath (enabled by NAFATH_ENABLED):
//
//	POST /api/v1/auth/nafath/start   national_id -> registered-user check
//	                                 -> GetAccessTokenByIdNumber + NafathSendRequest
//	                                 -> {challenge_id, random}
//	POST /api/v1/auth/nafath/status  challenge_id -> NafathCheckRequest
//	                                 -> waiting (a new random every NAFATH_ROTATE_SECONDS)
//	                                 -> approved: the normal Automax tokens
//	POST /api/v1/auth/nafath/cancel  challenge_id -> forget the login
//
// The gateway token and transIDs stay server-side in Redis under the random
// challenge_id; the browser only ever sees the number to pick. Automax signs
// the user in only after Nafath reports the request COMPLETED for the same
// National ID. Login never creates users: the National ID must belong to an
// active Automax user, checked before any Nafath request is sent.

// Error codes returned in error_code so the client can react without parsing
// translated messages.
const (
	nafathErrDisabled      = "nafath_disabled"
	nafathErrRateLimited   = "rate_limited"
	nafathErrInvalidID     = "nafath_invalid_id"
	nafathErrNotRegistered = "nafath_user_not_registered"
	nafathErrRestart       = "nafath_restart"
	nafathErrTimeout       = "nafath_timeout"
	nafathErrRejected      = "nafath_rejected"
	nafathErrRefused       = "nafath_refused"
	nafathErrIdentity      = "nafath_identity_mismatch"
	nafathErrUnavailable   = "nafath_unavailable"
)

const (
	nafathChallengePrefix = "nafath_challenge:"
	nafathLockSuffix      = ":lock"
	// Per-IP limits are looser than per-ID so one office behind NAT isn't
	// locked out by a few colleagues.
	nafathIPAttemptMultiplier = 4
	nafathDefaultPollSeconds  = 5
)

var (
	nafathIDPattern          = regexp.MustCompile(`^[12][0-9]{9}$`)
	nafathChallengeIDPattern = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

// NafathLoginHandler serves the Nafath login endpoints.
type NafathLoginHandler struct {
	cfg          *config.Config
	client       services.NafathClient // nil unless NAFATH_ENABLED
	userRepo     repository.UserRepository
	jwtManager   *utils.JWTManager
	sessionStore *database.SessionStore
	redis        *redis.Client
	now          func() time.Time
}

func NewNafathLoginHandler(cfg *config.Config, client services.NafathClient, userRepo repository.UserRepository,
	jwtManager *utils.JWTManager, sessionStore *database.SessionStore, redisClient *redis.Client) *NafathLoginHandler {
	return &NafathLoginHandler{cfg: cfg, client: client, userRepo: userRepo, jwtManager: jwtManager,
		sessionStore: sessionStore, redis: redisClient, now: time.Now}
}

// Enabled reports whether Nafath login is configured.
func (h *NafathLoginHandler) Enabled() bool {
	return h != nil && h.cfg.Nafath.Enabled && h.client != nil && h.redis != nil
}

// nafathChallengeState is one login in progress. Previous is the request
// replaced by the last rotation: the person may approve it just before the
// number changes, so it is still checked once more.
type nafathChallengeState struct {
	NationalID    string                  `json:"national_id"`
	UserID        string                  `json:"user_id"`
	Token         string                  `json:"token"`
	Current       services.NafathRequest  `json:"current"`
	CurrentSentAt int64                   `json:"current_sent_at"`
	Previous      *services.NafathRequest `json:"previous,omitempty"`
	LastCheckedAt int64                   `json:"last_checked_at"`
	Deadline      int64                   `json:"deadline"`
}

// NafathPendingResponse is the start / waiting status response.
type NafathPendingResponse struct {
	Status       string `json:"status"`
	ChallengeID  string `json:"challenge_id"`
	Random       string `json:"random"`
	RotateIn     int    `json:"rotate_in"`
	ExpiresIn    int    `json:"expires_in"`
	PollInterval int    `json:"poll_interval"`
}

type nafathStartRequest struct {
	NationalID string `json:"national_id"`
}

type nafathChallengeRequest struct {
	ChallengeID string `json:"challenge_id"`
}

func nafathError(c *fiber.Ctx, status int, code, message string) error {
	return c.Status(status).JSON(utils.ErrorCodeResponse{Success: false, ErrorCode: code, Error: message})
}

// maskNationalID keeps the last 4 digits for logs.
func maskNationalID(id string) string {
	if len(id) <= 4 {
		return "****"
	}
	return strings.Repeat("*", len(id)-4) + id[len(id)-4:]
}

// Start is step 1: check the user, then send the first Nafath request.
// POST /api/v1/auth/nafath/start
func (h *NafathLoginHandler) Start(c *fiber.Ctx) error {
	ctx := c.UserContext()
	if !h.Enabled() {
		return nafathError(c, fiber.StatusNotFound, nafathErrDisabled, i18n.T(ctx, "nafath_not_enabled"))
	}
	var req nafathStartRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(ctx, "invalid_request_body"))
	}
	nationalID := strings.TrimSpace(req.NationalID)
	if !nafathIDPattern.MatchString(nationalID) {
		return nafathError(c, fiber.StatusBadRequest, nafathErrInvalidID, i18n.T(ctx, "nafath_invalid_id"))
	}
	idKey, ipKey := "id:"+nationalID, "ip:"+c.IP()
	if h.limitBlocked(ctx, idKey, ipKey) {
		log.Printf("[NAFATH] start rate-limited id=%s ip=%s", maskNationalID(nationalID), c.IP())
		return nafathError(c, fiber.StatusTooManyRequests, nafathErrRateLimited, i18n.T(ctx, "too_many_login_attempts"))
	}
	// Every start counts: each one makes Nafath notify the person's phone,
	// so repeated starts must not be usable to flood someone with requests.
	// A successful login resets the counter.
	h.limitCount(ctx, idKey, ipKey)

	user, err := h.userRepo.FindByNationalIDForLogin(ctx, nationalID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		log.Printf("[NAFATH] start refused: id=%s not registered in Automax", maskNationalID(nationalID))
		return nafathError(c, fiber.StatusForbidden, nafathErrNotRegistered, i18n.T(ctx, "nafath_not_registered"))
	}
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(ctx, "failed_to_look_up_user"))
	}
	if !user.IsActive {
		log.Printf("[NAFATH] start refused: id=%s user=%s deactivated", maskNationalID(nationalID), user.ID)
		return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(ctx, "account_deactivated"))
	}

	token, err := h.client.GetToken(ctx, nationalID)
	if err != nil {
		return h.gatewayError(c, nationalID, "token", err)
	}
	nreq, err := h.client.SendRequest(ctx, token, nationalID)
	if err != nil {
		return h.gatewayError(c, nationalID, "send", err)
	}

	challengeID, err := randomHex(16)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(ctx, "internal_server_error"))
	}
	now := h.now()
	state := nafathChallengeState{
		NationalID:    nationalID,
		UserID:        user.ID.String(),
		Token:         token,
		Current:       *nreq,
		CurrentSentAt: now.UnixMilli(),
		LastCheckedAt: now.UnixMilli(), // just sent: nothing to check yet
		Deadline:      now.Add(time.Duration(h.cfg.Nafath.LoginTimeoutSeconds) * time.Second).UnixMilli(),
	}
	if err := h.saveState(ctx, challengeID, &state); err != nil {
		return utils.InternalErrorResponse(c, fmt.Errorf("store Nafath challenge: %w", err), i18n.T(ctx, "internal_server_error"))
	}
	log.Printf("[NAFATH] request sent id=%s user=%s challenge=%s…", maskNationalID(nationalID), user.ID, challengeID[:8])
	return c.JSON(fiber.Map{"success": true, "data": h.pending(challengeID, &state)})
}

// Status is step 2, polled by the login page: it checks the open requests,
// sends a new one when the number's time is up, and signs the user in once
// Nafath reports the request approved.
// POST /api/v1/auth/nafath/status
func (h *NafathLoginHandler) Status(c *fiber.Ctx) error {
	ctx := c.UserContext()
	if !h.Enabled() {
		return nafathError(c, fiber.StatusNotFound, nafathErrDisabled, i18n.T(ctx, "nafath_not_enabled"))
	}
	challengeID, ok := h.challengeIDFrom(c)
	if !ok {
		return nafathError(c, fiber.StatusUnauthorized, nafathErrRestart, i18n.T(ctx, "nafath_session_expired"))
	}
	key := nafathChallengePrefix + challengeID

	// One poll at a time per login, so two tabs or a slow gateway can't
	// rotate the number twice or sign in twice.
	locked, err := h.redis.SetNX(ctx, key+nafathLockSuffix, "1", time.Duration(3*h.cfg.Nafath.TimeoutSeconds+5)*time.Second).Result()
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(ctx, "internal_server_error"))
	}
	var state nafathChallengeState
	if err := h.sessionStore.Get(ctx, key, &state); err != nil {
		if locked {
			h.redis.Del(ctx, key+nafathLockSuffix)
		}
		return nafathError(c, fiber.StatusUnauthorized, nafathErrRestart, i18n.T(ctx, "nafath_session_expired"))
	}
	if !locked {
		return c.JSON(fiber.Map{"success": true, "data": h.pending(challengeID, &state)})
	}
	defer h.redis.Del(context.Background(), key+nafathLockSuffix)

	now := h.now()
	if now.UnixMilli() >= state.Deadline {
		h.redis.Del(ctx, key)
		log.Printf("[NAFATH] login timed out id=%s", maskNationalID(state.NationalID))
		return nafathError(c, fiber.StatusGone, nafathErrTimeout, i18n.T(ctx, "nafath_timeout"))
	}
	// Polled faster than the poll interval: answer from the stored state.
	if now.UnixMilli()-state.LastCheckedAt < int64(h.pollSeconds()-1)*1000 {
		return c.JSON(fiber.Map{"success": true, "data": h.pending(challengeID, &state)})
	}
	state.LastCheckedAt = now.UnixMilli()

	// Current request first, then the one the last rotation replaced.
	currentStatus, err := h.check(ctx, &state, state.Current)
	if err != nil {
		return h.gatewayError(c, state.NationalID, "check", err)
	}
	if currentStatus.Status == services.NafathStatusCompleted {
		return h.finish(c, key, &state, currentStatus)
	}
	if currentStatus.Status == services.NafathStatusRejected {
		h.redis.Del(ctx, key)
		log.Printf("[NAFATH] request rejected in the Nafath app id=%s", maskNationalID(state.NationalID))
		return nafathError(c, fiber.StatusForbidden, nafathErrRejected, i18n.T(ctx, "nafath_rejected"))
	}
	if state.Previous != nil {
		prev, err := h.check(ctx, &state, *state.Previous)
		if err != nil {
			return h.gatewayError(c, state.NationalID, "check", err)
		}
		switch prev.Status {
		case services.NafathStatusCompleted:
			return h.finish(c, key, &state, prev)
		case services.NafathStatusWaiting:
			// Still open: keep checking it next time.
		default:
			state.Previous = nil
		}
	}
	if currentStatus.RawStatus != services.NafathStatusWaiting && currentStatus.Status == services.NafathStatusWaiting {
		log.Printf("[NAFATH] undocumented checkStatus %q treated as waiting id=%s", currentStatus.RawStatus, maskNationalID(state.NationalID))
	}

	// Time for a new number (or the gateway already expired this one).
	rotateAfter := int64(h.cfg.Nafath.RotateSeconds) * 1000
	if currentStatus.Status == services.NafathStatusExpired || now.UnixMilli()-state.CurrentSentAt >= rotateAfter {
		nreq, err := h.client.SendRequest(ctx, state.Token, state.NationalID)
		if errors.Is(err, services.ErrNafathTokenRejected) {
			if err = h.refreshToken(ctx, &state); err == nil {
				nreq, err = h.client.SendRequest(ctx, state.Token, state.NationalID)
			}
		}
		if err != nil {
			return h.gatewayError(c, state.NationalID, "send", err)
		}
		if currentStatus.Status == services.NafathStatusWaiting {
			prev := state.Current
			state.Previous = &prev
		}
		state.Current = *nreq
		state.CurrentSentAt = now.UnixMilli()
	}

	if err := h.saveState(ctx, challengeID, &state); err != nil {
		return utils.InternalErrorResponse(c, fmt.Errorf("store Nafath challenge: %w", err), i18n.T(ctx, "internal_server_error"))
	}
	return c.JSON(fiber.Map{"success": true, "data": h.pending(challengeID, &state)})
}

// Cancel forgets a login in progress (the user pressed Cancel).
// POST /api/v1/auth/nafath/cancel
func (h *NafathLoginHandler) Cancel(c *fiber.Ctx) error {
	if h.Enabled() {
		if challengeID, ok := h.challengeIDFrom(c); ok {
			h.redis.Del(c.UserContext(), nafathChallengePrefix+challengeID)
		}
	}
	return c.JSON(fiber.Map{"success": true})
}

// check runs NafathCheckRequest for one request, getting a new gateway token
// once if the old one was refused. A request the gateway no longer knows
// counts as expired.
func (h *NafathLoginHandler) check(ctx context.Context, state *nafathChallengeState, req services.NafathRequest) (*services.NafathCheckResult, error) {
	res, err := h.client.CheckRequest(ctx, state.Token, state.NationalID, req)
	if errors.Is(err, services.ErrNafathTokenRejected) {
		if err = h.refreshToken(ctx, state); err == nil {
			res, err = h.client.CheckRequest(ctx, state.Token, state.NationalID, req)
		}
	}
	if errors.Is(err, services.ErrNafathUnknownRequest) {
		log.Printf("[NAFATH] gateway no longer knows request id=%s: %v", maskNationalID(state.NationalID), err)
		return &services.NafathCheckResult{Status: services.NafathStatusExpired, RawStatus: "UNKNOWN_REQUEST"}, nil
	}
	return res, err
}

func (h *NafathLoginHandler) refreshToken(ctx context.Context, state *nafathChallengeState) error {
	token, err := h.client.GetToken(ctx, state.NationalID)
	if err != nil {
		return err
	}
	state.Token = token
	return nil
}

// finish signs the user in after Nafath approved a request. The challenge is
// single use: only the call that deletes it continues.
func (h *NafathLoginHandler) finish(c *fiber.Ctx, key string, state *nafathChallengeState, res *services.NafathCheckResult) error {
	ctx := c.UserContext()
	if n, err := h.redis.Del(ctx, key).Result(); err != nil || n != 1 {
		return nafathError(c, fiber.StatusUnauthorized, nafathErrRestart, i18n.T(ctx, "nafath_session_expired"))
	}
	// Nafath must have confirmed the same person who started the login.
	if res.Person == nil || res.Person.IDNumber != state.NationalID {
		got := "none"
		if res.Person != nil {
			got = maskNationalID(res.Person.IDNumber)
		}
		log.Printf("[NAFATH] SECURITY: approval for id=%s returned identity %s — login refused", maskNationalID(state.NationalID), got)
		return nafathError(c, fiber.StatusForbidden, nafathErrIdentity, i18n.T(ctx, "nafath_identity_mismatch"))
	}

	// Re-read the user: they may have been deactivated or changed while
	// the person was approving.
	user, err := h.userRepo.FindByNationalIDForLogin(ctx, state.NationalID)
	if errors.Is(err, gorm.ErrRecordNotFound) || (err == nil && user.ID.String() != state.UserID) {
		return nafathError(c, fiber.StatusForbidden, nafathErrNotRegistered, i18n.T(ctx, "nafath_not_registered"))
	}
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(ctx, "failed_to_look_up_user"))
	}
	if !user.IsActive {
		return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(ctx, "account_deactivated"))
	}

	h.limitReset(ctx, "id:"+state.NationalID)
	log.Printf("[NAFATH] login success id=%s user=%s", maskNationalID(state.NationalID), user.ID)
	return h.issueTokens(c, user)
}

// issueTokens creates the Redis session (auth_source "nafath") and JWT pair,
// like the other login flows.
func (h *NafathLoginHandler) issueTokens(c *fiber.Ctx, user *models.User) error {
	ctx := c.UserContext()
	role := "user"
	if user.IsSuperAdmin {
		role = "admin"
	} else if len(user.Roles) > 0 {
		role = user.Roles[0].Code
	}
	sessionID, err := h.sessionStore.SetUserSessionMultiDevices(ctx, user.ID.String(), map[string]interface{}{
		"user_id":     user.ID,
		"email":       user.Email,
		"role":        role,
		"auth_source": "nafath",
	}, h.jwtManager.GetTokenExpiration())
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(ctx, "failed_to_store_session"))
	}
	tokenPair, err := h.jwtManager.GenerateTokenPair(user.ID, user.Email, role, sessionID, false)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(ctx, "failed_to_generate_tokens"))
	}
	go func() {
		_ = h.userRepo.UpdateLastLogin(context.Background(), user.ID)
	}()
	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"status":        "approved",
			"user":          models.ToUserLoginResponse(user),
			"token":         tokenPair.AccessToken,
			"refresh_token": tokenPair.RefreshToken,
			"expires_in":    tokenPair.ExpiresIn,
		},
	})
}

// gatewayError maps a gateway failure to the response.
func (h *NafathLoginHandler) gatewayError(c *fiber.Ctx, nationalID, step string, err error) error {
	ctx := c.UserContext()
	var refusal *services.NafathRefusal
	if errors.As(err, &refusal) {
		log.Printf("[NAFATH] %s refused id=%s: %v", step, maskNationalID(nationalID), err)
		return nafathError(c, fiber.StatusForbidden, nafathErrRefused, i18n.Tf(ctx, "nafath_refused", cleanGatewayMessage(refusal.Message)))
	}
	log.Printf("[NAFATH] %s unavailable id=%s: %v", step, maskNationalID(nationalID), err)
	return nafathError(c, fiber.StatusServiceUnavailable, nafathErrUnavailable, i18n.T(ctx, "nafath_unavailable"))
}

// cleanGatewayMessage makes a gateway message safe to show: one line, at
// most 300 characters.
func cleanGatewayMessage(msg string) string {
	msg = strings.Join(strings.Fields(msg), " ")
	if r := []rune(msg); len(r) > 300 {
		msg = string(r[:300]) + "…"
	}
	return msg
}

func (h *NafathLoginHandler) challengeIDFrom(c *fiber.Ctx) (string, bool) {
	var req nafathChallengeRequest
	if err := c.BodyParser(&req); err != nil {
		return "", false
	}
	id := strings.TrimSpace(req.ChallengeID)
	return id, nafathChallengeIDPattern.MatchString(id)
}

func (h *NafathLoginHandler) pollSeconds() int {
	return min(nafathDefaultPollSeconds, h.cfg.Nafath.RotateSeconds)
}

func (h *NafathLoginHandler) pending(challengeID string, state *nafathChallengeState) NafathPendingResponse {
	now := h.now().UnixMilli()
	rotateIn := (state.CurrentSentAt + int64(h.cfg.Nafath.RotateSeconds)*1000 - now + 999) / 1000
	expiresIn := (state.Deadline - now + 999) / 1000
	return NafathPendingResponse{
		Status:       "waiting",
		ChallengeID:  challengeID,
		Random:       state.Current.Random,
		RotateIn:     int(max(0, rotateIn)),
		ExpiresIn:    int(max(0, expiresIn)),
		PollInterval: h.pollSeconds(),
	}
}

func (h *NafathLoginHandler) saveState(ctx context.Context, challengeID string, state *nafathChallengeState) error {
	ttl := time.Duration(state.Deadline-h.now().UnixMilli())*time.Millisecond + time.Minute
	return h.sessionStore.Set(ctx, nafathChallengePrefix+challengeID, state, ttl)
}

// ─── Rate limiting ────────────────────────────────────────────────────────────
// Starts are counted per National ID and per IP in Redis using the
// LOGIN_RATE_LIMIT_* settings (MAX_LOGIN_ATTEMPTS per RATE_LIMIT_WINDOW
// minutes, then blocked for BLOCK_DURATION minutes). The IP limit is
// nafathIPAttemptMultiplier times the ID limit. Redis errors fail open, like
// the regular login rate limiter.

func (h *NafathLoginHandler) limitMax(key string) int {
	if strings.HasPrefix(key, "ip:") {
		return h.cfg.LoginRateLimit.MaxAttempts * nafathIPAttemptMultiplier
	}
	return h.cfg.LoginRateLimit.MaxAttempts
}

func (h *NafathLoginHandler) limitBlocked(ctx context.Context, keys ...string) bool {
	if !h.cfg.LoginRateLimit.Enabled {
		return false
	}
	for _, k := range keys {
		if n, _ := h.redis.Exists(ctx, "nafath_login_block:"+k).Result(); n > 0 {
			return true
		}
	}
	return false
}

func (h *NafathLoginHandler) limitCount(ctx context.Context, keys ...string) {
	if !h.cfg.LoginRateLimit.Enabled {
		return
	}
	window := time.Duration(h.cfg.LoginRateLimit.RateLimitWindow) * time.Minute
	block := time.Duration(h.cfg.LoginRateLimit.BlockDuration) * time.Minute
	for _, k := range keys {
		attemptsKey := "nafath_login_attempts:" + k
		n, err := h.redis.Incr(ctx, attemptsKey).Result()
		if err != nil {
			continue
		}
		if n == 1 {
			h.redis.Expire(ctx, attemptsKey, window)
		}
		if int(n) >= h.limitMax(k) {
			h.redis.Set(ctx, "nafath_login_block:"+k, "1", block)
			h.redis.Del(ctx, attemptsKey)
			log.Printf("[NAFATH] blocking %s for %s after %d login starts", maskLimitKey(k), block, n)
		}
	}
}

func (h *NafathLoginHandler) limitReset(ctx context.Context, keys ...string) {
	for _, k := range keys {
		h.redis.Del(ctx, "nafath_login_attempts:"+k, "nafath_login_block:"+k)
	}
}

func maskLimitKey(k string) string {
	if id, ok := strings.CutPrefix(k, "id:"); ok {
		return "id:" + maskNationalID(id)
	}
	return k
}

// BlockWhenEnabled refuses the old National-ID-only login
// (POST /auth/sso/login) while Nafath login is enabled: that endpoint signs
// in from the National ID alone, without the person's approval.
func (h *NafathLoginHandler) BlockWhenEnabled() fiber.Handler {
	return func(c *fiber.Ctx) error {
		if h.Enabled() {
			return nafathError(c, fiber.StatusForbidden, "use_nafath_login", i18n.T(c.UserContext(), "sso_login_use_nafath"))
		}
		return c.Next()
	}
}
