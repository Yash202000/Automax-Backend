package handlers

import (
	"encoding/json"
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
	internalUtils "github.com/automax/backend/internal/utils"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type IncidentHandler struct {
	service             services.IncidentService
	userService         services.UserService
	userRepo            repository.UserRepository
	incidentRepo        repository.IncidentRepository
	workflowRepo        repository.WorkflowRepository
	locationRepo        repository.LocationRepository
	classificationRepo  repository.ClassificationRepository
	storage             *storage.MinIOStorage
	presenceService     services.PresenceService
	readyToCloseService services.ReadyToCloseService
	ivrSmsLinkRepo      repository.IvrSmsLinkRepository
	publicFeedbackRepo  repository.IncidentPublicFeedbackRepository
	lookupRepo          repository.LookupRepository
	validator           *validator.Validate
}

func NewIncidentHandler(service services.IncidentService, userService services.UserService, userRepo repository.UserRepository, incidentRepo repository.IncidentRepository, workflowRepo repository.WorkflowRepository, locationRepo repository.LocationRepository, classificationRepo repository.ClassificationRepository, storage *storage.MinIOStorage, presenceService services.PresenceService) *IncidentHandler {
	return &IncidentHandler{
		service:            service,
		userService:        userService,
		userRepo:           userRepo,
		incidentRepo:       incidentRepo,
		workflowRepo:       workflowRepo,
		locationRepo:       locationRepo,
		classificationRepo: classificationRepo,
		storage:            storage,
		presenceService:    presenceService,
		validator:          validator.New(),
	}
}

// SetIvrSmsLinkRepo wires in the IvrSmsLinkRepository for SMS link lifecycle management.
func (h *IncidentHandler) SetIvrSmsLinkRepo(repo repository.IvrSmsLinkRepository) {
	h.ivrSmsLinkRepo = repo
}

// SetPublicFeedbackRepo wires in the feedback repository for submitted-status checks.
func (h *IncidentHandler) SetPublicFeedbackRepo(repo repository.IncidentPublicFeedbackRepository) {
	h.publicFeedbackRepo = repo
}

// SetReadyToCloseService wires in the ReadyToCloseService for duration-options endpoint.
func (h *IncidentHandler) SetReadyToCloseService(svc services.ReadyToCloseService) {
	h.readyToCloseService = svc
}

// SetLookupRepo wires in the LookupRepository for custom field category resolution.
func (h *IncidentHandler) SetLookupRepo(repo repository.LookupRepository) {
	h.lookupRepo = repo
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
		// Extract source from raw body for portal detection even when parsing fails
		source := extractSourceFromBody(c)
		return ErrorResponseWithKey(c, fiber.StatusBadRequest, "invalid_request_body", source)
	}

	// Parse query parameters
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		if isEpmPortalSource(req.Source) {
			var summaryParts []string
			for field, msg := range validationErrors {
				summaryParts = append(summaryParts, fmt.Sprintf("%s: %v", field, msg))
			}
			summary := strings.Join(summaryParts, "; ")
			return c.Status(fiber.StatusBadRequest).JSON(utils.ErrorCodeResponse{
				Success:   false,
				ErrorCode: "VALIDATION_ERROR",
				Error:     summary,
			})
		}
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
			return ErrorResponseWithKey(c, fiber.StatusConflict, "duplicate_incident", req.Source)

		case errors.Is(err, services.ErrInvalidLocation):
			return ErrorResponseWithKey(c, fiber.StatusBadRequest, "invalid_location_class", req.Source)

		default:
			return ErrorResponseWithKey(c, fiber.StatusInternalServerError, "internal_server_error", req.Source)
		}
	}

	if isEpmPortalRequest(c) || isEpmPortalSource(req.Source) {
		return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "incident_created"), h.buildEpmPortalResponse(c, incident, true))
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "incident_created"), incident)
}

func (h *IncidentHandler) GetIncident(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		// Not a UUID — try resolving by incident number
		inc, lookupErr := h.incidentRepo.FindByIncidentNumber(c.UserContext(), idStr)
		if lookupErr != nil {
			return ErrorResponseWithKey(c, fiber.StatusNotFound, "incident_not_found")
		}
		id = inc.ID
	}

	log.Printf("Generate Signed url: %s", utils.GenerateIncidentToken(id.String(), 24*time.Hour))
	incident, err := h.service.GetIncident(c.UserContext(), id)
	if err != nil {
		return ErrorResponseWithKey(c, fiber.StatusNotFound, "incident_not_found")
	}

	if isEpmPortalRequest(c) {
		// Phone-number scoping: if reporter_phone is provided, verify ownership
		if phone := c.Query("reporter_phone"); phone != "" {
			phoneWithPlus := "+" + phone
			if strings.HasPrefix(phone, "+") {
				phoneWithPlus = phone
				phone = strings.TrimPrefix(phone, "+")
			}
			incPhone := incident.ReporterPhone
			if incPhone != phone && incPhone != phoneWithPlus {
				return ErrorResponseWithKey(c, fiber.StatusNotFound, "incident_not_found")
			}
		}
		return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "incident_retrieved"), h.buildEpmPortalResponse(c, &incident.IncidentResponse, true))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "incident_retrieved"), incident)
}

