package services

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
)

// SearchLogs returns a page of escalation entries.
func (s *EscalationService) SearchLogs(ctx context.Context, filter *models.EscalationLogFilter) (*models.EscalationLogListResponse, error) {
	if filter == nil {
		filter = &models.EscalationLogFilter{}
	}

	page := filter.Page
	if page < 1 {
		page = 1
	}
	limit := filter.Limit
	if limit < 1 {
		limit = 10
	}
	if limit > 100 {
		limit = 100
	}
	filter.Page = page
	filter.Limit = limit

	records, total, err := s.repo.SearchLogs(ctx, filter)
	if err != nil {
		return nil, err
	}

	entries, err := s.buildLogEntries(ctx, records)
	if err != nil {
		return nil, err
	}

	return &models.EscalationLogListResponse{
		Entries: entries,
		Total:   int(total),
		Page:    page,
		Limit:   limit,
	}, nil
}

// GetLogsByIncident returns newest-first escalation entries for an incident.
func (s *EscalationService) GetLogsByIncident(ctx context.Context, incidentID uuid.UUID) ([]models.EscalationLogEntryResponse, error) {
	records, err := s.repo.GetByIncidentID(ctx, incidentID)
	if err != nil {
		return nil, err
	}
	return s.buildLogEntries(ctx, records)
}

// buildLogEntries builds entries and adds their delivery results.
func (s *EscalationService) buildLogEntries(ctx context.Context, records []models.EscalationSLA) ([]models.EscalationLogEntryResponse, error) {
	if len(records) == 0 {
		return []models.EscalationLogEntryResponse{}, nil
	}

	rowIDs := make([]string, len(records))
	for i := range records {
		rowIDs[i] = records[i].ID.String()
	}

	logsByRow := make(map[string][]models.NotificationLog)
	if s.notificationService != nil {
		logs, err := s.notificationService.FindLogsByMetaIDs(ctx, rowIDs)
		if err != nil {
			// Keep escalation entries visible if delivery details cannot be loaded.
			log.Printf("[EscalationService] failed to load notification logs for escalation log: %v", err)
		} else {
			for _, l := range logs {
				if l.Meta == nil || l.Meta.Type != "ESCALATION" {
					continue
				}
				logsByRow[l.Meta.ID] = append(logsByRow[l.Meta.ID], l)
			}
		}
	}

	entries := make([]models.EscalationLogEntryResponse, 0, len(records))
	for i := range records {
		row := &records[i]

		var fromLevel *int
		if row.EscalationPolicyStep != nil && row.IncidentID != nil {
			previous, err := s.repo.GetPreviousFiredLevel(
				ctx,
				*row.IncidentID,
				row.StateID,
				row.EscalationPolicyStep.StepOrder,
				escalationLogTime(row),
			)
			if err != nil {
				log.Printf("[EscalationService] failed to resolve previous escalation level for incident %s: %v", row.IncidentID, err)
			} else {
				fromLevel = previous
			}
		}

		entries = append(entries, buildEscalationLogEntry(row, logsByRow[row.ID.String()], fromLevel))
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].EscalatedAt.Equal(entries[j].EscalatedAt) {
			return entries[i].ID.String() < entries[j].ID.String()
		}
		return entries[i].EscalatedAt.After(entries[j].EscalatedAt)
	})

	return entries, nil
}

// buildEscalationLogEntry builds an entry from one recipient row.
func buildEscalationLogEntry(
	row *models.EscalationSLA,
	logs []models.NotificationLog,
	fromLevel *int,
) models.EscalationLogEntryResponse {
	entry := models.EscalationLogEntryResponse{
		ID:                     row.ID,
		IncidentID:             row.IncidentID,
		StateID:                row.StateID,
		EscalationType:         normalizedEscalationType(row),
		EscalatedAt:            escalationLogTime(row),
		FromLevel:              fromLevel,
		EscalationPolicyID:     row.EscalationPolicyID,
		EscalationPolicyStepID: row.EscalationPolicyStepID,
		SLAHoursAllowed:        row.SLAHoursAllowed,
		HoursInState:           row.HoursInState,
		CreatedAt:              row.CreatedAt,
	}

	if row.EscalationPolicyStep != nil {
		level := row.EscalationPolicyStep.StepOrder
		entry.ToLevel = &level
	}
	if row.Incident != nil {
		entry.IncidentNumber = row.Incident.IncidentNumber
		entry.ClassificationID = row.Incident.ClassificationID
		if row.Incident.Classification != nil {
			entry.ClassificationName = row.Incident.Classification.Name
		}
	}
	if row.State != nil {
		entry.StateName = row.State.Name
	} else if row.Incident != nil && row.Incident.CurrentState != nil {
		entry.StateName = row.Incident.CurrentState.Name
	}

	entry.Reason = buildEscalationReason(row, entry.StateName)
	entry.Recipient = buildEscalationRecipient(row, logs, row.EscalationPolicyStep)
	entry.Status = entry.Recipient.Status

	for _, action := range row.Actions {
		entry.Actions = append(entry.Actions, action)
	}
	sort.Strings(entry.Actions)

	return entry
}

