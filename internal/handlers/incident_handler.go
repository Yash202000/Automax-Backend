package handlers

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/internal/storage"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type IncidentHandler struct {
	service             services.IncidentService
	userService         services.UserService
	userRepo            repository.UserRepository
	incidentRepo        repository.IncidentRepository
	storage             *storage.MinIOStorage
	presenceService     services.PresenceService
	readyToCloseService services.ReadyToCloseService
	validator           *validator.Validate
}

func NewIncidentHandler(service services.IncidentService, userService services.UserService, userRepo repository.UserRepository, incidentRepo repository.IncidentRepository, storage *storage.MinIOStorage, presenceService services.PresenceService) *IncidentHandler {
	return &IncidentHandler{
		service:         service,
		userService:     userService,
		userRepo:        userRepo,
		incidentRepo:    incidentRepo,
		storage:         storage,
		presenceService: presenceService,
		validator:       validator.New(),
	}
}

// SetReadyToCloseService wires in the ReadyToCloseService for duration-options endpoint.
func (h *IncidentHandler) SetReadyToCloseService(svc services.ReadyToCloseService) {
	h.readyToCloseService = svc
}

// GetReadyToCloseDurationOptions returns the global default duration options.
// State-specific options are embedded in WorkflowStateResponse.duration_options.
// GET /api/v1/incidents/ready-to-close/duration-options
func (h *IncidentHandler) GetReadyToCloseDurationOptions(c *fiber.Ctx) error {
	if h.readyToCloseService == nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"success": false,
			"error":   "ReadyToClose service not available",
		})
	}
	options := h.readyToCloseService.GetDurationOptionsForState(nil)
	return c.JSON(fiber.Map{
		"success": true,
		"data":    options,
	})
}

// Helper to get user's role IDs
func (h *IncidentHandler) getUserRoleIDs(c *fiber.Ctx) []uuid.UUID {

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	roles, err := h.userRepo.GetUserRoles(c.UserContext(), userID)
	if err != nil {
		return []uuid.UUID{}
	}

	roleIDs := make([]uuid.UUID, len(roles))
	for i, role := range roles {
		roleIDs[i] = role.ID
	}
	return roleIDs
}

// Incident CRUD

