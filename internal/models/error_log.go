package models

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Error log modules.
const (
	ErrorModuleImport      = "import"
	ErrorModuleCall        = "call"
	ErrorModuleAttachment  = "attachment"
	ErrorModuleIncident    = "incident"
	ErrorModuleIntegration = "integration"
	ErrorModuleContact     = "contact"
	ErrorModuleAuth        = "auth"
)

// Error log severities.
const (
	ErrorSeverityLow      = "low"
	ErrorSeverityMedium   = "medium"
	ErrorSeverityHigh     = "high"
	ErrorSeverityCritical = "critical"
)

// Error log statuses.
const (
	ErrorStatusNew      = "new"
	ErrorStatusInReview = "in_review"
	ErrorStatusResolved = "resolved"
	ErrorStatusIgnored  = "ignored"
)

// Common error types / codes.
const (
	ErrorTypeConnectionFailed = "connection_failed"
	ErrorTypeTimeout          = "timeout"
	ErrorTypeInvalidFile      = "invalid_file"
	ErrorTypeInvalidResponse  = "invalid_response"
	ErrorTypeFileTooLarge     = "file_too_large"
	ErrorTypeStorageError     = "storage_error"
	ErrorTypeSaveFailed       = "save_failed"
	ErrorTypeAssignmentFailed = "assignment_failed"
	ErrorTypeRowFailed        = "row_failed"
	ErrorTypeUnexpected       = "unexpected_error"
)

