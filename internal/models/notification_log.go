package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Email Categories
const (
	CategoryInbox  = "inbox"
	CategorySent   = "sent"
	CategoryDraft  = "draft"
	CategoryOutbox = "outbox"
	CategoryTrash  = "trash"
	CategorySpam   = "spam"
)

// Email Directions
const (
	DirectionInbound  = "inbound"
	DirectionOutbound = "outbound"
)

// Email Status
const (
	StatusDraft     = "draft"
	StatusPending   = "pending"
	StatusSent      = "sent"
	StatusFailed    = "failed"
	StatusPartial   = "partial"
	StatusMockSent  = "mock-sent"
	StatusScheduled = "scheduled"
)

// RecipientInfo stores individual recipient delivery status
type RecipientInfo struct {
	Email  string `json:"email"`
	Type   string `json:"type"`   // "to" | "cc" | "bcc"
	Status string `json:"status"` // "success" | "failed"
	Error  string `json:"error,omitempty"`
}

// RecipientArray is a custom type for storing recipient info as JSONB
type RecipientArray []RecipientInfo

// Value implements the driver.Valuer interface
func (r RecipientArray) Value() (driver.Value, error) {
	return json.Marshal(r)
}

// Scan implements the sql.Scanner interface
func (r *RecipientArray) Scan(value interface{}) error {
	if value == nil {
		*r = nil
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("unsupported type: %T", value)
	}

	return json.Unmarshal(bytes, r)
}

// AttachmentInfo stores attachment metadata
type AttachmentInfo struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	URL         string `json:"url,omitempty"` // If stored externally
}

// AttachmentArray is a custom type for storing attachments as JSONB
type AttachmentArray []AttachmentInfo

// Value implements the driver.Valuer interface
func (a AttachmentArray) Value() (driver.Value, error) {
	return json.Marshal(a)
}

// Scan implements the sql.Scanner interface
func (a *AttachmentArray) Scan(value interface{}) error {
	if value == nil {
		*a = nil
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("unsupported type: %T", value)
	}

	return json.Unmarshal(bytes, a)
}

// StringArray for CC and BCC fields
type StringArray []string

// Value implements the driver.Valuer interface
func (s StringArray) Value() (driver.Value, error) {
	return json.Marshal(s)
}

// Scan implements the sql.Scanner interface
func (s *StringArray) Scan(value interface{}) error {
	if value == nil {
		*s = nil
		return nil
	}

	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("unsupported type: %T", value)
	}

	return json.Unmarshal(bytes, s)
}

type NotificationLog struct {
	ID           uuid.UUID       `gorm:"type:uuid;primaryKey" json:"id"`
	Channel      string          `gorm:"size:20;not null;index" json:"channel"` // email | sms
	Direction    string          `gorm:"size:10;not null;index;default:'outbound'" json:"direction"` // inbound | outbound
	Category     string          `gorm:"size:20;not null;index;default:'sent'" json:"category"` // inbox | sent | draft | outbox | trash | spam
	TemplateCode string          `gorm:"size:100;index" json:"template_code"`
	Language     string          `gorm:"size:10" json:"language"`
	Recipients   RecipientArray  `gorm:"type:jsonb;not null" json:"recipients"` // All recipients with status (TO, CC, BCC)
	CC           StringArray     `gorm:"type:jsonb" json:"cc,omitempty"`
	BCC          StringArray     `gorm:"type:jsonb" json:"bcc,omitempty"`
	From         string          `gorm:"size:255;index" json:"from,omitempty"` // Sender email (for inbound)
	Subject      string          `gorm:"type:text;index" json:"subject,omitempty"`
	Body         string          `gorm:"type:text;not null" json:"body"`
	BodyHTML     string          `gorm:"type:text" json:"body_html,omitempty"` // HTML version of body
	Status       string          `gorm:"size:20;not null;index" json:"status"` // sent | failed | mock-sent | partial | draft | pending
	Provider     string          `gorm:"size:50" json:"provider"` // smtp | twilio | mock | imap
	ErrorMessage string          `gorm:"type:text" json:"error_message,omitempty"`
	Attachments  AttachmentArray `gorm:"type:jsonb" json:"attachments,omitempty"`
	IsRead       bool            `gorm:"default:false;index" json:"is_read"` // For inbox emails
	IsStarred    bool            `gorm:"default:false;index" json:"is_starred"` // Star/flag important emails
	ThreadID     *uuid.UUID      `gorm:"type:uuid;index" json:"thread_id,omitempty"` // For email threading
	InReplyTo    *uuid.UUID      `gorm:"type:uuid;index" json:"in_reply_to,omitempty"` // Reply to which email
	SentBy       *uuid.UUID      `gorm:"type:uuid;index" json:"sent_by,omitempty"` // User who sent it (outbound)
	ReceivedBy   *uuid.UUID      `gorm:"type:uuid;index" json:"received_by,omitempty"` // User who received it (inbound)
	ScheduledAt  *time.Time      `json:"scheduled_at,omitempty"` // For scheduled sending (outbox)
	SentAt       *time.Time      `gorm:"index" json:"sent_at,omitempty"` // Actual sent time
	CreatedAt    time.Time       `gorm:"index" json:"created_at"`
	UpdatedAt    *time.Time      `json:"updated_at,omitempty"`
	DeletedAt    gorm.DeletedAt  `gorm:"index" json:"-"`
}