func (h *IncidentHandler) ListIncidents(c *fiber.Ctx) error {
	filter := &models.IncidentFilter{}
	// Parse query parameters
	if err := c.QueryParser(filter); err != nil {
		return ErrorResponseWithKey(c, fiber.StatusBadRequest, "invalid_query_parameters")
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

	// QueryParser cannot parse plain YYYY-MM-DD into *time.Time; do it manually.
	if startStr := c.Query("start_date"); startStr != "" {
		if t, err := time.Parse("2006-01-02", startStr); err == nil {
			filter.StartDate = &t
		} else if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			filter.StartDate = &t
		}
	}
	if endStr := c.Query("end_date"); endStr != "" {
		if t, err := time.Parse("2006-01-02", endStr); err == nil {
			// Include the full end day up to 23:59:59.
			endOfDay := time.Date(t.Year(), t.Month(), t.Day(), 23, 59, 59, 999999999, t.Location())
			filter.EndDate = &endOfDay
		} else if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			filter.EndDate = &t
		}
	}

	// Parse repeatable cf=key:value query params for flat custom_fields JSON filtering.
	// e.g. ?cf=caller_identity:1314245&cf=caller_identity_2:1111111 → AND both conditions.
	for _, cfParam := range c.Context().QueryArgs().PeekMulti("cf") {
		raw := string(cfParam)
		if idx := strings.IndexByte(raw, ':'); idx > 0 && idx < len(raw)-1 {
			filter.CustomFieldFilters = append(filter.CustomFieldFilters, models.CustomFieldFilter{
				Key:   raw[:idx],
				Value: raw[idx+1:],
			})
		}
	}

	// Restrict incident list by the user's assigned classifications and locations.
	// Super admins are exempt from this scoping unless RESTRICT_ADMIN_SCOPE=true,
	// where admins must also be restricted like regular users.
	noAccessSentinel := []string{"00000000-0000-0000-0000-000000000000"}
	restrictSuperAdmins := strings.EqualFold(strings.TrimSpace(os.Getenv("RESTRICT_ADMIN_SCOPE")), "true")
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	user, err := h.userRepo.FindByIDWithRelations(c.UserContext(), userID)

	if filter.ReporterPhoneSearch != "" && user != nil && !user.IsSuperAdmin && !user.HasPermission("incidents:filter_reporter_phone") {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "Insufficient permissions to filter by reporter phone")
	}

	if err == nil && user != nil && (!user.IsSuperAdmin || restrictSuperAdmins) {
		// Department-scoped: users with view_department_only see only their department's incidents.
		// Uses DeptManagerDepartmentID if set, otherwise falls back to DepartmentID.
		if user.HasPermission("incidents:view_department_only") {
			scopeDeptID := user.ScopeDepartmentID()
			if scopeDeptID != nil {
				deptIDStr := scopeDeptID.String()
				if len(filter.DepartmentID) > 0 {
					hasMatch := false
					for _, d := range filter.DepartmentID {
						if d == deptIDStr {
							hasMatch = true
							break
						}
					}
					if !hasMatch {
						filter.DepartmentID = noAccessSentinel
					}
				} else {
					filter.DepartmentID = []string{deptIDStr}
				}
			}
			// Also restrict by classification/location if DeptManager scope fields are set
			if user.DeptManagerClassificationID != nil {
				filter.ClassificationID = []string{user.DeptManagerClassificationID.String()}
			}
			if user.DeptManagerLocationID != nil {
				filter.LocationID = []string{user.DeptManagerLocationID.String()}
			}
		} else {
			userClassIDs := make([]string, 0, len(user.Classifications))
			for _, cls := range user.Classifications {
				userClassIDs = append(userClassIDs, cls.ID.String())
			}
			if len(userClassIDs) > 0 {
				if len(filter.ClassificationID) > 0 {
					// Intersect with request's classification filter
					requested := make(map[string]bool, len(filter.ClassificationID))
					for _, id := range filter.ClassificationID {
						requested[id] = true
					}
					var intersected []string
					for _, id := range userClassIDs {
						if requested[id] {
							intersected = append(intersected, id)
						}
					}
					if len(intersected) == 0 {
						// Requested classification(s) not among the user's own — no access, show nothing
						intersected = noAccessSentinel
					}
					filter.ClassificationID = intersected
				} else {
					filter.ClassificationID = userClassIDs
				}
			} else {
				// User has no classifications assigned — nothing should be visible
				filter.ClassificationID = noAccessSentinel
			}

			userLocIDs := make([]string, 0, len(user.Locations))
			for _, loc := range user.Locations {
				userLocIDs = append(userLocIDs, loc.ID.String())
			}
			if len(userLocIDs) > 0 {
				if len(filter.LocationID) > 0 {
					// Intersect with request's location filter
					requested := make(map[string]bool, len(filter.LocationID))
					for _, id := range filter.LocationID {
						requested[id] = true
					}
					var intersected []string
					for _, id := range userLocIDs {
						if requested[id] {
							intersected = append(intersected, id)
						}
					}
					if len(intersected) == 0 {
						// Requested location(s) not among the user's own — no access, show nothing
						intersected = noAccessSentinel
					}
					filter.LocationID = intersected
				} else {
					filter.LocationID = userLocIDs
				}
			} else {
				// User has no locations assigned — nothing should be visible
				filter.LocationID = noAccessSentinel
			}
		}

		// Pass user's role IDs so List() can expand my_record with state_viewable_roles
		roleIDs := make([]uuid.UUID, 0, len(user.Roles))
		for _, r := range user.Roles {
			roleIDs = append(roleIDs, r.ID)
		}
		filter.UserRoleIDs = roleIDs
	}

	incidents, total, err := h.service.ListIncidents(c.UserContext(), filter)
	if err != nil {
		return ErrorResponseWithKey(c, fiber.StatusInternalServerError, "internal_server_error")
	}

	totalPages := (int(total) + filter.Limit - 1) / filter.Limit

	if isEpmPortalRequest(c) {
		// @NOEDIT: this is not a bug this is a client requirement please dont touch
		// Portal requests must be scoped to a specific caller identity
		if filter.CallerIdentity == "" && filter.ReporterPhone == "" {
			return c.JSON(fiber.Map{
				"success":     true,
				"data":        []models.EpmPortalIncidentResponse{},
				"page":        filter.Page,
				"limit":       filter.Limit,
				"total_items": 0,
				"total_pages": 0,
			})
		}

		portalData := make([]models.EpmPortalIncidentResponse, len(incidents))
		for i := range incidents {
			portalData[i] = h.buildEpmPortalResponse(c, &incidents[i], false)
		}
		return c.JSON(fiber.Map{
			"success":     true,
			"data":        portalData,
			"page":        filter.Page,
			"limit":       filter.Limit,
			"total_items": total,
			"total_pages": totalPages,
		})
	}

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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "service_unavailable_client"))
	}

	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	last6Digits := c.Query("last6digits")
	if last6Digits == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "last_6_digits_required"))
	}

	// Validate signed token
	token := c.Query("signed_token")
	if token == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "token_required"))
	}
	log.Printf("Signed token recieved %s", token)
	if err := utils.ValidateIncidentToken(token, idStr); err != nil {
		log.Printf("Token validation failed: %v", err)
		switch err {
		case utils.ErrExpired:
			return utils.ErrorResponse(c, fiber.StatusGone, i18n.T(c.UserContext(), "link_expired"))
		case utils.ErrInvalid, utils.ErrIDMismatch:
			return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "invalid_token"))
		default:
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "malformed_token"))
		}
	}

	// Check the IVR SMS link record: is it still active, and has the citizen already submitted?
	if h.ivrSmsLinkRepo != nil {
		tokenHash := utils.HashToken(token)
		link, err := h.ivrSmsLinkRepo.FindByTokenHash(c.UserContext(), tokenHash, id)
		if err != nil || link == nil {
			log.Printf("IVR SMS link not found for token hash %s incident %s: %v", tokenHash, idStr, err)
			return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "link_not_recognized"))
		}
		if !link.IsActive {
			return utils.ErrorResponse(c, fiber.StatusGone, i18n.T(c.UserContext(), "newer_link_sent"))
		}
		if link.SubmittedAt != nil {
			// Citizen already submitted — tell the frontend to show the "Update Submitted" screen.
			return c.Status(fiber.StatusAlreadyReported).JSON(fiber.Map{
				"data": map[string]interface{}{
					"success":           false,
					"already_submitted": true,
					"error":             "You have already submitted your information for this incident.",
					// "error":             "You have already submitted your information for this incident.",
				},
			})
		}
	}

	log.Println("incident last6 digit op start")
	incident, err := h.service.FindByIDWithLast6DigitValidation(c.UserContext(), id, last6Digits)
	if err != nil {
		log.Printf("Err fetching Incident via last 6 digit and id %v ", err)
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "phone_not_recognized"))
	}

	authResponse, err := h.userService.GenerateTokenViaUserID(c.UserContext(), incident.ReporterID)
	if err != nil {
		log.Printf("Err creating auth response via last 6 digit and id %v ", err)
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "incident_not_exist_updated"))
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
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "incident_retrieved"), data)
}