// ErrorLog is a single recorded system error. SMS/email failures are NOT
// recorded here — they live in notification_logs (Communication module).
type ErrorLog struct {
	ID uuid.UUID `gorm:"type:uuid;primaryKey" json:"id"`
	// ErrorCode is the human-readable ID, e.g. ERR-000123.
	ErrorCode  string    `gorm:"size:20;uniqueIndex;not null" json:"error_code"`
	OccurredAt time.Time `gorm:"index;not null" json:"occurred_at"`

	Module    string `gorm:"size:50;index;not null" json:"module"`
	Action    string `gorm:"size:200;not null" json:"action"`
	ErrorType string `gorm:"size:50;index;not null" json:"error_type"`
	Severity  string `gorm:"size:20;index;not null;default:'medium'" json:"severity"`
	Message   string `gorm:"type:text;not null" json:"message"`
	// TechnicalDetails is sanitized, support-only text (stack/provider response).
	TechnicalDetails string `gorm:"type:text" json:"technical_details,omitempty"`

	// UserID is nil for background/system actions. UserName is a snapshot so
	// the list needs no join and survives user deletion.
	UserID   *uuid.UUID `gorm:"type:uuid;index" json:"user_id,omitempty"`
	UserName string     `gorm:"size:200" json:"user_name"`

	RelatedType  string `gorm:"size:50;index" json:"related_type,omitempty"` // incident, import, contact, ...
	RelatedID    string `gorm:"size:100;index" json:"related_id,omitempty"`
	RelatedLabel string `gorm:"size:255" json:"related_label,omitempty"` // incident number, file name

	Status     string `gorm:"size:20;index;not null;default:'new'" json:"status"`
	RetryCount int    `gorm:"not null;default:0" json:"retry_count"`

	// Fingerprint groups repeats of the same error (module+type+action).
	Fingerprint string `gorm:"size:64;index" json:"fingerprint"`

	History []ErrorLogHistory `gorm:"foreignKey:ErrorLogID;constraint:OnDelete:CASCADE" json:"history,omitempty"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// BeforeCreate assigns the UUID and the sequential ERR-xxxxxx code.
func (e *ErrorLog) BeforeCreate(tx *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	if e.ErrorCode == "" {
		var n int64
		if err := tx.Raw("SELECT nextval('error_log_seq')").Scan(&n).Error; err != nil {
			return err
		}
		e.ErrorCode = fmt.Sprintf("ERR-%06d", n)
	}
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now()
	}
	return nil
}

// ErrorLogHistory records every status change of an error log.
type ErrorLogHistory struct {
	ID          uuid.UUID  `gorm:"type:uuid;primaryKey" json:"id"`
	ErrorLogID  uuid.UUID  `gorm:"type:uuid;index;not null" json:"error_log_id"`
	FromStatus  string     `gorm:"size:20" json:"from_status"`
	ToStatus    string     `gorm:"size:20;not null" json:"to_status"`
	Note        string     `gorm:"type:text" json:"note,omitempty"`
	ChangedByID *uuid.UUID `gorm:"type:uuid" json:"changed_by_id,omitempty"`
	ChangedBy   string     `gorm:"size:200" json:"changed_by"`
	ChangedAt   time.Time  `gorm:"index" json:"changed_at"`
}

func (h *ErrorLogHistory) BeforeCreate(_ *gorm.DB) error {
	if h.ID == uuid.Nil {
		h.ID = uuid.New()
	}
	if h.ChangedAt.IsZero() {
		h.ChangedAt = time.Now()
	}
	return nil
}

// ErrorLogEntry is the input accepted by ErrorLogService.Log.
type ErrorLogEntry struct {
	Module           string
	Action           string
	ErrorType        string
	Severity         string
	Message          string
	TechnicalDetails string
	UserID           *uuid.UUID
	UserName         string
	RelatedType      string
	RelatedID        string
	RelatedLabel     string
	RetryCount       int
}

// ErrorLogFilter holds list/export query parameters.
type ErrorLogFilter struct {
	Search    string     `query:"search"`
	Module    string     `query:"module"`
	Severity  string     `query:"severity"`
	Status    string     `query:"status"`
	UserID    *uuid.UUID `query:"user_id"`
	StartDate string     `query:"start_date"` // YYYY-MM-DD
	EndDate   string     `query:"end_date"`   // YYYY-MM-DD
	Page      int        `query:"page"`
	Limit     int        `query:"limit"`
}

// UpdateErrorLogStatusRequest is the body of PATCH /error-logs/:id/status.
type UpdateErrorLogStatusRequest struct {
	Status string `json:"status" validate:"required,oneof=new in_review resolved ignored"`
	Note   string `json:"note" validate:"omitempty,max=1000"`
}

// ErrorLogUser is one entry of the User filter: a user that appears in the error log.
type ErrorLogUser struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// ReportErrorLogRequest is sent by the web app when it detects an error in the browser
// (for example an import file that cannot be read) that never reaches the API.
type ReportErrorLogRequest struct {
	Module       string `json:"module" validate:"required,oneof=import attachment incident integration contact auth"`
	Action       string `json:"action" validate:"required,max=200"`
	ErrorType    string `json:"error_type" validate:"omitempty,max=50"`
	Severity     string `json:"severity" validate:"omitempty,oneof=low medium high"`
	Message      string `json:"message" validate:"required,max=2000"`
	Details      string `json:"details" validate:"omitempty,max=8000"`
	RelatedType  string `json:"related_type" validate:"omitempty,max=50"`
	RelatedID    string `json:"related_id" validate:"omitempty,max=100"`
	RelatedLabel string `json:"related_label" validate:"omitempty,max=255"`
}

// ErrorLogResponse is the masked, permission-aware view of an ErrorLog.
type ErrorLogResponse struct {
	ID               uuid.UUID                  `json:"id"`
	ErrorCode        string                     `json:"error_code"`
	OccurredAt       time.Time                  `json:"occurred_at"`
	Module           string                     `json:"module"`
	Action           string                     `json:"action"`
	ErrorType        string                     `json:"error_type"`
	Severity         string                     `json:"severity"`
	Message          string                     `json:"message"`
	TechnicalDetails *string                    `json:"technical_details,omitempty"`
	UserID           *uuid.UUID                 `json:"user_id,omitempty"`
	User             string                     `json:"user"`
	RelatedType      string                     `json:"related_type,omitempty"`
	RelatedID        string                     `json:"related_id,omitempty"`
	RelatedLabel     string                     `json:"related_label,omitempty"`
	Status           string                     `json:"status"`
	RetryCount       int                        `json:"retry_count"`
	History          []ErrorLogHistoryResponse  `json:"history,omitempty"`
}

type ErrorLogHistoryResponse struct {
	FromStatus string    `json:"from_status"`
	ToStatus   string    `json:"to_status"`
	Note       string    `json:"note,omitempty"`
	ChangedBy  string    `json:"changed_by"`
	ChangedAt  time.Time `json:"changed_at"`
}
