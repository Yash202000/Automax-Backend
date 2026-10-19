package models

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// IncidentDraft is a partially filled Create Incident form. It is not an incident:
// no number, workflow, assignment or notification. Publishing only stamps PublishedAt;
// a user delete and the expiry prune hard-delete the row.
type IncidentDraft struct {
	ID          uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	DraftNumber string    `gorm:"size:20;uniqueIndex;not null" json:"draft_number"`

	// One column per form field (see IncidentDraftPayload); none are required.
	Title            string          `gorm:"size:200" json:"title"`
	Source           string          `gorm:"size:100" json:"source"`
	LookupValueIDs   datatypes.JSON  `gorm:"type:jsonb" json:"lookup_value_ids"` // JSON array of uuid strings
	ClassificationID *uuid.UUID      `gorm:"type:uuid;index" json:"classification_id"`
	Classification   *Classification `gorm:"foreignKey:ClassificationID" json:"classification,omitempty"`
	LocationID       *uuid.UUID      `gorm:"type:uuid" json:"location_id"`
	Location         *Location       `gorm:"foreignKey:LocationID" json:"location,omitempty"`
	// RecordType is incident|request|complaint|query; drafts of every type share this table.
	RecordType  string   `gorm:"size:20;not null;default:'incident';index" json:"record_type"`
	Description string   `gorm:"type:text" json:"description"`
	Latitude    *float64 `gorm:"type:decimal(10,8)" json:"latitude"`
	Longitude   *float64 `gorm:"type:decimal(11,8)" json:"longitude"`
	// CallerInfo is a CallerInfo JSON object: names, phone and alternate mobile.
	CallerInfo datatypes.JSON `gorm:"type:jsonb" json:"caller_info"`

	CreatedBy uuid.UUID `gorm:"type:uuid;not null;index:idx_incident_drafts_owner_updated,priority:1" json:"created_by"`

	// Version is the optimistic-lock counter; a stale update gets 409.
	Version int `gorm:"not null;default:1" json:"version"`
	// Keep exempts the draft from the expiry prune.
	Keep bool `gorm:"not null;default:false" json:"keep"`

	// PublishedAt is set when the draft becomes an incident. A published draft is read-only
	// (no update, no attachment upload) and drops out of the list and badge count until the
	// prune removes it. Delete and prune always remove the row for good.
	PublishedAt *time.Time `gorm:"index" json:"published_at"`
	// PublishedIncidentID is saved the moment publish creates the incident. While it is set
	// the draft is locked against edits, and a second publish only moves the attachments
	// that are still on the draft instead of creating another incident.
	PublishedIncidentID *uuid.UUID `gorm:"type:uuid;index" json:"published_incident_id"`

	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `gorm:"index;index:idx_incident_drafts_owner_updated,priority:2,sort:desc" json:"updated_at"`
}

func (d *IncidentDraft) BeforeCreate(tx *gorm.DB) error {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	return nil
}

// ToPayload rebuilds the form payload from the draft's columns.
func (d *IncidentDraft) ToPayload() IncidentDraftPayload {
	p := IncidentDraftPayload{
		Title:       d.Title,
		Description: d.Description,
		Source:      d.Source,
		RecordType:  d.RecordType,
		Latitude:    d.Latitude,
		Longitude:   d.Longitude,
	}
	idStr := func(u *uuid.UUID) *string {
		if u == nil {
			return nil
		}
		s := u.String()
		return &s
	}
	p.ClassificationID = idStr(d.ClassificationID)
	p.LocationID = idStr(d.LocationID)
	var ci CallerInfo
	_ = json.Unmarshal(d.CallerInfo, &ci)
	p.CallerFirstName, p.CallerMiddleName, p.CallerLastName = ci.FirstName, ci.MiddleName, ci.LastName
	p.CallerPhone, p.AlternateMobileNumber = ci.Phone, ci.AlternateMobileNumber
	_ = json.Unmarshal(d.LookupValueIDs, &p.LookupValueIDs)
	return p
}

// CallerInfo is what the caller_info jsonb column stores: the flat caller_* payload
// fields grouped as one object. On publish the names are joined into ReporterName,
// Phone becomes ReporterPhone and AlternateMobileNumber AlternateMobile.
type CallerInfo struct {
	FirstName             string `json:"first_name"`
	MiddleName            string `json:"middle_name"`
	LastName              string `json:"last_name"`
	Phone                 string `json:"phone"`
	AlternateMobileNumber string `json:"alternate_mobile_number"`
}

// FullName joins the non-empty name parts with single spaces.
func (c CallerInfo) FullName() string {
	parts := make([]string, 0, 3)
	for _, p := range []string{c.FirstName, c.MiddleName, c.LastName} {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " ")
}

// IncidentDraftPayload mirrors IncidentCreateRequest, but only format and length
// rules apply: nothing is required.
type IncidentDraftPayload struct {
	Title            string   `json:"title" validate:"omitempty,max=200"`
	Description      string   `json:"description" validate:"omitempty,max=1000"`
	ClassificationID *string  `json:"classification_id" validate:"omitempty,uuid"`
	Source           string   `json:"source" validate:"omitempty,max=100"`
	LocationID       *string  `json:"location_id" validate:"omitempty,uuid"`
	Latitude         *float64 `json:"latitude" validate:"omitempty,min=-90,max=90"`
	Longitude        *float64 `json:"longitude" validate:"omitempty,min=-180,max=180"`
	CallerFirstName  string   `json:"caller_first_name" validate:"omitempty,max=100"`
	CallerMiddleName string   `json:"caller_middle_name" validate:"omitempty,max=100"`
	CallerLastName   string   `json:"caller_last_name" validate:"omitempty,max=100"`
	// CallerPhone becomes ReporterPhone on publish.
	CallerPhone           string   `json:"caller_phone" validate:"omitempty,max=20"`
	AlternateMobileNumber string   `json:"caller_alternate_mobile_number" validate:"omitempty,mobile,max=20"`
	LookupValueIDs        []string `json:"lookup_value_ids,omitempty" validate:"omitempty,dive,uuid"`
	RecordType            string   `json:"record_type" validate:"omitempty,oneof=incident request complaint query"`
}

