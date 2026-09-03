package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/automax/backend/internal/database"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/storage"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EPMIncidentHandler struct {
	userRepo           repository.UserRepository
	locationRepo       repository.LocationRepository
	classificationRepo repository.ClassificationRepository
	incidentRepo       repository.IncidentRepository
	workflowRepo       repository.WorkflowRepository
	lookupRepo         repository.LookupRepository
	departmentRepo     repository.DepartmentRepository
	jwtManager         *utils.JWTManager
	sessionStore       *database.SessionStore
	storage            *storage.MinIOStorage
	db                 *gorm.DB
}

func NewEPMIncidentHandler(
	userRepo repository.UserRepository,
	locationRepo repository.LocationRepository,
	classificationRepo repository.ClassificationRepository,
	incidentRepo repository.IncidentRepository,
	workflowRepo repository.WorkflowRepository,
	lookupRepo repository.LookupRepository,
	departmentRepo repository.DepartmentRepository,
	jwtManager *utils.JWTManager,
	sessionStore *database.SessionStore,
	minioStorage *storage.MinIOStorage,
	db *gorm.DB,
) *EPMIncidentHandler {
	return &EPMIncidentHandler{
		userRepo:           userRepo,
		locationRepo:       locationRepo,
		classificationRepo: classificationRepo,
		incidentRepo:       incidentRepo,
		workflowRepo:       workflowRepo,
		lookupRepo:         lookupRepo,
		departmentRepo:     departmentRepo,
		jwtManager:         jwtManager,
		sessionStore:       sessionStore,
		storage:            minioStorage,
		db:                 db,
	}
}

// externalEntityDepartmentType is the Department.Type value used for External Entities (EE).
const externalEntityDepartmentType = "external"

// resolveEERoutingDepartment looks up the department an incident must be assigned to when
// EEFlag is true. eeName is matched against Department.Name / Department.NameAr and must
// resolve to an active, external-type department, mirroring TFIS rule IF01-V06 / ERR-003
// (an EE that is not defined or not active must reject the request, not silently proceed).
func (h *EPMIncidentHandler) resolveEERoutingDepartment(ctx context.Context, eeName string) (*uuid.UUID, error) {
	trimmed := strings.TrimSpace(eeName)
	if trimmed == "" {
		return nil, errors.New(i18n.T(ctx, "ee_name_required"))
	}

	dept, err := h.departmentRepo.FindByNameOrNameAr(ctx, trimmed, trimmed)
	if err != nil || dept == nil {
		return nil, fmt.Errorf(i18n.T(ctx, "ee_not_defined"), eeName)
	}
	if dept.Type != externalEntityDepartmentType || !dept.IsActive {
		return nil, fmt.Errorf(i18n.T(ctx, "ee_not_defined_or_inactive"), eeName)
	}

	return &dept.ID, nil
}

type EPMInsertIncidentRequest struct {
	Address              string `json:"address"`
	BeneficiaryInfo      string `json:"beneficiaryInfo"`
	DistrictCode         int    `json:"districtCode"`
	DistrictName         string `json:"districtName"`
	EEFlag               bool   `json:"eeFlag"`
	EEName               string `json:"eeName"`
	Email                string `json:"email"`
	FileKey              string `json:"fileKey"`
	FirstName            string `json:"firstName"`
	IncidentNo           string `json:"incidentNo"`
	IncidentStartDate    string `json:"incidentStartDate"`
	IncidentStatusID     int    `json:"incidentStatusID"`
	IqamaID              string `json:"iqamaID"`
	IssueDiscription     string `json:"issueDiscription"`
	Language             string `json:"language"`
	LastName             string `json:"lastName"`
	Latitude             string `json:"latitude"`
	LocationDirection    string `json:"locationDirection"`
	Longitude            string `json:"longitude"`
	MainClassificationID string `json:"mainClassificationID"`
	MiddleName           string `json:"middleName"`
	MobileNumber         string `json:"mobileNumber"`
	MunicipalityID       string `json:"municipalityID"`
	NationalID           string `json:"nationalID"`
	Priority             string `json:"priority"`
	SPLClassificationID  string `json:"splClassificationID"`
	SubBaladyaName       string `json:"subBaladyaName"`
	SubClassificationID  string `json:"subClassificationID"`
	SubMunicipalityID    string `json:"subMunicipalityID"`
	SubSubMunicipalityID string `json:"sub_SubMunicipalityID"`
}

type EPMInsertIncidentResponse struct {
	Ex             string `json:"ex"`
	HTTPStatusCode int    `json:"httpStatusCode"`
	Message        string `json:"message"`
	Result         bool   `json:"result"`
	TicketNumber   string `json:"ticketNumber"`
}

type EPMIncidentStatusItems struct {
	RequestID          string      `json:"requestID"`
	Status             interface{} `json:"status"`
	Comments           string      `json:"comments"`
	IsNotified         bool        `json:"isNotified"`
	CreatedDatetime    string      `json:"createdDatetime"`
	SynchedDatetime    string      `json:"synchedDatetime"`
	AmanaIncidentNo    string      `json:"amanaIncidentNo"`
	EvaluationFlag     int         `json:"evaluationFlag"`
	IncidentImageAlert interface{} `json:"incidentImageAlert"`
}

type EPMIncidentAttachmentItem struct {
	AttachmentID    int    `json:"attachmentID"`
	TicketNumber    string `json:"ticketNumber"`
	AttachmentsName string `json:"attachmentsName"`
	Attachments     string `json:"attachments"`
}

type EPMGetMomraIncidentStatusDetailsResponse struct {
	Items          *EPMIncidentStatusItems     `json:"items"`
	Attachements   []EPMIncidentAttachmentItem `json:"attachements"`
	Result         bool                        `json:"result"`
	Ex             string                      `json:"ex"`
	HTTPStatusCode int                         `json:"httpStatusCode"`
}

