package handlers

import (
	"fmt"

	"github.com/automax/backend/internal/services"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/websocket/v2"
	"github.com/google/uuid"
)

// WebSocketHandler handles WebSocket connections
type WebSocketHandler struct {
	hub *services.WSHub
}

// NewWebSocketHandler creates a new WebSocket handler
func NewWebSocketHandler(hub *services.WSHub) *WebSocketHandler {
	return &WebSocketHandler{
		hub: hub,
	}
}

// rejectConnection closes a freshly upgraded connection with a well-formed close
// frame explaining why.
//
// A close frame's payload must be a 2-byte big-endian status code followed by
// the UTF-8 reason. Writing the bare reason string makes clients decode its
// first two bytes as the status code — "Missing required parameters" surfaces in
// wscat as "invalid status code 19817" (0x4D69, the bytes 'M' and 'i') and hides
// the actual reason. FormatCloseMessage prepends the code correctly.
func rejectConnection(c *websocket.Conn, reason string) {
	_ = c.WriteMessage(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.ClosePolicyViolation, reason))
	_ = c.Close()
}

// HandleWebSocket handles WebSocket upgrade and connection
// Expected query params: incident_id, user_id
func (h *WebSocketHandler) HandleWebSocket(c *websocket.Conn) {
	// Get query parameters
	incidentIDStr := c.Query("incident_id")
	userIDStr := c.Query("user_id")

	if incidentIDStr == "" || userIDStr == "" {
		fmt.Println("[WebSocket] Missing incident_id or user_id")
		rejectConnection(c, "Missing required parameters")
		return
	}

	incidentID, err := uuid.Parse(incidentIDStr)
	if err != nil {
		fmt.Printf("[WebSocket] Invalid incident_id: %s\n", incidentIDStr)
		rejectConnection(c, "Invalid incident_id")
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		fmt.Printf("[WebSocket] Invalid user_id: %s\n", userIDStr)
		rejectConnection(c, "Invalid user_id")
		return
	}

	// Get user name from query (optional)
	userName := c.Query("user_name")
	if userName == "" {
		userName = "Unknown User"
	}

	// Create client
	client := &services.WSClient{
		ID:         uuid.New(),
		UserID:     userID,
		UserName:   userName,
		IncidentID: incidentID,
		Conn:       c,
		Send:       make(chan services.WSMessage, 256),
		Hub:        h.hub,
	}

	// Register client
	h.hub.Register() <- client

	// Start read/write pumps
	go client.WritePump()
	client.ReadPump() // Blocks until connection closes
}

// WebSocketUpgrader middleware for Fiber
func (h *WebSocketHandler) WebSocketUpgrader() fiber.Handler {
	return websocket.New(h.HandleWebSocket)
}

// HandleBroadcastWebSocket handles WebSocket connections for broadcast channel (incident list updates)
// Expected query params: user_id (optional), token
func (h *WebSocketHandler) HandleBroadcastWebSocket(c *websocket.Conn) {
	// Get query parameters
	userIDStr := c.Query("user_id")
	userName := c.Query("user_name")

	var userID uuid.UUID
	if userIDStr != "" {
		var err error
		userID, err = uuid.Parse(userIDStr)
		if err != nil {
			userID = uuid.New() // Generate random ID if invalid
		}
	} else {
		userID = uuid.New() // Generate random ID if not provided
	}

	if userName == "" {
		userName = "Anonymous"
	}

	// Create broadcast client
	client := &services.WSClient{
		ID:          uuid.New(),
		UserID:      userID,
		UserName:    userName,
		IsBroadcast: true, // This is a broadcast client
		Conn:        c,
		Send:        make(chan services.WSMessage, 256),
		Hub:         h.hub,
	}

	// Register client
	h.hub.Register() <- client

	// Start read/write pumps
	go client.WritePump()
	client.ReadPump() // Blocks until connection closes
}

// BroadcastWebSocketUpgrader middleware for Fiber
func (h *WebSocketHandler) BroadcastWebSocketUpgrader() fiber.Handler {
	return websocket.New(h.HandleBroadcastWebSocket)
}

