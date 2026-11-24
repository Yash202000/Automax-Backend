package services

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	pkgutils "github.com/automax/backend/pkg/utils"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Nazaha reporter-contact helpers. Reporter details live on the contact (user);
// the incident only links to it.

// findReporterUser finds the reporter's contact. Other clients match by mobile;
// Nazaha matches by national ID, then mobile, then email, and refuses a mobile or
// email that belongs to someone else. Returns gorm.ErrRecordNotFound when none.
func (s *incidentService) findReporterUser(ctx context.Context, req *models.IncidentCreateRequest, isNazaha bool) (*models.User, error) {
	if !isNazaha {
		return s.userRepo.FindByMobile(ctx, req.ReporterPhone)
	}

	var byMobile *models.User
	if req.ReporterPhone != "" {
		u, err := s.userRepo.FindByMobile(ctx, req.ReporterPhone)
		if err != nil && err != gorm.ErrRecordNotFound {
			return nil, err
		}
		byMobile = u
	}

	var contact *models.User
	if IsNationalID(req.ReporterNationalID) {
		byID, err := s.userRepo.FindByNationalID(ctx, req.ReporterNationalID)
		if err != nil && err != gorm.ErrRecordNotFound {
			return nil, err
		}
		if byID != nil {
			if byMobile != nil && byMobile.ID != byID.ID {
				return nil, contactConflict(ctx, "reporter_phone", "mobile_belongs_to_other_contact")
			}
			contact = byID
		}
	}
	if contact == nil && byMobile != nil {
		// Conflict only when a different national ID was given; mobile alone is the same contact.
		if IsNationalID(byMobile.NationalID) && req.ReporterNationalID != "" && byMobile.NationalID != req.ReporterNationalID {
			return nil, contactConflict(ctx, "reporter_phone", "mobile_belongs_to_other_contact")
		}
		contact = byMobile
	}

	if req.ReporterEmail != "" {
		byEmail, err := s.userRepo.FindByEmail(ctx, strings.ToLower(req.ReporterEmail))
		if err != nil && err != gorm.ErrRecordNotFound {
			return nil, err
		}
		switch {
		case byEmail == nil:
		case contact == nil:
			// Matched by email only: refuse if the ID belongs to someone else.
			if IsNationalID(byEmail.NationalID) && req.ReporterNationalID != "" && byEmail.NationalID != req.ReporterNationalID {
				return nil, contactConflict(ctx, "reporter_email", "email_belongs_to_other_contact")
			}
			contact = byEmail
		case byEmail.ID != contact.ID:
			return nil, contactConflict(ctx, "reporter_email", "email_belongs_to_other_contact")
		}
	}

	if contact == nil {
		return nil, gorm.ErrRecordNotFound
	}
	return contact, nil
}

func contactConflict(ctx context.Context, field, key string) IncidentFieldErrors {
	return IncidentFieldErrors{field: i18n.T(ctx, key)}
}

// nazahaReporterUserUpdates returns the contact fields to update from this report.
// user is nil for a contact that was just created.
func nazahaReporterUserUpdates(user *models.User, req *models.IncidentCreateRequest) map[string]interface{} {
	updates := map[string]interface{}{}
	// Empty values never clear what the contact already has.
	if nationality := parseUUIDPtr(derefTrim(req.ReporterNationalityID)); nationality != nil && (user == nil || !sameUUID(user.NationalityID, nationality)) {
		updates["nationality_id"] = *nationality
	}
	if req.ReporterGender != "" && (user == nil || user.Gender != req.ReporterGender) {
		updates["gender"] = req.ReporterGender
	}
	if req.ReporterAddress != "" && (user == nil || user.Address != req.ReporterAddress) {
		updates["address"] = req.ReporterAddress
	}
	if user != nil && !IsNationalID(user.NationalID) && IsNationalID(req.ReporterNationalID) {
		updates["national_id"] = req.ReporterNationalID
	}
	// findReporterUser already made sure no other contact has this mobile or email.
	if user != nil && req.ReporterEmail != "" && !strings.EqualFold(user.Email, req.ReporterEmail) {
		updates["email"] = strings.ToLower(req.ReporterEmail)
	}
	if user != nil && req.ReporterPhone != "" && !pkgutils.SameMobile(user.Phone, req.ReporterPhone) {
		updates["phone"] = pkgutils.NormalizeMobile(req.ReporterPhone, pkgutils.SystemCountryCode())
	}
	return updates
}

