package services

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/templates"
	"github.com/google/uuid"
)

// EscalationGroupService manages custom escalation group configuration and
// scheduled batch-notification dispatch.

type EscalationGroupService struct {
	groupRepo           repository.EscalationGroupRepository
	incidentRepo        repository.IncidentRepository
	userRepo            repository.UserRepository
	policyService       *EscalationPolicyService
	notificationService *NotificationService
	frontendURL         string
}

func NewEscalationGroupService(groupRepo repository.EscalationGroupRepository, incidentRepo repository.IncidentRepository, userRepo repository.UserRepository, notificationService *NotificationService, cfg config.EscalationConfig) *EscalationGroupService {
	return &EscalationGroupService{
		groupRepo:           groupRepo,
		incidentRepo:        incidentRepo,
		userRepo:            userRepo,
		notificationService: notificationService,
	}
}

// SetFrontendURL sets the base URL used to build deep-links in notification bodies.
func (s *EscalationGroupService) SetFrontendURL(url string) {
	s.frontendURL = url
}

// SetPolicyService injects the EscalationPolicyService for user resolution.
// Called after construction to avoid circular deps.
func (s *EscalationGroupService) SetPolicyService(ps *EscalationPolicyService) {
	s.policyService = ps
}

// ─── CRUD Apis

func (s *EscalationGroupService) Create(ctx context.Context, req *models.CreateEscalationGroupRequest) (*models.EscalationGroupResponse, error) {
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	scheduledTime := req.ScheduledTime
	if scheduledTime == "" {
		scheduledTime = "09:00"
	}
	group := &models.EscalationGroup{
		Name:              req.Name,
		Frequency:         req.Frequency,
		ScheduledTime:     scheduledTime,
		Channel:           req.Channel,
		EmailTemplateCode: req.EmailTemplateCode,
		SMSTemplateCode:   req.SMSTemplateCode,
		IsActive:          isActive,
	}

	// Legacy single classification_id
	if req.ClassificationID != "" {
		id, err := uuid.Parse(req.ClassificationID)
		if err != nil {
			return nil, fmt.Errorf("invalid classification_id: %w", err)
		}
		group.ClassificationID = &id
	}

	if err := s.groupRepo.Create(ctx, group); err != nil {
		return nil, err
	}

	// Multi-classification (preferred)
	classIDs := req.ClassificationIDs
	if len(classIDs) == 0 && req.ClassificationID != "" {
		classIDs = []string{req.ClassificationID}
	}
	if len(classIDs) > 0 {
		ids, err := parseUUIDSlice(classIDs)
		if err != nil {
			return nil, fmt.Errorf("invalid classification_id: %w", err)
		}
		if err := s.groupRepo.SetClassifications(ctx, group.ID, ids); err != nil {
			return nil, err
		}
	}

	if len(req.Targets) > 0 {
		targets, err := buildGroupTargets(req.Targets)
		if err != nil {
			return nil, err
		}
		if err := s.groupRepo.SetTargets(ctx, group.ID, targets); err != nil {
			return nil, err
		}
	} else if len(req.UserIDs) > 0 {
		// legacy path
		userIDs, err := parseUUIDSlice(req.UserIDs)
		if err != nil {
			return nil, fmt.Errorf("invalid user_id in list: %w", err)
		}
		if err := s.groupRepo.SetUsers(ctx, group.ID, userIDs); err != nil {
			return nil, err
		}
	}

	created, err := s.groupRepo.FindByID(ctx, group.ID)
	if err != nil {
		return nil, err
	}
	resp := models.ToEscalationGroupResponse(created)
	return &resp, nil
}

