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
	SubjectTemplate        string   `json:"subject_template"`
	BodyTemplate           string   `json:"body_template"`
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
		log.Printf("NotificationService not available; skipping email action %q", action.Name)
		return nil
	}

	emails := e.resolveRecipientEmails(ctx, config.Recipients, config.CustomEmails, incident)
	if len(emails) == 0 {
		return nil
	}

	subject := e.replacePlaceholders(config.SubjectTemplate, incident, transition, performedBy)
	body := e.replacePlaceholders(config.BodyTemplate, incident, transition, performedBy)

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

	_, err := e.notificationService.SendNotification(
		ctx, "email", nil, "en",
		emails, nil, nil,
		subject, body,
		nil, nil, nil, nil,
	)
	if err != nil {
		return fmt.Errorf("email send failed: %w", err)
	}
	return nil
}

// SmsConfig represents the configuration for an SMS action
type SmsConfig struct {
	Recipients     []string `json:"recipients"`      // "assignee", "reporter", "creator"
	CustomPhones   []string `json:"custom_phones"`   // explicit phone numbers
	MessageTemplate string  `json:"message_template"`
}

// executeSms sends SMS notifications via the notification service
func (e *actionExecutor) executeSms(ctx context.Context, action *models.TransitionAction, incident *models.Incident, transition *models.WorkflowTransition, performedBy *models.User) error {
	var config SmsConfig
	if err := json.Unmarshal([]byte(action.Config), &config); err != nil {
		return fmt.Errorf("invalid sms config: %w", err)
	}

	if e.notificationService == nil {
		log.Printf("NotificationService not available; skipping SMS action %q", action.Name)
		return nil
	}

	phones := e.resolveRecipientPhones(incident, config.Recipients, config.CustomPhones)
	if len(phones) == 0 {
		return nil
	}

	message := e.replacePlaceholders(config.MessageTemplate, incident, transition, performedBy)

	_, err := e.notificationService.SendNotification(
		ctx, "sms", nil, "en",
		phones, nil, nil,
		"", message,
		nil, nil, nil, nil,
	)
	if err != nil {
		return fmt.Errorf("sms send failed: %w", err)
	}
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
			if incident.Assignee != nil {
				add(incident.Assignee.Email)
			}
		case "reporter":
			if incident.Reporter != nil {
				add(incident.Reporter.Email)
			} else {
				add(incident.ReporterEmail)
			}
		case "creator":
			// Incident has no CreatedBy relation; creator email is not directly stored
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
func (e *actionExecutor) resolveRecipientPhones(incident *models.Incident, recipients []string, customPhones []string) []string {
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
			if incident.Assignee != nil {
				add(incident.Assignee.Phone)
			}
		case "reporter":
			if incident.Reporter != nil {
				add(incident.Reporter.Phone)
			}
		case "creator":
			add(incident.CreatedByMobile)
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