// FindByIDForFeedback validates a closure feedback link and returns minimal incident info.
// Public endpoint — no authentication required.
// Input: path param :id (incident UUID), query param signed_token (incident/feedback token).
// Returns 410 Gone if feedback has already been submitted.
func (h *IncidentHandler) FindByIDForFeedback(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	token := c.Query("signed_token")
	if token == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "token_required"))
	}

	// Determine token type and validate; also extract feedbackID when available.
	var feedbackID *uuid.UUID

	if parsed, uuidErr := uuid.Parse(token); uuidErr == nil {
		// Plain UUID — treat as feedbackID directly, no HMAC validation.
		feedbackID = &parsed
	} else if parts := strings.Split(token, "|"); len(parts) == 5 && parts[0] == "fb" {
		// fb-format token: fb|feedbackID|incidentID|expiresAt|hmac
		if _, valErr := utils.ValidateFeedbackToken(token, parts[1], idStr); valErr != nil {
			log.Printf("[FindByIDForFeedback] fb-token validation failed for incident %s: %v", idStr, valErr)
			switch {
			case errors.Is(valErr, utils.ErrExpired):
				return utils.ErrorResponse(c, fiber.StatusGone, i18n.T(c.UserContext(), "link_expired"))
			default:
				return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "invalid_token"))
			}
		}
		if parsed, pErr := uuid.Parse(parts[1]); pErr == nil {
			feedbackID = &parsed
		}
	} else {
		// 3-part incident token
		if valErr := utils.ValidateIncidentToken(token, idStr); valErr != nil {
			log.Printf("[FindByIDForFeedback] token validation failed for incident %s: %v", idStr, valErr)
			switch valErr {
			case utils.ErrExpired:
				return utils.ErrorResponse(c, fiber.StatusGone, i18n.T(c.UserContext(), "link_expired"))
			case utils.ErrInvalid, utils.ErrIDMismatch:
				return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "invalid_token"))
			default:
				return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "malformed_token"))
			}
		}
	}

	// Check if feedback has already been submitted — if so the link is invalid.
	if h.publicFeedbackRepo != nil {
		var f *models.IncidentPublicFeedback
		if feedbackID != nil {
			f, _ = h.publicFeedbackRepo.FindByID(c.UserContext(), *feedbackID)
		} else {
			f, _ = h.publicFeedbackRepo.FindLatestByIncidentID(c.UserContext(), id)
		}
		if f != nil && f.SubmittedAt != nil {
			log.Printf("[FindByIDForFeedback] feedback already submitted for incident %s feedbackID %s", idStr, f.ID)
			return utils.ErrorResponse(c, fiber.StatusGone, i18n.T(c.UserContext(), "feedback_already_submitted"))
		}
	}

	incident, err := h.incidentRepo.FindByIDWithRelations(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "incident_not_found"))
	}

	status := ""
	if incident.CurrentState != nil {
		status = incident.CurrentState.Name
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "incident_retrieved"), fiber.Map{
		"incident_number": incident.IncidentNumber,
		"description":     incident.Description,
		"status":          status,
	})
}

