package handlers

import (
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

// JWKSHandler serves the RSA public key in JWK Set format
type JWKSHandler struct {
	ssoJWT *utils.SSOJWTManager
}

func NewJWKSHandler(ssoJWT *utils.SSOJWTManager) *JWKSHandler {
	return &JWKSHandler{ssoJWT: ssoJWT}
}

// GetJWKS handles GET /.well-known/jwks.json
// Public endpoint — no authentication required.
func (h *JWKSHandler) GetJWKS(c *fiber.Ctx) error {
	c.Set("Cache-Control", "public, max-age=300")
	return c.JSON(h.ssoJWT.GetJWKS())
}
