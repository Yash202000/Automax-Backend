package services

import (
	"bytes"
	"context"
	"encoding/csv"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"
	_ "time/tzdata"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/templates"
	"github.com/google/uuid"
)

// EscalationGroupService manages custom escalation group configuration and

type EscalationGroupService struct {
	groupRepo           repository.EscalationGroupRepository
	incidentRepo        repository.IncidentRepository
	notificationService *NotificationService
	frontendURL         string
	dailyHour           int
	dailyMinute         int
	weeklyHour          int
	weeklyMinute        int
}

func NewEscalationGroupService(groupRepo repository.EscalationGroupRepository, incidentRepo repository.IncidentRepository, notificationService *NotificationService, cfg config.EscalationConfig) *EscalationGroupService {
	dailyHour, _ := strconv.Atoi(os.Getenv("ESCALATION_DAILY_HOUR"))
	dailyMinute, _ := strconv.Atoi(os.Getenv("ESCALATION_DAILY_MINUTE"))
	weeklyHour, _ := strconv.Atoi(os.Getenv("ESCALATION_WEEKLY_HOUR"))
	weeklyMinute, _ := strconv.Atoi(os.Getenv("ESCALATION_WEEKLY_MINUTE"))
	return &EscalationGroupService{
		groupRepo:           groupRepo,
		incidentRepo:        incidentRepo,
		notificationService: notificationService,
		dailyHour:           dailyHour,
		dailyMinute:         dailyMinute,
		weeklyHour:          weeklyHour,
		weeklyMinute:        weeklyMinute,
	}
}

// ─── CRUD Apis

