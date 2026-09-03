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
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type EPMIncidentHandler struct {
	userRepo               repository.UserRepository
	locationRepo           repository.LocationRepository
	classificationRepo     repository.ClassificationRepository
	incidentRepo           repository.IncidentRepository
	workflowRepo           repository.WorkflowRepository
	lookupRepo             repository.LookupRepository
	departmentRepo         repository.DepartmentRepository
	momraStatusMappingRepo repository.MOMRAStatusMappingRepository
	jwtManager             *utils.JWTManager
	sessionStore           *database.SessionStore
	storage                *storage.MinIOStorage
	db                     *gorm.DB
}

func NewEPMIncidentHandler(
	userRepo repository.UserRepository,
	locationRepo repository.LocationRepository,
	classificationRepo repository.ClassificationRepository,
	incidentRepo repository.IncidentRepository,
	workflowRepo repository.WorkflowRepository,
	lookupRepo repository.LookupRepository,
	departmentRepo repository.DepartmentRepository,
	momraStatusMappingRepo repository.MOMRAStatusMappingRepository,
	jwtManager *utils.JWTManager,
	sessionStore *database.SessionStore,
	minioStorage *storage.MinIOStorage,
	db *gorm.DB,
) *EPMIncidentHandler {
	return &EPMIncidentHandler{
		userRepo:               userRepo,
		locationRepo:           locationRepo,
		classificationRepo:     classificationRepo,
		incidentRepo:           incidentRepo,
		workflowRepo:           workflowRepo,
		lookupRepo:             lookupRepo,
		departmentRepo:         departmentRepo,
		momraStatusMappingRepo: momraStatusMappingRepo,
		jwtManager:             jwtManager,
		sessionStore:           sessionStore,
		storage:                minioStorage,
		db:                     db,
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
		return nil, fmt.Errorf("eeName is required when eeFlag is true")
	}

	dept, err := h.departmentRepo.FindByNameOrNameAr(ctx, trimmed, trimmed)
	if err != nil || dept == nil {
		return nil, fmt.Errorf("this EE is not defined: %s", eeName)
	}
	if dept.Type != externalEntityDepartmentType || !dept.IsActive {
		return nil, fmt.Errorf("this EE is not defined or not active: %s", eeName)
	}

	return &dept.ID, nil
}