func (s *EscalationGroupService) Update(ctx context.Context, id uuid.UUID, req *models.UpdateEscalationGroupRequest) (*models.EscalationGroupResponse, error) {
	group, err := s.groupRepo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("escalation group not found")
	}

	if req.Name != nil {
		group.Name = *req.Name
	}
	if req.ClassificationID != nil {
		classificationID, err := uuid.Parse(*req.ClassificationID)
		if err != nil {
			return nil, fmt.Errorf("invalid classification_id: %w", err)
		}
		group.ClassificationID = &classificationID
	}
	if req.Frequency != nil {
		group.Frequency = *req.Frequency
	}
	if req.ScheduledTime != nil {
		group.ScheduledTime = *req.ScheduledTime
	}
	if req.Channel != nil {
		group.Channel = *req.Channel
	}
	if req.EmailTemplateCode != nil {
		group.EmailTemplateCode = *req.EmailTemplateCode
	}
	if req.SMSTemplateCode != nil {
		group.SMSTemplateCode = *req.SMSTemplateCode
	}
	if req.IsActive != nil {
		group.IsActive = *req.IsActive
	}

	if err := s.groupRepo.Update(ctx, group); err != nil {
		return nil, err
	}

	// Multi-classification update (non-nil slice = full replacement)
	if req.ClassificationIDs != nil {
		ids, err := parseUUIDSlice(req.ClassificationIDs)
		if err != nil {
			return nil, fmt.Errorf("invalid classification_id: %w", err)
		}
		if err := s.groupRepo.SetClassifications(ctx, group.ID, ids); err != nil {
			return nil, err
		}
	} else if req.ClassificationID != nil {
		// Legacy single → also update the many2many
		id, err := uuid.Parse(*req.ClassificationID)
		if err != nil {
			return nil, fmt.Errorf("invalid classification_id: %w", err)
		}
		if err := s.groupRepo.SetClassifications(ctx, group.ID, []uuid.UUID{id}); err != nil {
			return nil, err
		}
	}

	// nil slice = no change; non-nil (even empty) = full replacement
	if req.Targets != nil {
		targets, err := buildGroupTargets(req.Targets)
		if err != nil {
			return nil, err
		}
		if err := s.groupRepo.SetTargets(ctx, group.ID, targets); err != nil {
			return nil, err
		}
	}
	if req.UserIDs != nil {
		userIDs, err := parseUUIDSlice(req.UserIDs)
		if err != nil {
			return nil, fmt.Errorf("invalid user_id in list: %w", err)
		}
		if err := s.groupRepo.SetUsers(ctx, group.ID, userIDs); err != nil {
			return nil, err
		}
	}

	updated, err := s.groupRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	resp := models.ToEscalationGroupResponse(updated)
	return &resp, nil
}

func (s *EscalationGroupService) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.groupRepo.FindByID(ctx, id); err != nil {
		return fmt.Errorf("escalation group not found")
	}
	return s.groupRepo.Delete(ctx, id)
}

func (s *EscalationGroupService) GetByID(ctx context.Context, id uuid.UUID) (*models.EscalationGroupResponse, error) {
	group, err := s.groupRepo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("escalation group not found")
	}
	resp := models.ToEscalationGroupResponse(group)
	return &resp, nil
}

func (s *EscalationGroupService) List(ctx context.Context) ([]models.EscalationGroupResponse, error) {
	groups, err := s.groupRepo.List(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]models.EscalationGroupResponse, len(groups))
	for i, g := range groups {
		result[i] = models.ToEscalationGroupResponse(&g)
	}
	return result, nil
}

// ─── SLA Processing ────────────────────────────────────────────────────────

