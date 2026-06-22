package handlers

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/automax/backend/internal/database"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/validation"
)

// SSOHandler handles SSO launch, callback, and SSO-specific auth requests
type SSOHandler struct {
	ssoJWT           *utils.SSOJWTManager
	jwtManager       *utils.JWTManager
	sessionStore     *database.SessionStore
	userRepo         repository.UserRepository
	appLinkRepo      repository.ApplicationLinkRepository
	userService      services.UserService
	otpService       *services.OTPService
	frontendURL      string // SSO_FRONTEND_URL — where /sso-complete lives (frontend origin)
	nafathAPIBaseURL string // NAFATH_API_BASE_URL — Nafath validation API
}

func NewSSOHandler(
	ssoJWT *utils.SSOJWTManager,
	jwtManager *utils.JWTManager,
	sessionStore *database.SessionStore,
	userRepo repository.UserRepository,
	appLinkRepo repository.ApplicationLinkRepository,
	userService services.UserService,
	otpService *services.OTPService,
	frontendURL string,
	nafathAPIBaseURL string,
) *SSOHandler {
	return &SSOHandler{
		ssoJWT:           ssoJWT,
		jwtManager:       jwtManager,
		sessionStore:     sessionStore,
		userRepo:         userRepo,
		appLinkRepo:      appLinkRepo,
		userService:      userService,
		otpService:       otpService,
		frontendURL:      frontendURL,
		nafathAPIBaseURL: nafathAPIBaseURL,
	}
}

// maskPhone masks the middle digits of a phone number for display.
func maskPhone(phone string) string {
	if len(phone) < 6 {
		return phone
	}
	return phone[:3] + "****" + phone[len(phone)-3:]
}

// launchRequest is the body expected by POST /api/v1/sso/launch
type launchRequest struct {
	AppLinkID string `json:"app_link_id"`
}

// Launch handles POST /api/v1/sso/launch (requires auth middleware)
func (h *SSOHandler) Launch(c *fiber.Ctx) error {
	// Extract authenticated user context set by auth middleware
	userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	if !ok {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "user_not_authenticated"))
	}
	email, _ := c.Locals(constants.ContextKeys.Email).(string)

	var req launchRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if req.AppLinkID == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "app_link_id_required"))
	}

	appLinkUUID, err := uuid.Parse(req.AppLinkID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_app_link_id"))
	}

	// Look up the application link
	appLink, err := h.appLinkRepo.FindByID(c.UserContext(), appLinkUUID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "application_link_not_found"))
	}

	if !appLink.SSOEnabled {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "sso_not_enabled"))
	}

	if appLink.SSOCallbackURL == "" {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "sso_no_callback_url"))
	}

	// Fetch user details for name and role
	user, err := h.userRepo.FindByIDWithRelations(c.UserContext(), userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_fetch_user"))
	}

	fullName := strings.TrimSpace(user.FirstName + " " + user.LastName)
	if fullName == "" {
		fullName = user.Username
	}

	role := ""
	if len(user.Roles) > 0 {
		role = user.Roles[0].Code
	}

	// Generate a unique token ID (jti) and store as nonce before signing the token
	jti := uuid.New()
	if err := h.sessionStore.SetSSONonce(c.UserContext(), jti.String(), 65*time.Second); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_store_sso_nonce"))
	}

	// Generate RS256 SSO token
	ssoToken, err := h.ssoJWT.GenerateSSOToken(
		userID.String(),
		email,
		fullName,
		appLink.Name,
		role,
		jti,
	)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_generate_sso"))
	}

	redirectURL := fmt.Sprintf("%s?token=%s", appLink.SSOCallbackURL, ssoToken)
	if appLink.SSORedirectPath != "" {
		redirectURL = fmt.Sprintf("%s&redirect_to=%s", redirectURL, url.QueryEscape(appLink.SSORedirectPath))
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": fiber.Map{
			"redirect_url": redirectURL,
			"token":        ssoToken,
		},
	})
}

