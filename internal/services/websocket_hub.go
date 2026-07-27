package services

import (
	"fmt"
	"sync"
	"time"

	"github.com/gofiber/websocket/v2"
	"github.com/google/uuid"
)

// WebSocket close codes
const (
	CloseNormalClosure   = 1000
	CloseGoingAway       = 1001
	CloseAbnormalClosure = 1006
)

// WSMessage represents a WebSocket message sent to clients
type WSMessage struct {
	Type       string      `json:"type"`                  // "incident_updated", "goal_updated", etc.
	IncidentID uuid.UUID   `json:"incident_id,omitempty"` // Which incident this message is about
	GoalID     uuid.UUID   `json:"goal_id,omitempty"`     // Which goal this message is about
	Data       interface{} `json:"data"`                  // Message payload
	UserID     uuid.UUID   `json:"user_id"`               // User who triggered the change
	Timestamp  int64       `json:"timestamp"`             // Unix timestamp
}

// WSClient represents a WebSocket client connection
type WSClient struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	UserName        string
	IncidentID      uuid.UUID // Which incident this client is subscribed to (empty for broadcast clients)
	IsBroadcast     bool      // True if this is a broadcast client (incident list page)
	GoalID          uuid.UUID // Which goal this client is subscribed to (empty for non-goal clients)
	IsGoalClient    bool      // True if this is a goal-specific client
	IsGoalBroadcast bool      // True if this is a goal broadcast client (goal list page)
	Conn            *websocket.Conn
	Send            chan WSMessage
	Hub             *WSHub
}

// WSHub manages all WebSocket connections
type WSHub struct {
	// Registered clients by client ID
	clients map[uuid.UUID]*WSClient

	// Map of incident ID to set of clients subscribed to it
	incidents map[uuid.UUID]map[uuid.UUID]*WSClient

	// Broadcast clients (subscribed to all incident updates)
	broadcastClients map[uuid.UUID]*WSClient

	// Map of goal ID to set of clients subscribed to it
	goals map[uuid.UUID]map[uuid.UUID]*WSClient

	// Goal broadcast clients (subscribed to all goal updates)
	goalBroadcastClients map[uuid.UUID]*WSClient

	// Register requests from clients
	register chan *WSClient

	// Unregister requests from clients
	unregister chan *WSClient

	// Broadcast messages to specific incident subscribers
	broadcast chan WSMessage

	// Mutex for thread-safe access
	mu sync.RWMutex
}

// NewWSHub creates a new WebSocket hub
func NewWSHub() *WSHub {
	return &WSHub{
		clients:              make(map[uuid.UUID]*WSClient),
		incidents:            make(map[uuid.UUID]map[uuid.UUID]*WSClient),
		broadcastClients:     make(map[uuid.UUID]*WSClient),
		goals:                make(map[uuid.UUID]map[uuid.UUID]*WSClient),
		goalBroadcastClients: make(map[uuid.UUID]*WSClient),
		register:             make(chan *WSClient),
		unregister:           make(chan *WSClient),
		broadcast:            make(chan WSMessage, 256),
	}
}

