package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/google/uuid"
)

// ActionExecutor handles the execution of transition actions
type ActionExecutor interface {
	ExecuteActions(ctx context.Context, incident *models.Incident, transition *models.WorkflowTransition, performedBy *models.User) error
	ExecuteAction(ctx context.Context, action *models.TransitionAction, incident *models.Incident, transition *models.WorkflowTransition, performedBy *models.User) error
}

type actionExecutor struct {
	incidentRepo        repository.IncidentRepository
	userRepo            repository.UserRepository
	notificationService *NotificationService
	httpClient          *http.Client
}

// NewActionExecutor creates a new action executor
func NewActionExecutor(incidentRepo repository.IncidentRepository, userRepo repository.UserRepository, notificationService *NotificationService) ActionExecutor {
	return &actionExecutor{
		incidentRepo:        incidentRepo,
		userRepo:            userRepo,
		notificationService: notificationService,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ExecuteActions executes all actions for a transition
func (e *actionExecutor) ExecuteActions(ctx context.Context, incident *models.Incident, transition *models.WorkflowTransition, performedBy *models.User) error {
	if len(transition.Actions) == 0 {
		return nil
	}

	// Sort actions by execution order
	actions := make([]models.TransitionAction, len(transition.Actions))
	copy(actions, transition.Actions)

	for i := 0; i < len(actions)-1; i++ {
		for j := i + 1; j < len(actions); j++ {
			if actions[i].ExecutionOrder > actions[j].ExecutionOrder {
				actions[i], actions[j] = actions[j], actions[i]
			}
		}
	}

	for _, action := range actions {
		if !action.IsActive {
			continue
		}

		if action.IsAsync {
			go func(act models.TransitionAction) {
				if err := e.ExecuteAction(context.Background(), &act, incident, transition, performedBy); err != nil {
					log.Printf("Async action execution failed (transition=%s action=%s): %v", transition.Name, act.Name, err)
				}
			}(action)
		} else {
			if err := e.ExecuteAction(ctx, &action, incident, transition, performedBy); err != nil {
				log.Printf("Action execution failed (transition=%s action=%s): %v", transition.Name, action.Name, err)
			}
		}
	}

	return nil
}

// ExecuteAction executes a single action
func (e *actionExecutor) ExecuteAction(ctx context.Context, action *models.TransitionAction, incident *models.Incident, transition *models.WorkflowTransition, performedBy *models.User) error {
	switch action.ActionType {
	case "notification":
		return e.executeNotification(ctx, action, incident, transition, performedBy)
	case "email":
		return e.executeEmail(ctx, action, incident, transition, performedBy)
	case "sms":
		return e.executeSms(ctx, action, incident, transition, performedBy)
	case "webhook":
		return e.executeWebhook(ctx, action, incident, transition, performedBy)
	case "field_update":
		return e.executeFieldUpdate(ctx, action, incident)
	default:
		return fmt.Errorf("unknown action type: %s", action.ActionType)
	}
}

// NotificationConfig represents the configuration for an in-app notification action
type NotificationConfig struct {
	Recipients []string `json:"recipients"` // "assignee", "reporter", "creator", "user:uuid"
	Title      string   `json:"title"`
	Message    string   `json:"message"`
}

// executeNotification sends in-app notifications via the notification service
func (e *actionExecutor) executeNotification(ctx context.Context, action *models.TransitionAction, incident *models.Incident, transition *models.WorkflowTransition, performedBy *models.User) error {
	var config NotificationConfig
	if err := json.Unmarshal([]byte(action.Config), &config); err != nil {
		return fmt.Errorf("invalid notification config: %w", err)
	}

	if e.notificationService == nil {
		log.Printf("NotificationService not available; skipping notification action %q", action.Name)
		return nil
	}

	emails := e.resolveRecipientEmails(ctx, config.Recipients, nil, incident)
	if len(emails) == 0 {
		return nil
	}

	title := e.replacePlaceholders(config.Title, incident, transition, performedBy)
	message := e.replacePlaceholders(config.Message, incident, transition, performedBy)

	_, err := e.notificationService.SendNotification(
		ctx, "notification", nil, "en",
		emails, nil, nil,
		title, message,
		nil, nil, nil, nil,
	)
	if err != nil {
		return fmt.Errorf("notification send failed: %w", err)
	}
	return nil
}

// EmailConfig matches the TransitionEmailConfig sent by the frontend
type EmailConfig struct {
	Recipients             []string `json:"recipients"`              // "assignee", "reporter", "creator", "department_head", "custom"
	CustomEmails           []string `json:"custom_emails"`           // explicit email addresses when "custom" is selected
	TemplateCode           string   `json:"template_code,omitempty"` // notification template code — overrides subject/body when set
	SubjectTemplate        string   `json:"subject_template"`
	BodyTemplate           string   `json:"body_template"`
	Language               string   `json:"language,omitempty"`      // "ar" or "en"; defaults to "ar" when empty
	IncludeIncidentDetails bool     `json:"include_incident_details"`
	IncludeTransitionInfo  bool     `json:"include_transition_info"`
	IncludeComments        bool     `json:"include_comments"`
}

// executeEmail sends email notifications via the notification service
func (e *actionExecutor) executeEmail(ctx context.Context, action *models.TransitionAction, incident *models.Incident, transition *models.WorkflowTransition, performedBy *models.User) error {
	var config EmailConfig
	if err := json.Unmarshal([]byte(action.Config), &config); err != nil {
		return fmt.Errorf("invalid email config: %w", err)
	}

	if e.notificationService == nil {
		log.Printf("[EMAIL-ACTION] NotificationService not available; skipping action %q", action.Name)
		return nil
	}

	transitionName := action.Name
	if transition != nil {
		transitionName = transition.Name
	}

	emails := e.resolveRecipientEmails(ctx, config.Recipients, config.CustomEmails, incident)
	log.Printf("[EMAIL-ACTION] transition=%q incident=%s recipients_config=%v resolved_emails=%v template=%q",
		transitionName, incident.IncidentNumber, config.Recipients, emails, config.TemplateCode)

	if len(emails) == 0 {
		log.Printf("[EMAIL-ACTION] transition=%q incident=%s — no email recipients resolved, skipping",
			transitionName, incident.IncidentNumber)
		return nil
	}

	vars := BuildIncidentVariables(incident, transition, performedBy)

	var templateCode *string
	var subject, body string

	if config.TemplateCode != "" {
		templateCode = &config.TemplateCode
	} else {
		subject = e.replacePlaceholders(config.SubjectTemplate, incident, transition, performedBy)
		body = e.replacePlaceholders(config.BodyTemplate, incident, transition, performedBy)

		if config.IncludeIncidentDetails {
			body += e.buildIncidentDetails(incident)
		}
		if config.IncludeTransitionInfo && transition != nil {
			fromState := ""
			if transition.FromState != nil {
				fromState = transition.FromState.Name
			}
			toState := ""
			if transition.ToState != nil {
				toState = transition.ToState.Name
			}
			body += fmt.Sprintf("\n\nTransition: %s → %s", fromState, toState)
		}
	}

	lang := config.Language
	if lang == "" {
		lang = "ar"
	}

	var sentByID *uuid.UUID
	if performedBy != nil {
		sentByID = &performedBy.ID
	}

	_, err := e.notificationService.SendNotification(
		ctx, "email", templateCode, lang,
		emails, nil, nil,
		subject, body,
		vars, nil, sentByID, nil,
	)
	if err != nil {
		log.Printf("[EMAIL-ACTION] transition=%q incident=%s emails=%v template=%q — FAILED: %v",
			transitionName, incident.IncidentNumber, emails, config.TemplateCode, err)
		return fmt.Errorf("email send failed: %w", err)
	}
	log.Printf("[EMAIL-ACTION] transition=%q incident=%s emails=%v template=%q — SENT OK",
		transitionName, incident.IncidentNumber, emails, config.TemplateCode)
	return nil
}

// SmsConfig represents the configuration for an SMS action
type SmsConfig struct {
	Recipients      []string `json:"recipients"`              // "assignee", "reporter", "creator", "caller"
	CustomPhones    []string `json:"custom_phones"`           // explicit phone numbers
	TemplateCode    string   `json:"template_code,omitempty"` // notification template code — overrides message_template when set
	MessageTemplate string   `json:"message_template"`
	Language        string   `json:"language,omitempty"` // "ar" or "en"; defaults to "ar" when empty
}

// executeSms sends SMS notifications via the notification service
func (e *actionExecutor) executeSms(ctx context.Context, action *models.TransitionAction, incident *models.Incident, transition *models.WorkflowTransition, performedBy *models.User) error {
	var config SmsConfig
	if err := json.Unmarshal([]byte(action.Config), &config); err != nil {
		return fmt.Errorf("invalid sms config: %w", err)
	}

	if e.notificationService == nil {
		log.Printf("[SMS-ACTION] NotificationService not available; skipping action %q", action.Name)
		return nil
	}

	transitionName := action.Name
	if transition != nil {
		transitionName = transition.Name
	}

	phones := e.resolveRecipientPhones(ctx, incident, config.Recipients, config.CustomPhones)
	log.Printf("[SMS-ACTION] transition=%q incident=%s recipients_config=%v resolved_phones=%v template=%q",
		transitionName, incident.IncidentNumber, config.Recipients, phones, config.TemplateCode)

	if len(phones) == 0 {
		log.Printf("[SMS-ACTION] transition=%q incident=%s — no phone recipients resolved, skipping",
			transitionName, incident.IncidentNumber)
		return nil
	}

	vars := BuildIncidentVariables(incident, transition, performedBy)

	var templateCode *string
	var message string

	if config.TemplateCode != "" {
		templateCode = &config.TemplateCode
	} else {
		message = e.replacePlaceholders(config.MessageTemplate, incident, transition, performedBy)
	}

	lang := config.Language
	if lang == "" {
		lang = "ar"
	}

	var smsSentByID *uuid.UUID
	if performedBy != nil {
		smsSentByID = &performedBy.ID
	}

	_, err := e.notificationService.SendNotification(
		ctx, "sms", templateCode, lang,
		phones, nil, nil,
		"", message,
		vars, nil, smsSentByID, nil,
	)
	if err != nil {
		log.Printf("[SMS-ACTION] transition=%q incident=%s phones=%v template=%q — FAILED: %v",
			transitionName, incident.IncidentNumber, phones, config.TemplateCode, err)
		return fmt.Errorf("sms send failed: %w", err)
	}
	log.Printf("[SMS-ACTION] transition=%q incident=%s phones=%v template=%q — SENT OK",
		transitionName, incident.IncidentNumber, phones, config.TemplateCode)
	return nil
}

// WebhookConfig represents the configuration for a webhook action
type WebhookConfig struct {
	URL     string            `json:"url"`
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
}

// executeWebhook calls an external webhook
func (e *actionExecutor) executeWebhook(ctx context.Context, action *models.TransitionAction, incident *models.Incident, transition *models.WorkflowTransition, performedBy *models.User) error {
	var config WebhookConfig
	if err := json.Unmarshal([]byte(action.Config), &config); err != nil {
		return fmt.Errorf("invalid webhook config: %w", err)
	}

	if config.Method == "" {
		config.Method = "POST"
	}

	body := e.replacePlaceholders(config.Body, incident, transition, performedBy)

	req, err := http.NewRequestWithContext(ctx, config.Method, config.URL, bytes.NewBufferString(body))
	if err != nil {
		return fmt.Errorf("failed to create webhook request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for key, value := range config.Headers {
		req.Header.Set(key, value)
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("webhook returned error status: %d", resp.StatusCode)
	}

	log.Printf("Webhook executed: URL=%s, Status=%d", config.URL, resp.StatusCode)
	return nil
}

// FieldUpdateConfig represents the configuration for a field update action
type FieldUpdateConfig struct {
	Field string      `json:"field"`
	Value interface{} `json:"value"`
}

// executeFieldUpdate updates incident fields
func (e *actionExecutor) executeFieldUpdate(ctx context.Context, action *models.TransitionAction, incident *models.Incident) error {
	var config FieldUpdateConfig
	if err := json.Unmarshal([]byte(action.Config), &config); err != nil {
		return fmt.Errorf("invalid field update config: %w", err)
	}

	updates := make(map[string]interface{})

	switch config.Field {
	case "assignee_id":
		if v, ok := config.Value.(string); ok {
			if v == "" || v == "null" {
				updates["assignee_id"] = nil
			} else {
				if uid, err := uuid.Parse(v); err == nil {
					updates["assignee_id"] = uid
				}
			}
		}
	case "department_id":
		if v, ok := config.Value.(string); ok {
			if v == "" || v == "null" {
				updates["department_id"] = nil
			} else {
				if uid, err := uuid.Parse(v); err == nil {
					updates["department_id"] = uid
				}
			}
		}
	default:
		return fmt.Errorf("unsupported field for update: %s", config.Field)
	}

	if len(updates) > 0 {
		if err := e.incidentRepo.UpdateFields(ctx, incident.ID, updates); err != nil {
			return fmt.Errorf("failed to update field: %w", err)
		}
		log.Printf("Field updated: Incident=%s, Field=%s", incident.IncidentNumber, config.Field)
	}

	return nil
}

// resolveRecipientEmails resolves recipient identifiers to email addresses.
// customEmails is merged in directly (used for the "custom" recipient type).
func (e *actionExecutor) resolveRecipientEmails(ctx context.Context, recipients []string, customEmails []string, incident *models.Incident) []string {
	var emails []string
	seen := make(map[string]bool)

	add := func(email string) {
		if email != "" && !seen[email] {
			emails = append(emails, email)
			seen[email] = true
		}
	}

	for _, recipient := range recipients {
		switch recipient {
		case "assignee":
			// Primary assignee
			if incident.Assignee != nil {
				log.Printf("[EMAIL-RESOLVE] assignee (primary) → %s (%s)", incident.Assignee.Email, incident.Assignee.Username)
				add(incident.Assignee.Email)
			}
			// Additional assignees (many-to-many)
			for _, a := range incident.Assignees {
				log.Printf("[EMAIL-RESOLVE] assignee (multi) → %s (%s)", a.Email, a.Username)
				add(a.Email)
			}
			if incident.Assignee == nil && len(incident.Assignees) == 0 {
				log.Printf("[EMAIL-RESOLVE] assignee → none (incident has no assignee)")
			}
		case "reporter":
			if incident.Reporter != nil {
				log.Printf("[EMAIL-RESOLVE] reporter → %s", incident.Reporter.Email)
				add(incident.Reporter.Email)
			} else {
				log.Printf("[EMAIL-RESOLVE] reporter (plain field) → %s", incident.ReporterEmail)
				add(incident.ReporterEmail)
			}
		case "creator":
			// Try mobile lookup first, then fall back to oldest transition history performer.
			if incident.CreatedByMobile != "" {
				if u, err := e.userRepo.FindByMobile(ctx, incident.CreatedByMobile); err == nil {
					log.Printf("[EMAIL-RESOLVE] creator → %s (via mobile %s)", u.Email, incident.CreatedByMobile)
					add(u.Email)
				} else {
					log.Printf("[EMAIL-RESOLVE] creator → mobile lookup failed: %v", err)
				}
			} else if len(incident.TransitionHistory) > 0 {
				oldest := incident.TransitionHistory[len(incident.TransitionHistory)-1]
				if oldest.PerformedBy != nil {
					log.Printf("[EMAIL-RESOLVE] creator → %s (oldest transition performer)", oldest.PerformedBy.Email)
					add(oldest.PerformedBy.Email)
				}
			} else {
				log.Printf("[EMAIL-RESOLVE] creator → could not resolve (no mobile, no history)")
			}
		case "previous_assignee":
			// PreviousAssigneeIDs = all historical assignees collected by ExecuteTransition.
			// Skip citizen/IVR users (username prefix "citizen_").
			if len(incident.PreviousAssigneeIDs) == 0 {
				log.Printf("[EMAIL-RESOLVE] previous_assignee → none found")
			}
			for _, uid := range incident.PreviousAssigneeIDs {
				if u, err := e.userRepo.FindByID(ctx, uid); err == nil {
					if strings.HasPrefix(u.Username, "citizen_") {
						log.Printf("[EMAIL-RESOLVE] previous_assignee → skipped citizen %s", u.Username)
					} else {
						log.Printf("[EMAIL-RESOLVE] previous_assignee → %s (%s)", u.Email, u.Username)
						add(u.Email)
					}
				} else {
					log.Printf("[EMAIL-RESOLVE] previous_assignee → lookup failed for %s: %v", uid, err)
				}
			}
		case "department_head":
			if incident.Department != nil && incident.Department.ManagerID != nil {
				if u, err := e.userRepo.FindByID(ctx, *incident.Department.ManagerID); err == nil {
					add(u.Email)
				}
			}
		case "custom":
			for _, em := range customEmails {
				add(strings.TrimSpace(em))
			}
		default:
			if strings.HasPrefix(recipient, "user:") {
				if uid, err := uuid.Parse(strings.TrimPrefix(recipient, "user:")); err == nil {
					if u, err := e.userRepo.FindByID(ctx, uid); err == nil {
						add(u.Email)
					}
				}
			} else if strings.HasPrefix(recipient, "email:") {
				add(strings.TrimPrefix(recipient, "email:"))
			}
		}
	}

	return emails
}

// resolveRecipientPhones resolves recipient identifiers to phone numbers for SMS.
func (e *actionExecutor) resolveRecipientPhones(ctx context.Context, incident *models.Incident, recipients []string, customPhones []string) []string {
	var phones []string
	seen := make(map[string]bool)

	add := func(phone string) {
		if phone != "" && !seen[phone] {
			phones = append(phones, phone)
			seen[phone] = true
		}
	}

	for _, recipient := range recipients {
		switch recipient {
		case "assignee":
			// Primary assignee
			if incident.Assignee != nil {
				add(incident.Assignee.Phone)
			}
			// Additional assignees (many-to-many)
			for _, a := range incident.Assignees {
				add(a.Phone)
			}
		case "reporter":
			if incident.Reporter != nil {
				add(incident.Reporter.Phone)
			} else {
				add(incident.ReporterPhone)
			}
		case "creator":
			add(incident.CreatedByMobile)
		case "department_head":
			if incident.Department != nil && incident.Department.ManagerID != nil {
				if u, err := e.userRepo.FindByID(ctx, *incident.Department.ManagerID); err == nil {
					add(u.Phone)
				}
			}
		case "caller":
			add(incident.ReporterPhone)
		case "previous_assignee":
			for _, uid := range incident.PreviousAssigneeIDs {
				if u, err := e.userRepo.FindByID(ctx, uid); err == nil {
					if !strings.HasPrefix(u.Username, "citizen_") {
						add(u.Phone)
					}
				}
			}
		case "custom":
			for _, p := range customPhones {
				add(strings.TrimSpace(p))
			}
		}
	}

	return phones
}


// replacePlaceholders replaces template placeholders with actual values
func (e *actionExecutor) replacePlaceholders(template string, incident *models.Incident, transition *models.WorkflowTransition, performedBy *models.User) string {
	replacements := map[string]string{
		"{{incident_number}}": incident.IncidentNumber,
		"{{incident_title}}":  incident.Title,
		"{{incident_id}}":     incident.ID.String(),
		"{{description}}":    incident.Description,
		"{{record_type}}":    incident.RecordType,
		"{{source}}":         incident.Source,
		"{{channel}}":        incident.Channel,
		"{{reporter_name}}":  incident.ReporterName,
		"{{reporter_email}}": incident.ReporterEmail,
		"{{reporter_phone}}": incident.ReporterPhone,
		"{{created_by_name}}": incident.CreatedByName,
		"{{sla_breached}}":   fmt.Sprintf("%t", incident.SLABreached),
		"{{address}}":        incident.Address,
		"{{city}}":           incident.City,
		"{{country}}":        incident.Country,
	}

	if incident.DueDate != nil {
		replacements["{{due_date}}"] = incident.DueDate.Format("2006-01-02 15:04:05")
	}
	if incident.SLADeadline != nil {
		replacements["{{sla_deadline}}"] = incident.SLADeadline.Format("2006-01-02 15:04:05")
	}
	replacements["{{created_at}}"] = incident.CreatedAt.Format("2006-01-02 15:04:05")

	if incident.Classification != nil {
		replacements["{{classification_name}}"] = incident.Classification.Name
	}
	if incident.Workflow != nil {
		replacements["{{workflow_name}}"] = incident.Workflow.Name
	}

	priority := "N/A"
	for _, lv := range incident.LookupValues {
		if lv.Category != nil && lv.Category.Code == "PRIORITY" {
			priority = lv.Name
		}
	}
	replacements["{{priority}}"] = priority

	if transition != nil {
		replacements["{{transition_name}}"] = transition.Name
		if transition.FromState != nil {
			replacements["{{from_state}}"] = transition.FromState.Name
		}
		if transition.ToState != nil {
			replacements["{{to_state}}"] = transition.ToState.Name
		}
	}

	if performedBy != nil {
		name := performedBy.Username
		if performedBy.FirstName != "" {
			name = performedBy.FirstName + " " + performedBy.LastName
		}
		replacements["{{performed_by}}"] = name
	}

	if incident.Assignee != nil {
		name := incident.Assignee.Username
		if incident.Assignee.FirstName != "" {
			name = incident.Assignee.FirstName + " " + incident.Assignee.LastName
		}
		replacements["{{assignee}}"] = name
		replacements["{{assignee_email}}"] = incident.Assignee.Email
		replacements["{{assignee_phone}}"] = incident.Assignee.Phone
	} else {
		replacements["{{assignee}}"] = "Unassigned"
	}

	if incident.CurrentState != nil {
		replacements["{{current_state}}"] = incident.CurrentState.Name
	}

	result := template
	for placeholder, value := range replacements {
		result = strings.ReplaceAll(result, placeholder, value)
	}
	return result
}

// buildIncidentDetails appends a plain-text summary of the incident to an email body
func (e *actionExecutor) buildIncidentDetails(incident *models.Incident) string {
	var b strings.Builder
	b.WriteString("\n\n--- Incident Details ---")
	b.WriteString("\nNumber: " + incident.IncidentNumber)
	b.WriteString("\nTitle: " + incident.Title)
	if incident.CurrentState != nil {
		b.WriteString("\nStatus: " + incident.CurrentState.Name)
	}
	if incident.Assignee != nil {
		name := incident.Assignee.Username
		if incident.Assignee.FirstName != "" {
			name = incident.Assignee.FirstName + " " + incident.Assignee.LastName
		}
		b.WriteString("\nAssignee: " + name)
	}
	return b.String()
}

// extractPreviousAssigneeID finds the assignee_id stored in OldValues of the most recent
// transition history entry that recorded an assignee change.
// TransitionHistory is ordered DESC (newest first), so we scan from index 0.
func extractPreviousAssigneeID(history []models.IncidentTransitionHistory) uuid.UUID {
	for _, h := range history {
		if h.OldValues == "" {
			continue
		}
		var old map[string]interface{}
		if err := json.Unmarshal([]byte(h.OldValues), &old); err != nil {
			continue
		}
		if raw, ok := old["assignee_id"]; ok && raw != nil {
			if idStr, ok := raw.(string); ok && idStr != "" {
				if uid, err := uuid.Parse(idStr); err == nil {
					return uid
				}
			}
		}
	}
	return uuid.Nil
}
