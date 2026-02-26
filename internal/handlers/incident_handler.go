package handlers

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/internal/storage"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type IncidentHandler struct {
	service         services.IncidentService
	userRepo        repository.UserRepository
	incidentRepo    repository.IncidentRepository
	storage         *storage.MinIOStorage
	presenceService services.PresenceService
	validator       *validator.Validate
}

func NewIncidentHandler(service services.IncidentService, userRepo repository.UserRepository, incidentRepo repository.IncidentRepository, storage *storage.MinIOStorage, presenceService services.PresenceService) *IncidentHandler {
	return &IncidentHandler{
		service:         service,
		userRepo:        userRepo,
		incidentRepo:    incidentRepo,
		storage:         storage,
		presenceService: presenceService,
		validator:       validator.New(),
	}
}

// Helper to get user's role IDs
func (h *IncidentHandler) getUserRoleIDs(c *fiber.Ctx) []uuid.UUID {
	userID := c.Locals("user_id").(uuid.UUID)
	roles, err := h.userRepo.GetUserRoles(c.Context(), userID)
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

	userID := c.Locals("user_id").(uuid.UUID)

	incident, err := h.service.CreateIncident(c.Context(), &req, userID)
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

	incident, err := h.service.GetIncident(c.Context(), id)
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

	incidents, total, err := h.service.ListIncidents(c.Context(), filter)
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

func (h *IncidentHandler) UpdateIncident(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	userID := c.Locals("user_id").(uuid.UUID)

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

	incident, err := h.service.UpdateIncident(c.Context(), id, &req, userID)
	if err != nil {
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

	if err := h.service.DeleteIncident(c.Context(), id); err != nil {
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

	userID := c.Locals("user_id").(uuid.UUID)
	roleIDs := h.getUserRoleIDs(c)

	result, err := h.service.ConvertToRequest(c.Context(), id, &req, userID, roleIDs)
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

	canConvert, reason, err := h.service.CanConvertToRequest(c.Context(), id, roleIDs)
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

	userID := c.Locals("user_id").(uuid.UUID)
	roleIDs := h.getUserRoleIDs(c)

	result, err := h.service.BulkConvertToRequest(c.Context(), &req, userID, roleIDs)
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

	userID := c.Locals("user_id").(uuid.UUID)
	roleIDs := h.getUserRoleIDs(c)

	incident, err := h.service.ExecuteTransition(c.Context(), id, &req, userID, roleIDs)
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

	transitions, err := h.service.GetAvailableTransitions(c.Context(), id, roleIDs)
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

	history, err := h.service.GetTransitionHistory(c.Context(), id)
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

	userID := c.Locals("user_id").(uuid.UUID)

	comment, err := h.service.AddComment(c.Context(), incidentID, &req, userID)
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

	comments, err := h.service.ListComments(c.Context(), incidentID)
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

	userID := c.Locals("user_id").(uuid.UUID)

	comment, err := h.service.UpdateComment(c.Context(), commentID, &req, userID)
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

	userID := c.Locals("user_id").(uuid.UUID)

	if err := h.service.DeleteComment(c.Context(), commentID, userID); err != nil {
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
	filePath, err := h.storage.UploadFile(c.Context(), src, file, folder)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to upload file")
	}

	userID := c.Locals("user_id").(uuid.UUID)

	attachment := &models.IncidentAttachment{
		FileName:     file.Filename,
		FileSize:     file.Size,
		MimeType:     file.Header.Get("Content-Type"),
		FilePath:     filePath,
		UploadedByID: userID,
	}

	result, err := h.service.AddAttachment(c.Context(), incidentID, attachment)
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

	attachments, err := h.service.ListAttachments(c.Context(), incidentID)
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

	userID := c.Locals("user_id").(uuid.UUID)

	if err := h.service.DeleteAttachment(c.Context(), attachmentID, userID); err != nil {
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

	attachment, err := h.service.GetAttachment(c.Context(), attachmentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Attachment not found")
	}

	file, err := h.storage.GetFile(c.Context(), attachment.FilePath)
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

	userID := c.Locals("user_id").(uuid.UUID)

	incident, err := h.service.AssignIncident(c.Context(), incidentID, assigneeID, userID)
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

	stats, err := h.service.GetStats(c.Context(), filter)
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

	counts, err := h.service.GetPriorityCounts(c.Context(), filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Priority counts retrieved", counts)
}

// User queries

func (h *IncidentHandler) GetMyAssigned(c *fiber.Ctx) error {
	userID := c.Locals("user_id").(uuid.UUID)

	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	recordType := c.Query("record_type", "") // Optional filter: incident, request, complaint

	incidents, total, err := h.service.GetMyAssigned(c.Context(), userID, recordType, page, limit)
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
	userID := c.Locals("user_id").(uuid.UUID)

	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "20"))
	recordType := c.Query("record_type", "") // Optional filter: incident, request, complaint

	incidents, total, err := h.service.GetMyReported(c.Context(), userID, recordType, page, limit)
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
	incidents, err := h.service.GetSLABreached(c.Context())
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

	revisions, total, err := h.service.ListRevisions(c.Context(), incidentID, filter)
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

	userID := c.Locals("user_id").(uuid.UUID)

	complaint, err := h.service.CreateComplaint(c.Context(), &req, userID)
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
	complaints, total, err := h.service.ListIncidents(c.Context(), filter)
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

	complaint, err := h.service.GetIncident(c.Context(), id)
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

	if err := h.service.IncrementEvaluationCount(c.Context(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	// Return updated complaint
	complaint, err := h.service.GetIncident(c.Context(), id)
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

	userID := c.Locals("user_id").(uuid.UUID)

	query, err := h.service.CreateQuery(c.Context(), &req, userID)
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

	queries, total, err := h.service.ListIncidents(c.Context(), filter)
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

	query, err := h.service.GetIncident(c.Context(), id)
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
	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, "User not authenticated")
	}

	userName, _ := c.Locals("user_name").(string)
	userEmail, _ := c.Locals("user_email").(string)

	user := services.PresenceInfo{
		UserID:    userID,
		UserName:  userName,
		UserEmail: userEmail,
	}

	if err := h.presenceService.MarkPresence(c.Context(), incidentID, user); err != nil {
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

	activeUsers, err := h.presenceService.GetActiveUsers(c.Context(), incidentID)
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

	userID, ok := c.Locals("user_id").(uuid.UUID)
	if !ok {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, "User not authenticated")
	}

	if err := h.presenceService.RemovePresence(c.Context(), incidentID, userID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to remove presence")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Presence removed", nil)
}
