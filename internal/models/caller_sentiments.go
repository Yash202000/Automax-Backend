package models

import (
	"time"

	"github.com/google/uuid"
)

type CallerSentiment struct {
	ID        uuid.UUID `gorm:"type:uuid;default:gen_random_uuid();primaryKey"`
	CallerID  string    `gorm:"type:uuid;not null" json:"caller_id"`        // agent
	CalleeID  string    `gorm:"type:varchar(50);not null" json:"callee_id"` // customer
	Sentiment int       `gorm:"type:smallint;not null" json:"sentiment"`
	Feedback  string    `gorm:"type:text" json:"feedback,omitempty"`
	CallUUID  string    `gorm:"type:uuid;uniqueIndex" json:"call_uuid"`
	CreatedAt time.Time `gorm:"autoCreateTime"`
}

type CreateSentimentRequest struct {
	CalleeID  string `json:"callee_id" validate:"required"` // customer
	Sentiment int    `json:"sentiment" validate:"required,min=1,max=5"`
	Feedback  string `json:"feedback,omitempty" validate:"max=500"`
	CallUUID  string `json:"call_uuid" validate:"required,uuid"`
}
type SentimentCount struct {
	Sentiment int    `json:"sentiment"`
	Count     int    `json:"count"`
	Percent   int    `json:"percent"`
	CalleeID  string `json:"callee_id"`
	CallUUID  string `json:"call_uuid"`
}

type CallHistory struct {
	CallUUID  string    `json:"call_uuid"`
	CalleeID  string    `json:"callee_id"`
	Sentiment int       `json:"sentiment"`
	Feedback  string    `json:"feedback,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type CallerSentimentSummaryResponse struct {
	CallerID string           `json:"caller_id"`
	Summary  []SentimentCount `json:"summary"`
	Dominant int              `json:"dominant"`
	Calls    []CallHistory    `json:"calls"`
}

type SentimentAgg struct {
	Sentiment int
	Count     int
}
