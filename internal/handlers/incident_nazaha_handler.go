package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/automax/backend/internal/middleware"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// Nazaha (CLIENT_CODE=NAZAHA) create/edit-incident screen. Create and edit use the
// generic incident endpoints, which call the checks here for Nazaha only. The
// previous-reports and reveal routes are registered only for Nazaha (cmd/server/main.go).

// Caller number (1.2): digits only, with an optional leading "+".
var nazahaCallerNumberPattern = regexp.MustCompile(`^\+?[0-9]{3,20}$`)

// ListCallerPreviousReports returns the caller's earlier incidents (1.3).
func (h *IncidentHandler) ListCallerPreviousReports(c *fiber.Ctx) error {
	ctx := c.UserContext()
	callerNumber := strings.TrimSpace(c.Query("caller_number"))
	if callerNumber == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(ctx, "caller_number_required"))
	}
	callerNumber = utils.NormalizeMobile(callerNumber, utils.SystemCountryCode())

	reports, err := h.incidentRepo.ListByCallerNumber(ctx, callerNumber)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(ctx, "internal_server_error"))
	}
	if reports == nil {
		reports = []models.CallerPreviousReport{}
	}
	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(ctx, "previous_reports_retrieved"), reports)
}

// prepareNazahaCreate checks the Nazaha fields of a create request (POST /incidents)
// and adds problems to fieldErrors, which already holds the struct-validation errors.
// It also normalizes the caller number, fills LinkedIncidentID and defaults the
// source. The returned error is for database failures only.
func (h *IncidentHandler) prepareNazahaCreate(ctx context.Context, req *models.IncidentCreateRequest, fieldErrors map[string]string) error {
	req.TrimNazahaFields()
	// Caller numbers are stored as +<country code><number> so lookups match.
	if req.CallerNumber != "" {
		req.CallerNumber = utils.NormalizeMobile(req.CallerNumber, utils.SystemCountryCode())
	}
	if req.Source == "" {
		req.Source = constants.INCIDENT_SOURCE.WEB
	}

	for field, msg := range validateNazahaFields(ctx, req) {
		if _, exists := fieldErrors[field]; !exists {
			fieldErrors[field] = msg
		}
	}
	h.checkNazahaRequiredFields(ctx, req, fieldErrors)
	if len(fieldErrors) != 0 {
		return nil
	}
	return h.checkNazahaReferences(ctx, req, fieldErrors)
}

// RevealReporter returns the reporter details of an incident, including an
// uncooperative reporter's. Every call is written to the action log under its own
// module so the reveal history can be filtered.
func (h *IncidentHandler) RevealReporter(c *fiber.Ctx) error {
	ctx := c.UserContext()
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(ctx, "invalid_incident_id"))
	}

	incident, err := h.incidentRepo.FindByID(ctx, id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(ctx, "incident_not_found"))
	}
	if !h.checkUserAccessToIncident(c, incident) {
		return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(ctx, "forbidden_no_access"))
	}
	// All reporter details live on the reporter's contact. Older reports link the
	// creating operator instead of a contact; their details are never shown as the reporter's.
	if models.HasReporterContact(incident) {
		if reporter, err := h.userRepo.FindByID(ctx, *incident.ReporterID); err == nil {
			if reporter.NationalityID != nil {
				if nationality, err := h.lookupRepo.FindValueByID(ctx, *reporter.NationalityID); err == nil {
					reporter.Nationality = nationality
				}
			}
			incident.Reporter = reporter
		}
	}

	if h.actionLogSvc != nil {
		middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
			Action:      "view",
			Module:      models.ActionLogModuleReporterReveal,
			ResourceID:  incident.ID.String(),
			Description: fmt.Sprintf("Viewed reporter details of incident %s", incident.IncidentNumber),
			Status:      "success",
		})
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(ctx, "reporter_details_retrieved"), models.ToReporterDetailsResponse(incident))
}

// validateNazahaFields runs the format checks the struct tags can't express.
// Keys are the Go field names, the same keys validation.ValidateStruct returns.
func validateNazahaFields(ctx context.Context, req *models.IncidentCreateRequest) map[string]string {
	errs := map[string]string{}

	if req.CallerNumber != "" && !nazahaCallerNumberPattern.MatchString(req.CallerNumber) {
		errs["CallerNumber"] = i18n.T(ctx, "invalid_caller_number")
	}
	if req.ReporterNationalID != "" && !services.IsNationalID(req.ReporterNationalID) {
		errs["ReporterNationalID"] = i18n.T(ctx, "national_id_invalid")
	}
	if req.ReporterPhone != "" && !validation.IsValidMobile(req.ReporterPhone) {
		errs["ReporterPhone"] = i18n.T(ctx, "invalid_mobile")
	}
	return errs
}

