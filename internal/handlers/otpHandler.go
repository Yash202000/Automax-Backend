package handlers

import (
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type OTPHandler struct {
	otpService  *services.OTPService
	userService services.UserService
}

func NewOTPHandler(otpService *services.OTPService,
	userService services.UserService,
) *OTPHandler {
	return &OTPHandler{
		otpService:  otpService,
		userService: userService,
	}
}

func (h *OTPHandler) SendOTP(c *fiber.Ctx) error {

	var req struct {
		Phone   string `json:"phone"   validate:"required,e164|numeric,max=20"`
		Name    string `json:"name"    validate:"omitempty,min=3,max=100"`
		Channel string `json:"channel" validate:"required,oneof=sms whatsapp voice email wa"`
	}

	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, 400, "invalid request")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	response, err := h.userService.ValidateMobileForLogin(c.UserContext(), req.Phone)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	if response == nil {
		return utils.ErrorResponse(c, 400, "no user found with this phone number")
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

	var req models.OTPReq

	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request")
	}

	if req.Phone == "" || req.OTP == "" || req.SessionID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "phone, session_id and otp required")
	}

	resp, err := h.otpService.VerifyOTP(c.UserContext(), req.Phone, req.SessionID, req.OTP)

	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "OTP verified successfully", resp)

}