// Run starts the hub's main loop
func (h *WSHub) Run() {
	// Periodic stats logging
	ticker := time.NewTicker(2 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// Log connection statistics every 2 minutes
			h.LogConnectionStats()

		case client := <-h.register:
			h.mu.Lock()
			h.clients[client.ID] = client

			if client.IsGoalBroadcast {
				// Goal broadcast client (goal list page)
				h.goalBroadcastClients[client.ID] = client
				fmt.Printf("[WSHub] Goal broadcast client %s registered (User: %s)\n",
					client.ID, client.UserName)
			} else if client.IsGoalClient {
				// Goal-specific client
				if _, ok := h.goals[client.GoalID]; !ok {
					h.goals[client.GoalID] = make(map[uuid.UUID]*WSClient)
				}
				h.goals[client.GoalID][client.ID] = client
				fmt.Printf("[WSHub] Client %s registered for goal %s (User: %s)\n",
					client.ID, client.GoalID, client.UserName)
			} else if client.IsBroadcast {
				// Broadcast client (incident list page)
				h.broadcastClients[client.ID] = client
				fmt.Printf("[WSHub] Broadcast client %s registered (User: %s)\n",
					client.ID, client.UserName)
			} else {
				// Incident-specific client
				if _, ok := h.incidents[client.IncidentID]; !ok {
					h.incidents[client.IncidentID] = make(map[uuid.UUID]*WSClient)
				}
				h.incidents[client.IncidentID][client.ID] = client

				fmt.Printf("[WSHub] Client %s registered for incident %s (User: %s)\n",
					client.ID, client.IncidentID, client.UserName)

				// Broadcast user joined to other viewers of this incident
				h.mu.Unlock()
				h.BroadcastToIncident(client.IncidentID, "user_joined", map[string]interface{}{
					"user_id":   client.UserID,
					"user_name": client.UserName,
				}, client.UserID)

				// Notify broadcast clients about viewer count change
				h.BroadcastViewerCount(client.IncidentID)
				h.mu.Lock()
			}
			h.logStats()
			h.mu.Unlock()

		case client := <-h.unregister:
			h.mu.Lock()
			if _, ok := h.clients[client.ID]; ok {
				delete(h.clients, client.ID)

				if client.IsGoalBroadcast {
					delete(h.goalBroadcastClients, client.ID)
					fmt.Printf("[WSHub] Goal broadcast client %s unregistered\n", client.ID)
				} else if client.IsGoalClient {
					if subscribers, ok := h.goals[client.GoalID]; ok {
						delete(subscribers, client.ID)
						if len(subscribers) == 0 {
							delete(h.goals, client.GoalID)
						}
					}
					fmt.Printf("[WSHub] Goal client %s unregistered\n", client.ID)
				} else if client.IsBroadcast {
					delete(h.broadcastClients, client.ID)
					fmt.Printf("[WSHub] Broadcast client %s unregistered\n", client.ID)
				} else {
					// Remove incident-specific client
					if subscribers, ok := h.incidents[client.IncidentID]; ok {
						delete(subscribers, client.ID)
						if len(subscribers) == 0 {
							delete(h.incidents, client.IncidentID)
						}
					}

					fmt.Printf("[WSHub] Client %s unregistered\n", client.ID)

					// Broadcast user left to other viewers
					h.mu.Unlock()
					h.BroadcastToIncident(client.IncidentID, "user_left", map[string]interface{}{
						"user_id":   client.UserID,
						"user_name": client.UserName,
					}, client.UserID)

					// Notify broadcast clients about viewer count change
					h.BroadcastViewerCount(client.IncidentID)
					h.mu.Lock()
				}

				close(client.Send)
			}
			h.mu.Unlock()

		case message := <-h.broadcast:
			h.mu.RLock()
			subscribers := h.incidents[message.IncidentID]
			h.mu.RUnlock()

			// Send to all subscribers of this incident (except the sender)
			for clientID, client := range subscribers {
				if client.UserID != message.UserID {
					select {
					case client.Send <- message:
						// Message sent successfully
					default:
						// Client's send buffer is full, close connection
						fmt.Printf("[WSHub] Client %s send buffer full, closing\n", clientID)
						h.mu.Lock()
						close(client.Send)
						delete(h.clients, clientID)
						delete(h.incidents[message.IncidentID], clientID)
						h.mu.Unlock()
					}
				}
			}
		}
	}
}

// BroadcastToIncident sends a message to all clients subscribed to an incident
func (h *WSHub) BroadcastToIncident(incidentID uuid.UUID, messageType string, data interface{}, userID uuid.UUID) {
	message := WSMessage{
		Type:       messageType,
		IncidentID: incidentID,
		Data:       data,
		UserID:     userID,
		Timestamp:  time.Now().Unix(),
	}

	select {
	case h.broadcast <- message:
		fmt.Printf("[WSHub] Broadcasting %s to incident %s\n", messageType, incidentID)
	default:
		fmt.Printf("[WSHub] Broadcast channel full, dropping message\n")
	}
}

// BroadcastToAll sends a message to all broadcast clients (incident list subscribers)
func (h *WSHub) BroadcastToAll(messageType string, data interface{}) {
	h.mu.RLock()
	broadcastClients := make([]*WSClient, 0, len(h.broadcastClients))
	for _, client := range h.broadcastClients {
		broadcastClients = append(broadcastClients, client)
	}
	h.mu.RUnlock()

	message := WSMessage{
		Type:      messageType,
		Data:      data,
		Timestamp: time.Now().Unix(),
	}

	// Send to all broadcast clients
	for _, client := range broadcastClients {
		select {
		case client.Send <- message:
			// Message sent successfully
		default:
			// Client send buffer full, skip
			fmt.Printf("[WSHub] Broadcast client %s send buffer full, skipping message\n", client.ID)
		}
	}
}

