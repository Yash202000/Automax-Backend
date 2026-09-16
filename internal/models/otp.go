package models

import (
	"time"

	"github.com/google/uuid"
)

type LoginResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	User         *User  `json:"user"`
}

type OTPData struct {
	Phone      string `json:"phone"`
	Hash       string `json:"hash"`
	SenderMode string `json:"senderMode"`
	Attempts   int    `json:"attempts"`
	Status     string `json:"status"`
	SessionID  string `json:"session_id"`

	SentAt     time.Time  `json:"sentAt"`
	VerifiedAt *time.Time `json:"verifiedAt,omitempty"`
	SentBy     *uuid.UUID `json:"sentBy"`

	// [Citizen Auto-Register] Name provided by citizen during OTP send, used to auto-create user on verify
	FirstName  string `json:"first_name,omitempty"`
	MiddleName string `json:"middle_name,omitempty"`
	LastName   string `json:"last_name,omitempty"`
}

type OTPReq struct {
	Phone     string `json:"phone" validate:"required,e164|numeric,max=20"`
	SessionID string `json:"session_id" validate:"required"`
	OTP       string `json:"otp" validate:"required"`
}
