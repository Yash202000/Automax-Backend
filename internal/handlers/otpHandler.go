package handlers

import (
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

type OTPHandler struct {
	otpService *services.OTPService
}

func NewOTPHandler(otpService *services.OTPService) *OTPHandler {
	return &OTPHandler{
		otpService: otpService,
	}
}

func (h *OTPHandler) SendLoginOTP(c *fiber.Ctx) error {

	var req struct {
		Phone string `json:"phone"`
	}

	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, 400, "invalid request")
	}

	err := h.otpService.SendLoginOTP(c.Context(), req.Phone)
	if err != nil {
		return utils.ErrorResponse(c, 400, err.Error())
	}

	return c.JSON(fiber.Map{"message": "OTP sent"})
}

func (h *OTPHandler) VerifyLoginOTP(c *fiber.Ctx) error {

	var req struct {
		Phone string `json:"phone"`
		OTP   string `json:"otp"`
	}

	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request")
	}

	if req.Phone == "" || req.OTP == "" {
		return fiber.NewError(fiber.StatusBadRequest, "phone and otp required")
	}

	err := h.otpService.VerifyLoginOTP(c.Context(), req.Phone, req.OTP)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	return c.JSON(fiber.Map{
		"message": "OTP verified successfully",
	})
}