// ProcessGroupEscalations is called by the SLA monitor on every tick.
//
// For each active escalation group whose scheduled send-time has arrived
// (and has not already fired in this window), it:
//  1. Finds all open SLA-breached incidents matching the group's classification.
//  2. Sends SMS and/or email (with attached CSV breach report) to every user
//     configured in the group, according to the group's channel setting.
//  3. Stamps last_notified_at so the same window is not fired twice.
func (s *EscalationGroupService) ProcessGroupEscalations(ctx context.Context) error {
	log.Println("[EscalationGroupService] Running custom escalation group check ...")

	groups, err := s.groupRepo.FindAllActive(ctx)
	if err != nil {
		return fmt.Errorf("FindAllActive: %w", err)
	}

	if len(groups) == 0 {
		log.Println("[EscalationGroupService] No active escalation groups configured.")
		return nil
	}

	for _, group := range groups {
		if !s.shouldSendNow(&group) {
			log.Printf("[EscalationGroupService] Group Name:'%s' & Frequency: (%s) — not yet time, skipping.",
				group.Name, group.Frequency)
			continue
		}

		// Merge recipients from both dept/role targets and directly-assigned users (deduplicated)
		seen := make(map[uuid.UUID]struct{})
		var recipients []models.User

		if len(group.Targets) > 0 && s.policyService != nil {
			resolved, err := s.policyService.ResolveGroupTargetUsers(ctx, group.Targets)
			if err != nil {
				log.Printf("[EscalationGroupService] ResolveGroupTargetUsers error for group '%s': %v", group.Name, err)
				continue
			}
			for _, u := range resolved {
				if _, ok := seen[u.ID]; !ok {
					seen[u.ID] = struct{}{}
					recipients = append(recipients, u)
				}
			}
		}

		for _, u := range group.Users {
			if _, ok := seen[u.ID]; !ok {
				seen[u.ID] = struct{}{}
				recipients = append(recipients, u)
			}
		}

		if len(recipients) == 0 {
			log.Printf("[EscalationGroupService] Group Name: '%s' & Frequency: (%s) — no recipients resolved, skipping.", group.Name, group.Frequency)
			continue
		}

		// Collect classification IDs to query (prefer many2many list, fall back to legacy single)
		var classificationIDs []uuid.UUID
		for _, c := range group.Classifications {
			classificationIDs = append(classificationIDs, c.ID)
		}
		if len(classificationIDs) == 0 && group.ClassificationID != nil {
			classificationIDs = []uuid.UUID{*group.ClassificationID}
		}

		// Fetch open SLA-breached incidents for all of the group's classifications.
		var incidents []models.Incident
		for _, classID := range classificationIDs {
			batch, err := s.incidentRepo.GetBreachedByFilter(ctx, uuid.UUID{}, classID)
			if err != nil {
				log.Printf("[EscalationGroupService] GetBreachedByFilter error for group '%s', classification %s: %v", group.Name, classID, err)
				continue
			}
			incidents = append(incidents, batch...)
		}
		// Deduplicate by incident ID
		seenIncidents := make(map[uuid.UUID]struct{})
		deduped := incidents[:0]
		for _, inc := range incidents {
			if _, ok := seenIncidents[inc.ID]; !ok {
				seenIncidents[inc.ID] = struct{}{}
				deduped = append(deduped, inc)
			}
		}
		incidents = deduped

		if len(incidents) == 0 {
			log.Printf("[EscalationGroupService] Group '%s' — no breached incidents for classifications, skipping.", group.Name)
			continue
		}

		// Build a combined classification name label for notifications
		classificationName := ""
		if len(group.Classifications) > 0 {
			names := make([]string, 0, len(group.Classifications))
			for _, c := range group.Classifications {
				names = append(names, c.Name)
			}
			classificationName = joinStrings(names, ", ")
		} else if group.Classification != nil {
			classificationName = group.Classification.Name
		}

		log.Printf("[EscalationGroupService] Group '%s' — %d breached incident(s), notifying %d recipient(s) via %s.",
			group.Name, len(incidents), len(recipients), group.Channel)

		slaPageURL := fmt.Sprintf("%s/incidents?sla_breached=true", s.frontendURL)

		// CSV report built once and shared across every recipient in this group
		csvData := buildGroupCSVReport(incidents, classificationName)

		for _, user := range recipients {
			s.sendGroupNotification(ctx, group, user, classificationName, slaPageURL, csvData, incidents)
		}

		// Stamp last_notified_at so this window is not fired again
		now := time.Now()
		if err := s.groupRepo.UpdateLastNotifiedAt(ctx, group.ID, now); err != nil {
			log.Printf("[EscalationGroupService] Failed to update last_notified_at for group '%s': %v", group.Name, err)
		}
	}

	return nil
}

