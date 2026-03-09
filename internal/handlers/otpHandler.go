package handlers

import (
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type OTPHandler struct {
	otpService *services.OTPService
}

func NewOTPHandler(otpService *services.OTPService) *OTPHandler {
	return &OTPHandler{
		otpService: otpService,
	}
}

func (h *OTPHandler) SendOTP(c *fiber.Ctx) error {

	var req struct {
		Phone   string `json:"phone"`
		Channel string `json:"channel"`
	}

	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, 400, "invalid request")
	}

	// Get userID from token
	var sentBy *uuid.UUID
	if userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID); ok {
		sentBy = &userID
	}

	sessionID, err := h.otpService.SendOTP(
		c.UserContext(),
		req.Phone,
		req.Channel,
		sentBy,
	)

	if err != nil {
		return utils.ErrorResponse(c, 400, err.Error())
	}

	return c.JSON(fiber.Map{
		"message":    "OTP sent",
		"session_id": sessionID,
	})

}

func (h *OTPHandler) VerifyOTP(c *fiber.Ctx) error {

	var req struct {
		Phone     string `json:"phone"`
		SessionID string `json:"session_id"`
		OTP       string `json:"otp"`
	}

	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request")
	}

	if req.Phone == "" || req.OTP == "" || req.SessionID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "phone, session_id and otp required")
	}

	err := h.otpService.VerifyOTP(
		c.UserContext(),
		req.Phone,
		req.SessionID,
		req.OTP,
	)

	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	return c.JSON(fiber.Map{
		"message": "OTP verified successfully",
	})
}
