package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
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
	Channel      string          `gorm:"size:20;not null;index" json:"channel"`
	TemplateCode string          `gorm:"size:100;index" json:"template_code"`
	Language     string          `gorm:"size:10" json:"language"`
	Recipients   RecipientArray  `gorm:"type:jsonb;not null" json:"recipients"` // All recipients with status (TO, CC, BCC)
	CC           StringArray     `gorm:"type:jsonb" json:"cc,omitempty"`
	BCC          StringArray     `gorm:"type:jsonb" json:"bcc,omitempty"`
	Subject      string          `gorm:"type:text;index" json:"subject,omitempty"`
	Body         string          `gorm:"type:text;not null" json:"body"`
	Status       string          `gorm:"size:20;not null;index" json:"status"` // sent | failed | mock-sent | partial
	Provider     string          `gorm:"size:50" json:"provider"`              // smtp | twilio | mock
	ErrorMessage string          `gorm:"type:text" json:"error_message,omitempty"`
	Attachments  AttachmentArray `gorm:"type:jsonb" json:"attachments,omitempty"`
	SentBy       *uuid.UUID      `gorm:"type:uuid;index" json:"sent_by,omitempty"` // User who sent it
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
	TemplateCode string           `json:"template_code"`
	Language     string           `json:"language"`
	Recipients   []RecipientInfo  `json:"recipients"`
	CC           []string         `json:"cc,omitempty"`
	BCC          []string         `json:"bcc,omitempty"`
	Subject      string           `json:"subject,omitempty"`
	Body         string           `json:"body"`
	Status       string           `json:"status"`
	Provider     string           `json:"provider"`
	ErrorMessage string           `json:"error_message,omitempty"`
	Attachments  []AttachmentInfo `json:"attachments,omitempty"`
	SentBy       *uuid.UUID       `json:"sent_by,omitempty"`
	CreatedAt    time.Time        `json:"created_at"`
	UpdatedAt    *time.Time       `json:"updated_at,omitempty"`
}

// NotificationLogFilter for filtering and searching notifications
type NotificationLogFilter struct {
	Channel      string     `json:"channel"`
	Status       string     `json:"status"`
	Search       string     `json:"search"` // Search across subject, body, recipient, cc, bcc
	SentBy       *uuid.UUID `json:"sent_by"`
	StartDate    *time.Time `json:"start_date"`
	EndDate      *time.Time `json:"end_date"`
	TemplateCode string     `json:"template_code"`
	Page         int        `json:"page"`
	Limit        int        `json:"limit"`
}

// ToNotificationLogResponse converts NotificationLog to response
func ToNotificationLogResponse(log *NotificationLog) NotificationLogResponse {
	return NotificationLogResponse{
		ID:           log.ID,
		Channel:      log.Channel,
		TemplateCode: log.TemplateCode,
		Language:     log.Language,
		Recipients:   log.Recipients,
		CC:           log.CC,
		BCC:          log.BCC,
		Subject:      log.Subject,
		Body:         log.Body,
		Status:       log.Status,
		Provider:     log.Provider,
		ErrorMessage: log.ErrorMessage,
		Attachments:  log.Attachments,
		SentBy:       log.SentBy,
		CreatedAt:    log.CreatedAt,
		UpdatedAt:    log.UpdatedAt,
	}
}

type AttachmentData struct {
	Filename    string
	ContentType string
	Data        []byte
}
