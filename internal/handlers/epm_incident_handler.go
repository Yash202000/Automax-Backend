package handlers

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/automax/backend/internal/database"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/storage"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type EPMIncidentHandler struct {
	userRepo          repository.UserRepository
	locationRepo      repository.LocationRepository
	classificationRepo repository.ClassificationRepository
	incidentRepo      repository.IncidentRepository
	workflowRepo      repository.WorkflowRepository
	lookupRepo        repository.LookupRepository
	jwtManager        *utils.JWTManager
	sessionStore      *database.SessionStore
	storage           *storage.MinIOStorage
	db                *gorm.DB
}

func NewEPMIncidentHandler(
	userRepo repository.UserRepository,
	locationRepo repository.LocationRepository,
	classificationRepo repository.ClassificationRepository,
	incidentRepo repository.IncidentRepository,
	workflowRepo repository.WorkflowRepository,
	lookupRepo repository.LookupRepository,
	jwtManager *utils.JWTManager,
	sessionStore *database.SessionStore,
	minioStorage *storage.MinIOStorage,
	db *gorm.DB,
) *EPMIncidentHandler {
	return &EPMIncidentHandler{
		userRepo:          userRepo,
		locationRepo:      locationRepo,
		classificationRepo: classificationRepo,
		incidentRepo:      incidentRepo,
		workflowRepo:      workflowRepo,
		lookupRepo:        lookupRepo,
		jwtManager:        jwtManager,
		sessionStore:      sessionStore,
		storage:           minioStorage,
		db:                db,
	}
}

type EPMInsertIncidentRequest struct {
	Address             string `json:"address"`
	BeneficiaryInfo     string `json:"beneficiaryInfo"`
	DistrictCode        int    `json:"districtCode"`
	DistrictName        string `json:"districtName"`
	Email               string `json:"email"`
	FileKey             string `json:"fileKey"`
	FirstName           string `json:"firstName"`
	IncidentNo          string `json:"incidentNo"`
	IncidentStartDate   string `json:"incidentStartDate"`
	IncidentStatusID    int    `json:"incidentStatusID"`
	IqamaID             string `json:"iqamaID"`
	IssueDiscription    string `json:"issueDiscription"`
	Language            string `json:"language"`
	LastName            string `json:"lastName"`
	Latitude            string `json:"latitude"`
	LocationDirection   string `json:"locationDirection"`
	Longitude           string `json:"longitude"`
	MainClassificationID string `json:"mainClassificationID"`
	MiddleName          string `json:"middleName"`
	MobileNumber        string `json:"mobileNumber"`
	MunicipalityID      string `json:"municipalityID"`
	NationalID          string `json:"nationalID"`
	Priority            string `json:"priority"`
	SPLClassificationID int    `json:"splClassificationID"`
	SubBaladyaName      string `json:"subBaladyaName"`
	SubClassificationID string `json:"subClassificationID"`
	SubMunicipalityID   string `json:"subMunicipalityID"`
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
	RequestID         string     `json:"requestID"`
	Status            interface{} `json:"status"`
	Comments          string     `json:"comments"`
	IsNotified        bool       `json:"isNotified"`
	CreatedDatetime   string     `json:"createdDatetime"`
	SynchedDatetime   string     `json:"synchedDatetime"`
	AmanaIncidentNo   string     `json:"amanaIncidentNo"`
	EvaluationFlag    int        `json:"evaluationFlag"`
	IncidentImageAlert interface{} `json:"incidentImageAlert"`
}

type EPMIncidentAttachmentItem struct {
	AttachmentID    int    `json:"attachmentID"`
	TicketNumber    string `json:"ticketNumber"`
	AttachmentsName string `json:"attachmentsName"`
	Attachments     string `json:"attachments"`
}

type EPMGetMomraIncidentStatusDetailsResponse struct {
	Items        *EPMIncidentStatusItems       `json:"items"`
	Attachements []EPMIncidentAttachmentItem  `json:"attachements"`
	Result       bool                          `json:"result"`
	Ex           string                        `json:"ex"`
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
			Message:        "Invalid or expired token",
			Result:         false,
		})
	}

	var sessionData map[string]interface{}
	if err := h.sessionStore.GetUserSession(c.UserContext(), claims.UserID.String(), &sessionData); err != nil {
		return c.Status(fiber.StatusUnauthorized).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusUnauthorized,
			Message:        "Session expired or invalid",
			Result:         false,
		})
	}

	var req EPMInsertIncidentRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(EPMInsertIncidentResponse{
			HTTPStatusCode: fiber.StatusBadRequest,
			Message:        "Invalid request body: " + err.Error(),
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
	custFieldBytes, _ := json.Marshal(custFields)

	// resolve location hierarchy
	var locationID *uuid.UUID
	if req.MunicipalityID != "" {
		loc, err := h.locationRepo.FindByExternalID(c.UserContext(), req.MunicipalityID)
		if err == nil && loc != nil {
			locationID = &loc.ID
		}
	}
	// try sub-municipality as more specific location
	if locationID == nil && req.SubMunicipalityID != "" {
		loc, err := h.locationRepo.FindByExternalID(c.UserContext(), req.SubMunicipalityID)
		if err == nil && loc != nil {
			locationID = &loc.ID
		}
	}
	if locationID == nil && req.SubSubMunicipalityID != "" {
		loc, err := h.locationRepo.FindByExternalID(c.UserContext(), req.SubSubMunicipalityID)
		if err == nil && loc != nil {
			locationID = &loc.ID
		}
	}

	// resolve classification hierarchy (prefer the most specific)
	var classificationID *uuid.UUID
	classificationID = h.resolveClassification(c.UserContext(), req.SubClassificationID)
	if classificationID == nil {
		classificationID = h.resolveClassification(c.UserContext(), req.MainClassificationID)
	}
	if classificationID == nil && req.SPLClassificationID != 0 {
		extID := fmt.Sprintf("%d", req.SPLClassificationID)
		classificationID = h.resolveClassification(c.UserContext(), extID)
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
			Message:        "Failed to create incident: " + err.Error(),
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
			Ex:             "Invalid or expired token",
			Result:         false,
		})
	}

	var sessionData map[string]interface{}
	if err := h.sessionStore.GetUserSession(c.UserContext(), claims.UserID.String(), &sessionData); err != nil {
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
