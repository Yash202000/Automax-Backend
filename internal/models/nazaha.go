package models

import (
	"regexp"
	"strings"
	"time"

	"github.com/automax/backend/pkg/constants"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Nazaha create-incident screen (CLIENT_CODE=NAZAHA).
// Everything in this file is used only when the deployment is Nazaha.

const (
	GenderMale   = "male"
	GenderFemale = "female"

	// AuthorityCategoryCode is the lookup category (master data) that holds the
	// Concerned Authority (2.4) values. Seeded empty for Nazaha; admins add values.
	AuthorityCategoryCode = "AUTHORITY"

	// NationalityCategoryCode is the lookup category (master data) that holds the
	// reporter Nationality (4.2) values. Seeded for Nazaha; admins can change it.
	NationalityCategoryCode = "NATIONALITY"

	// PermissionRevealReporter lets a user see the details of an uncooperative reporter.
	PermissionRevealReporter = "incidents:reveal-reporter"

	// ActionLogModuleReporterReveal is the action-log module every reveal is logged
	// under, so the reveal history can be filtered on its own.
	ActionLogModuleReporterReveal = "reporter_reveal"
)

// NazahaWorkflowFields are the workflow form-config names the Nazaha screen
// checks here (same names in the frontend workflow designer). The existing
// "reporter_phone" field is the Caller Number on the Nazaha screen; the
// reporter's mobile is the Nazaha field "reporter_mobile".
var NazahaWorkflowFields = []string{
	"reporter_phone", "previous_report_ids", "linked_incident_number", "concerned_authority_id",
	"reporter_mobile", "reporter_nationality", "reporter_national_id", "reporter_address",
	"reporter_gender", "content", "notes", "street",
}

// NazahaFieldErrorKey is the request field an error for a workflow field is
// reported on (the form shows it under that input).
func NazahaFieldErrorKey(field string) string {
	switch field {
	case "reporter_phone":
		return "caller_number"
	case "reporter_mobile":
		return "reporter_phone"
	}
	return field
}

// NazahaFieldValue returns the value of a workflow field by its workflow name
// ("" when empty), for the workflow required-field check.
func (r *IncidentCreateRequest) NazahaFieldValue(field string) string {
	switch field {
	case "reporter_phone":
		return r.CallerNumber
	case "reporter_mobile":
		return r.ReporterPhone
	case "previous_report_ids":
		return strings.Join(r.PreviousReportIDs, ",")
	case "linked_incident_number":
		return r.LinkedIncidentNumber
	case "concerned_authority_id":
		return derefString(r.ConcernedAuthorityID)
	case "reporter_nationality":
		return derefString(r.ReporterNationalityID)
	case "reporter_national_id":
		return r.ReporterNationalID
	case "reporter_address":
		return r.ReporterAddress
	case "reporter_gender":
		return r.ReporterGender
	case "content":
		return r.Content
	case "notes":
		return r.Notes
	case "street":
		return r.Street
	}
	return ""
}

// TrimNazahaFields removes surrounding spaces from the Nazaha and reporter text
// fields (and lower-cases the gender) before they are checked.
func (r *IncidentCreateRequest) TrimNazahaFields() {
	for _, f := range []*string{
		&r.CallerNumber, &r.LinkedIncidentNumber, &r.ReporterName, &r.ReporterNationalID,
		&r.ReporterPhone, &r.ReporterAddress, &r.ReporterEmail, &r.ReporterGender,
		&r.Content, &r.Notes, &r.City, &r.Street, &r.Description, &r.Comment,
	} {
		*f = strings.TrimSpace(*f)
	}
	for _, p := range []**string{&r.ConcernedAuthorityID, &r.ReporterNationalityID} {
		if *p != nil {
			if v := strings.TrimSpace(**p); v != "" {
				*p = &v
			} else {
				*p = nil
			}
		}
	}
	r.ReporterGender = strings.ToLower(r.ReporterGender)
}

// ClearNazahaFields empties the Nazaha-only fields. The create handler calls it for
// every client except Nazaha.
func (r *IncidentCreateRequest) ClearNazahaFields() {
	r.CallerNumber, r.LinkedIncidentNumber, r.Content, r.Notes, r.Street = "", "", "", "", ""
	r.ConcernedAuthorityID, r.ReporterNationalityID, r.LinkedIncidentID = nil, nil, nil
	r.PreviousReportIDs = nil
	r.ReporterNationalID, r.ReporterGender, r.ReporterAddress = "", "", ""
}

func derefString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// IncidentPreviousReport links an incident to one of the caller's earlier incidents
// selected in "Previous Reports" (1.3). An incident can have several.
type IncidentPreviousReport struct {
	ID                 uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	IncidentID         uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_incident_previous_report" json:"incident_id"`
	PreviousIncidentID uuid.UUID `gorm:"type:uuid;not null;uniqueIndex:idx_incident_previous_report;index" json:"previous_incident_id"`
	CreatedAt          time.Time `json:"created_at"`
}

func (p *IncidentPreviousReport) BeforeCreate(tx *gorm.DB) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	return nil
}

