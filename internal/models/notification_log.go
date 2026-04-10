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

// NotificationMeta stores metadata for in-app notifications
type NotificationMeta struct {
	ID   string `json:"id,omitempty"`
	Type string `json:"type,omitempty"` // INCIDENT | REQUEST | COMPLAINT | QUERY
}

// Value implements the driver.Valuer interface
func (m NotificationMeta) Value() (driver.Value, error) {
	return json.Marshal(m)
}

// Scan implements the sql.Scanner interface
func (m *NotificationMeta) Scan(value interface{}) error {
	if value == nil {
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
	return json.Unmarshal(bytes, m)
}

// RecipientInfo stores individual recipient delivery status
type RecipientInfo struct {
	Email        string `json:"email"`
	Channel      string `json:"channel"`
	Type         string `json:"type"`   // "to" | "cc" | "bcc"
	Status       string `json:"status"` // "success" | "failed"
	Error        string `json:"error,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

type MetaErrorResponse struct {
	Error struct {
		Message   string `json:"message"`
		Type      string `json:"type"`
		Code      int    `json:"code"`
		FBTraceID string `json:"fbtrace_id"`
	} `json:"error"`
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
	ID          string `json:"id,omitempty"` // UUID for attachment
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	URL         string `json:"url,omitempty"`          // API URL for download
	PreviewURL  string `json:"preview_url,omitempty"`  // API URL for preview
	StoragePath string `json:"storage_path,omitempty"` // Internal MinIO path (not exposed in response)
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
	ID           uuid.UUID      `gorm:"type:uuid;primaryKey" json:"id"`
	Channel      string         `gorm:"size:20;not null;index" json:"channel"`                      // email | sms |push-notification
	Direction    string         `gorm:"size:10;not null;index;default:'outbound'" json:"direction"` // inbound | outbound
	Category     string         `gorm:"size:20;not null;index;default:'sent'" json:"category"`      // inbox | sent | draft | outbox | trash | spam
	TemplateCode string         `gorm:"size:100;index" json:"template_code"`
	Language     string         `gorm:"size:10" json:"language"`
	Recipients   RecipientArray `gorm:"type:jsonb;not null" json:"recipients"` // All recipients with status (TO, CC, BCC)

	Subject  string `gorm:"type:text;index" json:"subject,omitempty"`
	Body     string `gorm:"type:text;not null" json:"body"`
	BodyHTML string `gorm:"type:text" json:"body_html,omitempty"` // HTML version of body
	Status   string `gorm:"size:20;not null;index" json:"status"` // sent | failed | mock-sent | partial | draft | pending

	Provider       string            `gorm:"size:50" json:"provider"`
	OTPSessionID   *uuid.UUID        `gorm:"type:uuid;index" json:"otp_session_id,omitempty"`
	OTPVerified    bool              `gorm:"default:false;index" json:"otp_verified"`
	OTPVerifiedAt  *time.Time        `gorm:"index" json:"otp_verified_at,omitempty"`
	CC             StringArray       `gorm:"type:jsonb" json:"cc,omitempty"`
	BCC            StringArray       `gorm:"type:jsonb" json:"bcc,omitempty"`
	From           string            `gorm:"size:255;index" json:"from,omitempty"` // Sender email (for inbound)
	ErrorMessage   string            `gorm:"type:text" json:"error_message,omitempty"`
	Attachments    AttachmentArray   `gorm:"type:jsonb" json:"attachments,omitempty"`
	IsRead         bool              `gorm:"default:false;index" json:"is_read"`           // For inbox emails
	IsStarred      bool              `gorm:"default:false;index" json:"is_starred"`        // Star/flag important emails
	ThreadID       *uuid.UUID        `gorm:"type:uuid;index" json:"thread_id,omitempty"`   // For email threading
	InReplyTo      *uuid.UUID        `gorm:"type:uuid;index" json:"in_reply_to,omitempty"` // Reply to which email
	Meta           *NotificationMeta `gorm:"type:jsonb" json:"meta,omitempty"`             // Contextual metadata (record id, type)
	SentBy         *uuid.UUID        `gorm:"type:uuid;index" json:"sent_by,omitempty"`     // User who sent it (outbound)
	ReceivedBy     *uuid.UUID        `gorm:"type:uuid;index" json:"received_by,omitempty"` // User who received it (inbound)
	ScheduledAt    *time.Time        `json:"scheduled_at,omitempty"`                       // For scheduled sending (outbox)
	SentAt         *time.Time        `gorm:"index" json:"sent_at,omitempty"`               // Actual sent time
	CreatedAt      time.Time         `gorm:"index" json:"created_at"`
	UpdatedAt      *time.Time        `json:"updated_at,omitempty"`
	DeletedAt      gorm.DeletedAt    `gorm:"index" json:"-"`
	SentByUser     *User             `gorm:"foreignKey:SentBy;references:ID" json:"sent_by_user,omitempty"`         // User details who sent it
	ReceivedByUser *User             `gorm:"foreignKey:ReceivedBy;references:ID" json:"received_by_user,omitempty"` // User details who received it
}

func (l *NotificationLog) BeforeCreate(tx *gorm.DB) error {
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	return nil
}

// UserBasicInfo contains basic user information for notification responses
type UserBasicInfo struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Avatar    string    `json:"avatar,omitempty"`
}

// NotificationLogResponse for API responses
type NotificationLogResponse struct {
	ID             uuid.UUID         `json:"id"`
	Channel        string            `json:"channel"`
	Direction      string            `json:"direction"`
	Category       string            `json:"category"`
	TemplateCode   string            `json:"template_code"`
	Language       string            `json:"language"`
	Recipients     []RecipientInfo   `json:"recipients"`
	CC             []string          `json:"cc,omitempty"`
	BCC            []string          `json:"bcc,omitempty"`
	From           string            `json:"from,omitempty"`
	Subject        string            `json:"subject,omitempty"`
	Body           string            `json:"body"`
	BodyHTML       string            `json:"body_html,omitempty"`
	Status         string            `json:"status"`
	Provider       string            `json:"provider"`
	ErrorMessage   string            `json:"error_message,omitempty"`
	Attachments    []AttachmentInfo  `json:"attachments,omitempty"`
	Meta           *NotificationMeta `json:"meta,omitempty"`
	IsRead         bool              `json:"is_read"`
	IsStarred      bool              `json:"is_starred"`
	ThreadID       *uuid.UUID        `json:"thread_id,omitempty"`
	InReplyTo      *uuid.UUID        `json:"in_reply_to,omitempty"`
	SentBy         *uuid.UUID        `json:"sent_by,omitempty"`
	SentByUser     *UserBasicInfo    `json:"sent_by_user,omitempty"`
	ReceivedBy     *uuid.UUID        `json:"received_by,omitempty"`
	ReceivedByUser *UserBasicInfo    `json:"received_by_user,omitempty"`
	ScheduledAt    *time.Time        `json:"scheduled_at,omitempty"`
	SentAt         *time.Time        `json:"sent_at,omitempty"`
	CreatedAt      time.Time         `json:"created_at"`
	UpdatedAt      *time.Time        `json:"updated_at,omitempty"`
}

// NotificationLogFilter for filtering and searching notifications
type NotificationLogFilter struct {
	Channel      string     `query:"channel" json:"channel" validate:"omitempty,oneof=email sms notification push-notification"`            // email | sms | notification | push-notification
	Direction    string     `query:"direction" json:"direction" validate:"omitempty,oneof=inbound outbound"`                                // inbound | outbound
	Category     string     `query:"category" json:"category" validate:"omitempty,oneof=inbox sent draft outbox trash spam"`                // inbox | sent | draft | outbox | trash | spam
	Status       string     `query:"status" json:"status" validate:"omitempty,oneof=sent failed mock-sent partial draft pending scheduled"` // sent | failed | mock-sent | partial | draft | pending | scheduled
	IsRead       *bool      `query:"is_read" json:"is_read" validate:"omitempty"`                                                           // Filter by read/unread
	IsStarred    *bool      `query:"is_starred" json:"is_starred" validate:"omitempty"`                                                     // Filter by starred
	Search       string     `query:"search" json:"search" validate:"omitempty,max=255"`                                                     // Search across subject, body, from, recipients
	UserID       *uuid.UUID `json:"user_id"`                                                                                                // Filter by current user (sent_by OR received_by)
	SentBy       *uuid.UUID `query:"sent_by" json:"sent_by" validate:"omitempty"`                                                           // Filter by sender (outbound)
	ReceivedBy   *uuid.UUID `query:"received_by" json:"received_by" validate:"omitempty"`                                                   // Filter by receiver (inbound)
	StartDate    *time.Time `query:"start_date" json:"start_date" validate:"omitempty"`                                                     // Filter by sent_at >= start_date
	EndDate      *time.Time `query:"end_date" json:"end_date" validate:"omitempty"`
	TemplateCode string     `query:"template_code" json:"template_code" validate:"omitempty,max=100"` // Filter by template code
	ThreadID     *uuid.UUID `query:"thread_id" json:"thread_id" validate:"omitempty"`                 // Get all emails in a thread
	Page         int        `query:"page" json:"page" validate:"omitempty,min=1"`                     // Pagination page number
	Limit        int        `query:"limit" json:"limit" validate:"omitempty,min=1,max=100"`           // Pagination page size
}

// ToNotificationLogResponse converts NotificationLog to response
func ToNotificationLogResponse(log *NotificationLog) NotificationLogResponse {
	// Transform attachments to include API URLs
	responseAttachments := make([]AttachmentInfo, len(log.Attachments))
	for i, att := range log.Attachments {
		responseAttachments[i] = AttachmentInfo{
			ID:          att.ID,
			Filename:    att.Filename,
			ContentType: att.ContentType,
			Size:        att.Size,
			URL:         fmt.Sprintf("/api/v1/attachments/%s", att.ID),
			PreviewURL:  fmt.Sprintf("/api/v1/attachments/%s/preview", att.ID),
			// Don't expose StoragePath in response
		}
	}

	// Transform user data
	var sentByUser *UserBasicInfo
	if log.SentByUser != nil {
		sentByUser = &UserBasicInfo{
			ID:        log.SentByUser.ID,
			Email:     log.SentByUser.Email,
			FirstName: log.SentByUser.FirstName,
			LastName:  log.SentByUser.LastName,
			Avatar:    log.SentByUser.Avatar,
		}
	}

	var receivedByUser *UserBasicInfo
	if log.ReceivedByUser != nil {
		receivedByUser = &UserBasicInfo{
			ID:        log.ReceivedByUser.ID,
			Email:     log.ReceivedByUser.Email,
			FirstName: log.ReceivedByUser.FirstName,
			LastName:  log.ReceivedByUser.LastName,
			Avatar:    log.ReceivedByUser.Avatar,
		}
	}

	return NotificationLogResponse{
		ID:             log.ID,
		Channel:        log.Channel,
		Direction:      log.Direction,
		Category:       log.Category,
		TemplateCode:   log.TemplateCode,
		Language:       log.Language,
		Recipients:     log.Recipients,
		CC:             log.CC,
		BCC:            log.BCC,
		From:           log.From,
		Subject:        log.Subject,
		Body:           log.Body,
		BodyHTML:       log.BodyHTML,
		Status:         log.Status,
		Provider:       log.Provider,
		ErrorMessage:   log.ErrorMessage,
		Attachments:    responseAttachments,
		Meta:           log.Meta,
		IsRead:         log.IsRead,
		IsStarred:      log.IsStarred,
		ThreadID:       log.ThreadID,
		InReplyTo:      log.InReplyTo,
		SentBy:         log.SentBy,
		SentByUser:     sentByUser,
		ReceivedBy:     log.ReceivedBy,
		ReceivedByUser: receivedByUser,
		ScheduledAt:    log.ScheduledAt,
		SentAt:         log.SentAt,
		CreatedAt:      log.CreatedAt,
		UpdatedAt:      log.UpdatedAt,
	}
}

// CreateDraftRequest for creating draft emails
type CreateDraftRequest struct {
	Channel     string           `json:"channel" validate:"required"`
	To          []string         `json:"to"`
	CC          []string         `json:"cc,omitempty"`
	BCC         []string         `json:"bcc,omitempty"`
	Subject     string           `json:"subject"`
	Body        string           `json:"body"`
	BodyHTML    string           `json:"body_html,omitempty"`
	Attachments []AttachmentData `json:"attachments,omitempty"`
	ScheduledAt *time.Time       `json:"scheduled_at,omitempty"`
}

// UpdateDraftRequest for updating draft emails
type UpdateDraftRequest struct {
	To          []string         `json:"to,omitempty"`
	CC          []string         `json:"cc,omitempty"`
	BCC         []string         `json:"bcc,omitempty"`
	Subject     string           `json:"subject,omitempty"`
	Body        string           `json:"body,omitempty"`
	BodyHTML    string           `json:"body_html,omitempty"`
	Attachments []AttachmentData `json:"attachments,omitempty"`
	ScheduledAt *time.Time       `json:"scheduled_at,omitempty"`
}

type AttachmentData struct {
	AttachmentID uuid.UUID
	Filename     string
	ContentType  string
	Data         []byte
}

type NotificationChannelStat struct {
	Channel string `json:"channel"`
	Total   int64  `json:"total"`
	Sent    int64  `json:"sent"`
	Failed  int64  `json:"failed"`
	Inbox   int64  `json:"inbox"`
	Draft   int64  `json:"draft"`
	Outbox  int64  `json:"outbox"`
	Trash   int64  `json:"trash"`
	Spam    int64  `json:"spam"`
}

// NotificationUserStatRow is the raw per-user count returned from the repository.
type NotificationUserStatRow struct {
	UserID uuid.UUID `gorm:"column:user_id"`
	Total  int64     `gorm:"column:total"`
	Sent   int64     `gorm:"column:sent"`
	Failed int64     `gorm:"column:failed"`
	Draft  int64     `gorm:"column:draft"`
	Trash  int64     `gorm:"column:trash"`
	Inbox  int64     `gorm:"column:inbox"`
	Outbox int64     `gorm:"column:outbox"`
	Spam   int64     `gorm:"column:spam"`
}

// NotificationUserCounts holds all category and status counts for a single user.
type NotificationUserCounts struct {
	Total  int64 `json:"total"`
	Sent   int64 `json:"sent"`
	Failed int64 `json:"failed"`
	Inbox  int64 `json:"inbox"`
	Draft  int64 `json:"draft"`
	Outbox int64 `json:"outbox"`
	Trash  int64 `json:"trash"`
	Spam   int64 `json:"spam"`
}

// NotificationUserStatsEntry is one row in the per-user stats response.
type NotificationUserStatsEntry struct {
	UserID string                 `json:"user_id"`
	Name   string                 `json:"name"`
	Email  string                 `json:"email"`
	Counts NotificationUserCounts `json:"counts"`
}

// NotificationStatsData is the top-level stats response body.
type NotificationStatsData struct {
	Channel string                       `json:"channel"`
	Users   []NotificationUserStatsEntry `json:"users"`
}

// NotificationStatsResponse is the flat single-user stats response body.
type NotificationStatsResponse struct {
	Channel string                 `json:"channel"`
	Counts  NotificationUserCounts `json:"counts"`
}