// BroadcastToUser sends a message to every live connection belonging to a single
// user, regardless of which page/subscription the connection is on. Used to nudge a
// logged-in user to refresh after their roles/permissions change.
func (h *WSHub) BroadcastToUser(userID uuid.UUID, messageType string, data interface{}) {
	h.mu.RLock()
	targets := make([]*WSClient, 0)
	for _, client := range h.clients {
		if client.UserID == userID {
			targets = append(targets, client)
		}
	}
	h.mu.RUnlock()

	if len(targets) == 0 {
		return
	}

	message := WSMessage{
		Type:      messageType,
		Data:      data,
		UserID:    userID,
		Timestamp: time.Now().Unix(),
	}

	for _, client := range targets {
		select {
		case client.Send <- message:
			// Message sent successfully
		default:
			// Client send buffer full, skip
			fmt.Printf("[WSHub] User client %s send buffer full, skipping message\n", client.ID)
		}
	}
}

// BroadcastToGoal sends a message to all clients subscribed to a goal
func (h *WSHub) BroadcastToGoal(goalID uuid.UUID, messageType string, data interface{}, userID uuid.UUID) {
	h.mu.RLock()
	subscribers := make([]*WSClient, 0)
	for _, client := range h.goals[goalID] {
		if client.UserID != userID {
			subscribers = append(subscribers, client)
		}
	}
	h.mu.RUnlock()

	if len(subscribers) == 0 {
		return
	}

	message := WSMessage{
		Type:      messageType,
		GoalID:    goalID,
		Data:      data,
		UserID:    userID,
		Timestamp: time.Now().Unix(),
	}

	for _, client := range subscribers {
		select {
		case client.Send <- message:
		default:
			fmt.Printf("[WSHub] Goal client %s send buffer full, skipping\n", client.ID)
		}
	}

	fmt.Printf("[WSHub] Broadcasting %s to goal %s (%d recipients)\n", messageType, goalID, len(subscribers))
}

// BroadcastGoalToAll sends a message to all goal broadcast clients (goal list subscribers)
func (h *WSHub) BroadcastGoalToAll(messageType string, data interface{}) {
	h.mu.RLock()
	clients := make([]*WSClient, 0, len(h.goalBroadcastClients))
	for _, client := range h.goalBroadcastClients {
		clients = append(clients, client)
	}
	h.mu.RUnlock()

	if len(clients) == 0 {
		return
	}

	message := WSMessage{
		Type:      messageType,
		Data:      data,
		Timestamp: time.Now().Unix(),
	}

	for _, client := range clients {
		select {
		case client.Send <- message:
		default:
			fmt.Printf("[WSHub] Goal broadcast client %s send buffer full, skipping\n", client.ID)
		}
	}
}

// logStats logs a summary of active connections (must be called with h.mu held)
func (h *WSHub) logStats() {
	incidentCount := 0
	for _, subs := range h.incidents {
		incidentCount += len(subs)
	}
	goalCount := 0
	for _, subs := range h.goals {
		goalCount += len(subs)
	}
	fmt.Printf("[WSHub] Active: Total=%d, IncBcast=%d, Inc=%d(%d), GoalBcast=%d, Goal=%d(%d)\n",
		len(h.clients), len(h.broadcastClients), incidentCount, len(h.incidents),
		len(h.goalBroadcastClients), goalCount, len(h.goals))
}

// ReadPump pumps messages from the websocket connection to the hub
func (c *WSClient) ReadPump() {
	defer func() {
		c.Hub.unregister <- c
		c.Conn.Close()
	}()

	c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	c.Conn.SetPongHandler(func(string) error {
		c.Conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})

	for {
		_, _, err := c.Conn.ReadMessage()
		if err != nil {
			// Check if it's an unexpected close error
			if !isNormalClose(err) {
				fmt.Printf("[WSClient] Unexpected close: %v\n", err)
			}
			break
		}
		// We don't process incoming messages from clients (server -> client only)
	}
}

// isNormalClose checks if the error is a normal WebSocket closure
func isNormalClose(err error) bool {
	if err == nil {
		return true
	}
	// Fiber WebSocket considers these normal closures
	closeErr := err.Error()
	return closeErr == "websocket: close 1000 (normal)" ||
		closeErr == "websocket: close 1001 (going away)" ||
		closeErr == "websocket: close sent"
}