func (h *IncidentHandler) UpdateIncident(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	roleIDs := h.getUserRoleIDs(c)

	var req models.IncidentUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
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
			return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(c.UserContext(), "forbidden_no_edit"))
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "incident_updated"), incident)
}

func (h *IncidentHandler) DeleteIncident(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	user, ok := c.Locals(constants.ContextKeys.User).(*models.User)
	if !ok || user == nil {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "Insufficient permissions")
	}

	record, err := h.incidentRepo.FindByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "incident_not_found"))
	}

	requiredPerm := "incidents:delete"
	if record.RecordType == "request" {
		requiredPerm = "requests:delete"
	}
	if !user.IsSuperAdmin && !user.HasPermission(requiredPerm) {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "Insufficient permissions")
	}

	if err := h.service.DeleteIncident(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "incident_deleted"), nil)
}

// ConvertToRequest converts an incident to a request
func (h *IncidentHandler) ConvertToRequest(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.ConvertToRequestRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
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

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "incident_converted"), result)
}

// CanConvertToRequest checks if the user can convert the incident to a request
func (h *IncidentHandler) CanConvertToRequest(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	roleIDs := h.getUserRoleIDs(c)

	canConvert, reason, err := h.service.CanConvertToRequest(c.UserContext(), id, roleIDs)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "incident_not_found"))
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "permission_check_completed"), fiber.Map{
		"can_convert": canConvert,
		"reason":      reason,
	})
}

// BulkConvertToRequest converts multiple incidents to requests in bulk
func (h *IncidentHandler) BulkConvertToRequest(c *fiber.Ctx) error {
	var req models.BulkConvertToRequestRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
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

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "bulk_conversion_completed"), result)
}

// State transitions

func (h *IncidentHandler) ExecuteTransition(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.IncidentTransitionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
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

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "transition_executed"), incident)
}

func (h *IncidentHandler) GetAvailableTransitions(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	roleIDs := h.getUserRoleIDs(c)

	transitions, err := h.service.GetAvailableTransitions(c.UserContext(), id, userID, roleIDs)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "available_transitions_retrieved"), transitions)
}

func (h *IncidentHandler) GetTransitionHistory(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	history, err := h.service.GetTransitionHistory(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "transition_history_retrieved"), history)
}

// Comments

func (h *IncidentHandler) AddComment(c *fiber.Ctx) error {
	incidentIDStr := c.Params("id")
	incidentID, err := uuid.Parse(incidentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
	}

	var req models.IncidentCommentRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
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

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "comment_added"), comment)
}

func (h *IncidentHandler) ListComments(c *fiber.Ctx) error {
	incidentIDStr := c.Params("id")
	incidentID, err := uuid.Parse(incidentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
	}

	comments, err := h.service.ListComments(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "comments_retrieved"), comments)
}

func (h *IncidentHandler) UpdateComment(c *fiber.Ctx) error {
	commentIDStr := c.Params("comment_id")
	commentID, err := uuid.Parse(commentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_comment_id"))
	}

	var req models.IncidentCommentRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	comment, err := h.service.UpdateComment(c.UserContext(), commentID, &req, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "comment_updated"), comment)
}

func (h *IncidentHandler) DeleteComment(c *fiber.Ctx) error {
	commentIDStr := c.Params("comment_id")
	commentID, err := uuid.Parse(commentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_comment_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	if err := h.service.DeleteComment(c.UserContext(), commentID, userID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "comment_deleted"), nil)
}

// Feedback

func (h *IncidentHandler) ListFeedbacks(c *fiber.Ctx) error {
	incidentIDStr := c.Params("id")
	incidentID, err := uuid.Parse(incidentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
	}

	feedbacks, err := h.service.ListFeedbacks(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "feedbacks_retrieved"), feedbacks)
}

// Attachments

func (h *IncidentHandler) UploadAttachment(c *fiber.Ctx) error {
	incidentIDStr := c.Params("id")
	incidentID, err := uuid.Parse(incidentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
	}

	file, err := c.FormFile("file")
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "no_file_uploaded"))
	}

	// Open the file
	src, err := file.Open()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_read_file"))
	}
	defer src.Close()

	// Upload to storage
	folder := fmt.Sprintf("incidents/%s", incidentID.String())
	filePath, err := h.storage.UploadFile(c.UserContext(), src, file, folder)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_upload_file"))
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

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "attachment_uploaded"), result)
}