func (h *IncidentHandler) CreateIncident(c *fiber.Ctx) error {
	var req models.IncidentCreateRequest
	if err := c.BodyParser(&req); err != nil {
		fmt.Printf("CreateIncident: Body parsing error: %v\n", err)
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	// Parse query parameters
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	incident, err := h.service.CreateIncident(c.UserContext(), &req, userID)
	if err != nil {
		fmt.Println("error:", err)

		switch {
		case errors.Is(err, services.ErrDuplicateIncident):
			return utils.ErrorResponse(
				c,
				fiber.StatusConflict,
				"You reported the same incident earlier; we are on it. Please wait for it to be resolved. Feel free to raise a new incident if the classification or location is different.",
			)

		case errors.Is(err, services.ErrInvalidLocation):
			return utils.ErrorResponse(
				c,
				fiber.StatusBadRequest,
				"Invalid location or classification",
			)

		default:
			return utils.ErrorResponse(
				c,
				fiber.StatusInternalServerError,
				"Internal server error",
			)
		}
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Incident created", incident)
}

func (h *IncidentHandler) GetIncident(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	log.Printf("Generate Signed url: %s", utils.GenerateIncidentToken(idStr, 24*time.Hour))
	incident, err := h.service.GetIncident(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Incident not found")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Incident retrieved", incident)
}

func (h *IncidentHandler) ListIncidents(c *fiber.Ctx) error {
	filter := &models.IncidentFilter{}

	// Parse query parameters
	if err := c.QueryParser(filter); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid query parameters")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), filter); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	if filter.Page < 1 {
		filter.Page = 1
	}

	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 20
	}

	incidents, total, err := h.service.ListIncidents(c.UserContext(), filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	totalPages := (int(total) + filter.Limit - 1) / filter.Limit

	return c.JSON(fiber.Map{
		"success":     true,
		"data":        incidents,
		"page":        filter.Page,
		"limit":       filter.Limit,
		"total_items": total,
		"total_pages": totalPages,
	})
}
func (h *IncidentHandler) FindByIDWithLast6DigitValidation(c *fiber.Ctx) error {
	clientCode := strings.TrimSpace(os.Getenv("CLIENT_CODE"))
	if !strings.EqualFold(clientCode, constants.CLIENT_CODE.EPM940) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "this service is not available for current client")
	}

	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	last6Digits := c.Query("last6digits")
	if last6Digits == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Last 6 digits of phone number are required")
	}

	// Validate signed token
	token := c.Query("signed_token")
	if token == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Token is required")
	}
	log.Printf("Signed token recieved %s", token)
	if err := utils.ValidateIncidentToken(token, idStr); err != nil {
		log.Printf("Token validation failed: %v", err)
		switch err {
		case utils.ErrExpired:
			return utils.ErrorResponse(c, fiber.StatusGone, "Link has expired")
		case utils.ErrInvalid, utils.ErrIDMismatch:
			return utils.ErrorResponse(c, fiber.StatusUnauthorized, "Invalid or tampered token")
		default:
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "Malformed token")
		}
	}

	log.Println("incident last6 digit op start")
	incident, err := h.service.FindByIDWithLast6DigitValidation(c.UserContext(), id, last6Digits)
	if err != nil {
		log.Printf("Err fetching Incident via last 6 digit and id %v ", err)
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Incident not exist or already updated")
	}

	authResponse, err := h.userService.GenerateTokenViaUserID(c.UserContext(), incident.ReporterID)
	if err != nil {
		log.Printf("Err creating auth response via last 6 digit and id %v ", err)
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Incident not exist or already updated")
	}

	log.Printf("Incident fetched successfully, proceeding to authenticate user via last 6 digits")
	// Create Session and store in session and in response for short life of 15 minutes, to be used for subsequent requests without requiring last 6 digit validation again
	// Generate a 15-minute IVR session token for subsequent IVR SMS requests

	// sessionToken := utils.GenerateIvrSessionToken(idStr, 10*time.Minute)
	log.Println("incident last6 digit op end, successful authentication")

	data := fiber.Map{
		"incident": incident,
		// "session_token": sessionToken,
		"auth_data": authResponse,
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "Incident retrieved", data)
}

func (h *IncidentHandler) UpdateIncident(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	roleIDs := h.getUserRoleIDs(c)

	var req models.IncidentUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}
	clientCode := strings.TrimSpace(os.Getenv("CLIENT_CODE"))
	if strings.EqualFold(req.Source, constants.INCIDENT_SOURCE.IVR) && strings.EqualFold(clientCode, constants.CLIENT_CODE.EPM940) {
		if req.Source == "" || req.Source != constants.INCIDENT_SOURCE.IVR {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"success": false,
				"errors": map[string]string{
					"source": "Source is required and should be 'ivr' for IVR updates",
				},
			})
		}

		if req.Comment == "" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"success": false,
				"errors": map[string]string{
					"comment": "Comment is required for IVR updates",
				},
			})
		}
	}
	incident, err := h.service.UpdateIncident(c.UserContext(), id, &req, userID, roleIDs)
	if err != nil {
		if errors.Is(err, services.ErrEditNotAllowed) {
			return utils.ErrorResponse(c, fiber.StatusForbidden, "Your role does not have edit access for this incident at its current stage")
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Incident updated", incident)
}

func (h *IncidentHandler) DeleteIncident(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	if err := h.service.DeleteIncident(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Incident deleted", nil)
}

// ConvertToRequest converts an incident to a request
func (h *IncidentHandler) ConvertToRequest(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	var req models.ConvertToRequestRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	roleIDs := h.getUserRoleIDs(c)

	result, err := h.service.ConvertToRequest(c.UserContext(), id, &req, userID, roleIDs)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Incident converted to request", result)
}

// CanConvertToRequest checks if the user can convert the incident to a request
func (h *IncidentHandler) CanConvertToRequest(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	roleIDs := h.getUserRoleIDs(c)

	canConvert, reason, err := h.service.CanConvertToRequest(c.UserContext(), id, roleIDs)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Permission check completed", fiber.Map{
		"can_convert": canConvert,
		"reason":      reason,
	})
}

