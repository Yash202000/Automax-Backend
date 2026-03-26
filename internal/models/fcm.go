package models

import (
	"github.com/google/uuid"
)

type PushRequest struct {
	UserID   uuid.UUID         `json:"user_id"`
	Title    string            `json:"title"`
	Body     string            `json:"body"`
	UserType string            `json:"user_type,omitempty"`
	Data     map[string]string `json:"data,omitempty"`
	SentBy   *uuid.UUID        `json:"-"`
}