// WritePump pumps messages from the hub to the websocket connection
func (c *WSClient) WritePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.Conn.Close()
	}()

	for {
		select {
		case message, ok := <-c.Send:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				// Hub closed the channel
				c.Conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			// Send JSON message
			if err := c.Conn.WriteJSON(message); err != nil {
				fmt.Printf("[WSClient] Failed to send message: %v\n", err)
				return
			}

		case <-ticker.C:
			c.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// GetSubscriberCount returns the number of clients subscribed to an incident
func (h *WSHub) GetSubscriberCount(incidentID uuid.UUID) int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if subscribers, ok := h.incidents[incidentID]; ok {
		return len(subscribers)
	}
	return 0
}

// BroadcastViewerCount sends viewer count update to all broadcast clients
func (h *WSHub) BroadcastViewerCount(incidentID uuid.UUID) {
	h.mu.RLock()
	count := 0
	if subscribers, ok := h.incidents[incidentID]; ok {
		count = len(subscribers)
	}
	broadcastClients := make([]*WSClient, 0, len(h.broadcastClients))
	for _, client := range h.broadcastClients {
		broadcastClients = append(broadcastClients, client)
	}
	h.mu.RUnlock()

	message := WSMessage{
		Type:       "viewer_count_update",
		IncidentID: incidentID,
		Data: map[string]interface{}{
			"incident_id":    incidentID,
			"active_viewers": count,
		},
		Timestamp: time.Now().Unix(),
	}

	// Send to all broadcast clients
	for _, client := range broadcastClients {
		select {
		case client.Send <- message:
			// Message sent
		default:
			// Client send buffer full, skip
			fmt.Printf("[WSHub] Broadcast client %s send buffer full\n", client.ID)
		}
	}
}

// GetConnectionStats returns detailed connection statistics
func (h *WSHub) GetConnectionStats() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()

	incidentStats := make(map[string]int)
	totalIncidentClients := 0
	for incidentID, subscribers := range h.incidents {
		count := len(subscribers)
		incidentStats[incidentID.String()] = count
		totalIncidentClients += count
	}

	goalStats := make(map[string]int)
	totalGoalClients := 0
	for goalID, subscribers := range h.goals {
		count := len(subscribers)
		goalStats[goalID.String()] = count
		totalGoalClients += count
	}

	return map[string]interface{}{
		"total_clients":            len(h.clients),
		"broadcast_clients":        len(h.broadcastClients),
		"incident_clients":         totalIncidentClients,
		"unique_incidents_watched": len(h.incidents),
		"incidents_detail":         incidentStats,
		"goal_broadcast_clients":   len(h.goalBroadcastClients),
		"goal_clients":             totalGoalClients,
		"unique_goals_watched":     len(h.goals),
		"goals_detail":             goalStats,
	}
}

// LogConnectionStats logs current connection statistics
func (h *WSHub) LogConnectionStats() {
	stats := h.GetConnectionStats()
	fmt.Printf("[WSHub] === Connection Statistics ===\n")
	fmt.Printf("[WSHub] Total Clients: %d\n", stats["total_clients"])
	fmt.Printf("[WSHub] Incident Broadcast: %d | Goal Broadcast: %d\n", stats["broadcast_clients"], stats["goal_broadcast_clients"])
	fmt.Printf("[WSHub] Incident Clients: %d (%d incidents) | Goal Clients: %d (%d goals)\n",
		stats["incident_clients"], stats["unique_incidents_watched"],
		stats["goal_clients"], stats["unique_goals_watched"])

	if details, ok := stats["incidents_detail"].(map[string]int); ok && len(details) > 0 {
		fmt.Printf("[WSHub] Per-Incident Breakdown:\n")
		for incidentID, count := range details {
			fmt.Printf("[WSHub]   - Incident %s: %d viewers\n", incidentID, count)
		}
	}
	if details, ok := stats["goals_detail"].(map[string]int); ok && len(details) > 0 {
		fmt.Printf("[WSHub] Per-Goal Breakdown:\n")
		for goalID, count := range details {
			fmt.Printf("[WSHub]   - Goal %s: %d viewers\n", goalID, count)
		}
	}
	fmt.Printf("[WSHub] ==============================\n")
}

// Register returns the register channel for adding clients
func (h *WSHub) Register() chan<- *WSClient {
	return h.register
}

// Unregister returns the unregister channel for removing clients
func (h *WSHub) Unregister() chan<- *WSClient {
	return h.unregister
}