func (h *IncidentHandler) UploadAttachmentIvrSms(c *fiber.Ctx) error {
	incidentIDStr := c.Params("id")
	incidentID, err := uuid.Parse(incidentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
	}

	incident, err := h.service.GetIncident(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "error_fetching_incident"))
	}

	if incident == nil || incident.ID == uuid.Nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "incident_not_found"))
	}
	// to add state check

	file, err := c.FormFile("file")
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "no_file_uploaded"))
	}

	// Open the file
	src, err := file.Open()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_read_file"))
	}
	defer src.Close()

	// Upload to storage
	folder := fmt.Sprintf("incidents/%s", incidentID.String())
	filePath, err := h.storage.UploadFile(c.UserContext(), src, file, folder)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_upload_file"))
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

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "attachment_uploaded"), result)
}

func (h *IncidentHandler) ListAttachments(c *fiber.Ctx) error {
	incidentIDStr := c.Params("id")
	incidentID, err := uuid.Parse(incidentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
	}

	attachments, err := h.service.ListAttachments(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "attachments_retrieved"), attachments)
}

func (h *IncidentHandler) DeleteAttachment(c *fiber.Ctx) error {
	attachmentIDStr := c.Params("attachment_id")
	attachmentID, err := uuid.Parse(attachmentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_attachment_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	if err := h.service.DeleteAttachment(c.UserContext(), attachmentID, userID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "attachment_deleted"), nil)
}

func (h *IncidentHandler) DownloadAttachment(c *fiber.Ctx) error {
	attachmentIDStr := c.Params("attachment_id")
	attachmentID, err := uuid.Parse(attachmentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_attachment_id"))
	}

	attachment, err := h.service.GetAttachment(c.UserContext(), attachmentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "attachment_not_found"))
	}

	file, err := h.storage.GetFile(c.UserContext(), attachment.FilePath)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_retrieve_file"))
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
	}

	var req struct {
		AssigneeID string `json:"assignee_id" validate:"required,uuid"`
	}
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	assigneeID, err := uuid.Parse(req.AssigneeID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_assignee_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	incident, err := h.service.AssignIncident(c.UserContext(), incidentID, assigneeID, userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "incident_assigned"), incident)
}

// Stats

func (h *IncidentHandler) GetStats(c *fiber.Ctx) error {
	filter := &models.IncidentFilter{}

	// Parse query parameters
	if err := c.QueryParser(filter); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_query_parameters"))
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

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "stats_retrieved"), stats)
}

func (h *IncidentHandler) GetStatsV2(c *fiber.Ctx) error {
	filter := &models.IncidentFilter{}

	// Parse query parameters
	if err := c.QueryParser(filter); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_query_parameters"))
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
	// except when RESTRICT_ADMIN_SCOPE=true, where super admins must also be scoped
	// by classification/location like regular users.
	restrictSuperAdmins := strings.EqualFold(strings.TrimSpace(os.Getenv("RESTRICT_ADMIN_SCOPE")), "true")
	isSuperAdmin := false
	if user, ok := c.Locals(constants.ContextKeys.User).(*models.User); ok && user != nil && user.IsSuperAdmin {
		isSuperAdmin = true
		filter.IsAdmin = true
	} else {
		filter.UserRoleIDs = h.getUserRoleIDs(c)
	}

	if !isSuperAdmin || restrictSuperAdmins {
		// Restrict stats to the user's assigned classifications and locations (mirrors ListIncidents scoping)
		noAccessSentinel := []string{"00000000-0000-0000-0000-000000000000"}
		statsUserID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
		if u, err := h.userRepo.FindByIDWithRelations(c.UserContext(), statsUserID); err == nil && u != nil {
			userClassIDs := make([]string, 0, len(u.Classifications))
			for _, cls := range u.Classifications {
				userClassIDs = append(userClassIDs, cls.ID.String())
			}
			if len(userClassIDs) > 0 {
				if len(filter.ClassificationID) > 0 {
					requested := make(map[string]bool, len(filter.ClassificationID))
					for _, id := range filter.ClassificationID {
						requested[id] = true
					}
					var intersected []string
					for _, id := range userClassIDs {
						if requested[id] {
							intersected = append(intersected, id)
						}
					}
					if len(intersected) == 0 {
						intersected = noAccessSentinel
					}
					filter.ClassificationID = intersected
				} else {
					filter.ClassificationID = userClassIDs
				}
			} else {
				filter.ClassificationID = noAccessSentinel
			}

			userLocIDs := make([]string, 0, len(u.Locations))
			for _, loc := range u.Locations {
				userLocIDs = append(userLocIDs, loc.ID.String())
			}
			if len(userLocIDs) > 0 {
				if len(filter.LocationID) > 0 {
					requested := make(map[string]bool, len(filter.LocationID))
					for _, id := range filter.LocationID {
						requested[id] = true
					}
					var intersected []string
					for _, id := range userLocIDs {
						if requested[id] {
							intersected = append(intersected, id)
						}
					}
					if len(intersected) == 0 {
						intersected = noAccessSentinel
					}
					filter.LocationID = intersected
				} else {
					filter.LocationID = userLocIDs
				}
			} else {
				filter.LocationID = noAccessSentinel
			}
		}
	}

	stats, err := h.service.GetStatsV2(c.UserContext(), filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "stats_retrieved"), stats)
}

