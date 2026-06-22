package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/automax/backend/pkg/i18n"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// PresenceInfo represents information about a user viewing an incident
type PresenceInfo struct {
	UserID    uuid.UUID `json:"user_id"`
	UserName  string    `json:"user_name"`
	UserEmail string    `json:"user_email"`
	Timestamp time.Time `json:"timestamp"`
}

// PresenceService manages user presence tracking for incidents
type PresenceService interface {
	MarkPresence(ctx context.Context, incidentID uuid.UUID, user PresenceInfo) error
	RemovePresence(ctx context.Context, incidentID uuid.UUID, userID uuid.UUID) error
	GetActiveUsers(ctx context.Context, incidentID uuid.UUID) ([]PresenceInfo, error)
}

type presenceService struct {
	redis *redis.Client
}

// NewPresenceService creates a new presence service
func NewPresenceService(redis *redis.Client) PresenceService {
	return &presenceService{redis: redis}
}

// MarkPresence marks a user as actively viewing an incident
func (s *presenceService) MarkPresence(ctx context.Context, incidentID uuid.UUID, user PresenceInfo) error {
	key := fmt.Sprintf("incident:presence:%s", incidentID.String())
	field := user.UserID.String()
	user.Timestamp = time.Now()

	data, err := json.Marshal(user)
	if err != nil {
		return fmt.Errorf("failed to marshal presence info: %w", err)
	}

	// Set the user's presence
	if err := s.redis.HSet(ctx, key, field, data).Err(); err != nil {
		return fmt.Errorf("%s: %w", i18n.T(ctx, "failed_to_set_presence"), err)
	}

	// Set TTL for automatic cleanup (5 minutes)
	if err := s.redis.Expire(ctx, key, 5*time.Minute).Err(); err != nil {
		return fmt.Errorf("%s: %w", i18n.T(ctx, "failed_to_set_presence_ttl"), err)
	}

	return nil
}

// RemovePresence removes a user's presence from an incident
func (s *presenceService) RemovePresence(ctx context.Context, incidentID uuid.UUID, userID uuid.UUID) error {
	key := fmt.Sprintf("incident:presence:%s", incidentID.String())
	field := userID.String()

	if err := s.redis.HDel(ctx, key, field).Err(); err != nil {
		return fmt.Errorf("%s: %w", i18n.T(ctx, "failed_to_remove_presence_svc"), err)
	}

	return nil
}

// GetActiveUsers retrieves all users currently viewing an incident
// Only includes users whose last presence update was within the last 3 minutes
func (s *presenceService) GetActiveUsers(ctx context.Context, incidentID uuid.UUID) ([]PresenceInfo, error) {
	key := fmt.Sprintf("incident:presence:%s", incidentID.String())

	data, err := s.redis.HGetAll(ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(ctx, "failed_to_get_presence_svc"), err)
	}

	var users []PresenceInfo
	cutoffTime := time.Now().Add(-3 * time.Minute)

	for _, value := range data {
		var user PresenceInfo
		if err := json.Unmarshal([]byte(value), &user); err != nil {
			// Skip invalid entries
			continue
		}

		// Only include if timestamp is within last 3 minutes (active)
		if user.Timestamp.After(cutoffTime) {
			users = append(users, user)
		}
	}

	return users, nil
}