// HandleGoalWebSocket handles WebSocket upgrade for goal-specific connections
// Expected query params: goal_id, user_id
func (h *WebSocketHandler) HandleGoalWebSocket(c *websocket.Conn) {
	goalIDStr := c.Query("goal_id")
	userIDStr := c.Query("user_id")

	if goalIDStr == "" || userIDStr == "" {
		fmt.Println("[WebSocket] Missing goal_id or user_id")
		rejectConnection(c, "Missing required parameters")
		return
	}

	goalID, err := uuid.Parse(goalIDStr)
	if err != nil {
		fmt.Printf("[WebSocket] Invalid goal_id: %s\n", goalIDStr)
		rejectConnection(c, "Invalid goal_id")
		return
	}

	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		fmt.Printf("[WebSocket] Invalid user_id: %s\n", userIDStr)
		rejectConnection(c, "Invalid user_id")
		return
	}

	userName := c.Query("user_name")
	if userName == "" {
		userName = "Unknown User"
	}

	client := &services.WSClient{
		ID:           uuid.New(),
		UserID:       userID,
		UserName:     userName,
		GoalID:       goalID,
		IsGoalClient: true,
		Conn:         c,
		Send:         make(chan services.WSMessage, 256),
		Hub:          h.hub,
	}

	h.hub.Register() <- client

	go client.WritePump()
	client.ReadPump()
}

// GoalWebSocketUpgrader middleware for goal-specific WebSocket
func (h *WebSocketHandler) GoalWebSocketUpgrader() fiber.Handler {
	return websocket.New(h.HandleGoalWebSocket)
}

// HandleGoalBroadcastWebSocket handles WebSocket connections for goal list broadcast channel
// Expected query params: user_id (optional), token
func (h *WebSocketHandler) HandleGoalBroadcastWebSocket(c *websocket.Conn) {
	userIDStr := c.Query("user_id")
	userName := c.Query("user_name")

	var userID uuid.UUID
	if userIDStr != "" {
		var err error
		userID, err = uuid.Parse(userIDStr)
		if err != nil {
			userID = uuid.New()
		}
	} else {
		userID = uuid.New()
	}

	if userName == "" {
		userName = "Anonymous"
	}

	client := &services.WSClient{
		ID:              uuid.New(),
		UserID:          userID,
		UserName:        userName,
		IsGoalBroadcast: true,
		Conn:            c,
		Send:            make(chan services.WSMessage, 256),
		Hub:             h.hub,
	}

	h.hub.Register() <- client

	go client.WritePump()
	client.ReadPump()
}

// GoalBroadcastWebSocketUpgrader middleware for goal broadcast WebSocket
func (h *WebSocketHandler) GoalBroadcastWebSocketUpgrader() fiber.Handler {
	return websocket.New(h.HandleGoalBroadcastWebSocket)
}

// GetConnectionStats returns WebSocket connection statistics
func (h *WebSocketHandler) GetConnectionStats(c *fiber.Ctx) error {
	stats := h.hub.GetConnectionStats()
	return c.JSON(fiber.Map{
		"success": true,
		"data":    stats,
	})
}

// IsWebSocketUpgrade checks if request is a WebSocket upgrade request
func IsWebSocketUpgrade(c *fiber.Ctx) bool {
	return c.Get("Upgrade") == "websocket" &&
		c.Get("Connection") == "Upgrade" &&
		c.Get("Sec-WebSocket-Version") == "13"
}

// WebSocketAuthMiddleware verifies JWT token for WebSocket connections
func WebSocketAuthMiddleware() fiber.Handler {
	return func(c *fiber.Ctx) error {
		// Check if this is a WebSocket upgrade request
		if !IsWebSocketUpgrade(c) {
			return c.Next()
		}

		// Get token from query param (since WebSocket can't set headers easily)
		token := c.Query("token")
		if token == "" {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Missing authentication token",
			})
		}

		// TODO: Validate JWT token here
		// For now, we'll trust the token and extract user info from query params
		// In production, you should validate the token and extract user info from it

		return c.Next()
	}
}