// checkNazahaRequiredFields adds an error for each Nazaha-only field the selected
// workflow marks required but that was left empty. (The workflow's other fields
// follow the generic create rules.) No or unknown workflow: nothing to check here,
// the service reports it.
func (h *IncidentHandler) checkNazahaRequiredFields(ctx context.Context, req *models.IncidentCreateRequest, fieldErrors map[string]string) {
	workflowID, err := uuid.Parse(req.WorkflowID)
	if err != nil {
		return
	}
	workflow, err := h.workflowRepo.FindByID(ctx, workflowID)
	if err != nil {
		return
	}
	var required []string
	_ = json.Unmarshal([]byte(workflow.RequiredFields), &required)
	for _, field := range required {
		if !slices.Contains(models.NazahaWorkflowFields, field) || req.NazahaFieldValue(field) != "" {
			continue
		}
		key := models.NazahaFieldErrorKey(field)
		if _, exists := fieldErrors[key]; !exists {
			fieldErrors[key] = i18n.T(ctx, "field_required")
		}
	}
}

// checkNazahaReferences checks the fields that point at other records (concerned
// authority, nationality, linked report, previous reports), sets LinkedIncidentID
// and removes duplicate previous reports. Problems are added to fieldErrors; the
// returned error is for database failures only.
func (h *IncidentHandler) checkNazahaReferences(ctx context.Context, req *models.IncidentCreateRequest, fieldErrors map[string]string) error {
	if req.ConcernedAuthorityID != nil && !h.isActiveLookupValue(ctx, *req.ConcernedAuthorityID, models.AuthorityCategoryCode) {
		fieldErrors["ConcernedAuthorityID"] = i18n.T(ctx, "invalid_concerned_authority")
	}
	if req.ReporterNationalityID != nil && !h.isActiveLookupValue(ctx, *req.ReporterNationalityID, models.NationalityCategoryCode) {
		fieldErrors["ReporterNationalityID"] = i18n.T(ctx, "invalid_nationality")
	}

	if req.LinkedIncidentNumber != "" {
		linked, err := h.incidentRepo.FindByIncidentNumber(ctx, req.LinkedIncidentNumber)
		if err != nil {
			fieldErrors["LinkedIncidentNumber"] = i18n.T(ctx, "linked_incident_not_found")
		} else {
			req.LinkedIncidentID = &linked.ID
		}
	}

	if len(req.PreviousReportIDs) > 0 {
		callerReports := map[string]bool{}
		if req.CallerNumber != "" {
			reports, err := h.incidentRepo.ListByCallerNumber(ctx, req.CallerNumber)
			if err != nil {
				return err
			}
			for _, r := range reports {
				callerReports[r.ID.String()] = true
			}
		}
		unique := make([]string, 0, len(req.PreviousReportIDs))
		for _, id := range req.PreviousReportIDs {
			id = strings.ToLower(id)
			if !callerReports[id] {
				fieldErrors["PreviousReportIDs"] = i18n.T(ctx, "invalid_previous_reports")
			}
			if !slices.Contains(unique, id) {
				unique = append(unique, id)
			}
		}
		req.PreviousReportIDs = unique
	}
	return nil
}

// isActiveLookupValue reports whether id is an active value of the master-data
// list (lookup category) with this code.
func (h *IncidentHandler) isActiveLookupValue(ctx context.Context, id string, categoryCode string) bool {
	valueID, err := uuid.Parse(id)
	if err != nil {
		return false
	}
	value, err := h.lookupRepo.FindValueByID(ctx, valueID)
	return err == nil && value.IsActive && value.Category != nil && value.Category.Code == categoryCode
}

