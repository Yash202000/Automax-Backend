package database

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/automax/backend/internal/config"
	"github.com/redis/go-redis/v9"
)

var RedisClient *redis.Client

func ConnectRedis(cfg *config.RedisConfig) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", cfg.Host, cfg.Port),
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Ping(ctx).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	RedisClient = client
	log.Println("Redis connected successfully")
	return client, nil
}

func CloseRedis(client *redis.Client) error {
	return client.Close()
}

type SessionStore struct {
	client *redis.Client
}

func NewSessionStore(client *redis.Client) *SessionStore {
	return &SessionStore{client: client}
}

func (s *SessionStore) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, key, data, expiration).Err()
}

func (s *SessionStore) Get(ctx context.Context, key string, dest interface{}) error {
	data, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dest)
}

func (s *SessionStore) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, key).Err()
}

func (s *SessionStore) Exists(ctx context.Context, key string) (bool, error) {
	result, err := s.client.Exists(ctx, key).Result()
	if err != nil {
		return false, err
	}
	return result > 0, nil
}

func (s *SessionStore) SetUserSession(ctx context.Context, userID string, sessionData interface{}, expiration time.Duration) error {
	key := fmt.Sprintf("session:%s", userID)
	return s.Set(ctx, key, sessionData, expiration)
}

func (s *SessionStore) GetUserSession(ctx context.Context, userID string, dest interface{}) error {
	key := fmt.Sprintf("session:%s", userID)
	return s.Get(ctx, key, dest)
}

func (s *SessionStore) DeleteUserSession(ctx context.Context, userID string) error {
	key := fmt.Sprintf("session:%s", userID)
	return s.Delete(ctx, key)
}

func (s *SessionStore) BlacklistToken(ctx context.Context, token string, expiration time.Duration) error {
	key := fmt.Sprintf("blacklist:%s", token)
	return s.client.Set(ctx, key, "1", expiration).Err()
}

func (s *SessionStore) IsTokenBlacklisted(ctx context.Context, token string) (bool, error) {
	key := fmt.Sprintf("blacklist:%s", token)
	return s.Exists(ctx, key)
}

// SetSSONonce stores a single-use SSO nonce (jti) with the given TTL.
// Key pattern: sso:nonce:{jti}
func (s *SessionStore) SetSSONonce(ctx context.Context, jti string, ttl time.Duration) error {
	key := fmt.Sprintf("sso:nonce:%s", jti)
	return s.client.Set(ctx, key, "1", ttl).Err()
}

// ConsumeSSONonce atomically checks for and deletes an SSO nonce.
// Returns true if the nonce existed (first use), false if already consumed or expired.
// Use this for same-deployment SSO where provider and receiver share the same Redis.
func (s *SessionStore) ConsumeSSONonce(ctx context.Context, jti string) (bool, error) {
	key := fmt.Sprintf("sso:nonce:%s", jti)
	pipe := s.client.Pipeline()
	getCmd := pipe.Get(ctx, key)
	pipe.Del(ctx, key)
	_, err := pipe.Exec(ctx)
	if err != nil && err != redis.Nil {
		return false, err
	}
	val, err := getCmd.Result()
	if err == redis.Nil || val == "" {
		return false, nil
	}
	return true, nil
}

// ClaimSSONonce atomically claims a JTI on first use via SET NX.
// Returns true if this is the first time this JTI has been seen (valid).
// Returns false if the JTI was already claimed (replay attack).
//
// Use this in the SSO callback receiver — works for both same-deployment and
// cross-deployment SSO because it tracks consumed JTIs independently in the
// receiver's own Redis, without relying on the provider having pre-stored the nonce.
func (s *SessionStore) ClaimSSONonce(ctx context.Context, jti string, ttl time.Duration) (bool, error) {
	key := fmt.Sprintf("sso:nonce:claimed:%s", jti)
	ok, err := s.client.SetNX(ctx, key, "1", ttl).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}

// ResetLoginAttempts resets the login attempt counters for an identifier
// This should be called on successful login
func ResetLoginAttempts(ctx context.Context, client *redis.Client, identifier string) error {
	// Use pipeline to delete both keys atomically
	pipe := client.Pipeline()
	
	// Remove attempt counter
	attemptsKey := fmt.Sprintf("login_attempts:%s", identifier)
	pipe.Del(ctx, attemptsKey)
	
	// Remove block if exists
	blockKey := fmt.Sprintf("login_block:%s", identifier)
	pipe.Del(ctx, blockKey)
	
	_, err := pipe.Exec(ctx)
	return err
}
