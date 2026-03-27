package handlers

import (
	"context"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/database"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// LDAPHandler handles LDAP authentication requests
type LDAPHandler struct {
	ldapService  services.LDAPService
	userService  services.UserService
	userRepo     repository.UserRepository
	jwtManager   *utils.JWTManager
	sessionStore *database.SessionStore
	config       *config.Config
}

func NewLDAPHandler(
	ldapService services.LDAPService,
	userService services.UserService,
	userRepo repository.UserRepository,
	jwtManager *utils.JWTManager,
	sessionStore *database.SessionStore,
	cfg *config.Config,
) *LDAPHandler {
	return &LDAPHandler{
		ldapService:  ldapService,
		userService:  userService,
		userRepo:     userRepo,
		jwtManager:   jwtManager,
		sessionStore: sessionStore,
		config:       cfg,
	}
}

// LDAPLoginRequest represents the LDAP login request body
type LDAPLoginRequest struct {
	Username string `json:"username" validate:"required"`
	Password string `json:"password" validate:"required"`
}

// LDAPLoginResponse represents the LDAP login response
type LDAPLoginResponse struct {
	User         *models.UserResponse `json:"user"`
	Token        string               `json:"token"`
	RefreshToken string               `json:"refresh_token,omitempty"`
	ExpiresIn    int64                `json:"expires_in,omitempty"`
	Source       string               `json:"source"` // "ldap" or "local"
}

// LDAPTestRequest represents the LDAP test connection request
type LDAPTestRequest struct {
	URL          string `json:"url"`
	BaseDN       string `json:"base_dn"`
	BindDN       string `json:"bind_dn"`
	BindPassword string `json:"bind_password"`
	UserSearchBase string `json:"user_search_base"`
}

// LDAPTestResponse represents the LDAP test connection response
type LDAPTestResponse struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
}

// LDAPSearchRequest represents the LDAP user search request
type LDAPSearchRequest struct {
	Username string `json:"username" validate:"required"`
}

// LDAPSearchResponse represents the LDAP user search response
type LDAPSearchResponse struct {
	Found   bool                `json:"found"`
	User    *LDAPUserInfoResponse `json:"user,omitempty"`
	Message string              `json:"message,omitempty"`
}

// LDAPUserInfoResponse represents LDAP user info in response
type LDAPUserInfoResponse struct {
	DN         string   `json:"dn"`
	Username   string   `json:"username"`
	Email      string   `json:"email"`
	FirstName  string   `json:"first_name"`
	LastName   string   `json:"last_name"`
	Phone      string   `json:"phone"`
	Department string   `json:"department"`
	Title      string   `json:"title"`
	Groups     []string `json:"groups,omitempty"`
}

// Login handles LDAP authentication
// POST /api/v1/ldap/login
func (h *LDAPHandler) Login(c *fiber.Ctx) error {
	// Check if LDAP is enabled
	if !h.config.LDAP.Enabled {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "LDAP authentication is not enabled")
	}

	var req LDAPLoginRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		// Convert validation errors to string
		errMsg := ""
		for field, err := range validationErrors {
			errMsg += field + ": " + err + "; "
		}
		return utils.ErrorResponse(c, fiber.StatusBadRequest, errMsg)
	}

	// Authenticate against LDAP
	ldapUser, err := h.ldapService.Authenticate(c.UserContext(), req.Username, req.Password)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, "LDAP authentication failed: "+err.Error())
	}

	// Check if user is enabled
	if !ldapUser.Enabled {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "Your account is disabled in Active Directory")
	}

	// Look up or auto-create user in local database
	user, err := h.userRepo.FindByEmail(c.UserContext(), ldapUser.Email)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			// Auto-create user
			user, err = h.createOrUpdateUserFromLDAP(c.UserContext(), ldapUser)
			if err != nil {
				return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create user from LDAP: "+err.Error())
			}
		} else {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to look up user: "+err.Error())
		}
	} else {
		// Update existing user with LDAP data
		user, err = h.updateUserFromLDAP(c.UserContext(), user, ldapUser)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to update user from LDAP: "+err.Error())
		}
	}

	// Check if user is active in local database
	if !user.IsActive {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "Your account is deactivated in the system")
	}

	// Determine role for JWT
	role := "user"
	if user.IsSuperAdmin {
		role = "admin"
	} else if len(user.Roles) > 0 {
		role = user.Roles[0].Code
	}

	// Generate JWT tokens
	tokenPair, err := h.jwtManager.GenerateTokenPair(user.ID, user.Email, role)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to generate authentication tokens")
	}

	// Store session
	if err := h.sessionStore.SetUserSession(c.UserContext(), user.ID.String(), map[string]interface{}{
		"user_id": user.ID,
		"email":   user.Email,
		"role":    role,
		"auth_source": "ldap",
	}, h.jwtManager.GetTokenExpiration()); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to store session")
	}

	// Update last login timestamp
	go func() {
		_ = h.userRepo.UpdateLastLogin(context.Background(), user.ID)
	}()

	userResponse := models.ToUserResponse(user)

	return c.JSON(fiber.Map{
		"success": true,
		"data": LDAPLoginResponse{
			User:         &userResponse,
			Token:        tokenPair.AccessToken,
			RefreshToken: tokenPair.RefreshToken,
			ExpiresIn:    tokenPair.ExpiresIn,
			Source:       "ldap",
		},
	})
}

