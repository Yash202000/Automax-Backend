package models

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
	// [Citizen Auto-Register] Name provided by citizen during OTP send, used to auto-create user on verify
	Name string `json:"name,omitempty"`
}

type OTPReq struct {
	Phone     string `json:"phone" validate:"required,e164|numeric,max=20"`
	SessionID string `json:"session_id" validate:"required"`
	OTP       string `json:"otp" validate:"required"`
}