func (h *IncidentHandler) GetPriorityCounts(c *fiber.Ctx) error {
	filter := &models.IncidentFilter{}

	// Filter by record_type (incident, request, complaint)

	// Parse query parameters
	if err := c.QueryParser(filter); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_query_parameters"))
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

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "priority_counts_retrieved"), counts)
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

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "sla_breached_retrieved"), incidents)
}

// Revisions

func (h *IncidentHandler) ListRevisions(c *fiber.Ctx) error {
	idParam := c.Params("id")
	incidentID, err := uuid.Parse(idParam)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
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

// applyDepartmentScope restricts the filter to the user's department scope.
// Uses DeptManagerDepartmentID if set, otherwise falls back to DepartmentID.
func (h *IncidentHandler) applyDepartmentScope(c *fiber.Ctx, filter *models.IncidentFilter) {
	noAccessSentinel := []string{"00000000-0000-0000-0000-000000000000"}
	userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	if !ok {
		return
	}
	user, err := h.userRepo.FindByIDWithRelations(c.UserContext(), userID)
	if err != nil || user == nil {
		return
	}
	if user.IsSuperAdmin || !user.HasPermission("incidents:view_department_only") {
		return
	}
	scopeDeptID := user.ScopeDepartmentID()
	if scopeDeptID != nil {
		deptIDStr := scopeDeptID.String()
		if len(filter.DepartmentID) > 0 {
			hasMatch := false
			for _, d := range filter.DepartmentID {
				if d == deptIDStr {
					hasMatch = true
					break
				}
			}
			if !hasMatch {
				filter.DepartmentID = noAccessSentinel
			}
		} else {
			filter.DepartmentID = []string{deptIDStr}
		}
	}
	if user.DeptManagerClassificationID != nil {
		filter.ClassificationID = []string{user.DeptManagerClassificationID.String()}
	}
	if user.DeptManagerLocationID != nil {
		filter.LocationID = []string{user.DeptManagerLocationID.String()}
	}
}

// Complaint handlers

func (h *IncidentHandler) CreateComplaint(c *fiber.Ctx) error {
	var req models.CreateComplaintRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
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

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "complaint_created"), complaint)
}

func (h *IncidentHandler) ListComplaints(c *fiber.Ctx) error {
	filter := &models.IncidentFilter{}

	// Force record_type to complaint
	recordType := "complaint"

	// Parse query parameters
	if err := c.QueryParser(filter); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_query_parameters"))
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
	h.applyDepartmentScope(c, filter)
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	complaint, err := h.service.GetIncident(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "complaint_not_found"))
	}

	// Verify it's a complaint
	if complaint.RecordType != "complaint" {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "complaint_not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "complaint_retrieved"), complaint)
}

func (h *IncidentHandler) IncrementEvaluation(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	// TriggerEvaluation validates the state, checks for active integration triggers,
	// increments the evaluation count, and fires the transition triggers.
	if err := h.service.TriggerEvaluation(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	// Return updated complaint
	complaint, err := h.service.GetIncident(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "evaluation_triggered"), complaint)
}

// Query handlers

func (h *IncidentHandler) CreateQuery(c *fiber.Ctx) error {
	var req models.CreateQueryRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
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

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "query_created"), query)
}

func (h *IncidentHandler) ListQueries(c *fiber.Ctx) error {
	filter := &models.IncidentFilter{}

	// Force record_type to query
	recordType := "query"

	// Parse query parameters
	if err := c.QueryParser(filter); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_query_parameters"))
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

	h.applyDepartmentScope(c, filter)

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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	query, err := h.service.GetIncident(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "query_not_found"))
	}

	// Verify it's a query
	if query.RecordType != "query" {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "query_not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "query_retrieved"), query)
}

// Presence Management

// MarkPresence marks a user as actively viewing an incident
// POST /incidents/:id/presence
func (h *IncidentHandler) MarkPresence(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
	}

	// Get user info from context (set by auth middleware)
	userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	if !ok {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "user_not_authenticated"))
	}

	userName, _ := c.Locals(constants.ContextKeys.UserName).(string)
	userEmail, _ := c.Locals(constants.ContextKeys.UserEmail).(string)

	user := services.PresenceInfo{
		UserID:    userID,
		UserName:  userName,
		UserEmail: userEmail,
	}

	if err := h.presenceService.MarkPresence(c.UserContext(), incidentID, user); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_mark_presence"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "presence_marked"), nil)
}

// GetPresence retrieves all users currently viewing an incident
// GET /incidents/:id/presence
func (h *IncidentHandler) GetPresence(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
	}

	activeUsers, err := h.presenceService.GetActiveUsers(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_get_presence"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "active_users_retrieved"), activeUsers)
}

// RemovePresence removes a user's presence from an incident
// DELETE /incidents/:id/presence
func (h *IncidentHandler) RemovePresence(c *fiber.Ctx) error {
	incidentID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
	}

	userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	if !ok {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "user_not_authenticated"))
	}

	if err := h.presenceService.RemovePresence(c.UserContext(), incidentID, userID); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_remove_presence"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "presence_removed"), nil)
}

