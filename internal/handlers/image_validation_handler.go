package handlers

import (
	"io"
	"log"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
)

// ImageValidationHandler exposes a standalone endpoint that Mobile App and
// Chatbot call to check whether a photo shows any meaningful, in-focus
// detail before letting the user submit it on an incident.
type ImageValidationHandler struct {
	service services.ImageValidationService
	cfg     config.ImageValidationConfig
}

func NewImageValidationHandler(service services.ImageValidationService, cfg config.ImageValidationConfig) *ImageValidationHandler {
	return &ImageValidationHandler{
		service: service,
		cfg:     cfg,
	}
}

type imageValidationResponse struct {
	Valid   bool   `json:"valid"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// ValidateImage handles POST /api/v1/images/validate.
func (h *ImageValidationHandler) ValidateImage(c *fiber.Ctx) (retErr error) {
	// Defensive recovery: this endpoint decodes untrusted, arbitrary binary
	// uploads, and some image codecs are known to panic on malformed input.
	// The global recover() middleware would also catch this, but recovering
	// here lets us log the specific request context and still return the
	// standard JSON error shape instead of a bare 500.
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[ImageValidationHandler] panic while validating image: %v", r)
			retErr = utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "internal_server_error"))
		}
	}()

	file, err := c.FormFile("file")
	if err != nil {
		log.Printf("[ImageValidationHandler] no file uploaded: %v", err)
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "no_file_uploaded"))
	}

	if int(file.Size) > h.cfg.MaxSizeBytes {
		log.Printf("[ImageValidationHandler] rejected upload %q: size %d bytes exceeds max %d bytes", file.Filename, file.Size, h.cfg.MaxSizeBytes)
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "image_validation_file_too_large"))
	}

	contentType := file.Header.Get("Content-Type")
	allowed := false
	for _, mimeType := range h.cfg.AllowedMimeTypes {
		if contentType == mimeType {
			allowed = true
			break
		}
	}
	if !allowed {
		log.Printf("[ImageValidationHandler] rejected upload %q: unsupported content type %q", file.Filename, contentType)
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_file_type_image"))
	}

	src, err := file.Open()
	if err != nil {
		log.Printf("[ImageValidationHandler] failed to open uploaded file %q: %v", file.Filename, err)
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_open_file"))
	}
	defer src.Close()

	data, err := io.ReadAll(src)
	if err != nil {
		log.Printf("[ImageValidationHandler] failed to read uploaded file %q: %v", file.Filename, err)
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_open_file"))
	}

	reason, err := h.service.ValidateImageQuality(data, h.cfg)
	if err != nil {
		log.Printf("[ImageValidationHandler] could not decode/validate image %q (%d bytes, %s): %v", file.Filename, len(data), contentType, err)
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_image_file"))
	}

	resp := imageValidationResponse{
		Valid:  reason == services.ImageValidationOK,
		Reason: string(reason),
	}

	statusCode := fiber.StatusOK
	switch reason {
	case services.ImageValidationMostlyBlack:
		resp.Message = i18n.T(c.UserContext(), "image_mostly_black")
		statusCode = fiber.StatusUnprocessableEntity
	case services.ImageValidationMostlyWhite:
		resp.Message = i18n.T(c.UserContext(), "image_mostly_white")
		statusCode = fiber.StatusUnprocessableEntity
	case services.ImageValidationLowDetail:
		resp.Message = i18n.T(c.UserContext(), "image_low_detail")
		statusCode = fiber.StatusUnprocessableEntity
	case services.ImageValidationBlurry:
		resp.Message = i18n.T(c.UserContext(), "image_blurry")
		statusCode = fiber.StatusUnprocessableEntity
	default:
		resp.Message = i18n.T(c.UserContext(), "image_valid")
	}

	if !resp.Valid {
		log.Printf("[ImageValidationHandler] rejected image %q: reason=%s", file.Filename, resp.Reason)
		return c.Status(statusCode).JSON(utils.Response{
			Success: false,
			Error:   resp.Message,
			Data:    resp,
		})
	}

	log.Printf("[ImageValidationHandler] accepted image %q", file.Filename)
	return utils.SuccessResponse(c, statusCode, resp.Message, resp)
}
