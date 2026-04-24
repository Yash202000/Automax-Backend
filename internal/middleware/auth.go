package middleware

import (
	"context"
	"log"
	"strings"

	"github.com/automax/backend/internal/database"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type AuthMiddleware struct {
	jwtManager   *utils.JWTManager
	sessionStore *database.SessionStore
	userRepo     repository.UserRepository
}

func NewAuthMiddleware(jwtManager *utils.JWTManager, sessionStore *database.SessionStore, userRepo repository.UserRepository) *AuthMiddleware {
	return &AuthMiddleware{
		jwtManager:   jwtManager,
		sessionStore: sessionStore,
		userRepo:     userRepo,
	}
}

func (m *AuthMiddleware) Authenticate() fiber.Handler {
	return func(c *fiber.Ctx) error {
		var token string
		// c.Request().Header.VisitAll(func(key, value []byte) {
		// 	log.Printf("Header: %s = %s", string(key), string(value))
		// })
		// First check Authorization header
		authHeader := c.Get("Authorization")
		if authHeader != "" {
			parts := strings.Split(authHeader, " ")
			if len(parts) == 2 && parts[0] == "Bearer" {
				token = parts[1]
			}
		}

		// If no token in header, check query parameter (for file downloads/images)
		if token == "" {
			token = c.Query("token")
		}

		if token == "" {
			return utils.ErrorResponse(c, fiber.StatusUnauthorized, "Missing authorization token")
		}

		isBlacklisted, err := m.sessionStore.IsTokenBlacklisted(context.Background(), token)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to validate token")
		}
		if isBlacklisted {
			return utils.ErrorResponse(c, fiber.StatusUnauthorized, "Token has been revoked")
		}

		claims, err := m.jwtManager.ValidateToken(token)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusUnauthorized, "Invalid or expired token")
		}

		c.Locals(constants.ContextKeys.UserID, claims.UserID)
		c.Locals(constants.ContextKeys.Email, claims.Email)
		c.Locals(constants.ContextKeys.UserEmail, claims.Email) // For compatibility with presence tracking
		c.Locals(constants.ContextKeys.Role, claims.Role)
		c.Locals(constants.ContextKeys.Token, token)

		// Preserve IP_ADDRESS and USER_AGENT from RequestContext middleware
		ipAddress, _ := c.Locals(constants.ContextKeys.IP_ADDRESS).(string)
		userAgent, _ := c.Locals(constants.ContextKeys.USER_AGENT).(string)

		ctx := c.UserContext()
		ctx = context.WithValue(ctx, constants.ContextKeys.UserID, claims.UserID)
		ctx = context.WithValue(ctx, constants.ContextKeys.Email, claims.Email)
		ctx = context.WithValue(ctx, constants.ContextKeys.Role, claims.Role)
		ctx = context.WithValue(ctx, constants.ContextKeys.Token, token)
		ctx = context.WithValue(ctx, constants.ContextKeys.IP_ADDRESS, ipAddress)
		ctx = context.WithValue(ctx, constants.ContextKeys.USER_AGENT, userAgent)
		// Fetch user details to get the name for presence tracking
		user, err := m.userRepo.FindByID(ctx, claims.UserID)
		if err == nil && user != nil {
			// Construct full name from first and last name
			userName := strings.TrimSpace(user.FirstName + " " + user.LastName)
			if userName == "" {
				userName = user.Username
			}
			c.Locals(constants.ContextKeys.UserName, userName)
			ctx = context.WithValue(ctx, constants.ContextKeys.UserName, userName)

		}

		c.SetUserContext(ctx)
		return c.Next()
	}
}

func (m *AuthMiddleware) RequireRole(roles ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userRole := c.Locals(constants.ContextKeys.Role).(string)

		for _, role := range roles {
			if userRole == role {
				return c.Next()
			}
		}

		return utils.ErrorResponse(c, fiber.StatusForbidden, "Insufficient permissions")
	}
}