// IncidentRef is a short reference to another incident.
type IncidentRef struct {
	ID             uuid.UUID `json:"id"`
	IncidentNumber string    `json:"incident_number"`
}

// CallerPreviousReport is one value of the "Previous Reports" (1.3) dropdown.
type CallerPreviousReport struct {
	ID                   uuid.UUID `json:"id"`
	IncidentNumber       string    `json:"incident_number"`
	CreatedAt            time.Time `json:"created_at"`
	ClassificationName   string    `json:"classification_name"`
	ClassificationNameAr string    `json:"classification_name_ar"`
	StatusName           string    `json:"status_name"`
	StatusNameAr         string    `json:"status_name_ar"`
}

// ReporterDetailsResponse is returned by the reveal endpoint.
type ReporterDetailsResponse struct {
	ReporterName        string               `json:"reporter_name"`
	ReporterPhone       string               `json:"reporter_phone"`
	ReporterEmail       string               `json:"reporter_email"`
	ReporterNationalID  string               `json:"reporter_national_id"`
	ReporterNationality *LookupValueResponse `json:"reporter_nationality,omitempty"`
	ReporterGender      string               `json:"reporter_gender"`
	ReporterAddress     string               `json:"reporter_address"`
}

// ToReporterDetailsResponse returns the reporter details of an incident, all taken
// from the reporter's contact (the incident keeps no copy). A generated login email
// and a generated placeholder national ID are left out: they are not real details.
func ToReporterDetailsResponse(i *Incident) ReporterDetailsResponse {
	u := i.Reporter
	if u == nil {
		return ReporterDetailsResponse{}
	}
	resp := ReporterDetailsResponse{
		ReporterName:    strings.Join(strings.Fields(u.FirstName+" "+u.MiddleName+" "+u.LastName), " "),
		ReporterPhone:   u.Phone,
		ReporterGender:  u.Gender,
		ReporterAddress: u.Address,
	}
	if u.Nationality != nil {
		nationality := ToLookupValueResponse(u.Nationality)
		resp.ReporterNationality = &nationality
	}
	if !strings.HasSuffix(strings.ToLower(u.Email), "@"+constants.APP.DOMAIN) {
		resp.ReporterEmail = u.Email
	}
	if nationalIDPattern.MatchString(u.NationalID) {
		resp.ReporterNationalID = u.NationalID
	}
	return resp
}

// nationalIDPattern matches a real national ID (10 digits, 1 = citizen, 2 = resident).
var nationalIDPattern = regexp.MustCompile(`^[12][0-9]{9}$`)

// hideReporterOfUncooperative is turned on at startup for Nazaha only
// (see SetHideUncooperativeReporter). Other clients never hide reporter details.
var hideReporterOfUncooperative bool

// SetHideUncooperativeReporter turns hiding of uncooperative reporters on or off.
// Called once from main after the config is loaded.
func SetHideUncooperativeReporter(on bool) {
	hideReporterOfUncooperative = on
}

// IsReporterHidden reports whether the reporter details of an incident with this
// cooperation status must be hidden.
func IsReporterHidden(cooperationStatus string) bool {
	return hideReporterOfUncooperative && cooperationStatus == CooperationStatusUncooperative
}

// HideReporter removes the reporter details from an incident in place. Used before
// the incident leaves the system (integrations, reports).
func HideReporter(i *Incident) {
	i.ReporterName = ""
	i.ReporterPhone = ""
	i.ReporterEmail = ""
	i.Reporter = nil
}

func hideUncooperativeReporter(resp *IncidentResponse) {
	if !IsReporterHidden(resp.CooperationStatus) {
		return
	}
	resp.ReporterName = ""
	resp.ReporterPhone = ""
	resp.ReporterEmail = ""
	resp.Reporter = nil
	resp.ReporterID = uuid.Nil
	resp.ReporterHidden = true
}

// HasReporterContact reports whether the incident's reporter link is a reporter
// contact. Reports created before contacts were kept link the creating operator.
func HasReporterContact(i *Incident) bool {
	return i.ReporterID != nil && (i.CreatedByID == nil || *i.CreatedByID != *i.ReporterID)
}