// TestConnection handles LDAP connection test
// POST /api/v1/ldap/test
func (h *LDAPHandler) TestConnection(c *fiber.Ctx) error {
	// Check if LDAP is enabled
	if !h.config.LDAP.Enabled {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "LDAP is not enabled")
	}

	// Test the connection
	err := h.ldapService.TestConnection(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "LDAP connection test failed: "+err.Error())
	}

	return c.JSON(fiber.Map{
		"success": true,
		"message": "LDAP connection successful",
	})
}

// SearchUser handles LDAP user search
// POST /api/v1/ldap/search
func (h *LDAPHandler) SearchUser(c *fiber.Ctx) error {
	// Check if LDAP is enabled
	if !h.config.LDAP.Enabled {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "LDAP is not enabled")
	}

	var req LDAPSearchRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		// Convert validation errors to string
		errMsg := ""
		for field, err := range validationErrors {
			errMsg += field + ": " + err + "; "
		}
		return utils.ErrorResponse(c, fiber.StatusBadRequest, errMsg)
	}

	// Search for user in LDAP
	ldapUser, err := h.ldapService.SearchUser(c.UserContext(), req.Username)
	if err != nil {
		if err.Error() == "user not found" {
			return c.JSON(fiber.Map{
				"success": true,
				"data": LDAPSearchResponse{
					Found:   false,
					Message: "User not found in LDAP",
				},
			})
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "LDAP search failed: "+err.Error())
	}

	// Search for user's groups
	groups, err := h.ldapService.SearchGroup(c.UserContext(), ldapUser.DN)
	if err != nil {
		// Log error but continue - groups are optional
		groups = []services.LDAPGroupInfo{}
	}

	groupNames := make([]string, len(groups))
	for i, g := range groups {
		groupNames[i] = g.Name
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data": LDAPSearchResponse{
			Found: true,
			User: &LDAPUserInfoResponse{
				DN:         ldapUser.DN,
				Username:   ldapUser.Username,
				Email:      ldapUser.Email,
				FirstName:  ldapUser.FirstName,
				LastName:   ldapUser.LastName,
				Phone:      ldapUser.Phone,
				Department: ldapUser.Department,
				Title:      ldapUser.Title,
				Groups:     groupNames,
			},
		},
	})
}

// SyncUser handles manual LDAP user sync
// POST /api/v1/ldap/sync
func (h *LDAPHandler) SyncUser(c *fiber.Ctx) error {
	// Check if LDAP is enabled
	if !h.config.LDAP.Enabled {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "LDAP is not enabled")
	}

	var req LDAPSearchRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		// Convert validation errors to string
		errMsg := ""
		for field, err := range validationErrors {
			errMsg += field + ": " + err + "; "
		}
		return utils.ErrorResponse(c, fiber.StatusBadRequest, errMsg)
	}

	// Search for user in LDAP
	ldapUser, err := h.ldapService.SearchUser(c.UserContext(), req.Username)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "User not found in LDAP: "+err.Error())
	}

	// Look up or create user in local database
	user, err := h.userRepo.FindByEmail(c.UserContext(), ldapUser.Email)
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			user, err = h.createOrUpdateUserFromLDAP(c.UserContext(), ldapUser)
			if err != nil {
				return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create user from LDAP: "+err.Error())
			}
		} else {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to look up user: "+err.Error())
		}
	} else {
		user, err = h.updateUserFromLDAP(c.UserContext(), user, ldapUser)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to update user from LDAP: "+err.Error())
		}
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    models.ToUserResponse(user),
		"message": "User synced successfully from LDAP",
	})
}

// createOrUpdateUserFromLDAP creates a new user from LDAP data
func (h *LDAPHandler) createOrUpdateUserFromLDAP(ctx context.Context, ldapUser *services.LDAPUserInfo) (*models.User, error) {
	// Generate a random unusable password since LDAP users won't use password login
	randomPw, _ := uuid.NewRandom()
	hashedPw, _ := bcrypt.GenerateFromPassword([]byte(randomPw.String()), bcrypt.DefaultCost)

	user := &models.User{
		Email:     ldapUser.Email,
		Username:  ldapUser.Username,
		Password:  string(hashedPw),
		FirstName: ldapUser.FirstName,
		LastName:  ldapUser.LastName,
		Phone:     ldapUser.Phone,
		IsActive:  true,
	}

	if err := h.userRepo.Create(ctx, user); err != nil {
		return nil, err
	}

	return user, nil
}

// updateUserFromLDAP updates an existing user with LDAP data
func (h *LDAPHandler) updateUserFromLDAP(ctx context.Context, user *models.User, ldapUser *services.LDAPUserInfo) (*models.User, error) {
	// Update user fields from LDAP
	user.FirstName = ldapUser.FirstName
	user.LastName = ldapUser.LastName
	user.Phone = ldapUser.Phone
	user.IsActive = ldapUser.Enabled

	if err := h.userRepo.Update(ctx, user); err != nil {
		return nil, err
	}

	return user, nil
}

// GetLDAPStatus returns the current LDAP configuration status
// GET /api/v1/ldap/status
func (h *LDAPHandler) GetLDAPStatus(c *fiber.Ctx) error {
	status := fiber.Map{
		"enabled": h.config.LDAP.Enabled,
		"url":     h.config.LDAP.URL,
		"base_dn": h.config.LDAP.BaseDN,
	}

	if h.config.LDAP.Enabled {
		// Test connection
		err := h.ldapService.TestConnection(c.UserContext())
		if err != nil {
			status["connected"] = false
			status["error"] = err.Error()
		} else {
			status["connected"] = true
		}
	}

	return c.JSON(fiber.Map{
		"success": true,
		"data":    status,
	})
}