func (h *EPMIncidentHandler) InsertIncidents(c *fiber.Ctx) error {
	authHeader := c.Get("Authorization")
	if authHeader == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusUnauthorized,
			Message:        "Missing Authorization header",
			Result:         false,
		})
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenString == authHeader {
		return c.Status(fiber.StatusUnauthorized).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusUnauthorized,
			Message:        "Invalid Authorization header format, use Bearer token",
			Result:         false,
		})
	}

	claims, err := h.jwtManager.ValidateToken(tokenString)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusUnauthorized,
			Message:        i18n.T(c.UserContext(), "invalid_or_expired_token"),
			Result:         false,
		})
	}

	var sessionData map[string]interface{}
	if err := h.sessionStore.GetUserSession(c.UserContext(), claims.SessionID, &sessionData); err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusUnauthorized,
			Message:        i18n.T(c.UserContext(), "session_expired_or_invalid"),
			Result:         false,
		})
	}

	var req EPMInsertIncidentRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        i18n.T(c.UserContext(), "invalid_request_body"),
			Result:         false,
		})
	}

	if req.IncidentNo == "" {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "incidentNo is required",
			Result:         false,
		})
	}
	if req.FirstName == "" {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "firstName is required",
			Result:         false,
		})
	}
	if req.IssueDiscription == "" {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "issueDiscription is required",
			Result:         false,
		})
	}
	if len(req.IssueDiscription) > 250 {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "issueDiscription exceeds maximum length of 250 characters",
			Result:         false,
		})
	}
	if req.Priority == "" {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "priority is required",
			Result:         false,
		})
	}
	if parsePriorityLevel(req.Priority) == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "priority must be valid value",
			Result:         false,
		})
	}
	if req.FileKey == "" {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "fileKey is required",
			Result:         false,
		})
	}
	if _, err := base64.StdEncoding.DecodeString(req.FileKey); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "fileKey must be a valid base64 encoded string",
			Result:         false,
		})
	}
	if req.Latitude == "" {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "latitude is required",
			Result:         false,
		})
	}
	if req.Longitude == "" {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "longitude is required",
			Result:         false,
		})
	}
	if _, err := strconv.ParseFloat(req.Latitude, 64); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "latitude must be a valid number",
			Result:         false,
		})
	}
	if _, err := strconv.ParseFloat(req.Longitude, 64); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "longitude must be a valid number",
			Result:         false,
		})
	}
	if req.MobileNumber == "" {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "mobileNumber is required",
			Result:         false,
		})
	}
	if !regexp.MustCompile(`^\+?[0-9\-\(\)\s]+$`).MatchString(req.MobileNumber) {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "mobileNumber must contain only digits and valid phone characters (+, -, (, ))",
			Result:         false,
		})
	}

	// check for duplicate momra incident number
	var duplicateCount int64
	h.db.WithContext(c.UserContext()).Model(&models.Incident{}).
		Where("source = ? AND custom_fields LIKE ?", "MOMRA", "%\"momra_incident_no\":\""+req.IncidentNo+"\"%").
		Count(&duplicateCount)
	if duplicateCount > 0 {
		return c.Status(fiber.StatusConflict).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusConflict,
			Message:        "Duplicate incident number: " + req.IncidentNo,
			Result:         false,
		})
	}

	custFields := map[string]interface{}{
		"momra_incident_no": req.IncidentNo,
		"source":            "MOMRA",
	}
	if req.BeneficiaryInfo != "" {
		custFields["beneficiaryInfo"] = req.BeneficiaryInfo
	}
	if req.DistrictName != "" {
		custFields["districtName"] = req.DistrictName
	}
	if req.IqamaID != "" {
		custFields["iqamaID"] = req.IqamaID
	}
	if req.NationalID != "" {
		custFields["nationalID"] = req.NationalID
	}
	if req.LocationDirection != "" {
		custFields["locationDirection"] = req.LocationDirection
	}
	if req.SubBaladyaName != "" {
		custFields["subBaladyaName"] = req.SubBaladyaName
	}
	if req.SubClassificationID != "" {
		custFields["subClassificationID"] = req.SubClassificationID
	}
	if req.SPLClassificationID != "" {
		custFields["splClassificationID"] = req.SPLClassificationID
	}
	if req.MunicipalityID != "" {
		custFields["municipalityID"] = req.MunicipalityID
	}
	custFieldBytes, _ := json.Marshal(custFields)

	// validate all locations are provided and exist in DB
	if req.MunicipalityID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "municipalityID is required",
			Result:         false,
		})
	}
	if req.SubMunicipalityID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "subMunicipalityID is required",
			Result:         false,
		})
	}
	if req.SubSubMunicipalityID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "sub_SubMunicipalityID is required",
			Result:         false,
		})
	}
	municipalityLoc, err := h.validateAndResolveLocation(c.UserContext(), req.MunicipalityID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "Municipality not found: " + req.MunicipalityID,
			Result:         false,
		})
	}

	subMunicipalityLoc, err := h.validateAndResolveLocation(c.UserContext(), req.SubMunicipalityID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "Sub municipality not found: " + req.SubMunicipalityID,
			Result:         false,
		})
	}

	subSubMunicipalityLoc, err := h.validateAndResolveLocation(c.UserContext(), req.SubSubMunicipalityID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "Sub sub municipality not found: " + req.SubSubMunicipalityID,
			Result:         false,
		})
	}

	// prefer the most specific location that was provided
	var locationID *uuid.UUID
	switch {
	case subSubMunicipalityLoc != nil:
		locationID = subSubMunicipalityLoc
	case subMunicipalityLoc != nil:
		locationID = subMunicipalityLoc
	case municipalityLoc != nil:
		locationID = municipalityLoc
	}

	// validate all classifications are provided and exist in DB
	if req.MainClassificationID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "mainClassificationID is required",
			Result:         false,
		})
	}
	if req.SubClassificationID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "subClassificationID is required",
			Result:         false,
		})
	}
	if req.SPLClassificationID == "" {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "splClassificationID is required",
			Result:         false,
		})
	}
	subCls, err := h.validateAndResolveClassification(c.UserContext(), req.SubClassificationID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "Sub classification not found: " + req.SubClassificationID,
			Result:         false,
		})
	}

	mainCls, err := h.validateAndResolveClassification(c.UserContext(), req.MainClassificationID)
	if err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "Main classification not found: " + req.MainClassificationID,
			Result:         false,
		})
	}

	var splCls *uuid.UUID
	if req.SPLClassificationID != "" {
		splCls, err = h.validateAndResolveClassification(c.UserContext(), req.SPLClassificationID)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
				HTTPStatusCode: fiber.StatusBadRequest,
				Message:        "Special classification not found: " + req.SPLClassificationID,
				Result:         false,
			})
		}
	}

	// Attach the incident to whichever provided classification is an actual leaf node
	// (no children) — not just the field name that "sounds" most specific. This handles
	// hierarchies of variable depth correctly (e.g. a branch with no Special level, where
	// Sub is genuinely the leaf) instead of assuming SPL > Sub > Main always applies.
	// Checked deepest-field-first since that's the most likely leaf when all three exist.
	var classificationID *uuid.UUID
	for _, candidate := range []*uuid.UUID{splCls, subCls, mainCls} {
		if candidate == nil {
			continue
		}
		children, err := h.classificationRepo.GetChildren(c.UserContext(), *candidate)
		if err == nil && len(children) == 0 {
			classificationID = candidate
			break
		}
	}
	if classificationID == nil {
		msg := "Main classification ID is missing"
		switch {
		case req.MainClassificationID == "":
			// keep default message
		case req.SubClassificationID == "":
			msg = "Sub classification ID is missing"
		default:
			msg = "None of the provided classification IDs resolve to a leaf classification"
		}
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        msg,
			Result:         false,
		})
	}

	// EE routing: when EEFlag is true, EEName must resolve to an active, external-type
	// department or the incident is rejected outright (no ticket is created). When EEFlag
	// is false, the incident is created unassigned — no department matching is attempted.
	var eeDepartmentID *uuid.UUID
	if req.EEFlag {
		eeDepartmentID, err = h.resolveEERoutingDepartment(c.UserContext(), req.EEName)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
				HTTPStatusCode: fiber.StatusBadRequest,
				Message:        err.Error(),
				Result:         false,
			})
		}
	}

	// get incident workflow and initial state
	workflow, err := h.workflowRepo.GetDefaultWorkflow(c.UserContext())
	if err != nil || workflow.RecordType != "incident" {
		workflows, listErr := h.workflowRepo.ListByRecordType(c.UserContext(), "incident", true)
		if listErr != nil || len(workflows) == 0 {
			return c.Status(fiber.StatusInternalServerError).JSON(EPMInsertIncidentResponse{
				HTTPStatusCode: fiber.StatusInternalServerError,
				Message:        "No incident workflow configured",
				Result:         false,
			})
		}
		workflow = &workflows[0]
	}

	initialState, err := h.workflowRepo.GetInitialState(c.UserContext(), workflow.ID)
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusInternalServerError,
			Message:        "No initial state found for incident workflow",
			Result:         false,
		})
	}

	title := req.IssueDiscription
	if title == "" {
		title = fmt.Sprintf("MOMRA Incident - %s", req.IncidentNo)
	}
	if len(title) > 200 {
		title = title[:200]
	}

	description := req.IssueDiscription
	if req.BeneficiaryInfo != "" {
		description = req.BeneficiaryInfo
		if req.IssueDiscription != "" {
			description += "\n" + req.IssueDiscription
		}
	}
	if len(description) > 1000 {
		description = description[:1000]
	}

	reporterName := strings.TrimSpace(req.FirstName + " " + req.MiddleName + " " + req.LastName)
	if reporterName == "" {
		reporterName = req.FirstName
	}

	var lat, lng *float64
	if req.Latitude != "" {
		var v float64
		if _, err := fmt.Sscanf(req.Latitude, "%f", &v); err == nil {
			lat = &v
		}
	}
	if req.Longitude != "" {
		var v float64
		if _, err := fmt.Sscanf(req.Longitude, "%f", &v); err == nil {
			lng = &v
		}
	}

	incidentNumber, err := h.incidentRepo.GenerateIncidentNumber(c.UserContext())
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusInternalServerError,
			Message:        "Failed to generate incident number",
			Result:         false,
		})
	}

	now := time.Now()
	incident := &models.Incident{
		ID:               uuid.New(),
		IncidentNumber:   incidentNumber,
		Title:            title,
		Description:      description,
		RecordType:       "incident",
		ClassificationID: classificationID,
		WorkflowID:       workflow.ID,
		CurrentStateID:   initialState.ID,
		LocationID:       locationID,
		DepartmentID:     eeDepartmentID,
		Latitude:         lat,
		Longitude:        lng,
		Address:          req.Address,
		Source:           "MOMRA",
		ReporterEmail:    req.Email,
		ReporterName:     reporterName,
		ReporterPhone:    req.MobileNumber,
		CreatedByName:    reporterName,
		CreatedByMobile:  req.MobileNumber,
		CustomFields:     string(custFieldBytes),
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := h.incidentRepo.Create(c.UserContext(), incident); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusInternalServerError,
			Message:        i18n.T(c.UserContext(), "failed_to_create_incident_epm"),
			Result:         false,
		})
	}

	if req.Priority != "" {
		priorityValues, err := h.lookupRepo.ListValuesByCategoryCode(c.UserContext(), "PRIORITY")
		if err == nil {
			level := parsePriorityLevel(req.Priority)
			for _, v := range priorityValues {
				if v.SortOrder == level {
					h.incidentRepo.SetLookupValues(c.UserContext(), incident.ID, []models.LookupValue{v})
					break
				}
			}
		}
	}

	if req.FileKey != "" {
		decoded, err := base64.StdEncoding.DecodeString(req.FileKey)
		if err == nil && len(decoded) > 0 {
			ext := detectImageExt(decoded)
			mimeType := "image/" + ext
			if ext == "jpeg" {
				mimeType = "image/jpeg"
			}
			filename := fmt.Sprintf("momra_%s_%s.%s", req.IncidentNo, uuid.New().String()[:8], ext)
			folder := fmt.Sprintf("incidents/%s", incident.ID.String())

			filePath, err := h.storage.UploadBytes(c.UserContext(), decoded, filename, mimeType, folder)
			if err == nil {
				attachment := &models.IncidentAttachment{
					ID:           uuid.New(),
					IncidentID:   incident.ID,
					FileName:     filename,
					FileSize:     int64(len(decoded)),
					MimeType:     mimeType,
					FilePath:     filePath,
					UploadedByID: claims.UserID,
				}
				_ = h.incidentRepo.CreateAttachment(c.UserContext(), attachment)
			}
		}
	}

	return c.Status(fiber.StatusOK).JSON(EPMInsertIncidentResponse{
		HTTPStatusCode: fiber.StatusOK,
		Message:        "Successful call with all required parameters",
		Result:         true,
		TicketNumber:   incident.IncidentNumber,
	})
}