// hasNazahaReporterDetails reports whether any reporter detail was given.
func hasNazahaReporterDetails(req *models.IncidentCreateRequest) bool {
	for _, v := range []string{
		req.ReporterName, req.ReporterPhone, req.ReporterEmail, req.ReporterNationalID,
		req.ReporterGender, req.ReporterAddress,
	} {
		if v != "" {
			return true
		}
	}
	return parseUUIDPtr(derefTrim(req.ReporterNationalityID)) != nil
}

// ── Editing ──

// nazahaEditPlan is what an edit changes beyond the generic incident fields.
type nazahaEditPlan struct {
	incidentUpdates map[string]interface{} // Nazaha columns on the incident
	reporter        *models.IncidentCreateRequest
	contact         *models.User // contact to update; nil = create one
	linkContact     bool         // set the incident's reporter to contact
	previousIDs     []uuid.UUID
	replacePrevious bool
}

// prepareNazahaEdit applies the Nazaha fields to the incident in memory and checks
// the contact changes. Reporter fields are removed from req so they never reach
// the incident.
func (s *incidentService) prepareNazahaEdit(ctx context.Context, incident *models.Incident, req *models.IncidentUpdateRequest) (*nazahaEditPlan, []models.IncidentFieldChange, error) {
	plan := &nazahaEditPlan{incidentUpdates: map[string]interface{}{}}
	var changes []models.IncidentFieldChange

	setText := func(field, label string, value *string, current *string) {
		if value == nil || *value == *current {
			return
		}
		oldVal, newVal := *current, *value
		changes = append(changes, models.IncidentFieldChange{FieldName: field, FieldLabel: label, OldValue: &oldVal, NewValue: &newVal})
		*current = newVal
		plan.incidentUpdates[field] = newVal
	}
	setText("caller_number", "Caller Number", req.CallerNumber, &incident.CallerNumber)
	setText("content", "Content", req.Content, &incident.Content)
	setText("notes", "Notes", req.Notes, &incident.Notes)
	setText("street", "Street", req.Street, &incident.Street)

	setID := func(field, label string, newID *uuid.UUID, current **uuid.UUID) {
		if sameUUID(newID, *current) {
			return
		}
		oldVal, newVal := nazahaUUIDString(*current), nazahaUUIDString(newID)
		changes = append(changes, models.IncidentFieldChange{FieldName: field, FieldLabel: label, OldValue: &oldVal, NewValue: &newVal})
		*current = newID
		if newID == nil {
			plan.incidentUpdates[field] = nil
		} else {
			plan.incidentUpdates[field] = *newID
		}
	}
	if req.ConcernedAuthorityID != nil {
		setID("concerned_authority_id", "Concerned Authority", parseUUIDPtr(*req.ConcernedAuthorityID), &incident.ConcernedAuthorityID)
	}
	if req.LinkedIncidentNumber != nil {
		var linkedID *uuid.UUID
		if number := strings.TrimSpace(*req.LinkedIncidentNumber); number != "" {
			linked, err := s.incidentRepo.FindByIncidentNumber(ctx, number)
			if err != nil {
				return nil, nil, IncidentFieldErrors{"linked_incident_number": i18n.T(ctx, "linked_incident_not_found")}
			}
			if linked.ID == incident.ID {
				return nil, nil, IncidentFieldErrors{"linked_incident_number": i18n.T(ctx, "linked_incident_self")}
			}
			linkedID = &linked.ID
		}
		setID("linked_incident_id", "Link to Previous Report", linkedID, &incident.LinkedIncidentID)
	}
	if req.PreviousReportIDs != nil {
		seen := map[uuid.UUID]bool{}
		for _, idStr := range *req.PreviousReportIDs {
			if id, err := uuid.Parse(idStr); err == nil && !seen[id] && id != incident.ID {
				seen[id] = true
				plan.previousIDs = append(plan.previousIDs, id)
			}
		}
		plan.replacePrevious = true
		newVal := strings.Join(*req.PreviousReportIDs, ", ")
		changes = append(changes, models.IncidentFieldChange{FieldName: "previous_report_ids", FieldLabel: "Previous Reports", NewValue: &newVal})
	}

	// Reporter details → the reporter's contact.
	reporter := &models.IncidentCreateRequest{
		ReporterName:          strings.TrimSpace(req.ReporterName),
		ReporterPhone:         strings.TrimSpace(req.ReporterPhone),
		ReporterEmail:         strings.TrimSpace(req.ReporterEmail),
		ReporterNationalID:    derefTrim(req.ReporterNationalID),
		ReporterNationalityID: req.ReporterNationalityID,
		ReporterGender:        derefTrim(req.ReporterGender),
		ReporterAddress:       derefTrim(req.ReporterAddress),
	}
	req.ReporterName, req.ReporterPhone, req.ReporterEmail = "", "", ""
	if !hasNazahaReporterDetails(reporter) {
		return plan, changes, nil
	}
	plan.reporter = reporter
	if contact := linkedContact(incident); contact != nil {
		if err := s.checkContactEdit(ctx, contact, reporter); err != nil {
			return nil, nil, err
		}
		plan.contact = contact
	} else {
		// Older report without a contact: find or create one.
		found, err := s.findReporterUser(ctx, reporter, true)
		if err != nil && err != gorm.ErrRecordNotFound {
			return nil, nil, err
		}
		plan.contact = found
		plan.linkContact = true
	}
	// Values aren't written to the history, since they may be hidden.
	changes = append(changes, models.IncidentFieldChange{FieldName: "reporter", FieldLabel: "Reporter Details"})
	return plan, changes, nil
}