// UpdateClosedIncidentSummary handles PATCH /incidents/:id/closed-summary
// Allows users with 'incidents:edit-closed' permission to edit closed incident descriptions
func (h *IncidentHandler) UpdateClosedIncidentSummary(c *fiber.Ctx) error {
	incidentIDStr := c.Params("id")
	incidentID, err := uuid.Parse(incidentIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
	}

	// Get current user
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	user, err := h.userRepo.FindByIDWithRelations(c.UserContext(), userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "user_not_found"))
	}

	// Get incident
	incident, err := h.service.GetIncident(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "incident_not_found"))
	}

	// Check if incident is closed (terminal state)
	if incident.CurrentState != nil && incident.CurrentState.StateType != "terminal" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "incident_not_closed"))
	}

	// Check permission
	if !user.HasPermission("incidents:edit-closed") {
		return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(c.UserContext(), "forbidden_edit_closed"))
	}

	// Parse request
	var req struct {
		Description string `json:"description" validate:"required"`
		Reason      string `json:"reason"`
	}
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
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

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "closed_summary_updated"), updatedIncident)
}

// RequestCitizenInfo sends an SMS to the citizen requesting additional information (attachments, location, comments)
// for an incident that lacks required fields like attachments.
func (h *IncidentHandler) RequestCitizenInfo(c *fiber.Ctx) error {
	idStr := c.Params("id")
	incidentID, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	// Fetch incident with relations to get attachments, reporter info
	incident, err := h.incidentRepo.FindByIDWithRelations(c.UserContext(), incidentID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "incident_not_found"))
	}

	// Determine citizen phone number to send SMS to.
	// ReporterPhone is the citizen's captured phone across all channels (IVR, web, mobile, agent-logged).
	// CreatedByMobile is a MOMRA/EPM-specific fallback (same value as ReporterPhone in that flow).
	// ReporterID fallback is only used when reporter is confirmed to have the citizen role.
	mobile := incident.ReporterPhone
	if mobile == "" {
		mobile = incident.CreatedByMobile
	}
	if mobile == "" && incident.ReporterID != nil {
		roles, err := h.userRepo.GetUserRoles(c.UserContext(), *incident.ReporterID)
		if err == nil {
			for _, r := range roles {
				if r.Code == constants.USER_ROLE.CITIZEN {
					if reporter, err := h.userRepo.FindByID(c.UserContext(), *incident.ReporterID); err == nil && reporter.Phone != "" {
						mobile = reporter.Phone
					}
					break
				}
			}
		}
	}
	if mobile == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "no_mobile_number"))
	}

	// Build secure SMS link — generate the raw token first so we can hash and store it.
	const ivrSmsDuration = 48 * time.Hour
	rawToken := utils.GenerateIncidentToken(incidentID.String(), ivrSmsDuration)
	smsLink := utils.BuildSMSLinkFromToken(c.UserContext(), incidentID.String(), rawToken)

	smsMessage := fmt.Sprintf(
		"Dear Citizen, please provide additional details for your reported incident (%s) using the following secure link: %s",
		incident.IncidentNumber,
		smsLink,
	)

	now := time.Now()
	smsErr := internalUtils.SendSMS(mobile, smsMessage)
	status := "sent"
	if smsErr != nil {
		status = "failed"
		log.Printf("REQUEST-INFO-SMS: Failed for incident %s to %s: %v", incident.IncidentNumber, mobile, smsErr)
	} else {
		log.Printf("REQUEST-INFO-SMS: Sent successfully to %s for incident %s", mobile, incident.IncidentNumber)
	}

	// Track IVR SMS link: deactivate old links, create new record.
	if h.ivrSmsLinkRepo != nil && smsErr == nil {
		_ = h.ivrSmsLinkRepo.DeactivateAllForIncident(c.UserContext(), incidentID)
		_ = h.ivrSmsLinkRepo.Create(c.UserContext(), &models.IvrSmsLink{
			IncidentID: incidentID,
			TokenHash:  utils.HashToken(rawToken),
			SentBy:     &userID,
			SentAt:     now,
			ExpiresAt:  now.Add(ivrSmsDuration),
			IsActive:   true,
		})
		// Log revision so agents can see the SMS send event in the audit trail.
		_ = h.service.CreateRevision(c.UserContext(), incidentID,
			models.RevisionActionIVRSmsSent,
			fmt.Sprintf("Agent requested additional information from citizen via SMS to %s", mobile),
			nil, userID)
	}

	// Log notification
	notification := &models.NotificationLog{
		Channel:    "sms",
		Direction:  "outbound",
		Category:   "sent",
		Language:   "en",
		Recipients: models.RecipientArray{{Email: mobile, Type: "to", Status: status}},
		Subject:    "Request Additional Information",
		Body:       smsMessage,
		Status:     status,
		Provider:   "twilio",
		IsRead:     false,
		SentBy:     &userID,
		SentAt:     &now,
	}
	if smsErr != nil {
		notification.ErrorMessage = smsErr.Error()
	}
	if err := h.incidentRepo.CreateNotification(c.UserContext(), notification); err != nil {
		log.Printf("REQUEST-INFO-SMS: Failed to log notification for incident %s: %v", incident.IncidentNumber, err)
	}

	if smsErr != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.Tf(c.UserContext(), "failed_to_send_sms", smsErr))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "sms_sent_citizen"), fiber.Map{
		"mobile":          mobile,
		"incident_number": incident.IncidentNumber,
		"status":          status,
	})
}