func (h *EPMIncidentHandler) GetMomraIncidentStatusDetails(c *fiber.Ctx) error {
	authHeader := c.Get("Authorization")
	if authHeader == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(EPMGetMomraIncidentStatusDetailsResponse{
			HTTPStatusCode: fiber.StatusUnauthorized,
			Ex:             "Missing Authorization header",
			Result:         false,
		})
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenString == authHeader {
		return c.Status(fiber.StatusUnauthorized).JSON(EPMGetMomraIncidentStatusDetailsResponse{
			HTTPStatusCode: fiber.StatusUnauthorized,
			Ex:             "Invalid Authorization header format, use Bearer token",
			Result:         false,
		})
	}

	claims, err := h.jwtManager.ValidateToken(tokenString)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(EPMGetMomraIncidentStatusDetailsResponse{
			HTTPStatusCode: fiber.StatusUnauthorized,
			Ex:             i18n.T(c.UserContext(), "invalid_or_expired_token"),
			Result:         false,
		})
	}

	var sessionData map[string]interface{}
	if err := h.sessionStore.GetUserSession(c.UserContext(), claims.SessionID, &sessionData); err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(EPMGetMomraIncidentStatusDetailsResponse{
			HTTPStatusCode: fiber.StatusUnauthorized,
			Ex:             "Session expired or invalid",
			Result:         false,
		})
	}

	ticketNumber := c.Query("ticketNumber")
	if ticketNumber == "" {
		return c.Status(fiber.StatusBadRequest).JSON(EPMGetMomraIncidentStatusDetailsResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Ex:             "ticketNumber query parameter is required",
			Result:         false,
		})
	}

	incident, err := h.incidentRepo.FindByIncidentNumber(c.UserContext(), ticketNumber)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(EPMGetMomraIncidentStatusDetailsResponse{
			HTTPStatusCode: fiber.StatusNotFound,
			Ex:             "Incident not found: " + ticketNumber,
			Result:         false,
		})
	}

	var status interface{}
	if incident.CurrentState != nil {
		status = incident.CurrentState.Name
	}

	var latestComment string
	comments, err := h.incidentRepo.ListComments(c.UserContext(), incident.ID)
	if err == nil && len(comments) > 0 {
		latestComment = comments[0].Content
	}

	items := &EPMIncidentStatusItems{
		RequestID:          incident.IncidentNumber,
		Status:             status,
		Comments:           latestComment,
		IsNotified:         false,
		CreatedDatetime:    incident.CreatedAt.Format("2006-01-02T15:04:05.00"),
		SynchedDatetime:    incident.UpdatedAt.Format("2006-01-02T15:04:05.00"),
		AmanaIncidentNo:    incident.IncidentNumber,
		EvaluationFlag:     incident.EvaluationCount,
		IncidentImageAlert: nil,
	}

	attachments, err := h.incidentRepo.ListAttachments(c.UserContext(), incident.ID)
	var attachmentItems []EPMIncidentAttachmentItem
	if err == nil {
		for i, att := range attachments {
			content, readErr := h.storage.GetFile(c.UserContext(), att.FilePath)
			encoded := ""
			if readErr == nil {
				data, readErr := io.ReadAll(content)
				content.Close()
				if readErr == nil {
					encoded = base64.StdEncoding.EncodeToString(data)
				}
			}
			attachmentItems = append(attachmentItems, EPMIncidentAttachmentItem{
				AttachmentID:    i + 1,
				TicketNumber:    incident.IncidentNumber,
				AttachmentsName: att.FileName,
				Attachments:     encoded,
			})
		}
	}
	if attachmentItems == nil {
		attachmentItems = []EPMIncidentAttachmentItem{}
	}

	return c.Status(fiber.StatusOK).JSON(EPMGetMomraIncidentStatusDetailsResponse{
		Items:          items,
		Attachements:   attachmentItems,
		Result:         true,
		HTTPStatusCode: fiber.StatusOK,
	})
}