// checkNazahaEdit checks the Nazaha fields of an edit (PUT /incidents/:id).
// It returns 403 when reporter details of a hidden (uncooperative) reporter are
// sent by a user without the reveal permission, and field errors otherwise.
func (h *IncidentHandler) checkNazahaEdit(c *fiber.Ctx, id uuid.UUID, req *models.IncidentUpdateRequest) (int, map[string]string, error) {
	ctx := c.UserContext()
	incident, err := h.incidentRepo.FindByID(ctx, id)
	if err != nil {
		return 0, nil, nil // not found is reported by the update itself
	}

	reporterSent := req.ReporterName != "" || req.ReporterPhone != "" || req.ReporterEmail != "" ||
		req.ReporterNationalID != nil || req.ReporterNationalityID != nil ||
		req.ReporterGender != nil || req.ReporterAddress != nil
	hidden := incident.CooperationStatus == models.CooperationStatusUncooperative ||
		(req.CooperationStatus != nil && *req.CooperationStatus == models.CooperationStatusUncooperative)
	if reporterSent && hidden {
		user, _ := c.Locals(constants.ContextKeys.User).(*models.User)
		if user == nil || !user.HasPermission(models.PermissionRevealReporter) {
			return fiber.StatusForbidden, nil, nil
		}
	}

	if req.CallerNumber != nil && *req.CallerNumber != "" {
		normalized := utils.NormalizeMobile(strings.TrimSpace(*req.CallerNumber), utils.SystemCountryCode())
		req.CallerNumber = &normalized
	}

	// Same format checks as create.
	if req.ReporterNationalityID != nil {
		trimmed := strings.TrimSpace(*req.ReporterNationalityID)
		req.ReporterNationalityID = &trimmed
	}
	concernedAuthorityID := strings.TrimSpace(deref(req.ConcernedAuthorityID))
	fieldErrors := validateNazahaFields(ctx, &models.IncidentCreateRequest{
		CallerNumber:       deref(req.CallerNumber),
		ReporterNationalID: strings.TrimSpace(deref(req.ReporterNationalID)),
	})

	// Fields the workflow requires may not be cleared.
	if workflow, err := h.workflowRepo.FindByID(ctx, incident.WorkflowID); err == nil {
		var required []string
		_ = json.Unmarshal([]byte(workflow.RequiredFields), &required)
		// Workflow field name → the value sent ("reporter_phone" is the Caller Number).
		sent := map[string]*string{
			"reporter_phone": req.CallerNumber, "linked_incident_number": req.LinkedIncidentNumber,
			"concerned_authority_id": req.ConcernedAuthorityID, "reporter_nationality": req.ReporterNationalityID,
			"reporter_national_id": req.ReporterNationalID, "reporter_address": req.ReporterAddress,
			"reporter_gender": req.ReporterGender, "content": req.Content, "notes": req.Notes, "street": req.Street,
		}
		for _, field := range required {
			if v, ok := sent[field]; ok && v != nil && strings.TrimSpace(*v) == "" {
				fieldErrors[models.NazahaFieldErrorKey(field)] = i18n.T(ctx, "field_required")
			}
			if field == "previous_report_ids" && req.PreviousReportIDs != nil && len(*req.PreviousReportIDs) == 0 {
				fieldErrors[field] = i18n.T(ctx, "field_required")
			}
		}
	}

	// Concerned authority must be in the list; previous reports must be the caller's.
	if concernedAuthorityID != "" && !h.isActiveLookupValue(ctx, concernedAuthorityID, models.AuthorityCategoryCode) {
		fieldErrors["ConcernedAuthorityID"] = i18n.T(ctx, "invalid_concerned_authority")
	}
	if req.ReporterNationalityID != nil && *req.ReporterNationalityID != "" &&
		!h.isActiveLookupValue(ctx, *req.ReporterNationalityID, models.NationalityCategoryCode) {
		fieldErrors["ReporterNationalityID"] = i18n.T(ctx, "invalid_nationality")
	}
	if req.PreviousReportIDs != nil && len(*req.PreviousReportIDs) > 0 {
		callerNumber := incident.CallerNumber
		if req.CallerNumber != nil {
			callerNumber = *req.CallerNumber
		}
		callerReports := map[string]bool{}
		if callerNumber != "" {
			reports, err := h.incidentRepo.ListByCallerNumber(ctx, callerNumber)
			if err != nil {
				return 0, nil, err
			}
			for _, r := range reports {
				callerReports[r.ID.String()] = true
			}
		}
		for _, idStr := range *req.PreviousReportIDs {
			if !callerReports[idStr] || idStr == incident.ID.String() {
				fieldErrors["PreviousReportIDs"] = i18n.T(ctx, "invalid_previous_reports")
			}
		}
	}
	return 0, fieldErrors, nil
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}