func escalationLogTime(row *models.EscalationSLA) time.Time {
	if row.NotifiedAt != nil {
		return row.NotifiedAt.UTC()
	}
	return row.CreatedAt.UTC()
}

func buildEscalationRecipient(
	row *models.EscalationSLA,
	logs []models.NotificationLog,
	step *models.EscalationPolicyStep,
) models.EscalationRecipientResponse {
	recipient := models.EscalationRecipientResponse{
		UserID:  row.NotifiedUserID,
		Name:    escalationRecipientName(row),
		Email:   row.Email,
		Phone:   row.Phone,
		Notices: []models.EscalationNoticeResultResponse{},
	}

	notices := noticesFromNotificationLogs(logs, row)
	existing := make(map[string]bool, len(notices))
	for _, notice := range notices {
		existing[strings.ToUpper(notice.Channel)] = true
	}
	notices = append(notices, noticesFromActions(row.Actions, row, expectedChannels(step), existing)...)
	sort.SliceStable(notices, func(i, j int) bool {
		return notices[i].Channel < notices[j].Channel
	})
	recipient.Notices = notices

	statuses := make([]string, 0, len(notices))
	for _, notice := range notices {
		statuses = append(statuses, notice.Status)
	}
	recipient.Status = aggregateStatuses(statuses)

	return recipient
}

func noticesFromNotificationLogs(logs []models.NotificationLog, row *models.EscalationSLA) []models.EscalationNoticeResultResponse {
	notices := make([]models.EscalationNoticeResultResponse, 0)
	for i := range logs {
		entry := &logs[i]
		// Prefer logged recipients because providers may normalize contact details.
		if len(entry.Recipients) == 0 {
			notices = append(notices, models.EscalationNoticeResultResponse{
				Channel:           strings.ToUpper(entry.Channel),
				Recipient:         recipientContact(row, entry.Channel),
				Status:            normalizeDeliveryStatus(entry.Status, ""),
				FailureCode:       entry.FailureCode,
				Error:             entry.ErrorMessage,
				NotificationLogID: &entry.ID,
				SentAt:            firstNonNilTime(entry.SentAt, &entry.CreatedAt),
			})
			continue
		}
		for _, info := range entry.Recipients {
			notices = append(notices, noticeFromRecipientInfo(entry, info, row))
		}
	}
	return notices
}

func noticeFromRecipientInfo(
	entry *models.NotificationLog,
	info models.RecipientInfo,
	row *models.EscalationSLA,
) models.EscalationNoticeResultResponse {
	recipient := info.Email
	if recipient == "" {
		recipient = recipientContact(row, entry.Channel)
	}
	failureCode := info.FailureCode
	if failureCode == "" {
		failureCode = entry.FailureCode
	}
	errorMessage := info.ErrorMessage
	if errorMessage == "" {
		errorMessage = info.Error
	}
	if errorMessage == "" {
		errorMessage = entry.ErrorMessage
	}

	return models.EscalationNoticeResultResponse{
		Channel:           strings.ToUpper(entry.Channel),
		Recipient:         recipient,
		Status:            normalizeDeliveryStatus(entry.Status, info.Status),
		FailureCode:       failureCode,
		Error:             errorMessage,
		NotificationLogID: &entry.ID,
		SentAt:            firstNonNilTime(entry.SentAt, &entry.CreatedAt),
	}
}

func noticesFromActions(
	actions models.TextArray,
	row *models.EscalationSLA,
	expected []string,
	existing map[string]bool,
) []models.EscalationNoticeResultResponse {
	notices := make([]models.EscalationNoticeResultResponse, 0)

	for _, action := range actions {
		channel, status := parseAction(action)
		if channel == "" || existing[channel] {
			continue
		}
		existing[channel] = true
		notices = append(notices, models.EscalationNoticeResultResponse{
			Channel:   channel,
			Recipient: recipientContact(row, strings.ToLower(channel)),
			Status:    status,
		})
	}

	for _, channel := range expected {
		channel = strings.ToUpper(channel)
		if existing[channel] {
			continue
		}
		existing[channel] = true
		contact := recipientContact(row, strings.ToLower(channel))
		status := "unknown"
		if contact == "" {
			status = "skipped"
		}
		notices = append(notices, models.EscalationNoticeResultResponse{
			Channel:   channel,
			Recipient: contact,
			Status:    status,
		})
	}

	return notices
}