// checkContactEdit refuses a national ID, mobile or email that belongs to another contact.
func (s *incidentService) checkContactEdit(ctx context.Context, contact *models.User, r *models.IncidentCreateRequest) error {
	if r.ReporterNationalID != "" && r.ReporterNationalID != contact.NationalID {
		if other, err := s.userRepo.FindByNationalID(ctx, r.ReporterNationalID); err == nil && other.ID != contact.ID {
			return contactConflict(ctx, "reporter_national_id", "national_id_belongs_to_other_contact")
		}
	}
	if r.ReporterPhone != "" && !pkgutils.SameMobile(contact.Phone, r.ReporterPhone) {
		if other, err := s.userRepo.FindByMobile(ctx, r.ReporterPhone); err == nil && other.ID != contact.ID {
			return contactConflict(ctx, "reporter_phone", "mobile_belongs_to_other_contact")
		}
	}
	if r.ReporterEmail != "" && !strings.EqualFold(contact.Email, r.ReporterEmail) {
		if other, err := s.userRepo.FindByEmail(ctx, strings.ToLower(r.ReporterEmail)); err == nil && other.ID != contact.ID {
			return contactConflict(ctx, "reporter_email", "email_belongs_to_other_contact")
		}
	}
	return nil
}

// applyNazahaEdit saves the previous-report links and contact changes of an
// edit. It runs inside the update transaction (txRepo); any error must roll
// the edit back. Contact writes are not part of the transaction.
func (s *incidentService) applyNazahaEdit(ctx context.Context, txRepo repository.IncidentRepository, incident *models.Incident, plan *nazahaEditPlan) error {
	if plan.replacePrevious {
		if err := txRepo.ReplacePreviousReportLinks(ctx, incident.ID, plan.previousIDs); err != nil {
			return err
		}
	}
	r := plan.reporter
	if r == nil {
		return nil
	}

	contact := plan.contact
	if contact == nil {
		created, err := s.registerNazahaContact(ctx, r)
		if err != nil {
			LogContactFailure("Create contact from incident edit", "", r.ReporterPhone, err)
			return contactSaveError(ctx, err)
		}
		contact = created
	}

	updates := nazahaReporterUserUpdates(contact, r)
	if r.ReporterName != "" {
		first, middle, last := splitReporterName(r.ReporterName)
		if first != contact.FirstName || middle != contact.MiddleName || last != contact.LastName {
			updates["first_name"], updates["middle_name"], updates["last_name"] = first, middle, last
		}
	}
	// A corrected national ID replaces the old one.
	if IsNationalID(r.ReporterNationalID) && contact.NationalID != r.ReporterNationalID {
		updates["national_id"] = r.ReporterNationalID
	}
	if len(updates) > 0 {
		updates["id"] = contact.ID
		if err := s.userRepo.UpdateProfile(ctx, updates); err != nil {
			LogContactFailure("Update contact from incident edit", contact.ID.String(), r.ReporterPhone, err)
			return contactSaveError(ctx, err)
		}
	}
	if plan.linkContact {
		if err := txRepo.UpdateFields(ctx, incident.ID, map[string]interface{}{"reporter_id": contact.ID}); err != nil {
			return err
		}
	}
	return nil
}