// ForceState directly sets current_state_id on an incident, bypassing workflow
// transition logic. Intended for system-level bridge syncs only.
// Accepts either state_id (UUID) or state_name, resolves and validates against
// the incident's assigned workflow before applying.
func (h *IncidentHandler) ForceState(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_incident_id"))
	}
	var req struct {
		StateID   string `json:"state_id"`
		StateName string `json:"state_name"`
	}
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if req.StateID == "" && req.StateName == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "provide_state_id_or_name"))
	}

	// Fetch incident to get its assigned workflow
	incident, err := h.incidentRepo.FindByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "incident_not_found"))
	}

	var resolvedStateID uuid.UUID

	if req.StateID != "" {
		// Resolve by ID — verify it belongs to this incident's workflow
		stateID, err := uuid.Parse(req.StateID)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_state_id"))
		}
		state, err := h.workflowRepo.FindStateByID(c.UserContext(), stateID)
		if err != nil || state == nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "state_not_found"))
		}
		if state.WorkflowID != incident.WorkflowID {
			return utils.ErrorResponse(c, fiber.StatusBadRequest,
				i18n.T(c.UserContext(), "state_not_in_workflow"))
		}
		resolvedStateID = stateID
	} else {
		// Resolve by name — look up within the incident's workflow states
		states, err := h.workflowRepo.ListStatesByWorkflowID(c.UserContext(), incident.WorkflowID)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_fetch_states"))
		}
		for _, s := range states {
			if s.Name == req.StateName {
				resolvedStateID = s.ID
				break
			}
		}
		if resolvedStateID == uuid.Nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest,
				i18n.T(c.UserContext(), "state_not_found"))
		}
	}

	if err := h.incidentRepo.UpdateFields(c.UserContext(), id, map[string]interface{}{
		"current_state_id": resolvedStateID,
	}); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update_state"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "state_updated"), nil)
}

// ---- EPM Portal helpers ----

func isEpmPortalRequest(c *fiber.Ctx) bool {
	clientCode := strings.TrimSpace(os.Getenv("CLIENT_CODE"))
	return strings.EqualFold(clientCode, constants.CLIENT_CODE.EPM940) &&
		strings.EqualFold(c.Query("source"), constants.INCIDENT_SOURCE.EPMPORTAL)
}

// isEpmPortalSource checks the request body source field (for POST endpoints where source is in the body, not query string).
func isEpmPortalSource(source string) bool {
	clientCode := strings.TrimSpace(os.Getenv("CLIENT_CODE"))
	return strings.EqualFold(clientCode, constants.CLIENT_CODE.EPM940) &&
		strings.EqualFold(source, constants.INCIDENT_SOURCE.EPMPORTAL)
}

// extractSourceFromBody attempts to read the "source" field from the raw request body.
// Used when BodyParser fails (malformed JSON) but we still need portal detection.
func extractSourceFromBody(c *fiber.Ctx) string {
	var partial struct {
		Source string `json:"source"`
	}
	if err := json.Unmarshal(c.Body(), &partial); err == nil && partial.Source != "" {
		return partial.Source
	}
	return ""
}

// buildEpmPortalResponse builds the portal-trimmed response. When includeGis is true
// (single-incident detail), the gis_location field is populated; for listing it is omitted.
func (h *IncidentHandler) buildEpmPortalResponse(c *fiber.Ctx, incident *models.IncidentResponse, includeGis bool) models.EpmPortalIncidentResponse {
	resp := models.EpmPortalIncidentResponse{
		ID:             incident.ID,
		IncidentNumber: incident.IncidentNumber,
		ReporterName:   incident.ReporterName,
		ReporterEmail:  incident.ReporterEmail,
		ReporterPhone:  incident.ReporterPhone,
		CallerIdentity: incident.CallerIdentity,
		Address:        incident.Address,
		Description:    incident.Description,
	}

	// Classification hierarchy — nested chain (root → leaf)
	if incident.Classification != nil {
		classID := incident.Classification.ID
		if nodes, err := h.classificationRepo.GetAncestors(c.UserContext(), classID); err == nil {
			flat := make([]models.EpmPortalHierarchyNode, len(nodes))
			for i, n := range nodes {
				flat[i] = models.EpmPortalHierarchyNode{Name: n.Name, NameAr: n.NameAr, Level: n.Level}
			}
			resp.Classification = models.BuildNestedHierarchy(flat)
		}
	}

	// Location hierarchy — nested chain (root → leaf)
	if incident.Location != nil {
		locID := incident.Location.ID
		if nodes, err := h.locationRepo.GetAncestors(c.UserContext(), locID); err == nil {
			flat := make([]models.EpmPortalHierarchyNode, len(nodes))
			for i, n := range nodes {
				flat[i] = models.EpmPortalHierarchyNode{Name: n.Name, NameAr: n.NameAr, Level: n.Level}
			}
			resp.Location = models.BuildNestedHierarchy(flat)
		}
	}

	// Current state
	if incident.CurrentState != nil {
		resp.CurrentState = &models.EpmPortalStateInfo{
			Name:   incident.CurrentState.Name,
			NameAr: incident.CurrentState.NameAr,
		}
	}

	// GIS location: only included for single-incident detail, not listing
	if includeGis && len(incident.GisLocation) > 0 {
		var cf interface{}
		if err := json.Unmarshal(incident.GisLocation, &cf); err == nil {
			resp.GisLocation = cf
		}
	}

	return resp
}