func (s *EscalationGroupService) Create(ctx context.Context, req *models.CreateEscalationGroupRequest) (*models.EscalationGroupResponse, error) {
	classificationID, err := uuid.Parse(req.ClassificationID)
	if err != nil {
		return nil, fmt.Errorf("invalid classification_id: %w", err)
	}

	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	group := &models.EscalationGroup{
		Name:             req.Name,
		ClassificationID: classificationID,
		Frequency:        req.Frequency,
		Channel:          req.Channel,
		IsActive:         isActive,
	}

	if err := s.groupRepo.Create(ctx, group); err != nil {
		return nil, err
	}

	if len(req.UserIDs) > 0 {
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
		group.ClassificationID = classificationID
	}
	if req.Frequency != nil {
		group.Frequency = *req.Frequency
	}
	if req.Channel != nil {
		group.Channel = *req.Channel
	}
	if req.IsActive != nil {
		group.IsActive = *req.IsActive
	}

	if err := s.groupRepo.Update(ctx, group); err != nil {
		return nil, err
	}

	// Replace user list when explicitly provided (nil slice = no change)
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

		if len(group.Users) == 0 {
			log.Printf("[EscalationGroupService] Group Name: '%s' & Frequency: (%s) — no users configured, skipping.", group.Name, group.Frequency)
			continue
		}

		// Fetch open SLA-breached incidents for this group's classification.
		// Pass zero UUID for locationID to skip that filter.
		incidents, err := s.incidentRepo.GetBreachedByFilter(ctx, uuid.UUID{}, group.ClassificationID)
		if err != nil {
			log.Printf("[EscalationGroupService] GetBreachedByFilter error for group '%s': %v", group.Name, err)
			continue
		}

		if len(incidents) == 0 {
			log.Printf("[EscalationGroupService] Group '%s' — no breached incidents for classification, skipping.", group.Name)
			continue
		}

		classificationName := ""
		if group.Classification != nil {
			classificationName = group.Classification.Name
		}

		log.Printf("[EscalationGroupService] Group '%s' — %d breached incident(s), notifying %d user(s) via %s.",
			group.Name, len(incidents), len(group.Users), group.Channel)

		// URL linking to the SLA-breached incidents page on the frontend
		frontendURL := os.Getenv("FRONTEND_BASE_URL")
		if frontendURL == "" {
			log.Printf("[EscalationGroupService] FRONTEND_BASE_URL is not set — skipping group '%s'", group.Name)
			continue
		}
		slaPageURL := fmt.Sprintf("%s/incidents?sla_breached=true", frontendURL)

		// CSV report built once and shared across every user in this group
		csvData := buildGroupCSVReport(incidents, classificationName)

		for _, user := range group.Users {
			s.sendGroupNotification(ctx, group, user, len(incidents), classificationName, slaPageURL, csvData)
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

func (s *EscalationGroupService) sendGroupNotification(ctx context.Context, group models.EscalationGroup, user models.User, incidentCount int,
	classificationName string,
	slaPageURL string,
	csvData []byte,
) {

	smsBody := templates.BuildSLABreachSMS(
		incidentCount,
		classificationName,
		slaPageURL,
	)

	emailSubject, emailBody := templates.BuildSLABreachEmail(
		user.FirstName,
		user.LastName,
		incidentCount,
		classificationName,
		slaPageURL,
	)

	sendSMS := group.Channel == "sms" || group.Channel == "both"
	sendEmail := group.Channel == "email" || group.Channel == "both"

	if sendSMS && user.Phone != "" {
		_, err := s.notificationService.SendNotification(
			ctx, "sms", nil, "en",
			[]string{user.Phone}, nil, nil,
			"", smsBody,
			nil, nil, nil, nil,
		)
		if err != nil {
			log.Printf("[EscalationGroupService] SMS failed for user %s (group '%s'): %v", user.Email, group.Name, err)
		} else {
			log.Printf("[EscalationGroupService] SMS sent to %s (group '%s')", user.Phone, group.Name)
		}
	}

	if sendEmail && user.Email != "" {
		attachments := []models.AttachmentData{
			{AttachmentID: uuid.New(),
				Filename:    fmt.Sprintf("sla_breach_report_%s.csv", time.Now().Format("2006-01-02")),
				ContentType: "text/csv; charset=UTF-8",
				Data:        csvData,
			},
		}
		_, err := s.notificationService.SendNotification(
			ctx, "email", nil, "en",
			[]string{user.Email}, nil, nil,
			emailSubject, emailBody,
			nil, attachments, nil, nil,
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

// shouldSendNow returns true when the group's scheduled send window has arrived

func (s *EscalationGroupService) shouldSendNow(group *models.EscalationGroup) bool {

	loc, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		log.Printf("Failed to load timezone: %v", err)
		return false
	}
	now := time.Now().In(loc)

	switch group.Frequency {
	case "daily":
		// The target time is today at dailyHour:dailyMinute
		target := time.Date(now.Year(), now.Month(), now.Day(),
			s.dailyHour, s.dailyMinute, 0, 0, loc)
		log.Printf(
			"[DEBUG] Now=%v | Target=%v | LastNotifiedAt=%v",
			now,
			target,
			group.LastNotifiedAt,
		)

		// Has the scheduled time arrived today?
		if now.Before(target) {
			return false // too early today
		}
		// Has it already been sent since this target time?
		if group.LastNotifiedAt == nil {
			return true
		}
		return group.LastNotifiedAt.Before(target)

	case "weekly":
		// Find this week's Monday
		weekday := int(now.Weekday()) // 0 = Sunday
		if weekday == 0 {
			weekday = 7 // treat Sunday as 7 so Monday is always offset -(weekday-1)
		}
		monday := now.AddDate(0, 0, -(weekday - 1))

		// The target time is this Monday at weeklyHour:weeklyMinute
		// target := time.Date(monday.Year(), monday.Month(), monday.Day(),
		// 	s.weeklyHour, s.weeklyMinute, 0, 0, now.Location())

		target := time.Date(monday.Year(), monday.Month(), monday.Day(),
			s.weeklyHour, s.weeklyMinute, 0, 0, loc)

		// Has the scheduled time arrived this week?
		if now.Before(target) {
			return false // too early this week (e.g. Monday but before 09:00)
		}
		// Has it already been sent since this target time?
		if group.LastNotifiedAt == nil {
			return true
		}

		return group.LastNotifiedAt.Before(target)
	}

	return false
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