// BulkConvertToRequest converts multiple incidents to requests in bulk
func (h *IncidentHandler) BulkConvertToRequest(c *fiber.Ctx) error {
	var req models.BulkConvertToRequestRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	roleIDs := h.getUserRoleIDs(c)

	result, err := h.service.BulkConvertToRequest(c.UserContext(), &req, userID, roleIDs)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Bulk conversion completed", result)
}

// State transitions

func (h *IncidentHandler) ExecuteTransition(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	var req models.IncidentTransitionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	roleIDs := h.getUserRoleIDs(c)

	incident, err := h.service.ExecuteTransition(c.UserContext(), id, &req, userID, roleIDs)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Transition executed", incident)
}

func (h *IncidentHandler) GetAvailableTransitions(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	roleIDs := h.getUserRoleIDs(c)

	transitions, err := h.service.GetAvailableTransitions(c.UserContext(), id, roleIDs)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Available transitions retrieved", transitions)
}

func (h *IncidentHandler) GetTransitionHistory(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	history, err := h.service.GetTransitionHistory(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Transition history retrieved", history)
}

// Comments

func (h *IncidentHandler) AddComment(c *fiber.Ctx) error {
	incidentIDStr := c.Params("id")
	incidentID, err := uuid.Parse(incidentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid incident ID")
	}

	var req models.IncidentCommentRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	comment, err := h.service.AddComment(c.UserContext(), incidentID, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Comment added", comment)
}

func (h *IncidentHandler) ListComments(c *fiber.Ctx) error {
	incidentIDStr := c.Params("id")
	incidentID, err := uuid.Parse(incidentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid incident ID")
	}

	comments, err := h.service.ListComments(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Comments retrieved", comments)
}

func (h *IncidentHandler) UpdateComment(c *fiber.Ctx) error {
	commentIDStr := c.Params("comment_id")
	commentID, err := uuid.Parse(commentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid comment ID")
	}

	var req models.IncidentCommentRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	comment, err := h.service.UpdateComment(c.UserContext(), commentID, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Comment updated", comment)
}

func (h *IncidentHandler) DeleteComment(c *fiber.Ctx) error {
	commentIDStr := c.Params("comment_id")
	commentID, err := uuid.Parse(commentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid comment ID")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	if err := h.service.DeleteComment(c.UserContext(), commentID, userID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Comment deleted", nil)
}

// Attachments

func (h *IncidentHandler) UploadAttachment(c *fiber.Ctx) error {
	incidentIDStr := c.Params("id")
	incidentID, err := uuid.Parse(incidentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid incident ID")
	}

	file, err := c.FormFile("file")
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "No file uploaded")
	}

	// Open the file
	src, err := file.Open()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to read file")
	}
	defer src.Close()

	// Upload to storage
	folder := fmt.Sprintf("incidents/%s", incidentID.String())
	filePath, err := h.storage.UploadFile(c.UserContext(), src, file, folder)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to upload file")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	attachment := &models.IncidentAttachment{
		FileName:     file.Filename,
		FileSize:     file.Size,
		MimeType:     file.Header.Get("Content-Type"),
		FilePath:     filePath,
		UploadedByID: userID,
	}

	result, err := h.service.AddAttachment(c.UserContext(), incidentID, attachment)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Attachment uploaded", result)
}

func (h *IncidentHandler) UploadAttachmentIvrSms(c *fiber.Ctx) error {
	incidentIDStr := c.Params("id")
	incidentID, err := uuid.Parse(incidentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid incident ID")
	}

	incident, err := h.service.GetIncident(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Error while fetching Incident")
	}

	if incident == nil || incident.ID == uuid.Nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Incident Not found")
	}
	// to add state check

	file, err := c.FormFile("file")
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "No file uploaded")
	}

	// Open the file
	src, err := file.Open()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to read file")
	}
	defer src.Close()

	// Upload to storage
	folder := fmt.Sprintf("incidents/%s", incidentID.String())
	filePath, err := h.storage.UploadFile(c.UserContext(), src, file, folder)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to upload file")
	}

	// userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	attachment := &models.IncidentAttachment{
		FileName: file.Filename,
		FileSize: file.Size,
		MimeType: file.Header.Get("Content-Type"),
		FilePath: filePath,
		// UploadedByID: userID,
	}

	result, err := h.service.AddAttachment(c.UserContext(), incidentID, attachment)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Attachment uploaded", result)
}

