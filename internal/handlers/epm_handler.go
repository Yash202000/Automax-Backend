package handlers

import (
	"context"
	"encoding/base64"
	"strings"

	"github.com/automax/backend/internal/database"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

type EPMHandler struct {
	userRepo     repository.UserRepository
	jwtManager   *utils.JWTManager
	sessionStore *database.SessionStore
}

func NewEPMHandler(userRepo repository.UserRepository, jwtManager *utils.JWTManager, sessionStore *database.SessionStore) *EPMHandler {
	return &EPMHandler{
		userRepo:     userRepo,
		jwtManager:   jwtManager,
		sessionStore: sessionStore,
	}
}

type EPMLoginResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

func (h *EPMHandler) Login(c *fiber.Ctx) error {
	authHeader := c.Get("Authorization")
	if authHeader == "" || !strings.HasPrefix(authHeader, "Basic ") {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"success": false,
			"message": "Missing or invalid Authorization header",
		})
	}

	payload, err := base64.StdEncoding.DecodeString(authHeader[6:])
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"success": false,
			"message": "Invalid Authorization header encoding",
		})
	}

	credentials := strings.SplitN(string(payload), ":", 2)
	if len(credentials) != 2 {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"success": false,
			"message": "Invalid Authorization header format",
		})
	}

	email := strings.TrimSpace(credentials[0])
	password := credentials[1]

	if email == "" || password == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"success": false,
			"message": "Email and password are required",
		})
	}

	user, err := h.userRepo.FindByEmailForLogin(c.UserContext(), email)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"success": false,
			"message": "Invalid email or password",
		})
	}

	if !user.IsActive {
		return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
			"success": false,
			"message": "Account is deactivated",
		})
	}

	if !utils.CheckPassword(password, user.Password) {
		return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
			"success": false,
			"message": "Invalid email or password",
		})
	}

	role := "user"
	if user.IsSuperAdmin {
		role = "admin"
	} else if len(user.Roles) > 0 {
		role = user.Roles[0].Code
	}

	tokenPair, err := h.jwtManager.GenerateTokenPair(user.ID, user.Email, "", role)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"message": "Failed to generate authentication token",
		})
	}

	if err := h.sessionStore.SetUserSession(c.UserContext(), user.ID.String(), map[string]interface{}{
		"user_id": user.ID,
		"email":   user.Email,
		"role":    role,
	}, h.jwtManager.GetTokenExpiration()); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"success": false,
			"message": "Failed to store session",
		})
	}

	userID := user.ID
	go func() {
		_ = h.userRepo.UpdateLastLogin(context.Background(), userID)
	}()

	c.Set("Token", tokenPair.AccessToken)
	c.Set("User-ID", user.ID.String())

	return c.JSON(EPMLoginResponse{
		Success: true,
		Message: "Authentication successful",
	})
}