// sendGroupNotification dispatches SMS and/or email to one user for one group run.
// When the group has a template code configured, it is passed to SendNotification for
// variable substitution; otherwise falls back to the built-in hardcoded message.
func (s *EscalationGroupService) sendGroupNotification(
	ctx context.Context,
	group models.EscalationGroup,
	user models.User,
	classificationName string,
	slaPageURL string,
	csvData []byte,
	incidents []models.Incident,
) {
	incidentCount := len(incidents)

	// Build template variables: base incident vars from a representative incident (if any),
	// then merge group-specific fields on top.
	// incidents_summary contains a numbered plain-text list of all breached incidents.
	var vars map[string]string
	if len(incidents) > 0 {
		vars = BuildIncidentVariables(&incidents[0], nil, nil)
	} else {
		vars = make(map[string]string)
	}
	vars["first_name"] = user.FirstName
	vars["last_name"] = user.LastName
	vars["incident_count"] = fmt.Sprintf("%d", incidentCount)
	vars["classification_name"] = classificationName
	vars["sla_page_url"] = slaPageURL
	vars["incidents_summary"] = buildIncidentsSummary(incidents)
	vars["report_date"] = time.Now().Format("02 Jan 2006, 15:04")

	// sentBy: default to admin@automax.com; fall back to the notified user
	var sentBy *uuid.UUID
	if admin, err := s.userRepo.FindByEmail(ctx, "admin@automax.com"); err == nil && admin != nil {
		sentBy = &admin.ID
	} else {
		sentBy = &user.ID
	}

	sendSMS := group.Channel == "sms" || group.Channel == "both"
	sendEmail := group.Channel == "email" || group.Channel == "both"

	if sendSMS && user.Phone != "" {
		// Always build the hardcoded SMS as fallback content.
		// If the group has a template code, SendNotification will override it with the DB template.
		smsBody := templates.BuildSLABreachSMS(user.FirstName, user.LastName, incidentCount, classificationName, slaPageURL)

		var templateCode *string
		var smsVars map[string]string
		if group.SMSTemplateCode != "" {
			templateCode = &group.SMSTemplateCode
			smsVars = vars
		}

		_, err := s.notificationService.SendNotification(
			ctx, "sms", templateCode, "en",
			[]string{user.Phone}, nil, nil,
			"", smsBody, smsVars, nil, sentBy, nil,
		)
		if err != nil {
			log.Printf("[EscalationGroupService] SMS failed for user %s (group '%s'): %v", user.Email, group.Name, err)
		} else {
			log.Printf("[EscalationGroupService] SMS sent to %s (group '%s')", user.Phone, group.Name)
		}
	}

	if sendEmail && user.Email != "" {
		// Always build the hardcoded email as fallback content.
		// If the group has a template code, SendNotification will override it with the DB template.
		emailSubject, emailBody := templates.BuildSLABreachEmail(user.FirstName, user.LastName, incidentCount, classificationName, slaPageURL)

		var templateCode *string
		var emailVars map[string]string
		if group.EmailTemplateCode != "" {
			templateCode = &group.EmailTemplateCode
			emailVars = vars
		}

		attachments := []models.AttachmentData{
			{
				AttachmentID: uuid.New(),
				Filename:     fmt.Sprintf("sla_breach_report_%s.csv", time.Now().Format("2006-01-02")),
				ContentType:  "text/csv; charset=UTF-8",
				Data:         csvData,
			},
		}
		_, err := s.notificationService.SendNotification(
			ctx, "email", templateCode, "en",
			[]string{user.Email}, nil, nil,
			emailSubject, emailBody,
			emailVars, attachments, sentBy, nil,
		)
		if err != nil {
			log.Printf("[EscalationGroupService] Email failed for user (group '%s'): %v", group.Name, err)
		} else {
			log.Printf("[EscalationGroupService] Email sent to (group '%s')", group.Name)
		}
	}
}

// buildGroupCSVReport generates a CSV report of all breached incidents.

