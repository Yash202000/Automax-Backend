package models

import (
	"regexp"
	"strings"
	"time"

	"github.com/automax/backend/pkg/constants"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Nazaha-only models and helpers.

const (
	GenderMale   = "male"
	GenderFemale = "female"

	// AuthorityCategoryCode is the lookup list of concerned authorities.
	AuthorityCategoryCode = "AUTHORITY"

	// NationalityCategoryCode is the lookup list of nationalities.
	NationalityCategoryCode = "NATIONALITY"

	// PermissionRevealReporter lets a user see the details of an uncooperative reporter.
	PermissionRevealReporter = "incidents:reveal-reporter"

	// ActionLogModuleReporterReveal is the action-log module for reveals.
	ActionLogModuleReporterReveal = "reporter_reveal"
)

// NazahaWorkflowFields are the workflow fields checked for Nazaha. "reporter_phone"
// is the caller number; the reporter's mobile is "reporter_mobile".
var NazahaWorkflowFields = []string{
	"reporter_phone", "previous_report_ids", "linked_incident_number", "concerned_authority_id",
	"reporter_mobile", "reporter_nationality", "reporter_national_id", "reporter_address",
	"reporter_gender", "content", "notes", "street",
}

// NazahaFieldErrorKey returns the request field an error for a workflow field goes on.
func NazahaFieldErrorKey(field string) string {
	switch field {
	case "reporter_phone":
		return "caller_number"
	case "reporter_mobile":
		return "reporter_phone"
	}
	return field
}

// NazahaFieldValue returns the value of a workflow field ("" when empty).
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

// TrimNazahaFields trims the Nazaha and reporter text fields.
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

// ClearNazahaFields empties the Nazaha fields (used for other clients).
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

// IncidentPreviousReport links an incident to one of the caller's earlier incidents.
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

// CallerPreviousReport is one option of the previous reports dropdown.
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

// ToReporterDetailsResponse returns the reporter details from the contact,
// leaving out generated emails and placeholder national IDs.
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

// hideReporterOfUncooperative is turned on at startup for Nazaha only.
var hideReporterOfUncooperative bool

// SetHideUncooperativeReporter turns hiding of uncooperative reporters on or off.
func SetHideUncooperativeReporter(on bool) {
	hideReporterOfUncooperative = on
}

// IsReporterHidden reports whether reporter details must be hidden for this status.
func IsReporterHidden(cooperationStatus string) bool {
	return hideReporterOfUncooperative && cooperationStatus == CooperationStatusUncooperative
}

// HideReporter removes the reporter details from an incident.
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

// HasReporterContact reports whether the reporter is a contact and not the
// operator who created the incident (older reports).
func HasReporterContact(i *Incident) bool {
	return i.ReporterID != nil && (i.CreatedByID == nil || *i.CreatedByID != *i.ReporterID)
}
