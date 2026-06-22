package handlers

import (
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
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
		return utils.ErrorResponse(c, 400, i18n.T(c.UserContext(), "invalid_request"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	// [Citizen Auto-Register] Removed ExistsByPhoneAndName check.
	// Previously, OTP send was blocked if no user existed with this phone+name.
	// Now we allow any phone number to receive OTP. If the user doesn't exist,
	// they will be auto-registered as a citizen upon successful OTP verification.
	// To revert: restore the ExistsByPhoneAndName check here and remove citizenName from SendOTP.

	// Get userID from token (nil for unauthenticated citizen requests)
	var sentBy *uuid.UUID
	if userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID); ok {
		sentBy = &userID
	}

	// [Citizen Auto-Register] Pass citizen name to SendOTP so it's stored in Redis
	// and available for auto-creating the user record during OTP verification.
	sessionID, err := h.otpService.SendOTP(
		c.UserContext(),
		req.Phone,
		req.Channel,
		sentBy,
		req.Name,
	)

	if err != nil {
		return utils.ErrorResponse(c, 400, err.Error())
	}

	return c.JSON(fiber.Map{
		"message":    i18n.T(c.UserContext(), "otp_sent"),
		"session_id": sessionID,
	})

}

func (h *OTPHandler) VerifyOTP(c *fiber.Ctx) error {

	var req models.OTPReq

	if err := c.BodyParser(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request"))
	}

	if req.Phone == "" || req.OTP == "" || req.SessionID == "" {
		return fiber.NewError(fiber.StatusBadRequest, "phone, session_id and otp required")
	}

	resp, err := h.otpService.VerifyOTP(c.UserContext(), req.Phone, req.SessionID, req.OTP)

	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "otp_verified_successfully"), resp)

}
