package models

import (
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Extension assignment action types (recorded in ExtensionAssignmentHistory).
const (
	ExtensionActionAssign   = "assign"   // extension assigned to a user
	ExtensionActionRelease  = "release"  // extension released from a user
	ExtensionActionTakeover = "takeover" // extension taken from a previous holder
	ExtensionActionCreate   = "create"   // extension created on the PBX (not assigned)
)

// ExtensionAssignment is the CURRENT-state table: exactly one row per extension
// that is currently assigned, and at most one row per user (enforced by unique
// indexes). It is fully decoupled from the users table — this, not
// User.Extension, is the source of truth for "who holds which extension".
type ExtensionAssignment struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	Extension string    `gorm:"size:50;not null;uniqueIndex:uq_ext_assign_extension" json:"extension"`
	UserID    uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:uq_ext_assign_user" json:"user_id"`
	User      *User     `gorm:"foreignKey:UserID" json:"user,omitempty"`

	AssignedBy     uuid.UUID `gorm:"type:uuid;not null" json:"assigned_by"`
	AssignedByUser *User     `gorm:"foreignKey:AssignedBy" json:"assigned_by_user,omitempty"`

	Note      string    `gorm:"size:500" json:"note,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (e *ExtensionAssignment) BeforeCreate(tx *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}

// ExtensionAssignmentHistory is an append-only audit log of every extension
// change (assign/release/takeover/create). Rows are immutable.
type ExtensionAssignmentHistory struct {
	ID        uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	Extension string    `gorm:"size:50;not null;index:idx_ext_hist_created,priority:1" json:"extension"`

	// UserID is the user the extension was assigned to (nil for a bare create event).
	UserID *uuid.UUID `gorm:"type:uuid;index:idx_ext_hist_user,priority:1" json:"user_id,omitempty"`
	User   *User      `gorm:"foreignKey:UserID" json:"user,omitempty"`

	AssignedBy     uuid.UUID `gorm:"type:uuid;index;not null" json:"assigned_by"`
	AssignedByUser *User     `gorm:"foreignKey:AssignedBy" json:"assigned_by_user,omitempty"`

	Action string `gorm:"size:20;not null" json:"action"`

	PreviousUserID *uuid.UUID `gorm:"type:uuid;index" json:"previous_user_id,omitempty"`
	PreviousUser   *User      `gorm:"foreignKey:PreviousUserID" json:"previous_user,omitempty"`

	Note      string    `gorm:"size:500" json:"note,omitempty"`
	CreatedAt time.Time `gorm:"index:idx_ext_hist_created,priority:2;index:idx_ext_hist_user,priority:2" json:"created_at"`
}

func (e *ExtensionAssignmentHistory) BeforeCreate(tx *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}

// --- Request DTOs ---

// ExtensionAssignRequest assigns an extension to a user (defaults to the caller).
type ExtensionAssignRequest struct {
	Extension string     `json:"extension" validate:"required"`
	UserID    *uuid.UUID `json:"user_id" validate:"omitempty,uuid4"`
	Note      string     `json:"note" validate:"omitempty,max=500"`
}

// ExtensionCreateRequest creates a new extension on the PBX (does not assign it).
type ExtensionCreateRequest struct {
	Extension string `json:"extension" validate:"required"`
	Password  string `json:"password" validate:"required"`
	Note      string `json:"note" validate:"omitempty,max=500"`
}

// --- Response DTOs (never expose the PBX password) ---

// ExtensionUserSummary is a lightweight user reference used in extension responses.
type ExtensionUserSummary struct {
	ID    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Email string    `json:"email"`
}

// ExtensionStatus is one entry in the pool listing: a PBX extension plus its
// current assignment status.
type ExtensionStatus struct {
	Extension  string                `json:"extension"`
	CallerName string                `json:"caller_name,omitempty"`
	CallGroup  string                `json:"callgroup,omitempty"`
	Status     string                `json:"status"` // "available" | "assigned"
	AssignedTo *ExtensionUserSummary `json:"assigned_to,omitempty"`
}

// ExtensionAssignmentResponse is a history row for API responses.
type ExtensionAssignmentResponse struct {
	ID           uuid.UUID             `json:"id"`
	Extension    string                `json:"extension"`
	Action       string                `json:"action"`
	User         *ExtensionUserSummary `json:"user,omitempty"`
	PreviousUser *ExtensionUserSummary `json:"previous_user,omitempty"`
	AssignedBy   *ExtensionUserSummary `json:"assigned_by,omitempty"`
	Note         string                `json:"note,omitempty"`
	CreatedAt    time.Time             `json:"created_at"`
}

// NewExtensionUserSummary builds a summary from a user, or nil if u is nil.
func NewExtensionUserSummary(u *User) *ExtensionUserSummary {
	if u == nil || u.ID == uuid.Nil {
		return nil
	}
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name == "" {
		name = u.Username
	}
	return &ExtensionUserSummary{ID: u.ID, Name: name, Email: u.Email}
}

// ToExtensionAssignmentResponse maps a history row (with associations preloaded)
// to its API response.
func ToExtensionAssignmentResponse(e *ExtensionAssignmentHistory) ExtensionAssignmentResponse {
	return ExtensionAssignmentResponse{
		ID:           e.ID,
		Extension:    e.Extension,
		Action:       e.Action,
		User:         NewExtensionUserSummary(e.User),
		PreviousUser: NewExtensionUserSummary(e.PreviousUser),
		AssignedBy:   NewExtensionUserSummary(e.AssignedByUser),
		Note:         e.Note,
		CreatedAt:    e.CreatedAt,
	}
}
