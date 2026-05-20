package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type CallLog struct {
	ID           uuid.UUID         `gorm:"type:uuid;primary_key" json:"id"`
	CallUuid     string            `gorm:"size:36;uniqueIndex" json:"call_uuid,omitempty"`
	CallType     string            `gorm:"size:20" json:"call_type"`
	Status       string            `gorm:"size:20;not null" json:"status"`
	StartAt      *time.Time        `json:"start_at,omitempty"`
	EndAt        *time.Time        `json:"end_at,omitempty"`
	RecordingUrl string            `gorm:"size:500" json:"recording_url,omitempty"`
	Meta         datatypes.JSON    `gorm:"type:jsonb" json:"meta,omitempty"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    *time.Time        `json:"updated_at,omitempty"`
	DeletedAt    gorm.DeletedAt    `gorm:"index" json:"-"`
	Participants []CallParticipant `gorm:"foreignKey:CallLogID" json:"participants,omitempty"`
}

func (c *CallLog) BeforeCreate(tx *gorm.DB) error {
	if c.ID == uuid.Nil {
		c.ID = uuid.New()
	}
	return nil
}

type CallParticipant struct {
	ID          uuid.UUID  `gorm:"type:uuid;primary_key" json:"id"`
	CallLogID   uuid.UUID  `gorm:"type:uuid;index" json:"call_log_id"`
	UserID      *uuid.UUID `gorm:"type:uuid;index" json:"user_id,omitempty"`
	PhoneNumber *string    `gorm:"size:50" json:"phone_number,omitempty"`
	Role        string     `gorm:"size:20" json:"role"`        // "initiator", "recipient", "participant"
	JoinStatus  string     `gorm:"size:20" json:"join_status"` // "invited", "joined", "declined", "missed"
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

// ParticipantData carries a resolved participant (user ID xor phone) for service calls.
type ParticipantData struct {
	UserID *uuid.UUID
	Phone  *string
}

// StartCallParty represents one party in a call start request (registered or guest).
type StartCallParty struct {
	Extension  string `json:"extension"`
	GuestPhone string `json:"guest_phone"`
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
	RecordingUrl string                 `json:"recording_url,omitempty" validate:"omitempty,max=500"`
	Meta         datatypes.JSON         `json:"meta,omitempty"`
	Participants []CallParticipantInput `json:"participants,omitempty"`
}

// CallLogUpdateRequest for updating a call log
type CallLogUpdateRequest struct {
	StartAt      *time.Time     `json:"start_at,omitempty"`
	EndAt        *time.Time     `json:"end_at,omitempty"`
	Status       string         `json:"status,omitempty" validate:"omitempty,oneof=initiated ongoing ended missed in_call cancelled complete completed"`
	RecordingUrl string         `json:"recording_url,omitempty" validate:"omitempty,max=500"`
	Meta         datatypes.JSON `json:"meta,omitempty"`
}

// UserMinimalResponse for minimal user info embedded in participant responses
type UserMinimalResponse struct {
	ID        uuid.UUID `json:"user_id"`
	Extension string    `json:"extension"`
}

// CallParticipantResponse for API responses
type CallParticipantResponse struct {
	ID          uuid.UUID            `json:"id"`
	UserID      *uuid.UUID           `json:"user_id,omitempty"`
	PhoneNumber *string              `json:"phone_number,omitempty"`
	Role        string               `json:"role"`
	JoinStatus  string               `json:"join_status"`
	JoinedAt    *time.Time           `json:"joined_at,omitempty"`
	LeftAt      *time.Time           `json:"left_at,omitempty"`
	User        *UserMinimalResponse `json:"user,omitempty"`
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
	Status        string     `json:"status" validate:"omitempty,oneof=initiated ongoing ended missed in_call cancelled complete completed"`
	StartDate     *time.Time `json:"start_date" validate:"omitempty"`
	EndDate       *time.Time `json:"end_date" validate:"omitempty"`
	Search        string     `json:"search" validate:"omitempty,max=255"`
	ParticipantID *uuid.UUID `json:"participant_id" validate:"omitempty,uuid4"`
	Page          int        `json:"page" validate:"omitempty,gte=1,lte=1000"`
	Limit         int        `json:"limit" validate:"omitempty,gte=10,lte=100"`
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
