package models

import (
	"time"

	"github.com/google/uuid"
)

// Recording audit activities. The Call Recording History holds these two only —
// viewing details, deleting, etc. are deliberately not recorded here.
const (
	RecordingActivityListened   = "listened"
	RecordingActivityDownloaded = "downloaded"
)

// CallRecordingAudit is one append-only entry of the Call Recording History:
// a single play or download of a call recording by a user.
//
// Rows are immutable. There is no UpdatedAt/DeletedAt, the repository exposes
// no update/delete, and a Postgres trigger (MigrateCallRecordingAudit) rejects
// UPDATE/DELETE/TRUNCATE for everyone, administrators included.
//
// The user's name, username and role are snapshotted at the time of the
// activity, so later renames or role changes do not rewrite history.
type CallRecordingAudit struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	CallLogID uuid.UUID `gorm:"type:uuid;index;not null" json:"call_log_id"`
	CallUuid  string    `gorm:"size:36;index" json:"call_uuid"`
	Activity  string    `gorm:"size:20;index;not null;check:chk_call_recording_audits_activity,activity IN ('listened','downloaded')" json:"activity"`
	UserID    uuid.UUID `gorm:"type:uuid;index;not null" json:"user_id"`
	UserName  string    `gorm:"size:300" json:"user_name"`
	Username  string    `gorm:"size:100" json:"username"`
	UserRole  string    `gorm:"size:500" json:"user_role"`
	IPAddress string    `gorm:"size:45" json:"ip_address"`
	UserAgent string    `gorm:"size:500" json:"user_agent"`
	// Always stamped by the database clock (single server time source); the
	// application never supplies it. See repository Create.
	CreatedAt time.Time `gorm:"not null;default:now();index" json:"created_at"`
}

func (CallRecordingAudit) TableName() string { return "call_recording_audits" }

// CallRecordingAccessRequest is the body of POST /call-recordings/:call_log_id/access.
type CallRecordingAccessRequest struct {
	Activity string `json:"activity" validate:"required,oneof=listened downloaded"`
}

// CallRecordingAccessResponse carries the short-lived grant URL that streams the
// recording. It is issued only after the audit entry has been saved.
type CallRecordingAccessResponse struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

// CallRecordingAuditFilter filters the Call Recording History list.
type CallRecordingAuditFilter struct {
	Activity  string     `query:"activity"    json:"activity"    validate:"omitempty,oneof=listened downloaded"`
	UserID    *uuid.UUID `query:"user_id"     json:"user_id"     validate:"omitempty"`
	CallLogID *uuid.UUID `query:"call_log_id" json:"call_log_id" validate:"omitempty"`
	Search    string     `query:"search"      json:"search"      validate:"omitempty,max=255"`
	// Parsed by hand in the handler (YYYY-MM-DD or RFC3339), see CallLogFilter.
	StartDate *time.Time `query:"-" json:"start_date"`
	EndDate   *time.Time `query:"-" json:"end_date"`
	Page      int        `query:"page"  json:"page"  validate:"omitempty,gte=1"`
	Limit     int        `query:"limit" json:"limit" validate:"omitempty,gte=1,lte=100"`
}

// CallRecordingAuditItem is one row of the Call Recording History screen.
// Incident fields are resolved at read time via incidents.call_uuid, so an
// incident created after the recording was played still shows up.
type CallRecordingAuditItem struct {
	ID             uuid.UUID  `json:"id"`
	CallLogID      uuid.UUID  `json:"call_log_id"`
	CallUuid       string     `json:"call_uuid"`
	IncidentID     *uuid.UUID `json:"incident_id,omitempty"`
	IncidentNumber *string    `json:"incident_number,omitempty"`
	Activity       string     `json:"activity"`
	UserID         uuid.UUID  `json:"user_id"`
	UserName       string     `json:"user_name"`
	Username       string     `json:"username"`
	UserRole       string     `json:"user_role"`
	CreatedAt      time.Time  `json:"created_at"`
}

// CallRecordingAuditCounts are the per-tab badge counts. They honour every
// filter except Activity, so all three tabs stay consistent.
type CallRecordingAuditCounts struct {
	All        int64 `json:"all"`
	Listened   int64 `json:"listened"`
	Downloaded int64 `json:"downloaded"`
}