// RequirePermission checks if the authenticated user has any of the specified permissions
func (m *AuthMiddleware) RequirePermission(permissions ...string) fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
		if !ok {
			return utils.ErrorResponse(c, fiber.StatusUnauthorized, "User not authenticated")
		}

		// Preserve IP_ADDRESS and USER_AGENT from RequestContext middleware
		ipAddress, _ := c.Locals(constants.ContextKeys.IP_ADDRESS).(string)
		userAgent, _ := c.Locals(constants.ContextKeys.USER_AGENT).(string)

		ctx := c.UserContext()
		ctx = context.WithValue(ctx, constants.ContextKeys.UserID, userID)
		ctx = context.WithValue(ctx, constants.ContextKeys.IP_ADDRESS, ipAddress)
		ctx = context.WithValue(ctx, constants.ContextKeys.USER_AGENT, userAgent)
		user, err := m.userRepo.FindByIDWithPermissions(ctx, userID)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusForbidden, "User not found")
		}

		// Super admin has all permissions
		if user.IsSuperAdmin {
			c.Locals(constants.ContextKeys.User, user)
			return c.Next()
		}

		// Check if user has any of the required permissions
		for _, perm := range permissions {
			if user.HasPermission(perm) {
				c.Locals(constants.ContextKeys.User, user)
				return c.Next()
			}
		}

		// Log permission denial for security monitoring
		log.Printf("[Auth] Permission denied for user %s: required %v", user.Email, permissions)
		return utils.ErrorResponse(c, fiber.StatusForbidden, "Insufficient permissions")
	}
}

func (m *AuthMiddleware) OptionalAuth() fiber.Handler {
	return func(c *fiber.Ctx) error {
		authHeader := c.Get("Authorization")
		if authHeader == "" {
			return c.Next()
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			return c.Next()
		}

		token := parts[1]

		isBlacklisted, _ := m.sessionStore.IsTokenBlacklisted(context.Background(), token)
		if isBlacklisted {
			return c.Next()
		}

		claims, err := m.jwtManager.ValidateToken(token)
		if err != nil {
			return c.Next()
		}

		c.Locals(constants.ContextKeys.UserID, claims.UserID)
		c.Locals(constants.ContextKeys.Email, claims.Email)
		c.Locals(constants.ContextKeys.UserEmail, claims.Email) // For compatibility with presence tracking
		c.Locals(constants.ContextKeys.Role, claims.Role)
		c.Locals(constants.ContextKeys.Token, token)

		// Preserve IP_ADDRESS and USER_AGENT from RequestContext middleware
		ipAddress, _ := c.Locals(constants.ContextKeys.IP_ADDRESS).(string)
		userAgent, _ := c.Locals(constants.ContextKeys.USER_AGENT).(string)

		ctx := c.UserContext()
		ctx = context.WithValue(ctx, constants.ContextKeys.UserID, claims.UserID)
		ctx = context.WithValue(ctx, constants.ContextKeys.Email, claims.Email)
		ctx = context.WithValue(ctx, constants.ContextKeys.Role, claims.Role)
		ctx = context.WithValue(ctx, constants.ContextKeys.Token, token)
		ctx = context.WithValue(ctx, constants.ContextKeys.IP_ADDRESS, ipAddress)
		ctx = context.WithValue(ctx, constants.ContextKeys.USER_AGENT, userAgent)
		c.SetUserContext(ctx)
		// Fetch user details to get the name for presence tracking
		user, err := m.userRepo.FindByID(ctx, claims.UserID)
		if err == nil && user != nil {
			// Construct full name from first and last name
			userName := strings.TrimSpace(user.FirstName + " " + user.LastName)
			if userName == "" {
				userName = user.Username
			}
			c.Locals(constants.ContextKeys.UserName, userName)
		}

		return c.Next()
	}
}