func buildGroupCSVReport(incidents []models.Incident, classificationName string) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0xEF, 0xBB, 0xBF})
	w := csv.NewWriter(&buf)

	now := time.Now()

	// Summary header rows (two metadata rows, then a blank separator)
	_ = w.Write([]string{fmt.Sprintf("SLA Breach Report — %s", classificationName)})
	_ = w.Write([]string{
		fmt.Sprintf("Generated: %s", now.Format("2006-01-02 15:04")),
		fmt.Sprintf("Total Breached: %d", len(incidents)),
	})
	_ = w.Write([]string{}) // blank separator row

	// Column headers
	_ = w.Write([]string{
		"Incident Number",
		"Title",
		"Current State",
		"Assignee",
		"Created At",
		"SLA Deadline",
		"Hours Overdue",
	})

	// Data rows
	for _, inc := range incidents {
		stateName := ""
		if inc.CurrentState != nil {
			stateName = inc.CurrentState.Name
		}

		assigneeName := "Unassigned"
		if inc.Assignee != nil {
			assigneeName = fmt.Sprintf("%s %s", inc.Assignee.FirstName, inc.Assignee.LastName)
		}

		slaDeadline := ""
		hoursOverdue := ""
		if inc.SLADeadline != nil {
			slaDeadline = inc.SLADeadline.Format("2006-01-02 15:04")
			if now.After(*inc.SLADeadline) {
				hoursOverdue = fmt.Sprintf("%.1f", now.Sub(*inc.SLADeadline).Hours())
			}
		}

		_ = w.Write([]string{
			inc.IncidentNumber,
			inc.Title,
			stateName,
			assigneeName,
			inc.CreatedAt.Format("2006-01-02 15:04"),
			slaDeadline,
			hoursOverdue,
		})
	}

	w.Flush()
	if err := w.Error(); err != nil {
		log.Println("CSV write error:", err)
	}

	return buf.Bytes()
}

// parseScheduledTime parses a "HH:MM" string and returns (hour, minute).
// Falls back to 09:00 on any parse error.
func parseScheduledTime(t string) (int, int) {
	if len(t) == 5 && t[2] == ':' {
		h, err1 := strconv.Atoi(t[0:2])
		m, err2 := strconv.Atoi(t[3:5])
		if err1 == nil && err2 == nil && h >= 0 && h <= 23 && m >= 0 && m <= 59 {
			return h, m
		}
	}
	return 9, 0
}

// shouldSendNow returns true when the group's scheduled send window has arrived

func (s *EscalationGroupService) shouldSendNow(group *models.EscalationGroup) bool {

	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		log.Printf("Failed to load timezone: %v", err)
		return false
	}
	now := time.Now().In(loc)

	schedHour, schedMinute := parseScheduledTime(group.ScheduledTime)

	switch group.Frequency {
	case "hourly":
		// Fire at schedMinute of each hour
		target := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), schedMinute, 0, 0, loc)
		if now.Before(target) {
			return false
		}
		if group.LastNotifiedAt == nil {
			return true
		}
		return group.LastNotifiedAt.Before(target)

	case "every_6_hours":
		// Fire at :schedMinute past 00, 06, 12, 18
		slot := (now.Hour() / 6) * 6
		target := time.Date(now.Year(), now.Month(), now.Day(), slot, schedMinute, 0, 0, loc)
		if now.Before(target) {
			return false
		}
		if group.LastNotifiedAt == nil {
			return true
		}
		return group.LastNotifiedAt.Before(target)

	case "every_12_hours":
		// Fire at :schedMinute past 00 and 12
		slot := (now.Hour() / 12) * 12
		target := time.Date(now.Year(), now.Month(), now.Day(), slot, schedMinute, 0, 0, loc)
		if now.Before(target) {
			return false
		}
		if group.LastNotifiedAt == nil {
			return true
		}
		return group.LastNotifiedAt.Before(target)

	case "daily":
		target := time.Date(now.Year(), now.Month(), now.Day(),
			schedHour, schedMinute, 0, 0, loc)
		log.Printf(
			"[DEBUG] Now=%v | Target=%v | LastNotifiedAt=%v",
			now,
			target,
			group.LastNotifiedAt,
		)
		if now.Before(target) {
			return false
		}
		if group.LastNotifiedAt == nil {
			return true
		}
		return group.LastNotifiedAt.Before(target)

	case "weekly":
		weekday := int(now.Weekday()) // 0 = Sunday
		if weekday == 0 {
			weekday = 7
		}
		monday := now.AddDate(0, 0, -(weekday - 1))
		target := time.Date(monday.Year(), monday.Month(), monday.Day(),
			schedHour, schedMinute, 0, 0, loc)
		if now.Before(target) {
			return false
		}
		if group.LastNotifiedAt == nil {
			return true
		}
		return group.LastNotifiedAt.Before(target)

	case "bi_weekly":
		weekday := int(now.Weekday())
		if weekday == 0 {
			weekday = 7
		}
		monday := now.AddDate(0, 0, -(weekday - 1))
		_, isoWeek := monday.ISOWeek()
		if isoWeek%2 != 1 {
			return false
		}
		target := time.Date(monday.Year(), monday.Month(), monday.Day(),
			schedHour, schedMinute, 0, 0, loc)
		if now.Before(target) {
			return false
		}
		if group.LastNotifiedAt == nil {
			return true
		}
		return group.LastNotifiedAt.Before(target)

	case "monthly":
		// Fire on the 1st of each month at the scheduled time
		if now.Day() != 1 {
			return false
		}
		target := time.Date(now.Year(), now.Month(), 1,
			schedHour, schedMinute, 0, 0, loc)
		if now.Before(target) {
			return false
		}
		if group.LastNotifiedAt == nil {
			return true
		}
		return group.LastNotifiedAt.Before(target)
	}

	return false
}

