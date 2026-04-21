package middleware

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/gofiber/fiber/v2"
	"github.com/redis/go-redis/v9"
)

func LoginRateLimitMiddleware(cfg *config.Config, redisClient *redis.Client, userService services.UserService) fiber.Handler {
	return func(c *fiber.Ctx) error {

		if !cfg.LoginRateLimit.Enabled {
			return c.Next()
		}

		ctx := c.UserContext()

		var reqBody map[string]interface{}
		if err := c.BodyParser(&reqBody); err != nil {
			log.Printf("[RATE_LIMIT] Failed to parse body: %v", err)
			return c.Next()
		}

		userIdentifier := extractUserIdentifier(reqBody)
		if userIdentifier == "" {
			log.Println("[RATE_LIMIT] No identifier found")
			return c.Next()
		}

		log.Printf("[RATE_LIMIT] Incoming login for: %s", userIdentifier)

		var key string
		if strings.Contains(userIdentifier, "@") {
			key = "email:" + strings.ToLower(userIdentifier)
		} else {
			key = "phone:" + userIdentifier
		}
		redisKey := "user:" + key
		log.Printf("[RATE_LIMIT] Normalized key: %s", redisKey)
		// Fetch user (email / phone)
		var user *models.User
		var err error
		//Get users
		if strings.Contains(userIdentifier, "@") {
			user, err = userService.GetUserByEmail(ctx, userIdentifier)
		} else {
			user, err = userService.GetUserByMobile(ctx, userIdentifier)
		}

		// ADMIN BYPASS
		if err == nil && user != nil {
			if cfg.LoginRateLimit.BypassForAdmin && user.IsSuperAdmin {
				log.Printf("[RATE_LIMIT] Admin bypass for user: %s", userIdentifier)
				return c.Next()
			}
		}

		// check block
		blocked, _ := checkIfBlocked(ctx, redisClient, redisKey)
		if blocked {
			log.Printf("[RATE_LIMIT] BLOCKED user: %s", redisKey)
			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "Too many login attempts. Please try again later.",
			})
		}

		// Attempts
		attempts, _ := getAttemptCount(ctx, redisClient, redisKey)
		log.Printf("[RATE_LIMIT] Attempts for %s: %d", redisKey, attempts)
		if attempts >= cfg.LoginRateLimit.MaxAttempts {
			log.Printf("[RATE_LIMIT] Blocking user: %s", redisKey)

			blockIdentifier(ctx, redisClient, redisKey, cfg)

			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
				"error": "Too many login attempts. Please try again later.",
			})
		}

		// STORE NORMALIZED KEY
		c.Locals("login_user_identifier", key)

		return c.Next()
	}
}

// LoginRateLimitMiddleware creates a middleware that rate limits login attempts
// based on IP address and user identifier (email/username/mobile)
// func LoginRateLimitMiddleware(cfg *config.Config, redisClient *redis.Client) fiber.Handler {
// 	return func(c *fiber.Ctx) error {
// 		// Skip if rate limiting is disabled
// 		if !cfg.LoginRateLimit.Enabled {
// 			return c.Next()
// 		}

// 		ctx := c.UserContext()
// 		ipAddress := c.IP()

// 		// Parse request body to get user identifier
// 		var reqBody map[string]interface{}
// 		if err := c.BodyParser(&reqBody); err != nil {
// 			// If we can't parse the body, continue without rate limiting
// 			return c.Next()
// 		}

// 		// Extract user identifier from request
// 		userIdentifier := extractUserIdentifier(reqBody)

// 		// Check if IP is blocked
// 		ipBlocked, err := checkIfBlocked(ctx, redisClient, "ip:"+ipAddress)
// 		if err != nil {
// 			// If there's an error checking, continue without rate limiting
// 			return c.Next()
// 		}

// 		if ipBlocked {
// 			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
// 				"success": false,
// 				"error":   "Too many login attempts. Please try again later.",
// 			})
// 		}

// 		// Check if user identifier is blocked
// 		if userIdentifier != "" {
// 			userBlocked, err := checkIfBlocked(ctx, redisClient, "user:"+userIdentifier)
// 			if err != nil {
// 				return c.Next()
// 			}

// 			if userBlocked {
// 				return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
// 					"success": false,
// 					"error":   "Too many login attempts. Please try again later.",
// 				})
// 			}
// 		}