func (h *IncidentHandler) ListAttachments(c *fiber.Ctx) error {
	incidentIDStr := c.Params("id")
	incidentID, err := uuid.Parse(incidentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid incident ID")
	}

	attachments, err := h.service.ListAttachments(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Attachments retrieved", attachments)
}

func (h *IncidentHandler) DeleteAttachment(c *fiber.Ctx) error {
	attachmentIDStr := c.Params("attachment_id")
	attachmentID, err := uuid.Parse(attachmentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid attachment ID")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	if err := h.service.DeleteAttachment(c.UserContext(), attachmentID, userID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Attachment deleted", nil)
}

func (h *IncidentHandler) DownloadAttachment(c *fiber.Ctx) error {
	attachmentIDStr := c.Params("attachment_id")
	attachmentID, err := uuid.Parse(attachmentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid attachment ID")
	}

	attachment, err := h.service.GetAttachment(c.UserContext(), attachmentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Attachment not found")
	}

	file, err := h.storage.GetFile(c.UserContext(), attachment.FilePath)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to retrieve file")
	}

	c.Set("Content-Type", attachment.MimeType)
	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", attachment.FileName))
	return c.SendStream(file)
}

// Assignment

func (h *IncidentHandler) AssignIncident(c *fiber.Ctx) error {
	incidentIDStr := c.Params("id")
	incidentID, err := uuid.Parse(incidentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid incident ID")
	}

	var req struct {
		AssigneeID string `json:"assignee_id" validate:"required,uuid"`
	}
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	assigneeID, err := uuid.Parse(req.AssigneeID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid assignee ID")
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	incident, err := h.service.AssignIncident(c.UserContext(), incidentID, assigneeID, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Incident assigned", incident)
}

// Stats

func (h *IncidentHandler) GetStats(c *fiber.Ctx) error {
	filter := &models.IncidentFilter{}

	// Parse query parameters
	if err := c.QueryParser(filter); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid query parameters")
	}

	filter.UserID = c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	if validationErrors := validation.ValidateStruct(c.UserContext(), filter); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	// Filter by record_type (incident, request, complaint)
	if filter.Page < 1 {
		filter.Page = 1
	}

	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 20
	}

	// Add user role IDs for state visibility filtering
	filter.UserRoleIDs = h.getUserRoleIDs(c)

	stats, err := h.service.GetStats(c.UserContext(), filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Stats retrieved", stats)
}

func (h *IncidentHandler) GetStatsV2(c *fiber.Ctx) error {
	filter := &models.IncidentFilter{}

	// Parse query parameters
	if err := c.QueryParser(filter); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid query parameters")
	}

	filter.UserID = c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	if validationErrors := validation.ValidateStruct(c.UserContext(), filter); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	// Filter by record_type (incident, request, complaint)
	if filter.Page < 1 {
		filter.Page = 1
	}

	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 20
	}

	// Super admins: IsAdmin=true (no role-visibility restriction on WorkflowStats),
	if user, ok := c.Locals(constants.ContextKeys.User).(*models.User); ok && user != nil && user.IsSuperAdmin {
		filter.IsAdmin = true
	} else {
		filter.UserRoleIDs = h.getUserRoleIDs(c)
	}

	stats, err := h.service.GetStatsV2(c.UserContext(), filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Stats retrieved", stats)
}

func (h *IncidentHandler) GetPriorityCounts(c *fiber.Ctx) error {
	filter := &models.IncidentFilter{}

	// Filter by record_type (incident, request, complaint)

	// Parse query parameters
	if err := c.QueryParser(filter); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid query parameters")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), filter); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	// Filter by record_type (incident, request, complaint)
	if filter.Page < 1 {
		filter.Page = 1
	}

	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 20
	}

	// Add user role IDs for state visibility filtering
	filter.UserRoleIDs = h.getUserRoleIDs(c)

	counts, err := h.service.GetPriorityCounts(c.UserContext(), filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Priority counts retrieved", counts)
}