// EPMPushOutcomeRequest is the shared payload for UpdateIncident (TFIS IF-03 status 002 -
// Resolved) and ReopenIncident (TFIS IF-03 status 003 - Rejected/Returned by EE).
type EPMPushOutcomeRequest struct {
	IncidentNo        string `json:"incidentNo"`
	AmanaIncidentNo   string `json:"amanaIncidentNo"`
	ActionDate        string `json:"actionDate"`
	ActionDescription string `json:"actionDescription"`
	EEName            string `json:"eeName"`
	EECode            string `json:"eeCode"`
}

// UpdateIncident handles TFIS IF-03 status 002 (Resolved): the EE component completed the
// incident and CRM pushes the outcome so AutoMax can move it to its terminal state.
func (h *EPMIncidentHandler) UpdateIncident(c *fiber.Ctx) error {
	return h.handleEEOutcome(c, true)
}

// ReopenIncident handles TFIS IF-03 status 003 (Rejected/Returned by EE): CRM pushes the
// outcome so AutoMax returns the incident to municipality processing (its initial state).
func (h *EPMIncidentHandler) ReopenIncident(c *fiber.Ctx) error {
	return h.handleEEOutcome(c, false)
}

// handleEEOutcome implements the shared logic behind UpdateIncident/ReopenIncident. resolve
// selects the target state: true moves the incident to its workflow's terminal state
// (Resolved), false moves it back to the workflow's initial state (Rejected/Returned,
// i.e. reopened for municipality processing), per TFIS §12.2.
//
// State changes are applied directly (UpdateFields + a system-triggered
// IncidentTransitionHistory row) rather than through incidentService.ExecuteTransition,
// since that engine requires a specific pre-configured WorkflowTransition ID and a
// permissioned human actor — neither of which applies to this system-to-system callback.
// IncidentTransitionHistory.TransitionID is nullable specifically for this case.
func (h *EPMIncidentHandler) handleEEOutcome(c *fiber.Ctx, resolve bool) error {
	authHeader := c.Get("Authorization")
	if authHeader == "" {
		return c.Status(fiber.StatusUnauthorized).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusUnauthorized,
			Message:        "Missing Authorization header",
			Result:         false,
		})
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenString == authHeader {
		return c.Status(fiber.StatusUnauthorized).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusUnauthorized,
			Message:        "Invalid Authorization header format, use Bearer token",
			Result:         false,
		})
	}

	claims, err := h.jwtManager.ValidateToken(tokenString)
	if err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusUnauthorized,
			Message:        i18n.T(c.UserContext(), "invalid_or_expired_token"),
			Result:         false,
		})
	}

	var sessionData map[string]interface{}
	if err := h.sessionStore.GetUserSession(c.UserContext(), claims.SessionID, &sessionData); err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusUnauthorized,
			Message:        i18n.T(c.UserContext(), "session_expired_or_invalid"),
			Result:         false,
		})
	}

	var req EPMPushOutcomeRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        i18n.T(c.UserContext(), "invalid_request_body"),
			Result:         false,
		})
	}

	if req.IncidentNo == "" {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "incidentNo is required",
			Result:         false,
		})
	}
	if req.AmanaIncidentNo == "" {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "amanaIncidentNo is required",
			Result:         false,
		})
	}
	if req.ActionDate == "" {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "actionDate is required",
			Result:         false,
		})
	}
	if req.EEName == "" {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "eeName is required",
			Result:         false,
		})
	}
	if req.EECode == "" {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "eeCode is required",
			Result:         false,
		})
	}
	// TFIS IF03-R05: ActionDescription is required for status 003 (Reject/Reopen) so the
	// rejection reason reaches AutoMax; optional for status 002 (Resolved).
	if !resolve && req.ActionDescription == "" {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "actionDescription is required when rejecting/returning an incident",
			Result:         false,
		})
	}

	incident, err := h.incidentRepo.FindByIncidentNumber(c.UserContext(), req.AmanaIncidentNo)
	if err != nil {
		return c.Status(fiber.StatusNotFound).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusNotFound,
			Message:        "Incident not found: " + req.AmanaIncidentNo,
			Result:         false,
		})
	}

	// TFIS IF03-R02: validate both incident identifiers correlate before applying the transition.
	var custFields map[string]interface{}
	_ = json.Unmarshal([]byte(incident.CustomFields), &custFields)
	if momraNo, _ := custFields["momra_incident_no"].(string); momraNo != req.IncidentNo {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "incidentNo does not correlate with amanaIncidentNo: " + req.AmanaIncidentNo,
			Result:         false,
		})
	}

	var targetState models.WorkflowState
	if resolve {
		err = h.db.WithContext(c.UserContext()).
			Where("workflow_id = ? AND state_type = ? AND is_active = ?", incident.WorkflowID, "terminal", true).
			Order("sort_order, id").
			First(&targetState).Error
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(EPMInsertIncidentResponse{
				HTTPStatusCode: fiber.StatusInternalServerError,
				Message:        "No terminal state configured for incident workflow",
				Result:         false,
			})
		}
	} else {
		state, err := h.workflowRepo.GetInitialState(c.UserContext(), incident.WorkflowID)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(EPMInsertIncidentResponse{
				HTTPStatusCode: fiber.StatusInternalServerError,
				Message:        "No initial state configured for incident workflow",
				Result:         false,
			})
		}
		targetState = *state
	}

	// TFIS IF03-R06: a duplicate identical outcome is acknowledged without reapplying the state change.
	if incident.CurrentStateID == targetState.ID {
		return c.Status(fiber.StatusOK).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusOK,
			Message:        "Outcome already applied",
			Result:         true,
			TicketNumber:   incident.IncidentNumber,
		})
	}

	now := time.Now()
	updates := map[string]interface{}{
		"current_state_id": targetState.ID,
		"updated_at":       now,
	}
	if resolve {
		updates["resolved_at"] = now
	} else {
		updates["resolved_at"] = nil
		updates["closed_at"] = nil
	}
	if err := h.incidentRepo.UpdateFields(c.UserContext(), incident.ID, updates); err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusInternalServerError,
			Message:        i18n.T(c.UserContext(), "failed_to_update_incident_epm"),
			Result:         false,
		})
	}

	history := &models.IncidentTransitionHistory{
		ID:             uuid.New(),
		IncidentID:     incident.ID,
		TransitionID:   nil,
		FromStateID:    incident.CurrentStateID,
		ToStateID:      targetState.ID,
		PerformedByID:  claims.UserID,
		Comment:        req.ActionDescription,
		IsSystemAction: true,
		TransitionedAt: now,
		CreatedAt:      now,
	}
	if err := h.incidentRepo.CreateTransitionHistory(c.UserContext(), history); err != nil {
		fmt.Printf("Warning: failed to record EE outcome transition history for incident %s: %v\n", incident.IncidentNumber, err)
	}

	message := "Incident outcome received successfully"
	return c.Status(fiber.StatusOK).JSON(EPMInsertIncidentResponse{
		HTTPStatusCode: fiber.StatusOK,
		Message:        message,
		Result:         true,
		TicketNumber:   incident.IncidentNumber,
	})
}

