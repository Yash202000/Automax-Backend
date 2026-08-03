package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type CallLog struct {
	ID uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	// CreatedBy is nil for system/machine-ingested rows (e.g. the Cintrix
	// webhook) — no user acted to create them. Column is nullable; do not
	// backfill with a fabricated user, that would misrepresent the audit trail.
	CreatedBy    *uuid.UUID          `gorm:"type:uuid;column:created_by" json:"created_by,omitempty"`
	CallUuid     string              `gorm:"size:36;uniqueIndex" json:"call_uuid,omitempty"`
	CallType     string              `gorm:"size:20" json:"call_type"`
	Status       string              `gorm:"size:20;not null" json:"status"`
	StartAt      *time.Time          `json:"start_at,omitempty"`
	EndAt        *time.Time          `json:"end_at,omitempty"`
	Meta         datatypes.JSON      `gorm:"type:jsonb" json:"meta,omitempty"`
	CreatedAt    time.Time           `json:"created_at"`
	UpdatedAt    *time.Time          `json:"updated_at,omitempty"`
	DeletedAt    gorm.DeletedAt      `gorm:"index" json:"-"`
	Participants []CallParticipant   `gorm:"foreignKey:CallLogID" json:"participants,omitempty"`
	Attachments  []CallLogAttachment `gorm:"foreignKey:CallLogID" json:"-"`
}

func (c *CallLog) BeforeCreate(tx *gorm.DB) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return nil
}

// RecordingURLFromMeta extracts the "recording_url" field from a CallLog's Meta
// JSON (as stored verbatim by CintrixWebhookHandler from the call.ended payload).
// Returns "" when Meta is empty, the field is absent/null, or Meta isn't valid JSON.
func RecordingURLFromMeta(meta datatypes.JSON) string {
	if len(meta) == 0 {
		return ""
	}
	var m struct {
		RecordingURL *string `json:"recording_url"`
	}
	if err := json.Unmarshal(meta, &m); err != nil || m.RecordingURL == nil {
		return ""
	}
	return *m.RecordingURL
}

// DirectionFromMeta extracts the "direction" field from a CallLog's Meta JSON
// (stored verbatim by CintrixWebhookHandler from the call event payload).
// Used for the unscoped admin list view, where there is no "current user" whose
// phone could tell incoming from outgoing. Returns "" when Meta is empty, the
// field is absent/null, or Meta isn't valid JSON.
func DirectionFromMeta(meta datatypes.JSON) string {
	if len(meta) == 0 {
		return ""
	}
	var m struct {
		Direction *string `json:"direction"`
	}
	if err := json.Unmarshal(meta, &m); err != nil || m.Direction == nil {
		return ""
	}
	return *m.Direction
}

type CallParticipant struct {
	ID          uuid.UUID  `gorm:"type:uuid;primary_key" json:"id"`
	CallLogID   uuid.UUID  `gorm:"type:uuid;index" json:"call_log_id"`
	PhoneNumber string     `gorm:"size:50;not null" json:"phone_number"`
	Role        string     `gorm:"size:20" json:"role"`                    // "initiator", "recipient", "participant"
	DisplayName string     `gorm:"size:255" json:"display_name,omitempty"` // caller/contact name from the CTI payload
	JoinStatus  string     `gorm:"size:20" json:"join_status"`             // "invited", "joined", "declined", "missed"
	JoinedAt    *time.Time `json:"joined_at,omitempty"`
	LeftAt      *time.Time `json:"left_at,omitempty"`
}

func (p *CallParticipant) BeforeCreate(tx *gorm.DB) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	return nil
}