func (l *NotificationLog) BeforeCreate(tx *gorm.DB) error {
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	return nil
}

// NotificationLogResponse for API responses
type NotificationLogResponse struct {
	ID           uuid.UUID        `json:"id"`
	Channel      string           `json:"channel"`
	Direction    string           `json:"direction"`
	Category     string           `json:"category"`
	TemplateCode string           `json:"template_code"`
	Language     string           `json:"language"`
	Recipients   []RecipientInfo  `json:"recipients"`
	CC           []string         `json:"cc,omitempty"`
	BCC          []string         `json:"bcc,omitempty"`
	From         string           `json:"from,omitempty"`
	Subject      string           `json:"subject,omitempty"`
	Body         string           `json:"body"`
	BodyHTML     string           `json:"body_html,omitempty"`
	Status       string           `json:"status"`
	Provider     string           `json:"provider"`
	ErrorMessage string           `json:"error_message,omitempty"`
	Attachments  []AttachmentInfo `json:"attachments,omitempty"`
	IsRead       bool             `json:"is_read"`
	IsStarred    bool             `json:"is_starred"`
	ThreadID     *uuid.UUID       `json:"thread_id,omitempty"`
	InReplyTo    *uuid.UUID       `json:"in_reply_to,omitempty"`
	SentBy       *uuid.UUID       `json:"sent_by,omitempty"`
	ReceivedBy   *uuid.UUID       `json:"received_by,omitempty"`
	ScheduledAt  *time.Time       `json:"scheduled_at,omitempty"`
	SentAt       *time.Time       `json:"sent_at,omitempty"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    *time.Time       `json:"updated_at,omitempty"`
}

// NotificationLogFilter for filtering and searching notifications
type NotificationLogFilter struct {
	Channel      string     `json:"channel"`
	Direction    string     `json:"direction"` // inbound | outbound
	Category     string     `json:"category"`  // inbox | sent | draft | outbox | trash | spam
	Status       string     `json:"status"`
	IsRead       *bool      `json:"is_read"`     // Filter by read/unread
	IsStarred    *bool      `json:"is_starred"`  // Filter by starred
	Search       string     `json:"search"`      // Search across subject, body, from, recipients
	SentBy       *uuid.UUID `json:"sent_by"`     // Filter by sender (outbound)
	ReceivedBy   *uuid.UUID `json:"received_by"` // Filter by receiver (inbound)
	StartDate    *time.Time `json:"start_date"`
	EndDate      *time.Time `json:"end_date"`
	TemplateCode string     `json:"template_code"`
	ThreadID     *uuid.UUID `json:"thread_id"` // Get all emails in a thread
	Page         int        `json:"page"`
	Limit        int        `json:"limit"`
}

// ToNotificationLogResponse converts NotificationLog to response
func ToNotificationLogResponse(log *NotificationLog) NotificationLogResponse {
	return NotificationLogResponse{
		ID:           log.ID,
		Channel:      log.Channel,
		Direction:    log.Direction,
		Category:     log.Category,
		TemplateCode: log.TemplateCode,
		Language:     log.Language,
		Recipients:   log.Recipients,
		CC:           log.CC,
		BCC:          log.BCC,
		From:         log.From,
		Subject:      log.Subject,
		Body:         log.Body,
		BodyHTML:     log.BodyHTML,
		Status:       log.Status,
		Provider:     log.Provider,
		ErrorMessage: log.ErrorMessage,
		Attachments:  log.Attachments,
		IsRead:       log.IsRead,
		IsStarred:    log.IsStarred,
		ThreadID:     log.ThreadID,
		InReplyTo:    log.InReplyTo,
		SentBy:       log.SentBy,
		ReceivedBy:   log.ReceivedBy,
		ScheduledAt:  log.ScheduledAt,
		SentAt:       log.SentAt,
		CreatedAt:    log.CreatedAt,
		UpdatedAt:    log.UpdatedAt,
	}
}

// CreateDraftRequest for creating draft emails
type CreateDraftRequest struct {
	Channel      string            `json:"channel" validate:"required"`
	To           []string          `json:"to"`
	CC           []string          `json:"cc,omitempty"`
	BCC          []string          `json:"bcc,omitempty"`
	Subject      string            `json:"subject"`
	Body         string            `json:"body"`
	BodyHTML     string            `json:"body_html,omitempty"`
	Attachments  []AttachmentData  `json:"attachments,omitempty"`
	ScheduledAt  *time.Time        `json:"scheduled_at,omitempty"`
}

// UpdateDraftRequest for updating draft emails
type UpdateDraftRequest struct {
	To           []string          `json:"to,omitempty"`
	CC           []string          `json:"cc,omitempty"`
	BCC          []string          `json:"bcc,omitempty"`
	Subject      string            `json:"subject,omitempty"`
	Body         string            `json:"body,omitempty"`
	BodyHTML     string            `json:"body_html,omitempty"`
	Attachments  []AttachmentData  `json:"attachments,omitempty"`
	ScheduledAt  *time.Time        `json:"scheduled_at,omitempty"`
}

type AttachmentData struct {
	Filename    string
	ContentType string
	Data        []byte
}