// resolveEERoutingDepartmentByCodeOrName is the EEList-aware counterpart of
// resolveEERoutingDepartment: TFIS v1.0's IF-01 payloads carry an EEList array (each
// entry an EntityID/EEName pair, per Table 30/32 — see EPMExternalEntity) rather than a
// single eeFlag/eeName. The identifier (EntityID, or EECode as a fallback) is tried first
// since it's the same stable key synced from MOMRA's EE master (3.35, see
// momra_sync_service.go) into Department.Code; eeName is a fallback for entries that omit
// both. Same reject-if-undefined-or-inactive rule as resolveEERoutingDepartment.
func (h *EPMIncidentHandler) resolveEERoutingDepartmentByCodeOrName(ctx context.Context, eeCode, eeName string) (*uuid.UUID, error) {
	code := strings.TrimSpace(eeCode)
	name := strings.TrimSpace(eeName)
	if code == "" && name == "" {
		return nil, fmt.Errorf("EEList entry requires EECode or EEName")
	}

	identifier := code
	if identifier == "" {
		identifier = name
	}

	var dept *models.Department
	if code != "" {
		if d, err := h.departmentRepo.FindByCode(ctx, code); err == nil {
			dept = d
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	if dept == nil && name != "" {
		if d, err := h.departmentRepo.FindByNameOrNameAr(ctx, name, name); err == nil {
			dept = d
		}
	}
	if dept == nil {
		return nil, fmt.Errorf("this EE is not defined: %s", identifier)
	}
	if dept.Type != externalEntityDepartmentType || !dept.IsActive {
		return nil, fmt.Errorf("this EE is not defined or not active: %s", identifier)
	}

	return &dept.ID, nil
}

// EPMIncidentPhoto is one entry of IncidentPhotos, matching TFIS v1.0's
// IncidentPhotos[].IncidentImage array shape. Unlike TFIS's description (a downloadable
// URL), AutoMax's deployed contract expects base64-encoded image content directly, same
// as the legacy singular fileKey field.
type EPMIncidentPhoto struct {
	IncidentImage string `json:"IncidentImage"`
}

// EPMExternalEntity is one entry of EEList, per TFIS v1.0 Table 30 (IF-01 field
// dictionary) and its Table 32 sample payload: EEList[].EntityID + EEList[].EEName.
// EntityID's value is the same identifier as EECode used by IF-02 through IF-05 (Table
// 30 row 11: "It must map exactly to EECode used by IF-02 to IF-05") — only the field
// *name* differs for this interface. EECode is also accepted as a fallback key since the
// spec itself documents this exact naming inconsistency across interfaces (CL-003: "EE
// identifier appears as EntityID, EECode and EE_Code").
type EPMExternalEntity struct {
	EntityID string `json:"EntityID"`
	EECode   string `json:"EECode"`
	EEName   string `json:"EEName"`
}

// code returns whichever EE identifier field is populated — EntityID (TFIS v1.0's
// documented IF-01 field name) takes priority over EECode (the fallback alias).
func (e EPMExternalEntity) code() string {
	if strings.TrimSpace(e.EntityID) != "" {
		return e.EntityID
	}
	return e.EECode
}

type EPMInsertIncidentRequest struct {
	Address         string `json:"address"`
	BeneficiaryInfo string `json:"beneficiaryInfo"`
	DistrictCode    int    `json:"districtCode"`
	DistrictName    string `json:"districtName"`
	EEFlag          bool   `json:"eeFlag"`
	EEName          string `json:"eeName"`
	// EEList is the array shape MOMRA's real payloads send instead of the single
	// eeFlag/eeName pair — see resolveEERoutingDepartmentByCodeOrName. Kept alongside
	// eeFlag/eeName rather than replacing them, since the legacy pair may still arrive
	// from other callers/test fixtures.
	EEList []EPMExternalEntity `json:"EEList"`
	Email  string              `json:"email"`
	// FileKey is the legacy single-attachment field (base64-encoded image). Kept for
	// backward compatibility; new callers should use IncidentPhotos instead, which
	// matches TFIS v1.0's array shape and supports more than one attachment.
	FileKey        string             `json:"fileKey"`
	IncidentPhotos []EPMIncidentPhoto `json:"incidentPhotos"`
	// IncidentImage is a third accepted shape: a top-level array of base64-encoded
	// image strings directly (no wrapping object), which is what MOMRA's actual test
	// payloads send — distinct from EPMIncidentPhoto.IncidentImage, which is the same
	// field name nested one level down inside IncidentPhotos.
	IncidentImage     []string `json:"IncidentImage"`
	FirstName         string   `json:"firstName"`
	IncidentNo        string   `json:"incidentNo"`
	IncidentStartDate string   `json:"incidentStartDate"`
	IncidentStatusID  int      `json:"incidentStatusID"`
	IqamaID           string   `json:"iqamaID"`
	IssueDiscription  string   `json:"issueDiscription"`
	Language          string   `json:"language"`
	LastName          string   `json:"lastName"`
	Latitude          string   `json:"latitude"`
	Longitude         string   `json:"longitude"`
	// LocationDirections is the key MOMRA's real payloads use.
	LocationDirections   string `json:"LocationDirections"`
	MainClassificationID string `json:"mainClassificationID"`
	MiddleName           string `json:"middleName"`
	MobileNumber         string `json:"mobileNumber"`
	MunicipalityID       string `json:"municipalityID"`
	NationalID           string `json:"nationalID"`
	Priority             string `json:"priority"`
	SPLClassificationID  string `json:"splClassificationID"`
	// SubBaladiyaName is the key MOMRA's real payloads use ("Baladiya" — the
	// correct transliteration); TFIS v1.0 Table 30's own field dictionary spells it
	// "SubBaladyaName" (no "i"), but its own Table 32 sample and every real payload
	// tested use "SubBaladiyaName" — another instance of the spec's own internal
	// inconsistency (see CL-0xx catalog), resolved here in favor of what MOMRA
	// actually sends.
	SubBaladiyaName      string `json:"subBaladiyaName"`
	SubClassificationID  string `json:"subClassificationID"`
	SubMunicipalityID    string `json:"subMunicipalityID"`
	SubSubMunicipalityID string `json:"sub_SubMunicipalityID"`
}

// EPMInsertIncidentResponse is IF-01's documented response contract (TFIS v1.0 Table
// 33). SuccessfullySubmited is a legacy String "Yes"/"No" — not a JSON boolean — per
// the spec's own CL-004 callout: MOMRA's actual system expects the string, this isn't
// a typo to "fix" into a bool. EAmanaIncidentNo is not a separately generated ID: Table
// 33's rule ("unique AutoMax/Amana incident number") and Para 118 (MOMRA reuses this
// exact value in later IF-02/IF-03 calls as AmanaIncidentNo) both confirm it's the same
// value as Incident.IncidentNumber, already reused that way by UpdateIncident/
// UpdateIncidentV2 today. ErrorCode/RequestID are marked "Proposed" in Table 33 (OD-004,
// still an open decision) rather than mandatory — they're always present in the JSON
// (never omitted) but null/empty when unused, matching the documented sample where a
// EPMInsertIncidentResponse is the shared response envelope for InsertIncidents and
// the V1 UpdateIncident/ReopenIncident (handleEEOutcome) endpoints. InsertIncidents'
// response briefly used TFIS v1.0 Table 33's SuccessfullySubmited/ResponseDescription/
// EAmanaIncidentNo/ErrorCode shape, then reverted back to this
// ex/httpStatusCode/message/result/ticketNumber shape after confirming the actual
// caller exercising this endpoint expects the legacy shape, not the documented CRM
// contract — the same divergence-from-docs pattern seen everywhere else in this
// integration. Consolidated with the V1 outcome endpoints' identical
// EPMOutcomeResponse type once both shapes matched again — split them back apart if
// either contract diverges in the future.
type EPMInsertIncidentResponse struct {
	Ex             string `json:"ex"`
	HTTPStatusCode int    `json:"httpStatusCode"`
	Message        string `json:"message"`
	Result         bool   `json:"result"`
	TicketNumber   string `json:"ticketNumber"`
}

// epmInsertErrorResponse sends the failure shape: Result false, Message set to the
// failure summary, httpStatusCode echoed in both the actual HTTP status and the body.
func epmInsertErrorResponse(c *fiber.Ctx, status int, description string) error {
	return c.Status(status).JSON(EPMInsertIncidentResponse{
		HTTPStatusCode: status,
		Message:        description,
		Result:         false,
	})
}

// epmInsertSuccessResponse sends the success shape: Result true, Message, and
// TicketNumber set to AutoMax's own generated incident number (unchanged numbering
// scheme — "INC-YYYY-NNNNNN" — this only affects the response envelope, not what
// number gets generated).
func epmInsertSuccessResponse(c *fiber.Ctx, description, ticketNumber string) error {
	return c.Status(fiber.StatusOK).JSON(EPMInsertIncidentResponse{
		HTTPStatusCode: fiber.StatusOK,
		Message:        description,
		Result:         true,
		TicketNumber:   ticketNumber,
	})
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
		return epmInsertErrorResponse(c, fiber.StatusUnauthorized, "Missing Authorization header")
	}

	tokenString := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenString == authHeader {
		return epmInsertErrorResponse(c, fiber.StatusUnauthorized, "Invalid Authorization header format, use Bearer token")
	}

	claims, err := h.jwtManager.ValidateToken(tokenString)
	if err != nil {
		return epmInsertErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "invalid_or_expired_token"))
	}

	var sessionData map[string]interface{}
	if err := h.sessionStore.GetUserSession(c.UserContext(), claims.SessionID, &sessionData); err != nil {
		return epmInsertErrorResponse(c, fiber.StatusUnauthorized, "Session expired or invalid")
	}

	var req EPMInsertIncidentRequest
	if err := c.BodyParser(&req); err != nil {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "Invalid request body: "+err.Error())
	}

	if req.IncidentNo == "" {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "incidentNo is required")
	}
	if req.FirstName == "" {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "firstName is required")
	}
	if req.IssueDiscription == "" {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "issueDiscription is required")
	}
	if len(req.IssueDiscription) > 250 {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "issueDiscription exceeds maximum length of 250 characters")
	}
	if req.Priority == "" {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "priority is required")
	}
	// Resolved once here and reused after incident creation below (see
	// resolvePriorityValue) — MOMRA's real test payloads have sent Priority as a
	// leading numeral, an English word (TFIS v1.0's documented High/Medium/Low —
	// OD-011 flags this as an open, never-resolved inconsistency in the spec
	// itself), and a diacritic-marked Arabic word (e.g. "حَرَج" = Critical), so a
	// plain leading-digit check is no longer sufficient to validate this field. If
	// the lookup query itself fails (infra issue, not a bad value), validation is
	// skipped rather than hard-rejecting the incident.
	priorityValues, priorityLookupErr := h.lookupRepo.ListValuesByCategoryCode(c.UserContext(), "PRIORITY")
	var resolvedPriority *models.LookupValue
	if priorityLookupErr == nil {
		resolvedPriority = resolvePriorityValue(priorityValues, req.Priority)
		if resolvedPriority == nil {
			return epmInsertErrorResponse(c, fiber.StatusBadRequest, "priority must be valid value")
		}
	}
	// Accept any of three shapes — the legacy singular fileKey, the IncidentPhotos
	// array of {IncidentImage} objects (TFIS v1.0's documented shape), or a top-level
	// IncidentImage array of base64 strings directly (what MOMRA's actual payloads
	// send) — at least one photo is required, and every one provided must be valid
	// base64. photoKeys drives the actual attachment upload loop further below.
	var photoKeys []string
	if req.FileKey != "" {
		photoKeys = append(photoKeys, req.FileKey)
	}
	for _, p := range req.IncidentPhotos {
		if p.IncidentImage != "" {
			photoKeys = append(photoKeys, p.IncidentImage)
		}
	}
	for _, img := range req.IncidentImage {
		if img != "" {
			photoKeys = append(photoKeys, img)
		}
	}
	if len(photoKeys) == 0 {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "fileKey or incidentPhotos or IncidentImage is required")
	}
	for _, key := range photoKeys {
		if _, err := base64.StdEncoding.DecodeString(key); err != nil {
			return epmInsertErrorResponse(c, fiber.StatusBadRequest, "fileKey, incidentPhotos, and IncidentImage entries must be valid base64 encoded strings")
		}
	}
	if req.Latitude == "" {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "latitude is required")
	}
	if req.Longitude == "" {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "longitude is required")
	}
	if _, err := strconv.ParseFloat(req.Latitude, 64); err != nil {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "latitude must be a valid number")
	}
	if _, err := strconv.ParseFloat(req.Longitude, 64); err != nil {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "longitude must be a valid number")
	}
	if req.MobileNumber == "" {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "mobileNumber is required")
	}
	if !regexp.MustCompile(`^\+?[0-9\-\(\)\s]+$`).MatchString(req.MobileNumber) {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "mobileNumber must contain only digits and valid phone characters (+, -, (, ))")
	}

	// check for duplicate momra incident number
	var duplicateCount int64
	h.db.WithContext(c.UserContext()).Model(&models.Incident{}).
		Where("source = ? AND custom_fields LIKE ?", "MOMRA", "%\"momra_incident_no\":\""+req.IncidentNo+"\"%").
		Count(&duplicateCount)
	if duplicateCount > 0 {
		return epmInsertErrorResponse(c, fiber.StatusConflict, "Duplicate incident number: "+req.IncidentNo)
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
	if req.LocationDirections != "" {
		custFields["locationDirection"] = req.LocationDirections
	}
	if req.SubBaladiyaName != "" {
		custFields["subBaladiyaName"] = req.SubBaladiyaName
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

	// req.EEList is stored on the dedicated Incident.AvailableEEList jsonb column
	// (models/incident.go), not folded into custFields — see that field's doc comment
	// for why. Always marshaled (even when empty) so an incident deliberately submitted
	// with no eligible EEs is distinguishable from one where this was never populated.
	eeListToStore := req.EEList
	if eeListToStore == nil {
		eeListToStore = []EPMExternalEntity{}
	}
	availableEEListBytes, _ := json.Marshal(eeListToStore)

	// validate all locations are provided and exist in DB
	if req.MunicipalityID == "" {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "municipalityID is required")
	}
	if req.SubMunicipalityID == "" {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "subMunicipalityID is required")
	}
	if req.SubSubMunicipalityID == "" {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "sub_SubMunicipalityID is required")
	}
	municipalityLoc, err := h.validateAndResolveLocation(c.UserContext(), req.MunicipalityID)
	if err != nil {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "Municipality not found: "+req.MunicipalityID)
	}

	subMunicipalityLoc, err := h.validateAndResolveLocation(c.UserContext(), req.SubMunicipalityID)
	if err != nil {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "Sub municipality not found: "+req.SubMunicipalityID)
	}

	subSubMunicipalityLoc, err := h.validateAndResolveLocation(c.UserContext(), req.SubSubMunicipalityID)
	if err != nil {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "Sub sub municipality not found: "+req.SubSubMunicipalityID)
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
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "mainClassificationID is required")
	}
	if req.SubClassificationID == "" {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "subClassificationID is required")
	}
	if req.SPLClassificationID == "" {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "splClassificationID is required")
	}
	subCls, err := h.validateAndResolveClassification(c.UserContext(), req.SubClassificationID)
	if err != nil {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "Sub classification not found: "+req.SubClassificationID)
	}

	mainCls, err := h.validateAndResolveClassification(c.UserContext(), req.MainClassificationID)
	if err != nil {
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, "Main classification not found: "+req.MainClassificationID)
	}

	var splCls *uuid.UUID
	if req.SPLClassificationID != "" {
		splCls, err = h.validateAndResolveClassification(c.UserContext(), req.SPLClassificationID)
		if err != nil {
			return epmInsertErrorResponse(c, fiber.StatusBadRequest, "Special classification not found: "+req.SPLClassificationID)
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
		return epmInsertErrorResponse(c, fiber.StatusBadRequest, msg)
	}

	// EE routing: when EEList is populated (this handler is MOMRA-only, so Source is
	// always "MOMRA"), department assignment happens exclusively through the "EE
	// Assign" workflow transition (validateExternalDepartmentAssignment /
	// ExecuteTransition's EEList-aware auto-detect merge, incident_service.go) — NOT
	// here at creation time. Previously this pre-picked EEList[0] and wrote it
	// straight to DepartmentID, which silently pre-assigned an EE before any agent (or
	// the transition's own auto-detect) ever got to choose among the incident's actual
	// eligible candidates (AvailableEEList, set below) — defeating the point of that
	// transition. The primary entry is still validated here (reject outright per
	// IF01-V06/ERR-003 if it's undefined/inactive), just not wired into DepartmentID.
	// The legacy single-value EEFlag/EEName path is untouched: with no list to defer a
	// choice on, resolving and assigning immediately is still correct there.
	var eeDepartmentID *uuid.UUID
	var primaryEE *EPMExternalEntity
	for i := range req.EEList {
		if req.EEList[i].code() != "" || strings.TrimSpace(req.EEList[i].EEName) != "" {
			primaryEE = &req.EEList[i]
			break
		}
	}
	switch {
	case primaryEE != nil:
		if _, err = h.resolveEERoutingDepartmentByCodeOrName(c.UserContext(), primaryEE.code(), primaryEE.EEName); err != nil {
			return epmInsertErrorResponse(c, fiber.StatusBadRequest, err.Error())
		}
	case req.EEFlag:
		eeDepartmentID, err = h.resolveEERoutingDepartment(c.UserContext(), req.EEName)
		if err != nil {
			return epmInsertErrorResponse(c, fiber.StatusBadRequest, err.Error())
		}
	}

	// get incident workflow and initial state
	workflow, err := h.workflowRepo.GetDefaultWorkflow(c.UserContext())
	if err != nil || workflow.RecordType != "incident" {
		workflows, listErr := h.workflowRepo.ListByRecordType(c.UserContext(), "incident", true)
		if listErr != nil || len(workflows) == 0 {
			return epmInsertErrorResponse(c, fiber.StatusInternalServerError, "No incident workflow configured")
		}
		workflow = &workflows[0]
	}

	initialState, err := h.workflowRepo.GetInitialState(c.UserContext(), workflow.ID)
	if err != nil {
		return epmInsertErrorResponse(c, fiber.StatusInternalServerError, "No initial state found for incident workflow")
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
		return epmInsertErrorResponse(c, fiber.StatusInternalServerError, "Failed to generate incident number")
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
		AvailableEEList:  datatypes.JSON(availableEEListBytes),
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	if err := h.incidentRepo.Create(c.UserContext(), incident); err != nil {
		return epmInsertErrorResponse(c, fiber.StatusInternalServerError, "Failed to create incident: "+err.Error())
	}

	if resolvedPriority != nil {
		h.incidentRepo.SetLookupValues(c.UserContext(), incident.ID, []models.LookupValue{*resolvedPriority})
	}

	// Upload every provided photo (legacy singular fileKey plus any IncidentPhotos
	// entries, collected into photoKeys above) as its own attachment.
	for _, key := range photoKeys {
		decoded, err := base64.StdEncoding.DecodeString(key)
		if err != nil || len(decoded) == 0 {
			continue
		}
		ext := detectImageExt(decoded)
		mimeType := "image/" + ext
		if ext == "jpeg" {
			mimeType = "image/jpeg"
		}
		filename := fmt.Sprintf("momra_%s_%s.%s", req.IncidentNo, uuid.New().String()[:8], ext)
		folder := fmt.Sprintf("incidents/%s", incident.ID.String())

		filePath, err := h.storage.UploadBytes(c.UserContext(), decoded, filename, mimeType, folder)
		if err != nil {
			continue
		}
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

	return epmInsertSuccessResponse(c, "Incident submitted successfully", incident.IncidentNumber)
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
			Message:        "Session expired or invalid",
			Result:         false,
		})
	}

	var req EPMPushOutcomeRequest
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
			Message:        "Failed to update incident: " + err.Error(),
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