// ---- V2 (compose) interface ----
//
// These implement MOMRA's newer contract: PATCH methods, a flat JSON request body (no
// envelope), a flat JSON response body (no envelope), and a single UpdateIncidentV2
// endpoint that handles both resolve (IncidentStatusID "002") and reject (IncidentStatusID
// "003") instead of two separate endpoints. The V1 endpoints above (UpdateIncident/
// ReopenIncident) are left in place alongside these.

type momraV2ResponseBody struct {
	Ex      string `json:"ex"`
	Message string `json:"message"`
	Result  bool   `json:"result"`
	Status  int    `json:"status"`
}

func momraV2Response(c *fiber.Ctx, status int, message string, result bool) error {
	return c.Status(status).JSON(momraV2ResponseBody{
		Message: message,
		Result:  result,
		Status:  status,
	})
}

type EPMUpdateIncidentV2Request struct {
	EENotes           string `json:"EENotes"`
	AmanaIncidentNo   string `json:"AmanaIncidentNo"`
	IncidentNo        string `json:"IncidentNO"`
	ActionDate        string `json:"ActionDate"`
	EECode            string `json:"EECode"`
	EEName            string `json:"EEName"`
	IncidentStatusID  string `json:"IncidentStatusID"`
	ActionDescription string `json:"ActionDescription"`
}