// contactSaveError turns a duplicate mobile, email or national ID (another
// contact saved it in the meantime) into a field error; other errors pass through.
func contactSaveError(ctx context.Context, err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "idx_users_phone"), msg == i18n.T(ctx, "phone_number_in_use"):
		return contactConflict(ctx, "reporter_phone", "mobile_belongs_to_other_contact")
	case strings.Contains(msg, "idx_users_email"), msg == i18n.T(ctx, "email_already_exists"):
		return contactConflict(ctx, "reporter_email", "email_belongs_to_other_contact")
	case strings.Contains(msg, "idx_users_national_id"):
		return contactConflict(ctx, "reporter_national_id", "national_id_belongs_to_other_contact")
	}
	return err
}

// registerNazahaContact creates a contact from the reporter details.
func (s *incidentService) registerNazahaContact(ctx context.Context, r *models.IncidentCreateRequest) (*models.User, error) {
	phone := pkgutils.NormalizeMobile(r.ReporterPhone, pkgutils.SystemCountryCode())
	idPart := strings.TrimPrefix(phone, "+")
	if idPart == "" {
		idPart = r.ReporterNationalID
	}
	if idPart == "" {
		idPart = strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	}
	epoch := time.Now().Unix()
	req := &models.UserRegisterRequest{
		Phone:      phone,
		Email:      fmt.Sprintf("web_%s_%d@%s", idPart, epoch, constants.APP.DOMAIN),
		Username:   fmt.Sprintf("%s_%s_%d", constants.ROLES.CITIZEN, idPart, epoch),
		Password:   pkgutils.GenerateRandomPassword(12),
		NationalID: r.ReporterNationalID,
	}
	if r.ReporterEmail != "" {
		req.Email = strings.ToLower(r.ReporterEmail)
	}
	req.FirstName, req.MiddleName, req.LastName = splitReporterName(r.ReporterName)
	if role, err := s.roleRepo.FindByCode(ctx, constants.ROLES.CITIZEN); err == nil && role != nil {
		req.RoleIDs = []uuid.UUID{role.ID}
	}
	resp, err := s.userService.Register(ctx, req)
	if err != nil {
		return nil, err
	}
	return s.userRepo.FindByID(ctx, resp.User.ID)
}

// linkedContact returns the reporter contact, or nil when the reporter is the
// operator who created the incident (older reports).
func linkedContact(incident *models.Incident) *models.User {
	if incident.Reporter == nil || !models.HasReporterContact(incident) {
		return nil
	}
	return incident.Reporter
}

func derefTrim(v *string) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(*v)
}

func parseUUIDPtr(s string) *uuid.UUID {
	if id, err := uuid.Parse(strings.TrimSpace(s)); err == nil {
		return &id
	}
	return nil
}

// validUUIDs returns the valid IDs in ids (the handler already checked them).
func validUUIDs(ids []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(ids))
	for _, s := range ids {
		if id := parseUUIDPtr(s); id != nil {
			out = append(out, *id)
		}
	}
	return out
}

func sameUUID(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

func nazahaUUIDString(id *uuid.UUID) string {
	if id == nil {
		return ""
	}
	return id.String()
}