// User queries

func (h *IncidentHandler) GetMyAssigned(c *fiber.Ctx) error {
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	recordType := c.Query("record_type", "") // Optional filter: incident, request, complaint

	incidents, total, err := h.service.GetMyAssigned(c.UserContext(), userID, recordType, page, limit)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	totalPages := (int(total) + limit - 1) / limit

	return c.JSON(fiber.Map{
		"success":     true,
		"data":        incidents,
		"page":        page,
		"limit":       limit,
		"total_items": total,
		"total_pages": totalPages,
	})
}

func (h *IncidentHandler) GetMyReported(c *fiber.Ctx) error {
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	recordType := c.Query("record_type", "") // Optional filter: incident, request, complaint

	incidents, total, err := h.service.GetMyReported(c.UserContext(), userID, recordType, page, limit)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	totalPages := (int(total) + limit - 1) / limit

	return c.JSON(fiber.Map{
		"success":     true,
		"data":        incidents,
		"page":        page,
		"limit":       limit,
		"total_items": total,
		"total_pages": totalPages,
	})
}

func (h *IncidentHandler) GetSLABreached(c *fiber.Ctx) error {
	incidents, err := h.service.GetSLABreached(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "SLA breached incidents retrieved", incidents)
}

// Revisions

func (h *IncidentHandler) ListRevisions(c *fiber.Ctx) error {
	idParam := c.Params("id")
	incidentID, err := uuid.Parse(idParam)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid incident ID")
	}

	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))

	filter := &models.IncidentRevisionFilter{
		Page:  page,
		Limit: limit,
	}

	// Optional action_type filter
	if actionType := c.Query("action_type"); actionType != "" {
		at := models.IncidentRevisionActionType(actionType)
		filter.ActionType = &at
	}

	// Optional performed_by_id filter
	if performedByStr := c.Query("performed_by_id"); performedByStr != "" {
		performedByID, err := uuid.Parse(performedByStr)
		if err == nil {
			filter.PerformedByID = &performedByID
		}
	}

	// Optional date filters
	if startDateStr := c.Query("start_date"); startDateStr != "" {
		startDate, err := time.Parse(time.RFC3339, startDateStr)
		if err == nil {
			filter.StartDate = &startDate
		}
	}
	if endDateStr := c.Query("end_date"); endDateStr != "" {
		endDate, err := time.Parse(time.RFC3339, endDateStr)
		if err == nil {
			filter.EndDate = &endDate
		}
	}

	revisions, total, err := h.service.ListRevisions(c.UserContext(), incidentID, filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	totalPages := (int(total) + limit - 1) / limit

	return c.JSON(fiber.Map{
		"success":     true,
		"data":        revisions,
		"page":        page,
		"limit":       limit,
		"total_items": total,
		"total_pages": totalPages,
	})
}

// Complaint handlers