// Callback handles GET /sso/callback (public — browser redirect landing point)
// Used when Automax is the SSO receiver (target).
func (h *SSOHandler) Callback(c *fiber.Ctx) error {
	tokenStr := c.Query("token")
	if tokenStr == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "missing_token"))
	}

	// Verify RS256 signature and expiry.
	// ValidateSSOTokenAuto resolves the signing key automatically:
	//   - iss is a URL  → fetches public key from {iss}/.well-known/jwks.json (cross-deployment)
	//   - iss is "automax" → uses the local key (same-deployment)
	claims, err := h.ssoJWT.ValidateSSOTokenAuto(c.UserContext(), tokenStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "sso_token_expired"))
	}

	// Claim the JTI nonce — prevents replay attacks.
	// ClaimSSONonce uses SET NX so it works regardless of whether the provider and
	// receiver share the same Redis instance (cross-deployment safe).
	claimed, err := h.sessionStore.ClaimSSONonce(c.UserContext(), claims.JTI, 65*time.Second)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_validate_sso"))
	}
	if !claimed {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "sso_token_used"))
	}

	// Look up or auto-create user
	user, err := h.userRepo.FindByEmail(c.UserContext(), claims.Email)
	if err != nil {
		// User not found — auto-create
		parts := strings.SplitN(claims.Name, " ", 2)
		firstName := parts[0]
		lastName := ""
		if len(parts) > 1 {
			lastName = parts[1]
		}

		// Parse the subject as UUID for the new user ID (preserve identity from provider)
		newID := uuid.New()
		if parsed, parseErr := uuid.Parse(claims.Subject); parseErr == nil {
			newID = parsed
		}

		// Generate a random unusable password since SSO users won't use password login
		randomPw, _ := uuid.NewRandom()
		hashedPw, _ := bcrypt.GenerateFromPassword([]byte(randomPw.String()), bcrypt.DefaultCost)

		user = &models.User{
			ID:        newID,
			Email:     claims.Email,
			Username:  claims.Email,
			Password:  string(hashedPw),
			FirstName: firstName,
			LastName:  lastName,
			IsActive:  true,
		}

		if createErr := h.userRepo.Create(c.UserContext(), user); createErr != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create_sso_user"))
		}
	}

	// Determine role for token generation
	role := ""
	if len(user.Roles) > 0 {
		role = user.Roles[0].Code
	}

	// Store local session
	sessionID, err := h.sessionStore.SetUserSessionMultiDevices(c.UserContext(), user.ID.String(), map[string]interface{}{
		"user_id": user.ID,
		"email":   user.Email,
		"role":    role,
	}, h.jwtManager.GetTokenExpiration())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_store_session"))
	}

	// Generate local Automax token pair
	tokenPair, err := h.jwtManager.GenerateTokenPair(user.ID, user.Email, role, sessionID, false)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_generate_session"))
	}

	// Redirect to frontend /sso-complete page.
	// Use SSO_FRONTEND_URL when set (required when backend and frontend run on different
	// ports, e.g. backend :8081, frontend :3000).
	// Falls back to the request's own origin — only correct when backend and frontend
	// are served from the same host (e.g. behind a reverse proxy).
	base := strings.TrimRight(h.frontendURL, "/")
	if base == "" {
		scheme := "http"
		if c.Protocol() == "https" {
			scheme = "https"
		}
		base = fmt.Sprintf("%s://%s", scheme, c.Hostname())
	}
	redirectURL := fmt.Sprintf("%s/sso-complete?token=%s&refresh=%s",
		base, tokenPair.AccessToken, tokenPair.RefreshToken)
	if redirectTo := c.Query("redirect_to"); redirectTo != "" {
		redirectURL = fmt.Sprintf("%s&redirect_to=%s", redirectURL, url.QueryEscape(redirectTo))
	}

	return c.Redirect(redirectURL, fiber.StatusFound)
}

// SSORegister handles POST /auth/sso/register (public)
func (h *SSOHandler) SSORegister(c *fiber.Ctx) error {
	var req models.SSORegisterRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	_, err := h.userService.SSORegister(c.UserContext(), &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	sessionID, err := h.otpService.SendOTP(c.UserContext(), req.Phone, "sms", nil)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to send OTP: "+err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "otp_sent_mobile"), fiber.Map{
		"session_id":   sessionID,
		"phone":        req.Phone,
		"phone_masked": maskPhone(req.Phone),
	})
}

// SSOLogin handles POST /auth/sso/login (public)
func (h *SSOHandler) SSOLogin(c *fiber.Ctx) error {
	var req models.SSOLoginRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	resp, err := h.userService.SSOLogin(c.UserContext(), &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, err.Error())
	}

	if h.nafathAPIBaseURL != "" {
		base := strings.TrimRight(h.nafathAPIBaseURL, "/")
		resp.ValidationURL = base + "/validate?token=" + resp.Token
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "login_successful"), resp)
}

// SSOValidate handles GET /auth/sso/validate?token=<JWT>
// Validates the JWT and returns user info for the 3rd party validation app.
func (h *SSOHandler) SSOValidate(c *fiber.Ctx) error {
	tokenStr := c.Query("token")
	if tokenStr == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "missing_token"))
	}

	claims, err := h.jwtManager.ValidateToken(tokenStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "invalid_or_expired_token"))
	}

	user, err := h.userRepo.FindByID(c.UserContext(), claims.UserID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "user_not_found"))
	}

	fullName := strings.TrimSpace(user.FirstName + " " + user.LastName)

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "token_validated"), fiber.Map{
		"user_id":     user.ID.String(),
		"national_id": user.NationalID,
		"full_name":   fullName,
		"email":       user.Email,
		"phone":       user.Phone,
	})
}