func parseAction(action string) (channel, status string) {
	upper := strings.ToUpper(strings.TrimSpace(action))
	switch {
	case upper == "EMAIL":
		return "EMAIL", "sent"
	case upper == "SMS":
		return "SMS", "sent"
	case strings.HasPrefix(upper, "EMAIL_FAILED"):
		return "EMAIL", "failed"
	case strings.HasPrefix(upper, "SMS_FAILED"):
		return "SMS", "failed"
	case strings.HasPrefix(upper, "EMAIL_SKIPPED"):
		return "EMAIL", "skipped"
	case strings.HasPrefix(upper, "SMS_SKIPPED"):
		return "SMS", "skipped"
	default:
		return "", ""
	}
}

func expectedChannels(step *models.EscalationPolicyStep) []string {
	if step == nil {
		return []string{"EMAIL", "SMS"}
	}
	switch step.Channel {
	case "email":
		return []string{"EMAIL"}
	case "sms":
		return []string{"SMS"}
	default:
		return []string{"EMAIL", "SMS"}
	}
}

func recipientContact(row *models.EscalationSLA, channel string) string {
	switch strings.ToLower(channel) {
	case "email":
		return row.Email
	case "sms":
		return row.Phone
	default:
		if row.Email != "" {
			return row.Email
		}
		return row.Phone
	}
}

func escalationRecipientName(row *models.EscalationSLA) string {
	if row.NotifiedUser != nil {
		name := strings.TrimSpace(row.NotifiedUser.FirstName + " " + row.NotifiedUser.LastName)
		if name != "" {
			return name
		}
		if row.NotifiedUser.Username != "" {
			return row.NotifiedUser.Username
		}
	}
	return row.Email
}

func normalizeDeliveryStatus(logStatus, recipientStatus string) string {
	status := strings.ToLower(strings.TrimSpace(recipientStatus))
	if status == "" || status == "success" {
		status = strings.ToLower(strings.TrimSpace(logStatus))
	}
	switch status {
	case "sent", "mock-sent", "delivered", "read", "queued", "success":
		return "sent"
	case "failed", "undeliverable", "expired":
		return "failed"
	case "partial":
		return "partial"
	case "skipped":
		return "skipped"
	default:
		return "unknown"
	}
}

func aggregateStatuses(statuses []string) string {
	if len(statuses) == 0 {
		return "unknown"
	}
	var sent, failed, skipped, partial, unknown int
	for _, status := range statuses {
		switch status {
		case "sent":
			sent++
		case "failed":
			failed++
		case "skipped":
			skipped++
		case "partial":
			partial++
		default:
			unknown++
		}
	}

	switch {
	case failed > 0 && (sent > 0 || partial > 0):
		return "partial"
	case failed > 0:
		return "failed"
	case partial > 0:
		return "partial"
	case sent > 0 && (skipped > 0 || unknown > 0):
		return "partial"
	case sent > 0:
		return "sent"
	case skipped > 0 && unknown == 0:
		return "skipped"
	default:
		return "unknown"
	}
}

func normalizedEscalationType(row *models.EscalationSLA) string {
	if row.EscalationType == "global_sla" {
		return "global_sla"
	}
	if row.StateID == nil && row.EscalationPolicyStepID == nil {
		// Legacy global rows may have the default state_sla type.
		if row.EscalationType == "" || row.EscalationType == "state_sla" {
			return "global_sla"
		}
	}
	if row.EscalationType == "" {
		return "state_sla"
	}
	return row.EscalationType
}

func buildEscalationReason(row *models.EscalationSLA, stateName string) string {
	breach := row.HoursInState - float64(row.SLAHoursAllowed)
	if normalizedEscalationType(row) == "global_sla" {
		if stateName != "" {
			return fmt.Sprintf(
				"Global SLA time limit passed: %.1f hours open, allowed %d hours (current state: %s, breach +%.1f hours).",
				row.HoursInState, row.SLAHoursAllowed, stateName, breach,
			)
		}
		return fmt.Sprintf(
			"Global SLA time limit passed: %.1f hours open, allowed %d hours (breach +%.1f hours).",
			row.HoursInState, row.SLAHoursAllowed, breach,
		)
	}

	if stateName != "" {
		return fmt.Sprintf(
			"SLA time limit passed in state %q: %.1f hours in state, allowed %d hours (breach +%.1f hours).",
			stateName, row.HoursInState, row.SLAHoursAllowed, breach,
		)
	}
	return fmt.Sprintf(
		"SLA time limit passed: %.1f hours in state, allowed %d hours (breach +%.1f hours).",
		row.HoursInState, row.SLAHoursAllowed, breach,
	)
}