func (h *IncidentHandler) CreateComplaint(c *fiber.Ctx) error {
	var req models.CreateComplaintRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	complaint, err := h.service.CreateComplaint(c.UserContext(), &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Complaint created", complaint)
}

func (h *IncidentHandler) ListComplaints(c *fiber.Ctx) error {
	filter := &models.IncidentFilter{}

	// Force record_type to complaint
	recordType := "complaint"

	// Parse query parameters
	if err := c.QueryParser(filter); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid query parameters")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), filter); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	// Filter by record_type (incident, request, complaint)
	if filter.Page < 1 {
		filter.Page = 1
	}

	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 20
	}

	filter.RecordType = &recordType
	complaints, total, err := h.service.ListIncidents(c.UserContext(), filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	totalPages := (int(total) + filter.Limit - 1) / filter.Limit

	return c.JSON(fiber.Map{
		"success":     true,
		"data":        complaints,
		"page":        filter.Page,
		"limit":       filter.Limit,
		"total_items": total,
		"total_pages": totalPages,
	})
}

func (h *IncidentHandler) GetComplaint(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	complaint, err := h.service.GetIncident(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Complaint not found")
	}

	// Verify it's a complaint
	if complaint.RecordType != "complaint" {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Complaint not found")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Complaint retrieved", complaint)
}

func (h *IncidentHandler) IncrementEvaluation(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	if err := h.service.IncrementEvaluationCount(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	// Return updated complaint
	complaint, err := h.service.GetIncident(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Evaluation count incremented", complaint)
}

// Query handlers

func (h *IncidentHandler) CreateQuery(c *fiber.Ctx) error {
	var req models.CreateQueryRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	query, err := h.service.CreateQuery(c.UserContext(), &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Query created", query)
}

func (h *IncidentHandler) ListQueries(c *fiber.Ctx) error {
	filter := &models.IncidentFilter{}

	// Force record_type to query
	recordType := "query"

	// Parse query parameters
	if err := c.QueryParser(filter); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid query parameters")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), filter); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	// Filter by record_type (incident, request, complaint)
	if filter.Page < 1 {
		filter.Page = 1
	}

	if filter.Limit < 1 || filter.Limit > 100 {
		filter.Limit = 20
	}
	filter.RecordType = &recordType

	queries, total, err := h.service.ListIncidents(c.UserContext(), filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	totalPages := (int(total) + filter.Limit - 1) / filter.Limit

	return c.JSON(fiber.Map{
		"success":     true,
		"data":        queries,
		"page":        filter.Page,
		"limit":       filter.Limit,
		"total_items": total,
		"total_pages": totalPages,
	})
}

func (h *IncidentHandler) GetQuery(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	query, err := h.service.GetIncident(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Query not found")
	}

	// Verify it's a query
	if query.RecordType != "query" {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Query not found")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Query retrieved", query)
}

// Presence Management

// MarkPresence marks a user as actively viewing an incident
// POST /incidents/:id/presence
func (h *IncidentHandler) MarkPresence(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid incident ID")
	}

	// Get user info from context (set by auth middleware)
	userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	if !ok {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, "User not authenticated")
	}

	userName, _ := c.Locals(constants.ContextKeys.UserName).(string)
	userEmail, _ := c.Locals(constants.ContextKeys.UserEmail).(string)

	user := services.PresenceInfo{
		UserID:    userID,
		UserName:  userName,
		UserEmail: userEmail,
	}

	if err := h.presenceService.MarkPresence(c.UserContext(), incidentID, user); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to mark presence")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Presence marked", nil)
}

// GetPresence retrieves all users currently viewing an incident
// GET /incidents/:id/presence
func (h *IncidentHandler) GetPresence(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid incident ID")
	}

	activeUsers, err := h.presenceService.GetActiveUsers(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to get presence data")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Active users retrieved", activeUsers)
}

// RemovePresence removes a user's presence from an incident
// DELETE /incidents/:id/presence
func (h *IncidentHandler) RemovePresence(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid incident ID")
	}

	userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	if !ok {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, "User not authenticated")
	}

	if err := h.presenceService.RemovePresence(c.UserContext(), incidentID, userID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to remove presence")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Presence removed", nil)
}

// UpdateClosedIncidentSummary handles PATCH /incidents/:id/closed-summary
// Allows users with 'incidents:edit-closed' permission to edit closed incident descriptions
func (h *IncidentHandler) UpdateClosedIncidentSummary(c *fiber.Ctx) error {
	incidentIDStr := c.Params("id")
	incidentID, err := uuid.Parse(incidentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid incident ID")
	}

	// Get current user
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	user, err := h.userRepo.FindByIDWithRelations(c.UserContext(), userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, "User not found")
	}

	// Get incident
	incident, err := h.service.GetIncident(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Incident not found")
	}

	// Check if incident is closed (terminal state)
	if incident.CurrentState != nil && incident.CurrentState.StateType != "terminal" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Incident is not closed")
	}

	// Check permission
	if !user.HasPermission("incidents:edit-closed") {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "You don't have permission to edit closed incidents")
	}

	// Parse request
	var req struct {
		Description string `json:"description" validate:"required"`
		Reason      string `json:"reason"`
	}
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	// Call service
	updatedIncident, err := h.service.UpdateClosedIncidentSummary(
		c.UserContext(),
		incidentID,
		userID,
		req.Description,
		req.Reason,
	)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Closed incident summary updated", updatedIncident)
}