// UpdateIncidentV2 implements PATCH /api/compose/MomraAPI/UpdateIncidentV2.
// IncidentStatusID "002" resolves the incident (terminal state); "003" rejects it back
// to its initial state - the same two outcomes the V1 UpdateIncident/ReopenIncident
// endpoints handled as separate calls.
func (h *EPMIncidentHandler) UpdateIncidentV2(c *fiber.Ctx) error {
	authHeader := c.Get("Authorization")
	if authHeader == "" {
		return momraV2Response(c, fiber.StatusUnauthorized, "Missing Authorization header", false)
	}
	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenString == authHeader {
		return momraV2Response(c, fiber.StatusUnauthorized, "Invalid Authorization header format, use Bearer token", false)
	}
	claims, err := h.jwtManager.ValidateToken(tokenString)
	if err != nil {
		return momraV2Response(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "invalid_or_expired_token"), false)
	}
	var sessionData map[string]interface{}
	if err := h.sessionStore.GetUserSession(c.UserContext(), claims.SessionID, &sessionData); err != nil {
		return momraV2Response(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "session_expired_or_invalid"), false)
	}

	var req EPMUpdateIncidentV2Request
	if err := c.BodyParser(&req); err != nil {
		return momraV2Response(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"), false)
	}

	if req.IncidentNo == "" {
		return momraV2Response(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "incident_no_required"), false)
	}
	if req.AmanaIncidentNo == "" {
		return momraV2Response(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "amana_incident_no_required"), false)
	}
	if req.ActionDate == "" {
		return momraV2Response(c, fiber.StatusBadRequest, "ActionDate is required", false)
	}
	if req.EEName == "" {
		return momraV2Response(c, fiber.StatusBadRequest, "EEName is required", false)
	}
	if req.EECode == "" {
		return momraV2Response(c, fiber.StatusBadRequest, "EECode is required", false)
	}
	var resolve bool
	switch req.IncidentStatusID {
	case "002":
		resolve = true
	case "003":
		resolve = false
	default:
		return momraV2Response(c, fiber.StatusBadRequest, "IncidentStatusID must be 002 (Approved) or 003 (Rejected)", false)
	}
	// TFIS IF03-R05: a rejection reason is required for status 003.
	if !resolve && req.ActionDescription == "" {
		return momraV2Response(c, fiber.StatusBadRequest, "ActionDescription is required when IncidentStatusID is 003", false)
	}

	incident, err := h.incidentRepo.FindByIncidentNumber(c.UserContext(), req.AmanaIncidentNo)
	if err != nil {
		return momraV2Response(c, fiber.StatusNotFound, "Incident not found: "+req.AmanaIncidentNo, false)
	}

	var custFields map[string]interface{}
	_ = json.Unmarshal([]byte(incident.CustomFields), &custFields)
	if custFields == nil {
		custFields = map[string]interface{}{}
	}
	if momraNo, _ := custFields["momra_incident_no"].(string); momraNo != req.IncidentNo {
		return momraV2Response(c, fiber.StatusBadRequest, "IncidentNO does not correlate with AmanaIncidentNo: "+req.AmanaIncidentNo, false)
	}

	var targetState models.WorkflowState
	if resolve {
		err = h.db.WithContext(c.UserContext()).
			Where("workflow_id = ? AND state_type = ? AND is_active = ?", incident.WorkflowID, "terminal", true).
			Order("sort_order, id").
			First(&targetState).Error
		if err != nil {
			return momraV2Response(c, fiber.StatusInternalServerError, "No terminal state configured for incident workflow", false)
		}
	} else {
		state, err := h.workflowRepo.GetInitialState(c.UserContext(), incident.WorkflowID)
		if err != nil {
			return momraV2Response(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "no_initial_state_epm"), false)
		}
		targetState = *state
	}

	if req.EENotes != "" {
		custFields["ee_notes"] = req.EENotes
	}
	custFieldBytes, _ := json.Marshal(custFields)

	if incident.CurrentStateID == targetState.ID {
		return momraV2Response(c, fiber.StatusOK, "Incident status updated successfully", true)
	}

	now := time.Now()
	updates := map[string]interface{}{
		"current_state_id": targetState.ID,
		"custom_fields":    string(custFieldBytes),
		"updated_at":       now,
	}
	if resolve {
		updates["resolved_at"] = now
	} else {
		updates["resolved_at"] = nil
		updates["closed_at"] = nil
	}
	if err := h.incidentRepo.UpdateFields(c.UserContext(), incident.ID, updates); err != nil {
		return momraV2Response(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update_incident_epm"), false)
	}

	history := &models.IncidentTransitionHistory{
		ID:             uuid.New(),
		IncidentID:     incident.ID,
		TransitionID:   nil,
		FromStateID:    incident.CurrentStateID,
		ToStateID:      targetState.ID,
		PerformedByID:  claims.UserID,
		Comment:        req.ActionDescription,
		IsSystemAction: true,
		TransitionedAt: now,
		CreatedAt:      now,
	}
	if err := h.incidentRepo.CreateTransitionHistory(c.UserContext(), history); err != nil {
		fmt.Printf("Warning: failed to record EE outcome transition history for incident %s: %v\n", incident.IncidentNumber, err)
	}

	return momraV2Response(c, fiber.StatusOK, "Incident status updated successfully", true)
}

type EPMReopenIncidentV2Request struct {
	AmanaIncidentNo  string `json:"amanaincidentno"`
	IncidentNo       string `json:"incident_no"`
	EvaluationStatus string `json:"evaluation_status"`
}

// dissatisfiedEvaluationStatus reports whether an evaluation_status value indicates the
// beneficiary was NOT satisfied with the resolution. MOMRA's spec gives only one example
// value ("not satisfied"), so this matches on the presence of a negation ("not"/"un"/"dis"
// combined with "satisf...") rather than an exact string, case- and spacing-insensitive.
func dissatisfiedEvaluationStatus(status string) bool {
	normalized := strings.ToLower(strings.TrimSpace(status))
	normalized = strings.Join(strings.Fields(normalized), " ")
	if !strings.Contains(normalized, "satisf") {
		return false
	}
	return strings.Contains(normalized, "not satisf") ||
		strings.Contains(normalized, "unsatisf") ||
		strings.Contains(normalized, "dissatisf")
}