// IncidentDraftSaveRequest is the body for create and (full-replace) update.
type IncidentDraftSaveRequest struct {
	IncidentDraftPayload

	// Version is required on update and must match the stored version.
	Version int `json:"version" validate:"omitempty,min=0"`
	// Keep marks the draft as exempt from expiry. Nil leaves it unchanged on update.
	Keep *bool `json:"keep"`
	// ClientRequestID makes the first save idempotent against double submits.
	ClientRequestID string `json:"client_request_id" validate:"omitempty,max=100"`
}

// IncidentDraftFilter filters the owner's draft list.
type IncidentDraftFilter struct {
	Search           string `query:"search" json:"search" validate:"omitempty,max=100"`
	ClassificationID string `query:"classification_id" json:"classification_id" validate:"omitempty,uuid"`
	RecordType       string `query:"record_type" json:"record_type" validate:"omitempty,oneof=incident request complaint query"`
	Page             int    `query:"page" json:"page" validate:"omitempty,min=1"`
	Limit            int    `query:"limit" json:"limit" validate:"omitempty,min=1,max=100"`
}

// IncidentDraftResponse is the single-draft API shape.
type IncidentDraftResponse struct {
	ID          uuid.UUID `json:"id"`
	DraftNumber string    `json:"draft_number"`
	Version     int       `json:"version"`
	Keep        bool      `json:"keep"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	// PublishedAt is non-nil once the draft became an incident; it can no longer be edited.
	PublishedAt *time.Time `json:"published_at"`
	// PublishedIncidentID is set once an incident exists; the draft stays listed until every
	// attachment has moved to it.
	PublishedIncidentID *uuid.UUID `json:"published_incident_id"`

	// The form fields, flattened into the response so the UI can refill the form.
	IncidentDraftPayload

	FilledFields   int  `json:"filled_fields"`
	RequiredFields int  `json:"required_fields"`
	DaysToExpiry   *int `json:"days_to_expiry,omitempty"`
}

// IncidentDraftListItem is one row of the My Drafts list.
type IncidentDraftListItem struct {
	ID               uuid.UUID  `json:"id"`
	DraftNumber      string     `json:"draft_number"`
	RecordType       string     `json:"record_type"`
	ClassificationID *uuid.UUID `json:"classification_id"`
	ClassName        string     `json:"classification_name"`
	ClassNameAr      string     `json:"classification_name_ar"`
	LocationID       *uuid.UUID `json:"location_id"`
	LocationName     string     `json:"location_name"`
	LocationNameAr   string     `json:"location_name_ar"`
	Description      string     `json:"description"`
	Latitude         *float64   `json:"latitude"`
	Longitude        *float64   `json:"longitude"`
	CallerName       string     `json:"caller_name"`
	CallerPhone      string     `json:"caller_phone"` // masked
	Keep             bool       `json:"keep"`
	Version          int        `json:"version"`
	UpdatedAt        time.Time  `json:"updated_at"`

	FilledFields   int  `json:"filled_fields"`
	RequiredFields int  `json:"required_fields"`
	DaysToExpiry   *int `json:"days_to_expiry,omitempty"`
}

// MaskPhone hides all but the last three digits, e.g. "0501234567" -> "*******567".
func MaskPhone(phone string) string {
	phone = strings.TrimSpace(phone)
	if len(phone) <= 3 {
		return phone
	}
	return strings.Repeat("*", len(phone)-3) + phone[len(phone)-3:]
}

// IncidentDraftAttachment is a file uploaded to a draft. It is separate from
// IncidentAttachment, which needs an incident. On publish the rows move to
// incident_attachments pointing at the same stored object; on delete or expiry both
// the row and the object go. Rows are hard-deleted.
type IncidentDraftAttachment struct {
	ID      uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	DraftID uuid.UUID `gorm:"type:uuid;index;not null" json:"draft_id"` // no FK: cleaned up by the draft repository

	FileName string `gorm:"size:255;not null" json:"file_name"`
	FileSize int64  `json:"file_size"`
	MimeType string `gorm:"size:100" json:"mime_type"`
	FilePath string `gorm:"size:500;not null" json:"-"` // storage object key

	UploadedByID uuid.UUID `gorm:"type:uuid;not null" json:"uploaded_by_id"`
	CreatedAt    time.Time `json:"created_at"`
}

func (a *IncidentDraftAttachment) BeforeCreate(tx *gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}

// IncidentDraftAttachmentResponse is the API shape; URL is the owner-only download route.
type IncidentDraftAttachmentResponse struct {
	ID        uuid.UUID `json:"id"`
	DraftID   uuid.UUID `json:"draft_id"`
	FileName  string    `json:"file_name"`
	FileSize  int64     `json:"file_size"`
	MimeType  string    `json:"mime_type"`
	URL       string    `json:"url"`
	CreatedAt time.Time `json:"created_at"`
}