// buildGroupTargets converts API target requests to model structs.
func buildGroupTargets(reqs []models.EscalationGroupTargetRequest) ([]models.EscalationGroupTarget, error) {
	targets := make([]models.EscalationGroupTarget, 0, len(reqs))
	for _, r := range reqs {
		t := models.EscalationGroupTarget{
			ExcludedUserIDs: models.TextArray(r.ExcludedUserIDs),
		}
		if t.ExcludedUserIDs == nil {
			t.ExcludedUserIDs = models.TextArray{}
		}
		if r.DepartmentID != nil {
			id, err := uuid.Parse(*r.DepartmentID)
			if err != nil {
				return nil, fmt.Errorf("invalid department_id: %w", err)
			}
			t.DepartmentID = &id
		}
		if r.RoleID != nil {
			id, err := uuid.Parse(*r.RoleID)
			if err != nil {
				return nil, fmt.Errorf("invalid role_id: %w", err)
			}
			t.RoleID = &id
		}
		targets = append(targets, t)
	}
	return targets, nil
}

// joinStrings joins a slice of strings with a separator.
func joinStrings(ss []string, sep string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}

// buildIncidentsSummary returns a numbered plain-text list of breached incidents
// suitable for use as the {{incidents_summary}} template variable in group emails.
// Each line: "N. INC-0001 — Title (State: X | SLA exceeded by 5.2h)"
func buildIncidentsSummary(incidents []models.Incident) string {
	now := time.Now()
	var sb strings.Builder
	for i, inc := range incidents {
		stateName := "Unknown"
		if inc.CurrentState != nil {
			stateName = inc.CurrentState.Name
		}
		overdue := ""
		if inc.SLADeadline != nil && now.After(*inc.SLADeadline) {
			h := now.Sub(*inc.SLADeadline).Hours()
			overdue = fmt.Sprintf(" | SLA exceeded by %.1fh", h)
		}
		sb.WriteString(fmt.Sprintf("%d. %s — %s (State: %s%s)\n",
			i+1, inc.IncidentNumber, inc.Title, stateName, overdue))
	}
	return sb.String()
}

// parseUUIDSlice parses a slice of UUID strings into []uuid.UUID.
func parseUUIDSlice(strs []string) ([]uuid.UUID, error) {
	ids := make([]uuid.UUID, 0, len(strs))
	for _, s := range strs {
		id, err := uuid.Parse(s)
		if err != nil {
			return nil, fmt.Errorf("invalid UUID %q: %w", s, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}
