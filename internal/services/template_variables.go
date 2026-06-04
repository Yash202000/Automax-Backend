package services

import (
	"fmt"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	pkgutils "github.com/automax/backend/pkg/utils"
)

// BuildIncidentVariables builds the complete map of template variables for an incident.
//
// This is the SINGLE SOURCE OF TRUTH for template variable mapping.
// Add new variables here — they automatically become available in every
// notification path (transitions, escalations, new-incident, group alerts).
//
// Callers that have additional context-specific values (e.g. hours_in_state,
// policy_name for escalation steps, or incident_count for group alerts) should
// merge those on top of the returned map.
//
// Variable semantics:
//   - first_name / last_name : the performedBy user (person who triggered the action).
//     Escalation services should override these with the notified user's name.
//   - state_name             : alias for current_state.
//   - hours_in_state         : calculated from the latest TransitionHistory entry if loaded;
//     escalation callers override with their own computed value.
func BuildIncidentVariables(
	incident *models.Incident,
	transition *models.WorkflowTransition,
	performedBy *models.User,
) map[string]string {
	vars := map[string]string{
		// Core incident fields
		"incident_number": incident.IncidentNumber,
		"incident_title":  incident.Title,
		"incident_id":     incident.ID.String(),
		"description":     incident.Description,
		"record_type":     incident.RecordType,
		"source":          incident.Source,
		"channel":         incident.Channel,
		// Reporter plain fields (overridden below if Reporter relation is loaded)
		"reporter_name":  incident.ReporterName,
		"reporter_email": incident.ReporterEmail,
		"reporter_phone": incident.ReporterPhone,
		// Creator / location
		"created_by_name": incident.CreatedByName,
		"address":         incident.Address,
		"city":            incident.City,
		"country":         incident.Country,
		// Geolocation
		"latitude":     "",
		"longitude":    "",
		"map_url":      "",
		"location_url": "",
		"map_link":     "",
		// Relation fields — overridden below when relations are loaded; empty string ensures
		// RenderTemplate substitutes them (even if empty) rather than leaving {{placeholder}} literal.
		"classification_name": "",
		"workflow_name":       "",
		"location_name":       "",
		"current_state":       "",
		"assignee":            "",
		"assignee_email":      "",
		"assignee_phone":      "",
		"reporter":            "",
		"performed_by":        "",
		"first_name":          "",
		"last_name":           "",
		"transition_name":     "",
		"from_state":          "",
		"to_state":            "",
		"priority":            "",
		"due_date":            "",
		"sla_deadline":        "",
		// Comment aliases
		"comment":            "",
		"comments":           "",
		"transition_comment": "",
		// SLA
		"sla_breached": fmt.Sprintf("%t", incident.SLABreached),
		// Citizen-facing link (SMS_PORTAL_URL/incidents/{id}, falls back to FRONTEND_URL)
		"sms_link": "",
		// Citizen feedback link (SMS_PORTAL_URL/feedback/{id}?signed_token=...)
		"feedback_url":  "",
		"feedback_link": "",
		"sla_page_link": "",
		// Escalation-specific — empty by default; callers override
		"hours_in_state":    "",
		"sla_hours":         "",
		"state_name":        "",
		"hours_in_breach":   "",
		"policy_name":       "",
		"step_order":        "",
		"incident_count":    "",
		"incidents_summary": "",
		"report_date":       "",
	}

	// Build URL variables.
	// SMS_PORTAL_URL is a citizen-facing base URL (e.g. a public portal).
	// Falls back to FRONTEND_URL if not set.
	// Escalation callers override incident_url with their own pre-built value.
	frontendBase := strings.TrimRight(os.Getenv("FRONTEND_URL"), "/")
	smsPortalBase := strings.TrimRight(os.Getenv("SMS_PORTAL_URL"), "/")
	if smsPortalBase == "" {
		smsPortalBase = frontendBase
	}
	if frontendBase != "" {
		vars["incident_url"] = frontendBase + "/incidents/" + incident.ID.String()
		vars["sla_page_url"] = frontendBase + "/incidents?sla_breached=true"
		vars["sla_page_link"] = fmt.Sprintf(`<a href="%s">SLA Breached Incidents</a>`, vars["sla_page_url"])
	} else {
		vars["incident_url"] = ""
		vars["sla_page_url"] = ""
		vars["sla_page_link"] = ""
	}
	if smsPortalBase != "" {
		token := pkgutils.GenerateIncidentToken(incident.ID.String(), 24*time.Hour)
		vars["sms_link"] = fmt.Sprintf("%s/ivr/incident/sms-link/%s?signed_token=%s",
			smsPortalBase, incident.ID.String(), url.QueryEscape(token))
		log.Printf(" incident.FeedbackID  before not  nil case incidentID=%s feedbackID=%s ", incident.ID, incident.FeedbackID)
		if incident.FeedbackID != nil {
			log.Printf("incident.FeedbackID  inside  not nil case")
			vars["feedback_url"] = fmt.Sprintf("%s/feedback/%s?signed_token=%s",
				smsPortalBase, incident.ID.String(), incident.FeedbackID.String())
			log.Printf("[template_vars] incidentID=%s feedbackID=%s feedback_url=%s", incident.ID, incident.FeedbackID, vars["feedback_url"])
		} else {
			feedbackToken := pkgutils.GenerateIncidentToken(incident.ID.String(), feedbackTokenDuration)
			vars["feedback_url"] = fmt.Sprintf("%s/feedback/%s?signed_token=%s",
				smsPortalBase, incident.ID.String(), feedbackToken)
			log.Printf("[template_vars] incidentID=%s feedbackID=nil feedback_url=%s", incident.ID, vars["feedback_url"])
		}
		vars["feedback_link"] = fmt.Sprintf(`<a href="%s">تقييم الخدمة</a>`, vars["feedback_url"])
	} else {
		vars["sms_link"] = ""
		vars["feedback_url"] = ""
		vars["feedback_link"] = ""
	}

	// Dates
	vars["created_at"] = incident.CreatedAt.Format("2006-01-02 15:04:05")
	if incident.DueDate != nil {
		vars["due_date"] = incident.DueDate.Format("2006-01-02 15:04:05")
	} else {
		vars["due_date"] = ""
	}
	if incident.SLADeadline != nil {
		vars["sla_deadline"] = incident.SLADeadline.Format("2006-01-02 15:04:05")
	} else {
		vars["sla_deadline"] = ""
	}

	// Geolocation — build map URL when lat/lng are present
	if incident.Latitude != nil && incident.Longitude != nil {
		vars["latitude"] = fmt.Sprintf("%f", *incident.Latitude)
		vars["longitude"] = fmt.Sprintf("%f", *incident.Longitude)
		mapURL := fmt.Sprintf("http://maps.google.com/?q=%f,%f", *incident.Latitude, *incident.Longitude)
		vars["map_url"] = mapURL
		vars["location_url"] = mapURL
		vars["map_link"] = fmt.Sprintf(`<a href="%s">View on Map</a>`, mapURL)
	}

	// Classification
	if incident.Classification != nil {
		vars["classification_name"] = incident.Classification.Name
	}

	// Workflow
	if incident.Workflow != nil {
		vars["workflow_name"] = incident.Workflow.Name
	}

	// Location
	if incident.Location != nil {
		vars["location_name"] = incident.Location.Name
	}

	// Current state + SLA hours for this state
	if incident.CurrentState != nil {
		vars["current_state"] = incident.CurrentState.Name
		vars["state_name"] = incident.CurrentState.Name
		if incident.CurrentState.SLAHours != nil {
			vars["sla_hours"] = fmt.Sprintf("%d", *incident.CurrentState.SLAHours)
		}
	}

	// Assignee
	if incident.Assignee != nil {
		aname := incident.Assignee.Username
		if incident.Assignee.FirstName != "" {
			aname = incident.Assignee.FirstName + " " + incident.Assignee.LastName
		}
		vars["assignee"] = aname
		vars["assignee_email"] = incident.Assignee.Email
		vars["assignee_phone"] = incident.Assignee.Phone
	} else {
		vars["assignee"] = "Unassigned"
		vars["assignee_email"] = ""
		vars["assignee_phone"] = ""
	}

	// Reporter relation (overrides plain string fields where relation has richer data)
	if incident.Reporter != nil {
		rname := incident.Reporter.Username
		if incident.Reporter.FirstName != "" {
			rname = incident.Reporter.FirstName + " " + incident.Reporter.LastName
		}
		vars["reporter"] = rname
		if vars["reporter_name"] == "" {
			vars["reporter_name"] = rname
		}
		if vars["reporter_email"] == "" {
			vars["reporter_email"] = incident.Reporter.Email
		}
		if vars["reporter_phone"] == "" {
			vars["reporter_phone"] = incident.Reporter.Phone
		}
	}

	// Transition fields
	if transition != nil {
		vars["transition_name"] = transition.Name
		if transition.FromState != nil {
			vars["from_state"] = transition.FromState.Name
		}
		if transition.ToState != nil {
			vars["to_state"] = transition.ToState.Name
		}
	}

	// Performed-by user (first_name / last_name refer to this person)
	if performedBy != nil {
		name := performedBy.Username
		if performedBy.FirstName != "" {
			name = performedBy.FirstName + " " + performedBy.LastName
		}
		vars["performed_by"] = name
		vars["first_name"] = performedBy.FirstName
		vars["last_name"] = performedBy.LastName
	}

	// Priority from lookup values
	for _, lv := range incident.LookupValues {
		if lv.Category != nil && lv.Category.Code == "PRIORITY" {
			vars["priority"] = lv.Name
			break
		}
	}

	// Latest transition comment + hours_in_state (requires TransitionHistory to be preloaded)
	if len(incident.TransitionHistory) > 0 {
		latest := incident.TransitionHistory[len(incident.TransitionHistory)-1]
		vars["comments"] = latest.Comment
		vars["comment"] = latest.Comment
		vars["transition_comment"] = latest.Comment
		hoursInState := time.Since(latest.TransitionedAt).Hours()
		vars["hours_in_state"] = fmt.Sprintf("%.1f", hoursInState)
	}

	return vars
}
