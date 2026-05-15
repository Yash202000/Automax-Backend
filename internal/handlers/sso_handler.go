package handlers

import (
	"fmt"
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

	"github.com/automax/backend/pkg/validation"
)

// SSOHandler handles SSO launch, callback, and SSO-specific auth requests
type SSOHandler struct {
	ssoJWT            *utils.SSOJWTManager
	jwtManager        *utils.JWTManager
	sessionStore      *database.SessionStore
	userRepo          repository.UserRepository
	appLinkRepo       repository.ApplicationLinkRepository
	userService       services.UserService
	otpService        *services.OTPService
	frontendURL       string // SSO_FRONTEND_URL — where /sso-complete lives (frontend origin)
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
		ssoJWT:            ssoJWT,
		jwtManager:        jwtManager,
		sessionStore:      sessionStore,
		userRepo:          userRepo,
		appLinkRepo:       appLinkRepo,
		userService:       userService,
		otpService:        otpService,
		frontendURL:       frontendURL,
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
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, "User not authenticated")
	}
	email, _ := c.Locals(constants.ContextKeys.Email).(string)

	var req launchRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if req.AppLinkID == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "app_link_id is required")
	}

	appLinkUUID, err := uuid.Parse(req.AppLinkID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid app_link_id format")
	}

	// Look up the application link
	appLink, err := h.appLinkRepo.FindByID(c.UserContext(), appLinkUUID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Application link not found")
	}

	if !appLink.SSOEnabled {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "SSO is not enabled for this application link")
	}

	if appLink.SSOCallbackURL == "" {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "SSO callback URL is not configured for this application link")
	}

	// Fetch user details for name and role
	user, err := h.userRepo.FindByIDWithRelations(c.UserContext(), userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to fetch user details")
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
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to store SSO nonce")
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
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to generate SSO token")
	}

	redirectURL := fmt.Sprintf("%s?token=%s", appLink.SSOCallbackURL, ssoToken)

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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Missing token parameter")
	}

	// Verify RS256 signature and expiry.
	// ValidateSSOTokenAuto resolves the signing key automatically:
	//   - iss is a URL  → fetches public key from {iss}/.well-known/jwks.json (cross-deployment)
	//   - iss is "automax" → uses the local key (same-deployment)
	claims, err := h.ssoJWT.ValidateSSOTokenAuto(c.UserContext(), tokenStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, "Invalid or expired SSO token")
	}

	// Claim the JTI nonce — prevents replay attacks.
	// ClaimSSONonce uses SET NX so it works regardless of whether the provider and
	// receiver share the same Redis instance (cross-deployment safe).
	claimed, err := h.sessionStore.ClaimSSONonce(c.UserContext(), claims.JTI, 65*time.Second)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to validate SSO nonce")
	}
	if !claimed {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, "SSO token has already been used")
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
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create SSO user")
		}
	}

	// Determine role for token generation
	role := ""
	if len(user.Roles) > 0 {
		role = user.Roles[0].Code
	}

	// Generate local Automax token pair
	tokenPair, err := h.jwtManager.GenerateTokenPair(user.ID, user.Email, "", role)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to generate session tokens")
	}

	// Store local session
	if err := h.sessionStore.SetUserSession(c.UserContext(), user.ID.String(), map[string]interface{}{
		"user_id": user.ID,
		"email":   user.Email,
		"role":    role,
	}, h.jwtManager.GetTokenExpiration()); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to store session")
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

	return c.Redirect(redirectURL, fiber.StatusFound)
}

// SSORegister handles POST /auth/sso/register (public)
func (h *SSOHandler) SSORegister(c *fiber.Ctx) error {
	var req models.SSORegisterRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
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

	return utils.SuccessResponse(c, fiber.StatusCreated, "OTP sent to your mobile number", fiber.Map{
		"session_id":   sessionID,
		"phone":        req.Phone,
		"phone_masked": maskPhone(req.Phone),
	})
}

// SSOLogin handles POST /auth/sso/login (public)
func (h *SSOHandler) SSOLogin(c *fiber.Ctx) error {
	var req models.SSOLoginRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
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

	return utils.SuccessResponse(c, fiber.StatusOK, "Login successful", resp)
}

// SSOValidate handles GET /auth/sso/validate?token=<JWT>
// Validates the JWT and returns user info for the 3rd party validation app.
func (h *SSOHandler) SSOValidate(c *fiber.Ctx) error {
	tokenStr := c.Query("token")
	if tokenStr == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Missing token parameter")
	}

	claims, err := h.jwtManager.ValidateToken(tokenStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, "Invalid or expired token")
	}

	user, err := h.userRepo.FindByID(c.UserContext(), claims.UserID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "User not found")
	}

	fullName := strings.TrimSpace(user.FirstName + " " + user.LastName)

	return utils.SuccessResponse(c, fiber.StatusOK, "Token validated", fiber.Map{
		"user_id":     user.ID.String(),
		"national_id": user.NationalID,
		"full_name":   fullName,
		"email":       user.Email,
		"phone":       user.Phone,
	})
}