func firstNonNilTime(values ...*time.Time) *time.Time {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

// ExportLogs exports the filtered escalation log to Excel.
func (s *EscalationService) ExportLogs(ctx context.Context, filter *models.EscalationLogFilter) ([]byte, error) {
	if filter == nil {
		filter = &models.EscalationLogFilter{}
	}
	records, err := s.repo.SearchLogsAll(ctx, filter)
	if err != nil {
		return nil, err
	}
	entries, err := s.buildLogEntries(ctx, records)
	if err != nil {
		return nil, err
	}
	return buildEscalationLogExcel(entries)
}

func buildEscalationLogExcel(entries []models.EscalationLogEntryResponse) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	const sheet = "Escalation Log"
	if err := f.SetSheetName("Sheet1", sheet); err != nil {
		return nil, err
	}

	headers := []string{
		"Incident",
		"Escalated At",
		"Type",
		"Reason",
		"From Level",
		"To Level",
		"Classification",
		"State",
		"Recipient",
		"Delivery",
		"Status",
	}
	for i, header := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		if err := f.SetCellValue(sheet, cell, header); err != nil {
			return nil, err
		}
	}

	for i, entry := range entries {
		row := i + 2
		values := []interface{}{
			entry.IncidentNumber,
			entry.EscalatedAt.UTC().Format("2006-01-02 15:04:05"),
			entry.EscalationType,
			entry.Reason,
			formatLevel(entry.FromLevel),
			formatLevel(entry.ToLevel),
			entry.ClassificationName,
			entry.StateName,
			formatPersonTold(entry.Recipient),
			formatChannelResults(entry.Recipient.Notices),
			entry.Status,
		}
		for col, value := range values {
			cell, _ := excelize.CoordinatesToCellName(col+1, row)
			if err := f.SetCellValue(sheet, cell, value); err != nil {
				return nil, err
			}
		}
	}

	headerStyle, err := f.NewStyle(&excelize.Style{
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
		Fill:      excelize.Fill{Type: "pattern", Color: []string{"3B82F6"}, Pattern: 1},
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
	})
	if err == nil {
		_ = f.SetRowStyle(sheet, 1, 1, headerStyle)
	}
	_ = f.SetRowHeight(sheet, 1, 26)
	_ = f.SetColWidth(sheet, "A", "A", 18)
	_ = f.SetColWidth(sheet, "B", "B", 22)
	_ = f.SetColWidth(sheet, "C", "C", 16)
	_ = f.SetColWidth(sheet, "D", "D", 60)
	_ = f.SetColWidth(sheet, "E", "F", 12)
	_ = f.SetColWidth(sheet, "G", "H", 24)
	_ = f.SetColWidth(sheet, "I", "I", 42)
	_ = f.SetColWidth(sheet, "J", "J", 42)
	_ = f.SetColWidth(sheet, "K", "K", 12)
	_ = f.SetPanes(sheet, &excelize.Panes{
		Freeze:      true,
		YSplit:      1,
		TopLeftCell: "A2",
		ActivePane:  "bottomLeft",
	})
	_ = f.AutoFilter(sheet, "A1:K1", nil)

	buffer, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buffer.Bytes(), nil
}

func formatLevel(level *int) string {
	if level == nil {
		return ""
	}
	return fmt.Sprintf("%d", *level)
}

func formatPersonTold(recipient models.EscalationRecipientResponse) string {
	contact := recipient.Email
	if contact == "" {
		contact = recipient.Phone
	}
	if contact != "" {
		return fmt.Sprintf("%s <%s>", recipient.Name, contact)
	}
	return recipient.Name
}

func formatChannelResults(notices []models.EscalationNoticeResultResponse) string {
	parts := make([]string, 0, len(notices))
	for _, notice := range notices {
		result := fmt.Sprintf("%s: %s", notice.Channel, notice.Status)
		if notice.FailureCode != "" {
			result += " (" + notice.FailureCode + ")"
		}
		parts = append(parts, result)
	}
	return strings.Join(parts, "; ")
}