// CallLogAttachment represents a file attached to a call log
type CallLogAttachment struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	CallLogID uuid.UUID `gorm:"type:uuid;index;not null" json:"call_uuid"`
	FileName  string    `gorm:"size:255;not null" json:"file_name"`
	FileSize  int64     `json:"file_size"`
	MimeType  string    `gorm:"size:100" json:"mime_type"`
	FilePath  string    `gorm:"size:500;not null" json:"file_path"`

	UploadedByID uuid.UUID `gorm:"type:uuid;index;not null" json:"uploaded_by_id"`
	UploadedBy   *User     `gorm:"foreignKey:UploadedByID" json:"uploaded_by,omitempty"`

	CreatedAt time.Time      `json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (a *CallLogAttachment) BeforeCreate(tx *gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}

type CallLogMeta struct {
	Duration   int                    `json:"duration,omitempty"`
	Platform   string                 `json:"platform,omitempty"`
	DeviceInfo map[string]interface{} `json:"device_info,omitempty"`
	Quality    string                 `json:"quality,omitempty"`
	Notes      string                 `json:"notes,omitempty"`
}

// ParticipantData carries a phone/extension for service calls.
type ParticipantData struct {
	Phone string
}

// StartCallParty represents one party in a call start request (registered or guest).
type StartCallParty struct {
	Phone string `json:"phone" validate:"required"`
}

// StartCallRequest is the payload for POST /api/v1/calls/start.
// Direct call:  provide initiator + recipient.
// Group call:   provide initiator + participants (array).
type StartCallRequest struct {
	CallUUID     string           `json:"call_uuid" validate:"required,max=36"`
	CallType     string           `json:"call_type" validate:"required,oneof=direct group"`
	Initiator    StartCallParty   `json:"initiator"`
	Recipient    *StartCallParty  `json:"recipient,omitempty"`
	Participants []StartCallParty `json:"participants,omitempty"`
}

// CallParticipantInput is used in the admin create endpoint.
// Identify a registered user via Extension, or an external party via PhoneNumber.
type CallParticipantInput struct {
	Extension   string `json:"extension,omitempty"`
	PhoneNumber string `json:"phone_number,omitempty"`
	Role        string `json:"role" validate:"required,oneof=initiator recipient participant"`
	JoinStatus  string `json:"join_status,omitempty" validate:"omitempty,oneof=invited joined declined missed ended completed complete in_call cancelled"`
}

// CallLogCreateRequest for the admin endpoint
type CallLogCreateRequest struct {
	CallUuid     string                 `json:"call_uuid,omitempty" validate:"omitempty,max=36"`
	CallType     string                 `json:"call_type" validate:"required,oneof=direct group"`
	Status       string                 `json:"status" validate:"required,oneof=initiated ongoing ended missed in_call cancelled completed complete"`
	StartAt      *time.Time             `json:"start_at,omitempty"`
	EndAt        *time.Time             `json:"end_at,omitempty"`
	Meta         datatypes.JSON         `json:"meta,omitempty"`
	Participants []CallParticipantInput `json:"participants,omitempty"`
}

// CallLogUpdateRequest for updating a call log
type CallLogUpdateRequest struct {
	StartAt *time.Time     `json:"start_at,omitempty"`
	EndAt   *time.Time     `json:"end_at,omitempty"`
	Status  string         `json:"status,omitempty" validate:"omitempty,oneof=initiated ongoing ended missed in_call cancelled complete completed"`
	Meta    datatypes.JSON `json:"meta,omitempty"`
}

// UserMinimalResponse for minimal user info embedded in participant responses
type UserMinimalResponse struct {
	ID        uuid.UUID `json:"user_id"`
	Extension string    `json:"extension"`
}

// CallParticipantResponse for API responses
type CallParticipantResponse struct {
	ID          uuid.UUID  `json:"id"`
	PhoneNumber string     `json:"phone_number"`
	Name        string     `json:"name,omitempty"`
	Role        string     `json:"role"`
	JoinStatus  string     `json:"join_status"`
	JoinedAt    *time.Time `json:"joined_at,omitempty"`
	LeftAt      *time.Time `json:"left_at,omitempty"`
}

// CallLogResponse for API responses
type CallLogResponse struct {
	ID           uuid.UUID                 `json:"id"`
	CallUuid     string                    `json:"call_uuid,omitempty"`
	CallType     string                    `json:"call_type"`
	Status       string                    `json:"status"`
	StartAt      *time.Time                `json:"start_at,omitempty"`
	EndAt        *time.Time                `json:"end_at,omitempty"`
	RecordingUrl string                    `json:"recording_url,omitempty"`
	Meta         datatypes.JSON            `json:"meta,omitempty"`
	Participants []CallParticipantResponse `json:"participants"`
	CreatedAt    time.Time                 `json:"created_at"`
	UpdatedAt    *time.Time                `json:"updated_at,omitempty"`
}

type CallLogAttachmentResponse struct {
	ID         uuid.UUID     `json:"id"`
	CallLogID  uuid.UUID     `json:"call_uuid"`
	FileName   string        `json:"file_name"`
	FileSize   int64         `json:"file_size"`
	MimeType   string        `json:"mime_type"`
	URL        string        `json:"url,omitempty"`
	UploadedBy *UserResponse `json:"uploaded_by,omitempty"`
	CreatedAt  time.Time     `json:"created_at"`
}

// CallLogListItem is the slim response returned by the listing API.
type CallLogListItem struct {
	ID                  uuid.UUID `json:"id"`
	CallUuid            string    `json:"call_uuid"`
	CallType            string    `json:"call_type"`
	Status              string    `json:"status"`
	Direction           string    `json:"direction"`
	OtherPartyName      string    `json:"other_party_name"`
	OtherPartyExtension string    `json:"other_party_extension"`
	OtherPartyPhone     string    `json:"other_party_phone,omitempty"`
	Duration            int       `json:"duration"`
	RecordingUrl        string    `json:"recording_url"`
	CreatedAt           time.Time `json:"created_at"`
}

// CallLogFilter for filtering call logs
type CallLogFilter struct {
	Status string `query:"status" json:"status" validate:"omitempty,oneof=initiated ongoing ended missed in_call cancelled complete completed"`
	Search string `query:"search" json:"search" validate:"omitempty,max=255"`

	// Dates are query:"-" and parsed by hand in the handler: QueryParser errors
	// outright on anything time.Time's UnmarshalText rejects, so leaving these
	// bindable would 400 the whole request for a plain YYYY-MM-DD.
	StartDate *time.Time `query:"-" json:"start_date" validate:"omitempty"`
	EndDate   *time.Time `query:"-" json:"end_date"   validate:"omitempty"`

	// Super-admin-only scope overrides; silently ignored for everyone else.
	// AgentID wins over All when both are supplied.
	AgentID *uuid.UUID `query:"agent_id" json:"agent_id" validate:"omitempty,uuid4"`
	All     bool       `query:"all"      json:"all"`

	// ParticipantID is set by the handler from the resolved caller, never from the
	// query string — binding it would let any caller read another user's calls.
	ParticipantID *uuid.UUID `query:"-" json:"participant_id" validate:"omitempty,uuid4"`

	Page  int `query:"page"  json:"page"  validate:"omitempty,gte=1,lte=1000"`
	Limit int `query:"limit" json:"limit" validate:"omitempty,gte=1,lte=100"`
}

// CallLogStats represents statistics for call logs
type CallLogStats struct {
	TotalCalls    int64            `json:"total_calls"`
	RecentCalls   int64            `json:"recent_calls"`
	CallsByStatus map[string]int64 `json:"calls_by_status"`
}

func ToCallLogAttachmentResponse(a *CallLogAttachment, url string) CallLogAttachmentResponse {
	resp := CallLogAttachmentResponse{
		ID:        a.ID,
		CallLogID: a.CallLogID,
		FileName:  a.FileName,
		FileSize:  a.FileSize,
		MimeType:  a.MimeType,
		URL:       url,
		CreatedAt: a.CreatedAt,
	}
	if a.UploadedBy != nil {
		uploaderResp := ToUserResponse(a.UploadedBy)
		resp.UploadedBy = &uploaderResp
	}
	return resp
}
