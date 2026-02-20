package middleware

import (
	"context"
	"time"

	"github.com/automax/backend/internal/services"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type ActionLoggerConfig struct {
	Enabled      bool
	SkipPaths    []string
	SkipMethods  []string
	LogService   services.ActionLogService
}

func ActionLogger(config ActionLoggerConfig) fiber.Handler {
	skipPaths := make(map[string]bool)
	for _, path := range config.SkipPaths {
		skipPaths[path] = true
	}

	skipMethods := make(map[string]bool)
	for _, method := range config.SkipMethods {
		skipMethods[method] = true
	}

	return func(c *fiber.Ctx) error {
		// Skip if logging is disabled
		if !config.Enabled {
			return c.Next()
		}

		// Skip certain paths
		if skipPaths[c.Path()] {
			return c.Next()
		}

		// Skip certain methods (e.g., GET requests)
		if skipMethods[c.Method()] {
			return c.Next()
		}

		start := time.Now()

		// Process request
		err := c.Next()

		// Calculate duration
		duration := time.Since(start).Milliseconds()

		// Get user ID from context (set by auth middleware)
		userIDInterface := c.Locals("user_id")
		if userIDInterface == nil {
			return err
		}

		userID, ok := userIDInterface.(uuid.UUID)
		if !ok {
			return err
		}

		// Determine action and module from request
		action := getActionFromMethod(c.Method())
		module := getModuleFromPath(c.Path())

		// Determine status
		status := "success"
		errorMsg := ""
		if err != nil {
			status = "failed"
			errorMsg = err.Error()
		} else if c.Response().StatusCode() >= 400 {
			status = "failed"
		}

		// Get resource ID from params if available
		resourceID := c.Params("id")

		// Capture IP and User-Agent before async goroutine (context may be released)
		ipAddress := c.IP()
		userAgent := c.Get("User-Agent")

		// Log the action asynchronously using background context
		// We use context.Background() instead of c.Context() because the request
		// context may be cancelled when the HTTP response is sent
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			logErr := config.LogService.LogAction(ctx, &services.LogActionParams{
				UserID:      userID,
				Action:      action,
				Module:      module,
				ResourceID:  resourceID,
				Description: generateDescription(action, module, resourceID),
				IPAddress:   ipAddress,
				UserAgent:   userAgent,
				Status:      status,
				ErrorMsg:    errorMsg,
				Duration:    duration,
			})
			if logErr != nil {
				// Log error but don't fail the request
				// In production, you might want to use a proper logger
			}
		}()

		return err
	}
}

func getActionFromMethod(method string) string {
	switch method {
	case "POST":
		return "create"
	case "PUT", "PATCH":
		return "update"
	case "DELETE":
		return "delete"
	case "GET":
		return "view"
	default:
		return "other"
	}
}

func getModuleFromPath(path string) string {
	// Extract module from path like /api/admin/users -> users
	// or /api/v1/departments -> departments
	// or /api/admin/workflows/:id/states -> workflow_states
	segments := splitPath(path)

	// Check for workflow sub-resources (states, transitions)
	// Path pattern: /api/admin/workflows/:id/states or /api/admin/workflows/:id/transitions
	for i, seg := range segments {
		if seg == "workflows" && i+2 < len(segments) {
			subResource := segments[i+2]
			if subResource == "states" || subResource == "transitions" {
				return "workflow_" + subResource
			}
		}
	}

	// Find the relevant segment (skip api, admin, v1, etc.)
	for i := len(segments) - 1; i >= 0; i-- {
		seg := segments[i]
		if seg != "" && seg != "api" && seg != "admin" && seg != "v1" && !isUUID(seg) {
			return seg
		}
	}

	return "unknown"
}

func splitPath(path string) []string {
	var segments []string
	current := ""
	for _, char := range path {
		if char == '/' {
			if current != "" {
				segments = append(segments, current)
				current = ""
			}
		} else {
			current += string(char)
		}
	}
	if current != "" {
		segments = append(segments, current)
	}
	return segments
}

func isUUID(s string) bool {
	_, err := uuid.Parse(s)
	return err == nil
}

func generateDescription(action, module, resourceID string) string {
	// Special handling for workflow-related modules
	if module == "workflows" {
		switch action {
		case "create":
			return "Created new workflow" + withResourceID(resourceID)
		case "update":
			return "Updated workflow" + withResourceID(resourceID)
		case "delete":
			return "Deleted workflow" + withResourceID(resourceID)
		}
	}
	if module == "workflow_states" {
		switch action {
		case "create":
			return "Created new workflow state" + withResourceID(resourceID)
		case "update":
			return "Updated workflow state" + withResourceID(resourceID)
		case "delete":
			return "Deleted workflow state" + withResourceID(resourceID)
		}
	}
	if module == "workflow_transitions" {
		switch action {
		case "create":
			return "Created new workflow transition" + withResourceID(resourceID)
		case "update":
			return "Updated workflow transition" + withResourceID(resourceID)
		case "delete":
			return "Deleted workflow transition" + withResourceID(resourceID)
		}
	}

	// Default description
	desc := action + " " + module
	if resourceID != "" {
		desc += " (ID: " + resourceID + ")"
	}
	return desc
}

func withResourceID(resourceID string) string {
	if resourceID != "" {
		return " (ID: " + resourceID + ")"
	}
	return ""
}

// LogAction is a helper function to manually log actions
func LogAction(c *fiber.Ctx, logService services.ActionLogService, params *services.LogActionParams) {
	// Get user ID from context
	userIDInterface := c.Locals("user_id")
	if userIDInterface == nil {
		return
	}

	userID, ok := userIDInterface.(uuid.UUID)
	if !ok {
		return
	}

	params.UserID = userID
	params.IPAddress = c.IP()
	params.UserAgent = c.Get("User-Agent")

	go func() {
		_ = logService.LogAction(c.Context(), params)
	}()
}