// ReopenIncidentV2 implements PATCH /api/compose/MomraAPI/ReOpenIncidentV2. It is driven
// by the beneficiary's post-resolution satisfaction survey: when evaluation_status
// indicates dissatisfaction (e.g. "not satisfied"), the incident is sent back to its
// workflow's initial state for reprocessing. Any other value (e.g. "satisfied") is
// recorded on the incident and the evaluation count is still incremented, but the
// incident's state is left untouched - the survey result is stored, not a rejection.
func (h *EPMIncidentHandler) ReopenIncidentV2(c *fiber.Ctx) error {
	authHeader := c.Get("Authorization")
	if authHeader == "" {
		return momraV2Response(c, fiber.StatusUnauthorized, "Missing Authorization header", false)
	}
	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenString == authHeader {
		return momraV2Response(c, fiber.StatusUnauthorized, "Invalid Authorization header format, use Bearer token", false)
	}
	claims, err := h.jwtManager.ValidateToken(tokenString)
	if err != nil {
		return momraV2Response(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "invalid_or_expired_token"), false)
	}
	var sessionData map[string]interface{}
	if err := h.sessionStore.GetUserSession(c.UserContext(), claims.SessionID, &sessionData); err != nil {
		return momraV2Response(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "session_expired_or_invalid"), false)
	}

	var req EPMReopenIncidentV2Request
	if err := c.BodyParser(&req); err != nil {
		return momraV2Response(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"), false)
	}

	if req.AmanaIncidentNo == "" {
		return momraV2Response(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "amana_incident_no_required"), false)
	}
	if req.IncidentNo == "" {
		return momraV2Response(c, fiber.StatusBadRequest, "incident_no is required", false)
	}
	if req.EvaluationStatus == "" {
		return momraV2Response(c, fiber.StatusBadRequest, "evaluation_status is required", false)
	}

	incident, err := h.incidentRepo.FindByIncidentNumber(c.UserContext(), req.AmanaIncidentNo)
	if err != nil {
		return momraV2Response(c, fiber.StatusNotFound, "Incident not found: "+req.AmanaIncidentNo, false)
	}

	var custFields map[string]interface{}
	_ = json.Unmarshal([]byte(incident.CustomFields), &custFields)
	if custFields == nil {
		custFields = map[string]interface{}{}
	}
	if momraNo, _ := custFields["momra_incident_no"].(string); momraNo != req.IncidentNo {
		return momraV2Response(c, fiber.StatusBadRequest, "incident_no does not correlate with amanaincidentno: "+req.AmanaIncidentNo, false)
	}

	custFields["evaluation_status"] = req.EvaluationStatus
	custFieldBytes, _ := json.Marshal(custFields)
	now := time.Now()

	// incidentRepo.IncrementEvaluationCount is scoped to record_type='complaint' and would
	// silently no-op here - MOMRA incidents have record_type='incident' - so increment
	// directly instead.
	if err := h.db.WithContext(c.UserContext()).Model(&models.Incident{}).
		Where("id = ?", incident.ID).
		Update("evaluation_count", gorm.Expr("evaluation_count + 1")).Error; err != nil {
		fmt.Printf("Warning: failed to increment evaluation count for incident %s: %v\n", incident.IncidentNumber, err)
	}

	if !dissatisfiedEvaluationStatus(req.EvaluationStatus) {
		// Beneficiary is satisfied (or the value is neither): record the survey result,
		// but the resolution stands - no state transition.
		if err := h.incidentRepo.UpdateFields(c.UserContext(), incident.ID, map[string]interface{}{
			"custom_fields": string(custFieldBytes),
			"updated_at":    now,
		}); err != nil {
			return momraV2Response(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_record_evaluation"), false)
		}
		return momraV2Response(c, fiber.StatusOK, "Evaluation recorded; incident remains resolved", true)
	}

	initialState, err := h.workflowRepo.GetInitialState(c.UserContext(), incident.WorkflowID)
	if err != nil {
		return momraV2Response(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "no_initial_state_epm"), false)
	}

	if incident.CurrentStateID == initialState.ID {
		return momraV2Response(c, fiber.StatusOK, "Incident has been reopened successfully", true)
	}

	updates := map[string]interface{}{
		"current_state_id": initialState.ID,
		"custom_fields":    string(custFieldBytes),
		"resolved_at":      nil,
		"closed_at":        nil,
		"updated_at":       now,
	}
	if err := h.incidentRepo.UpdateFields(c.UserContext(), incident.ID, updates); err != nil {
		return momraV2Response(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_reopen_incident_epm"), false)
	}

	history := &models.IncidentTransitionHistory{
		ID:             uuid.New(),
		IncidentID:     incident.ID,
		TransitionID:   nil,
		FromStateID:    incident.CurrentStateID,
		ToStateID:      initialState.ID,
		PerformedByID:  claims.UserID,
		Comment:        "Reopened - beneficiary evaluation: " + req.EvaluationStatus,
		IsSystemAction: true,
		TransitionedAt: now,
		CreatedAt:      now,
	}
	if err := h.incidentRepo.CreateTransitionHistory(c.UserContext(), history); err != nil {
		fmt.Printf("Warning: failed to record reopen transition history for incident %s: %v\n", incident.IncidentNumber, err)
	}

	return momraV2Response(c, fiber.StatusOK, "Incident has been reopened successfully", true)
}

// EPMAssignExternalEntityRequest is a best-guess shape for the still-unconfirmed
// MOMRA->Automax external-entity assignment notification (docs/MOMRA_Outbound_Integration_Spec_v1.0.md
// §7, OD-N2). Field names/casing mirror the sibling V2 endpoints' conventions
// (EPMUpdateIncidentV2Request above) since MOMRA hasn't published this contract yet —
// expect to adjust field names once confirmed; the underlying Incident schema
// (ExternalEntityID/ExternalAssignmentStatus/ExternalAssignedAt) is not expected to
// need rework.
type EPMAssignExternalEntityRequest struct {
	AmanaIncidentNo  string `json:"AmanaIncidentNo"`
	IncidentNo       string `json:"IncidentNO"`
	EECode           string `json:"EECode"`
	EEName           string `json:"EEName"`
	AssignmentStatus string `json:"AssignmentStatus"` // e.g. "assigned"
	ActionDate       string `json:"ActionDate"`
}