// UpdateIncidentV2 implements PATCH /Momra/API/EPM/UpdateIncidentV2. The target workflow
// state is resolved from the admin-configured WorkflowState -> CaseStatusID mapping
// (momra_status_mappings) for the incident's workflow, so any mapped IncidentStatusID is
// accepted, not just the historical 002 (resolve)/003 (reject) pair. EECode/EEName are
// persisted into Incident.ExternalEntityID/ExternalAssignmentStatus/ExternalAssignedAt on
// every call, independent of whether the status actually changed, so a MOMRA-side EE
// reassignment is recorded even without a status transition.
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
		return momraV2Response(c, fiber.StatusUnauthorized, "Session expired or invalid", false)
	}

	var req EPMUpdateIncidentV2Request
	if err := c.BodyParser(&req); err != nil {
		return momraV2Response(c, fiber.StatusBadRequest, "Invalid request body: "+err.Error(), false)
	}

	if req.IncidentNo == "" {
		return momraV2Response(c, fiber.StatusBadRequest, "IncidentNO is required", false)
	}
	if req.AmanaIncidentNo == "" {
		return momraV2Response(c, fiber.StatusBadRequest, "AmanaIncidentNo is required", false)
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
	if req.IncidentStatusID == "" {
		return momraV2Response(c, fiber.StatusBadRequest, "IncidentStatusID is required", false)
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

	// Resolve the target state from the admin-configured WorkflowState -> CaseStatusID
	// mapping (the same table that drives the outbound status sync, used here in
	// reverse) instead of hardcoding recognition of only 002/003 — whatever statuses
	// are mapped for this workflow are accepted.
	mapping, err := h.momraStatusMappingRepo.FindActiveByWorkflowAndCaseStatusID(c.UserContext(), incident.WorkflowID, req.IncidentStatusID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return momraV2Response(c, fiber.StatusBadRequest, "No workflow state is mapped to IncidentStatusID "+req.IncidentStatusID+" for this incident's workflow", false)
		}
		return momraV2Response(c, fiber.StatusInternalServerError, "Failed to resolve status mapping: "+err.Error(), false)
	}
	var targetState models.WorkflowState
	if err := h.db.WithContext(c.UserContext()).First(&targetState, "id = ?", mapping.StateID).Error; err != nil {
		return momraV2Response(c, fiber.StatusInternalServerError, "Mapped workflow state not found", false)
	}

	// A reason is required for any non-closure transition (e.g. rejection/return),
	// generalizing TFIS IF03-R05's "required for status 003" rule.
	if !mapping.IsClosureStatus && req.ActionDescription == "" {
		return momraV2Response(c, fiber.StatusBadRequest, "ActionDescription is required for this status", false)
	}

	if req.EENotes != "" {
		custFields["ee_notes"] = req.EENotes
	}
	custFieldBytes, _ := json.Marshal(custFields)

	now := time.Now()
	assignedAt := now
	if parsed, parseErr := time.Parse(time.RFC3339, req.ActionDate); parseErr == nil {
		assignedAt = parsed
	}
	assignmentStatus := "rejected"
	if mapping.IsClosureStatus {
		assignmentStatus = "resolved"
	}

	// Persist the external-entity assignment on every call, not just when the status
	// changes — MOMRA may report a reassignment to a different EE independently of a
	// state transition. EECode resolves against Department{Type:"external"}, the same
	// model the outbound sync uses.
	updates := map[string]interface{}{
		"custom_fields":              string(custFieldBytes),
		"updated_at":                 now,
		"external_assignment_status": assignmentStatus,
		"external_assigned_at":       assignedAt,
	}
	if ee, eeErr := h.departmentRepo.FindByCode(c.UserContext(), req.EECode); eeErr == nil && ee.Type == externalEntityDepartmentType {
		updates["external_entity_id"] = ee.ID
	} else {
		fmt.Printf("Warning: EECode %s on incident %s did not resolve to a known external entity; ExternalEntityID left unchanged\n", req.EECode, incident.IncidentNumber)
	}

	stateChanged := incident.CurrentStateID != targetState.ID
	if stateChanged {
		updates["current_state_id"] = targetState.ID
		if mapping.IsClosureStatus {
			updates["resolved_at"] = now
		} else {
			updates["resolved_at"] = nil
			updates["closed_at"] = nil
		}
	}

	if err := h.incidentRepo.UpdateFields(c.UserContext(), incident.ID, updates); err != nil {
		return momraV2Response(c, fiber.StatusInternalServerError, "Failed to update incident: "+err.Error(), false)
	}

	if stateChanged {
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
		return momraV2Response(c, fiber.StatusUnauthorized, "Session expired or invalid", false)
	}

	var req EPMReopenIncidentV2Request
	if err := c.BodyParser(&req); err != nil {
		return momraV2Response(c, fiber.StatusBadRequest, "Invalid request body: "+err.Error(), false)
	}

	if req.AmanaIncidentNo == "" {
		return momraV2Response(c, fiber.StatusBadRequest, "amanaincidentno is required", false)
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
			return momraV2Response(c, fiber.StatusInternalServerError, "Failed to record evaluation: "+err.Error(), false)
		}
		return momraV2Response(c, fiber.StatusOK, "Evaluation recorded; incident remains resolved", true)
	}

	initialState, err := h.workflowRepo.GetInitialState(c.UserContext(), incident.WorkflowID)
	if err != nil {
		return momraV2Response(c, fiber.StatusInternalServerError, "No initial state configured for incident workflow", false)
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
		return momraV2Response(c, fiber.StatusInternalServerError, "Failed to reopen incident: "+err.Error(), false)
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

// EPMAssignExternalEntityRequest is a best-guess shape for a dedicated MOMRA->Automax
// external-entity assignment notification (docs/MOMRA_Outbound_Integration_Spec_v1.0.md
// §7, OD-N2). Field names/casing mirror the sibling V2 endpoints' conventions since
// MOMRA hasn't published this contract yet. Kept alongside UpdateIncidentV2 (which now
// also persists EECode/EEName on every call) as a separate, explicit path for MOMRA to
// report an EE assignment/reassignment on its own, independent of any status change —
// expect to adjust field names once MOMRA confirms the real mechanism.
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

// stripArabicDiacritics removes Arabic harakat/tashkeel marks (fatha, damma, kasra,
// shadda, sukun, tanwin, superscript alef, and Quranic annotation marks) so a value
// like "حَرَج" (with diacritics) matches a stored lookup name like "حرج" (without).
// MOMRA's real InsertIncidents test payloads have sent Priority as a diacritic-marked
// Arabic word rather than the numeric code parsePriorityLevel originally assumed.
func stripArabicDiacritics(s string) string {
	var b strings.Builder
	for _, r := range s {
		if (r >= 0x064B && r <= 0x065F) || r == 0x0670 || (r >= 0x06D6 && r <= 0x06ED) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// normalizePriorityText trims, lowercases, and strips Arabic diacritics so priority
// values can be matched against LookupValue.Code/Name/NameAr regardless of case or
// optional tashkeel marks.
func normalizePriorityText(s string) string {
	return strings.ToLower(strings.TrimSpace(stripArabicDiacritics(s)))
}

// resolvePriorityValue finds the PRIORITY lookup value matching the incoming MOMRA
// priority string. MOMRA's real payloads have used at least three different shapes for
// this field across testing: a leading numeral (e.g. "1"), an English word (TFIS
// v1.0's documented "High"/"Medium"/"Low" — see OD-011, an open decision the spec
// itself never resolved), and a diacritic-marked Arabic word (e.g. "حَرَج" = Critical,
// matching the seeded LookupValue.NameAr "حرج"). All three are matched here rather
// than picking one, since MOMRA has not committed to a single wire format; the
// leading-digit scheme is kept as a last-resort fallback for backward compatibility.
func resolvePriorityValue(priorityValues []models.LookupValue, priority string) *models.LookupValue {
	trimmed := strings.TrimSpace(priority)
	if trimmed == "" {
		return nil
	}
	normalized := normalizePriorityText(trimmed)
	for i := range priorityValues {
		v := &priorityValues[i]
		if normalizePriorityText(v.Code) == normalized ||
			normalizePriorityText(v.Name) == normalized ||
			normalizePriorityText(v.NameAr) == normalized {
			return v
		}
	}
	if level := parsePriorityLevel(trimmed); level != 0 {
		for i := range priorityValues {
			if priorityValues[i].SortOrder == level {
				return &priorityValues[i]
			}
		}
	}
	return nil
}
