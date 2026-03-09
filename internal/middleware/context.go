package middleware

import (
	"context"

	"github.com/automax/backend/pkg/constants"
	"github.com/gofiber/fiber/v2"
)

// RequestContext sets request metadata (hostname, protocol, IP, user-agent) as
// Fiber Locals for every request, so all routes — including unauthenticated ones
// — have access to this data without calling c.IP() or c.Get("User-Agent") directly.
func RequestContext() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Locals(constants.ContextKeys.HOSTNAME, c.Hostname())
		c.Locals(constants.ContextKeys.PROTOCOL, c.Protocol())
		c.Locals(constants.ContextKeys.IP_ADDRESS, c.IP())
		c.Locals(constants.ContextKeys.USER_AGENT, c.Get("User-Agent"))

		ctx := c.UserContext()
		ctx = context.WithValue(ctx, constants.ContextKeys.HOSTNAME, c.Hostname())
		ctx = context.WithValue(ctx, constants.ContextKeys.PROTOCOL, c.Protocol())
		ctx = context.WithValue(ctx, constants.ContextKeys.IP_ADDRESS, c.IP())
		ctx = context.WithValue(ctx, constants.ContextKeys.USER_AGENT, c.Get("User-Agent"))
		c.SetUserContext(ctx)
		return c.Next()
	}
}