// 		// Check attempt count for IP
// 		ipAttempts, err := getAttemptCount(ctx, redisClient, "ip:"+ipAddress)
// 		if err != nil {
// 			return c.Next()
// 		}

// 		if ipAttempts >= cfg.LoginRateLimit.MaxAttempts {
// 			// Block the IP
// 			err := blockIdentifier(ctx, redisClient, "ip:"+ipAddress, cfg)
// 			if err != nil {
// 				return c.Next()
// 			}
// 			return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
// 				"success": false,
// 				"error":   "Too many login attempts. Please try again later.",
// 			})
// 		}

// 		// Check attempt count for user identifier
// 		if userIdentifier != "" {
// 			userAttempts, err := getAttemptCount(ctx, redisClient, "user:"+userIdentifier)
// 			if err != nil {
// 				return c.Next()
// 			}

// 			if userAttempts >= cfg.LoginRateLimit.MaxAttempts {
// 				// Block the user identifier
// 				err := blockIdentifier(ctx, redisClient, "user:"+userIdentifier, cfg)
// 				if err != nil {
// 					return c.Next()
// 				}
// 				return c.Status(fiber.StatusTooManyRequests).JSON(fiber.Map{
// 					"success": false,
// 					"error":   "Too many login attempts. Please try again later.",
// 				})
// 			}
// 		}

// 		// Increment attempt counters
// 		if err := incrementAttempt(ctx, redisClient, "ip:"+ipAddress, cfg); err != nil {
// 			// If we can't increment, continue without rate limiting
// 			return c.Next()
// 		}

// 		if userIdentifier != "" {
// 			if err := incrementAttempt(ctx, redisClient, "user:"+userIdentifier, cfg); err != nil {
// 				return c.Next()
// 			}
// 		}

// 		// Store user identifier in context for later use (on successful login)
// 		if userIdentifier != "" {
// 			c.Locals("login_user_identifier", userIdentifier)
// 		}

// 		return c.Next()
// 	}
// }

// extractUserIdentifier extracts the user identifier from the login request body
func extractUserIdentifier(reqBody map[string]interface{}) string {
	// Check for email
	if email, ok := reqBody["email"].(string); ok && email != "" {
		return strings.ToLower(strings.TrimSpace(email))
	}

	// Check for username
	if username, ok := reqBody["username"].(string); ok && username != "" {
		return strings.ToLower(strings.TrimSpace(username))
	}

	// Check for phone/mobile
	if phone, ok := reqBody["phone"].(string); ok && phone != "" {
		return strings.TrimSpace(phone)
	}

	return ""
}

// checkIfBlocked checks if an identifier is currently blocked
func checkIfBlocked(ctx context.Context, redisClient *redis.Client, identifier string) (bool, error) {
	key := fmt.Sprintf("login_block:%s", identifier)
	exists, err := redisClient.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return exists > 0, nil
}

// getAttemptCount gets the current attempt count for an identifier
func getAttemptCount(ctx context.Context, redisClient *redis.Client, identifier string) (int, error) {
	key := fmt.Sprintf("login_attempts:%s", identifier)
	val, err := redisClient.Get(ctx, key).Result()
	if err == redis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	var count int
	if _, err := fmt.Sscanf(val, "%d", &count); err != nil {
		return 0, err
	}
	return count, nil
}

// blockIdentifier blocks an identifier for the configured block duration
func blockIdentifier(ctx context.Context, redisClient *redis.Client, identifier string, cfg *config.Config) error {
	blockKey := fmt.Sprintf("login_block:%s", identifier)
	blockDuration := time.Duration(cfg.LoginRateLimit.BlockDuration) * time.Minute

	// Set block key with expiration
	if err := redisClient.Set(ctx, blockKey, "1", blockDuration).Err(); err != nil {
		return err
	}

	return nil
}

// incrementAttempt increments the attempt count for an identifier
func incrementAttempt(ctx context.Context, redisClient *redis.Client, identifier string, cfg *config.Config) error {
	key := fmt.Sprintf("login_attempts:%s", identifier)
	windowDuration := time.Duration(cfg.LoginRateLimit.RateLimitWindow) * time.Minute

	// Use INCR and set expiry only if key is new
	count, err := redisClient.Incr(ctx, key).Result()
	if err != nil {
		return err
	}

	// Set expiration only when the key is newly created
	if count == 1 {
		if err := redisClient.Expire(ctx, key, windowDuration).Err(); err != nil {
			return err
		}
	}

	return nil
}
