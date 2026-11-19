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
//	                                 (polled every NAFATH_POLL_SECONDS, default 10 s)
//	                                 -> Nafath's checkStatus decides:
//	                                    WAITING   -> waiting, same number
//	                                    COMPLETED -> the normal Automax tokens
//	                                    REJECTED  -> 403 nafath_rejected
//	                                    EXPIRED   -> 410 nafath_timeout (start again)
//	                                 Automax sends one request per login and adds no
//	                                 timer of its own.
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
	nafathChallengePrefix    = "nafath_challenge:"
	nafathLockSuffix         = ":lock"
	nafathDefaultPollSeconds = 10
	// A login's Redis state is dropped this long after its last check (a
	// closed browser tab), not as a time limit on the login itself.
	nafathStateIdleTTL = 10 * time.Minute
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

// nafathChallengeState is one login in progress: one Nafath request.
type nafathChallengeState struct {
	NationalID    string                 `json:"national_id"`
	UserID        string                 `json:"user_id"`
	Token         string                 `json:"token"`
	Current       services.NafathRequest `json:"current"`
	LastCheckedAt int64                  `json:"last_checked_at"`
}

// NafathPendingResponse is the start / waiting status response.
type NafathPendingResponse struct {
	Status       string `json:"status"`
	ChallengeID  string `json:"challenge_id"`
	Random       string `json:"random"`
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
		LastCheckedAt: now.UnixMilli(), // just sent: nothing to check yet
	}
	if err := h.saveState(ctx, challengeID, &state); err != nil {
		return utils.InternalErrorResponse(c, fmt.Errorf("store Nafath challenge: %w", err), i18n.T(ctx, "internal_server_error"))
	}
	log.Printf("[NAFATH] request sent id=%s user=%s challenge=%s…", maskNationalID(nationalID), user.ID, challengeID[:8])
	return c.JSON(fiber.Map{"success": true, "data": h.pending(challengeID, &state)})
}

// Status is step 2, polled by the login page: it runs NafathCheckRequest and
// does what Nafath's checkStatus says (see the top of this file).
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
	// Polled faster than the poll interval: answer from the stored state.
	if now.UnixMilli()-state.LastCheckedAt < int64(h.pollSeconds()-1)*1000 {
		return c.JSON(fiber.Map{"success": true, "data": h.pending(challengeID, &state)})
	}
	state.LastCheckedAt = now.UnixMilli()

	res, err := h.check(ctx, &state)
	if err != nil {
		return h.gatewayError(c, state.NationalID, "check", err)
	}
	switch res.Status {
	case services.NafathStatusCompleted:
		return h.finish(c, key, &state, res)
	case services.NafathStatusRejected:
		h.redis.Del(ctx, key)
		log.Printf("[NAFATH] request rejected in the Nafath app id=%s msg=%q", maskNationalID(state.NationalID), res.Message)
		return nafathError(c, fiber.StatusForbidden, nafathErrRejected, gatewayMessageOr(res.Message, i18n.T(ctx, "nafath_rejected")))
	case services.NafathStatusExpired:
		h.redis.Del(ctx, key)
		log.Printf("[NAFATH] request expired (Nafath) id=%s msg=%q", maskNationalID(state.NationalID), res.Message)
		return nafathError(c, fiber.StatusGone, nafathErrTimeout, gatewayMessageOr(res.Message, i18n.T(ctx, "nafath_timeout")))
	}
	if res.RawStatus != services.NafathStatusWaiting {
		log.Printf("[NAFATH] undocumented checkStatus %q treated as waiting id=%s", res.RawStatus, maskNationalID(state.NationalID))
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

// check runs NafathCheckRequest for the login's request, getting a new
// gateway token once if the old one was refused.
func (h *NafathLoginHandler) check(ctx context.Context, state *nafathChallengeState) (*services.NafathCheckResult, error) {
	res, err := h.client.CheckRequest(ctx, state.Token, state.NationalID, state.Current)
	if errors.Is(err, services.ErrNafathTokenRejected) {
		if err = h.refreshToken(ctx, state); err == nil {
			res, err = h.client.CheckRequest(ctx, state.Token, state.NationalID, state.Current)
		}
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
		return nafathError(c, fiber.StatusForbidden, nafathErrRefused, gatewayMessageOr(refusal.Message, i18n.T(ctx, "nafath_unavailable")))
	}
	// No message from the gateway (no answer, timeout, unreadable reply):
	// the only case where Automax's own text is shown.
	log.Printf("[NAFATH] %s unavailable id=%s: %v", step, maskNationalID(nationalID), err)
	return nafathError(c, fiber.StatusServiceUnavailable, nafathErrUnavailable, i18n.T(ctx, "nafath_unavailable"))
}

// gatewayMessageOr shows the gateway's own message when it sent one, else
// the fallback.
func gatewayMessageOr(msg, fallback string) string {
	if m := cleanGatewayMessage(msg); m != "" {
		return m
	}
	return fallback
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
	if h.cfg.Nafath.PollSeconds > 0 {
		return h.cfg.Nafath.PollSeconds
	}
	return nafathDefaultPollSeconds
}

func (h *NafathLoginHandler) pending(challengeID string, state *nafathChallengeState) NafathPendingResponse {
	return NafathPendingResponse{
		Status:       "waiting",
		ChallengeID:  challengeID,
		Random:       state.Current.Random,
		PollInterval: h.pollSeconds(),
	}
}

func (h *NafathLoginHandler) saveState(ctx context.Context, challengeID string, state *nafathChallengeState) error {
	return h.sessionStore.Set(ctx, nafathChallengePrefix+challengeID, state, nafathStateIdleTTL)
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