// AssignExternalEntity is a STUB implementing the not-yet-confirmed MOMRA->Automax
// assignment-notification interface (Story D / OD-N2). It exists so backend + UI are
// ready to wire in as soon as MOMRA confirms the mechanism, without a data-model
// change — see the Incident model fields it writes. Registered at
// PATCH /Momra/API/EPM/AssignExternalEntity, mirroring UpdateIncidentV2/ReOpenIncidentV2's
// route family and auth pattern.
func (h *EPMIncidentHandler) AssignExternalEntity(c *fiber.Ctx) error {
	authHeader := c.Get("Authorization")
	if authHeader == "" {
		return momraV2Response(c, fiber.StatusUnauthorized, "Missing Authorization header", false)
	}
	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenString == authHeader {
		return momraV2Response(c, fiber.StatusUnauthorized, "Invalid Authorization header format, use Bearer token", false)
	}
	claims, err := h.jwtManager.ValidateToken(tokenString)
	if err != nil {
		return momraV2Response(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "invalid_or_expired_token"), false)
	}
	var sessionData map[string]interface{}
	if err := h.sessionStore.GetUserSession(c.UserContext(), claims.SessionID, &sessionData); err != nil {
		return momraV2Response(c, fiber.StatusUnauthorized, "Session expired or invalid", false)
	}

	var req EPMAssignExternalEntityRequest
	if err := c.BodyParser(&req); err != nil {
		return momraV2Response(c, fiber.StatusBadRequest, "Invalid request body: "+err.Error(), false)
	}
	if req.IncidentNo == "" {
		return momraV2Response(c, fiber.StatusBadRequest, "IncidentNO is required", false)
	}
	if req.AmanaIncidentNo == "" {
		return momraV2Response(c, fiber.StatusBadRequest, "AmanaIncidentNo is required", false)
	}
	if req.EECode == "" {
		return momraV2Response(c, fiber.StatusBadRequest, "EECode is required", false)
	}

	incident, err := h.incidentRepo.FindByIncidentNumber(c.UserContext(), req.AmanaIncidentNo)
	if err != nil {
		return momraV2Response(c, fiber.StatusNotFound, "Incident not found: "+req.AmanaIncidentNo, false)
	}

	var custFields map[string]interface{}
	_ = json.Unmarshal([]byte(incident.CustomFields), &custFields)
	if momraNo, _ := custFields["momra_incident_no"].(string); momraNo != req.IncidentNo {
		return momraV2Response(c, fiber.StatusBadRequest, "IncidentNO does not correlate with AmanaIncidentNo: "+req.AmanaIncidentNo, false)
	}

	// EECode resolves against Department{Type:"external"}, the same model used
	// throughout the MOMRA integration (internal/services/momra_sync_service.go).
	ee, err := h.departmentRepo.FindByCode(c.UserContext(), req.EECode)
	if err != nil || ee.Type != externalEntityDepartmentType {
		return momraV2Response(c, fiber.StatusBadRequest, "This EE is not defined or not active: "+req.EECode, false)
	}

	assignedAt := time.Now()
	if req.ActionDate != "" {
		if parsed, parseErr := time.Parse(time.RFC3339, req.ActionDate); parseErr == nil {
			assignedAt = parsed
		}
	}
	assignmentStatus := req.AssignmentStatus
	if assignmentStatus == "" {
		assignmentStatus = "assigned"
	}

	updates := map[string]interface{}{
		"external_entity_id":         ee.ID,
		"external_assignment_status": assignmentStatus,
		"external_assigned_at":       assignedAt,
		"updated_at":                 time.Now(),
	}
	if err := h.incidentRepo.UpdateFields(c.UserContext(), incident.ID, updates); err != nil {
		return momraV2Response(c, fiber.StatusInternalServerError, "Failed to record external entity assignment: "+err.Error(), false)
	}

	return momraV2Response(c, fiber.StatusOK, "External entity assignment recorded successfully", true)
}

func (h *EPMIncidentHandler) validateAndResolveLocation(ctx context.Context, externalID string) (*uuid.UUID, error) {
	if externalID == "" {
		return nil, nil
	}

	loc, err := h.locationRepo.FindByExternalID(ctx, externalID)
	if err != nil || loc == nil {
		return nil, fmt.Errorf("location not found: %s", externalID)
	}

	return &loc.ID, nil
}

func (h *EPMIncidentHandler) validateAndResolveClassification(ctx context.Context, externalID string) (*uuid.UUID, error) {
	if externalID == "" {
		return nil, nil
	}

	cls, err := h.classificationRepo.FindByExternalID(ctx, externalID)
	if err != nil || cls == nil {
		return nil, fmt.Errorf("classification not found: %s", externalID)
	}

	return &cls.ID, nil
}

func (h *EPMIncidentHandler) resolveClassification(ctx context.Context, externalID string) *uuid.UUID {
	if externalID == "" {
		return nil
	}

	cls, err := h.classificationRepo.FindByExternalID(ctx, externalID)
	if err == nil && cls != nil {
		return &cls.ID
	}

	// fallback: strip municipality prefix and match by classification code
	if idx := strings.Index(externalID, "_"); idx > 0 && idx < len(externalID)-1 {
		suffix := externalID[idx+1:]
		var lookup models.Classification
		if err := h.db.WithContext(ctx).Where("external_id LIKE ?", "%"+suffix).First(&lookup).Error; err == nil {
			return &lookup.ID
		}
	}

	return nil
}

func detectImageExt(data []byte) string {
	if len(data) < 4 {
		return "png"
	}
	if data[0] == 0xFF && data[1] == 0xD8 {
		return "jpeg"
	}
	if data[0] == 0x89 && data[1] == 0x50 && data[2] == 0x4E && data[3] == 0x47 {
		return "png"
	}
	if data[0] == 0x47 && data[1] == 0x49 && data[2] == 0x46 {
		return "gif"
	}
	if data[0] == 0x42 && data[1] == 0x4D {
		return "bmp"
	}
	if data[0] == 0x52 && data[1] == 0x49 && data[2] == 0x46 && data[3] == 0x46 {
		return "webp"
	}
	return "png"
}

func parsePriorityLevel(priority string) int {
	if len(priority) == 0 {
		return 0
	}
	first := priority[0] - '0'
	if first >= 1 && first <= 5 {
		return int(first)
	}
	return 0
}
