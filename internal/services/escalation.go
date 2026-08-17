package services

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/google/uuid"
)

// EscalationService handles SLA breach detection and notification dispatch.
type EscalationService struct {
	repo                repository.EscalationRepository
	policyRepo          repository.EscalationPolicyRepository
	incidentRepo        repository.IncidentRepository
	workflowRepo        repository.WorkflowRepository
	userRepo            repository.UserRepository
	policyService       *EscalationPolicyService
	notificationService *NotificationService
	frontendURL         string
}

func NewEscalationService(
	repo repository.EscalationRepository,
	policyRepo repository.EscalationPolicyRepository,
	incidentRepo repository.IncidentRepository,
	workflowRepo repository.WorkflowRepository,
	userRepo repository.UserRepository,
	notificationService *NotificationService,
) *EscalationService {
	return &EscalationService{
		repo:                repo,
		policyRepo:          policyRepo,
		incidentRepo:        incidentRepo,
		workflowRepo:        workflowRepo,
		userRepo:            userRepo,
		notificationService: notificationService,
	}
}

// SetFrontendURL sets the base URL used to build incident deep-links in notification bodies.
func (s *EscalationService) SetFrontendURL(url string) {
	s.frontendURL = url
}

// SetPolicyService injects the shared EscalationPolicyService for user resolution.
func (s *EscalationService) SetPolicyService(ps *EscalationPolicyService) {
	s.policyService = ps
}

// GetBreachLogs returns all SLA breach notification records.
func (s *EscalationService) GetBreachLogs(ctx context.Context) ([]models.EscalationSLA, error) {
	return s.repo.GetAll(ctx)
}

// GetBreachLogsByIncident returns all SLA breach notifications for a specific incident.
func (s *EscalationService) GetBreachLogsByIncident(ctx context.Context, incidentID uuid.UUID) ([]models.EscalationSLA, error) {
	return s.repo.GetByIncidentID(ctx, incidentID)
}

// ProcessTransitionSLAAlerts is the main SLA breach notification engine.
func (s *EscalationService) ProcessTransitionSLAAlerts(ctx context.Context) error {
	log.Println("[EscalationService] Running transition SLA alert check ...")

	//Collect incidents that have exceeded their current state's SLA ---
	incidents, err := s.incidentRepo.
		GetIncidentsExceedingStateSLA(ctx)
	if err != nil {
		return fmt.Errorf("GetIncidentsExceedingStateSLA: %w", err)
	}

	if len(incidents) == 0 {
		log.Println("[EscalationService] No incidents exceeding state SLA — nothing to do.")
		return nil
	}

	log.Printf("[EscalationService] %d incident(s) have exceeded their current state SLA.", len(incidents))

	var processed, skippedNoPolicy, skippedOther int
	for _, incident := range incidents {
		if incident.CurrentState == nil {
			log.Printf("[EscalationService] Incident %s has no CurrentState loaded — skipping", incident.IncidentNumber)
			skippedOther++
			continue
		}
		state := incident.CurrentState
		if state.SLAHours == nil {
			log.Printf("[EscalationService] Incident %s / state '%s' has no SLAHours — skipping", incident.IncidentNumber, state.Name)
			skippedOther++
			continue
		}

		// Calculate actual hours spent in the current state
		hoursInState := s.hoursInCurrentState(ctx, incident)
		slaUnit := state.SLAUnit
		if slaUnit == "" {
			slaUnit = "hours"
		}
		// log.Printf("[EscalationService] Incident %s has been in state '%s' for %.2fh (SLA: %d %s)",
		// 	incident.IncidentNumber, state.Name, hoursInState, *state.SLAHours, slaUnit)

		// ── Policy-driven path ──────────────────────────────────────────────
		if state.EscalationPolicyID != nil {
			if s.policyService != nil {
				s.processPolicySteps(ctx, incident, state, hoursInState)
				processed++
			} else {
				log.Printf("[EscalationService] Incident %s / state '%s' has escalation_policy_id set but policyService is nil — skipping",
					incident.IncidentNumber, state.Name)
				skippedOther++
			}
			continue
		}

		// No escalation policy attached to this state — skip to avoid unintended notifications.
		// log.Printf("[EscalationService] Incident %s / state '%s' has no escalation policy attached — assign one to enable SLA notifications",
		// 	incident.IncidentNumber, state.Name)
		skippedNoPolicy++
		continue

		// ── Legacy path (disabled — states must have an escalation policy) ───
		// transitions, err := s.workflowRepo.ListTransitionsFromState(ctx, state.ID)
		// if err != nil {
		// 	log.Printf("[EscalationService] Failed to list transitions from state '%s': %v", state.Name, err)
		// 	continue
		// }

		// if len(transitions) == 0 {
		// 	log.Printf("[EscalationService] No outgoing transitions from state '%s' — nothing to notify", state.Name)
		// 	continue
		// }
		// log.Printf("[EscalationService] Found %d outgoing transition(s) from state '%s'", len(transitions), state.Name)

		// notifiedUsers := make(map[uuid.UUID]bool)

		// for _, transition := range transitions {
		// 	if len(transition.AllowedRoles) == 0 {
		// 		log.Printf("[EscalationService] Transition '%s' has no AllowedRoles — skipping targeted notify", transition.Name)
		// 		continue
		// 	}

		// 	roleIDs := make([]uuid.UUID, 0, len(transition.AllowedRoles))
		// 	for _, role := range transition.AllowedRoles {
		// 		roleIDs = append(roleIDs, role.ID)
		// 	}

		// 	users, err := s.userRepo.FindByRoleAndContext(
		// 		ctx, roleIDs,
		// 		incident.ClassificationID, incident.LocationID, incident.DepartmentID,
		// 	)
		// 	if err != nil {
		// 		log.Printf("[EscalationService] FindByRoleAndContext error for transition '%s': %v", transition.Name, err)
		// 		continue
		// 	}
		// 	if len(users) == 0 {
		// 		users, err = s.userRepo.FindByRoleAndContext(ctx, roleIDs, nil, nil, nil)
		// 		if err != nil {
		// 			log.Printf("[EscalationService] FindByRoleAndContext (role-only) error for transition '%s': %v", transition.Name, err)
		// 			continue
		// 		}
		// 		log.Printf("[EscalationService] Transition '%s': role-only fallback found %d user(s)", transition.Name, len(users))
		// 	}
		// 	if len(users) == 0 {
		// 		continue
		// 	}

		// 	transitionID := transition.ID
		// 	for _, user := range users {
		// 		if notifiedUsers[user.ID] {
		// 			continue
		// 		}
		// 		alreadyNotified, err := s.repo.HasBeenNotified(ctx, incident.ID, state.ID, user.ID)
		// 		if err != nil {
		// 			log.Printf("[EscalationService] HasBeenNotified check failed for user %s: %v", user.Email, err)
		// 			continue
		// 		}
		// 		if alreadyNotified {
		// 			notifiedUsers[user.ID] = true
		// 			continue
		// 		}

		// 		// Create breach log BEFORE sending so deduplication always works,
		// 		// even if the notification send fails.
		// 		now := time.Now()
		// 		breach := &models.EscalationSLA{
		// 			IncidentID:      &incident.ID,
		// 			StateID:         &state.ID,
		// 			TransitionID:    &transitionID,
		// 			NotifiedUserID:  &user.ID,
		// 			Email:           user.Email,
		// 			Phone:           user.Phone,
		// 			Actions:         models.TextArray{},
		// 			SLAHoursAllowed: int(state.SLAAsHours()),
		// 			HoursInState:    hoursInState,
		// 			EscalationType:  "state_sla",
		// 			NotifiedAt:      &now,
		// 		}
		// 		if err := s.repo.Create(ctx, breach); err != nil {
		// 			log.Printf("[EscalationService] Failed to persist breach log for user %s / incident %s: %v — skipping send",
		// 				user.Email, incident.IncidentNumber, err)
		// 			continue
		// 		}

		// 		sentChannels := s.sendNotifications(ctx, incident, state, transition.Name, user, hoursInState)
		// 		if sentChannels == nil {
		// 			sentChannels = []string{}
		// 		}

		// 		// Update breach log with actual channels sent
		// 		breach.Actions = sentChannels
		// 		if err := s.repo.Update(ctx, breach); err != nil {
		// 			log.Printf("[EscalationService] Failed to update breach log actions for user %s: %v", user.Email, err)
		// 		} else {
		// 			log.Printf("[EscalationService] Breach log stored — incident %s, state '%s', user %s, channels %v",
		// 				incident.IncidentNumber, state.Name, user.Email, sentChannels)
		// 		}
		// 		notifiedUsers[user.ID] = true
		// 	}
		// }
	}

	// log.Printf("[EscalationService] Done — processed: %d, skipped (no policy): %d, skipped (other): %d",
	// 	processed, skippedNoPolicy, skippedOther)
	return nil
}

// processPolicySteps fires the escalation policy steps for a state-SLA breach.
// Steps with DelayHours <= hoursInBreach that have not yet fired are executed.
func (s *EscalationService) processPolicySteps(
	ctx context.Context,
	incident models.Incident,
	state *models.WorkflowState,
	hoursInState float64,
) {
	policy, err := s.policyRepo.FindByID(ctx, *state.EscalationPolicyID)
	if err != nil {
		log.Printf("[EscalationService] Policy %s not found for state '%s': %v", state.EscalationPolicyID, state.Name, err)
		return
	}
	if !policy.IsActive {
		return
	}

	slaAsHours := state.SLAAsHours()
	hoursInBreach := hoursInState - slaAsHours

	for _, step := range policy.Steps {
		// Not yet time for this step
		if float64(step.DelayHours) > hoursInBreach {
			continue
		}

		// Deduplication: already fired for this incident+state+step
		fired, err := s.policyRepo.HasStepBeenFired(ctx, incident.ID, state.ID, step.ID)
		if err != nil {
			log.Printf("[EscalationService] HasStepBeenFired error step %s: %v", step.ID, err)
			continue
		}
		if fired {
			continue
		}

		// Resolve recipients
		users, err := s.policyService.ResolveStepTargetUsers(ctx, step.Targets)
		if err != nil {
			log.Printf("[EscalationService] ResolveStepTargetUsers error step %s: %v", step.ID, err)
			continue
		}
		if len(users) == 0 {
			log.Printf("[EscalationService] Policy step %d of '%s': no recipients resolved — skipping", step.StepOrder, policy.Name)
			continue
		}

		log.Printf("[EscalationService] Firing policy '%s' step %d for incident %s (breach +%.1fh)",
			policy.Name, step.StepOrder, incident.IncidentNumber, hoursInBreach)

		for _, user := range users {
			// Create the breach log BEFORE sending so deduplication always works,
			// even if the notification send fails.
			now := time.Now()
			stepID := step.ID
			breach := &models.EscalationSLA{
				IncidentID:             &incident.ID,
				StateID:                &state.ID,
				NotifiedUserID:         &user.ID,
				Email:                  user.Email,
				Phone:                  user.Phone,
				Actions:                models.TextArray{},
				SLAHoursAllowed:        int(slaAsHours),
				HoursInState:           hoursInState,
				EscalationPolicyID:     &policy.ID,
				EscalationPolicyStepID: &stepID,
				EscalationType:         "state_sla",
				NotifiedAt:             &now,
			}
			if err := s.repo.Create(ctx, breach); err != nil {
				log.Printf("[EscalationService] Failed to store policy breach log for user %s / incident %s: %v — skipping send",
					user.Email, incident.IncidentNumber, err)
				continue
			}

			sentChannels := s.sendPolicyNotification(ctx, incident, state, policy.Name, step, user, hoursInState)
			if sentChannels == nil {
				sentChannels = []string{}
			}

			// Update the breach log with which channels were actually used.
			breach.Actions = sentChannels
			if err := s.repo.Update(ctx, breach); err != nil {
				log.Printf("[EscalationService] Failed to update breach log actions for user %s: %v", user.Email, err)
			}
		}

		// Fire at most one new step per incident per monitor run. If multiple steps
		// are all eligible at once (e.g. a long-breached incident where delay_hours=0,1,4
		// are all due), firing them all simultaneously produces an email burst. Instead,
		// advance one step per cycle — the next monitor run will fire the following step.
		break
	}
}

// sendPolicyNotification dispatches email/SMS for a policy step breach.
func (s *EscalationService) sendPolicyNotification(
	ctx context.Context,
	incident models.Incident,
	state *models.WorkflowState,
	policyName string,
	step models.EscalationPolicyStep,
	user models.User,
	hoursInState float64,
) []string {
	sent := make([]string, 0)
	slaHours := 0
	if state.SLAHours != nil {
		slaHours = *state.SLAHours
	}

	incidentURL := fmt.Sprintf("%s/incidents/%s", s.frontendURL, incident.ID)

	// Fallback content used when the DB template does not exist yet.
	subject := fmt.Sprintf("SLA Alert [Step %d]: Incident %s exceeded SLA in state '%s'",
		step.StepOrder, incident.IncidentNumber, state.Name)
	emailBody := fmt.Sprintf(
		"Dear %s %s,\n\nEscalation policy \"%s\" (step %d) triggered.\n\n"+
			"Incident %s - \"%s\" in state \"%s\" for %.1f hours (SLA: %d h, breach +%.1fh).\n\nView: %s",
		user.FirstName, user.LastName, policyName, step.StepOrder,
		incident.IncidentNumber, incident.Title, state.Name,
		hoursInState, slaHours, hoursInState-float64(slaHours), incidentURL,
	)
	smsBody := fmt.Sprintf(
		"SLA ALERT [Step %d] — %s: Incident %s in '%s' for %.1fh (limit %dh). View: %s",
		step.StepOrder, policyName, incident.IncidentNumber, state.Name, hoursInState, slaHours, incidentURL,
	)

	// Start from the full incident variable set, then override/add policy-step-specific values.
	vars := BuildIncidentVariables(&incident, nil, nil)
	vars["first_name"] = user.FirstName
	vars["last_name"] = user.LastName
	vars["state_name"] = state.Name
	vars["current_state"] = state.Name
	vars["hours_in_state"] = fmt.Sprintf("%.1f", hoursInState)
	vars["sla_hours"] = fmt.Sprintf("%d", slaHours)
	vars["policy_name"] = policyName
	vars["step_order"] = fmt.Sprintf("%d", step.StepOrder)
	vars["hours_in_breach"] = fmt.Sprintf("%.1f", hoursInState-float64(slaHours))
	vars["incident_url"] = incidentURL
	vars["sla_page_url"] = fmt.Sprintf("%s/incidents?sla_breached=true", s.frontendURL)

	// Only look up a DB template when the step has one explicitly configured.
	// nil = use the hardcoded subject/body above directly.
	var emailCodePtr *string
	if step.EmailTemplateCode != "" {
		emailCodePtr = &step.EmailTemplateCode
	}
	var smsCodePtr *string
	if step.SMSTemplateCode != "" {
		smsCodePtr = &step.SMSTemplateCode
	}

	sendEmail := step.Channel == "email" || step.Channel == "both"
	sendSMS := step.Channel == "sms" || step.Channel == "both"

	if sendEmail && user.Email != "" {
		if result, err := s.notificationService.SendNotification(ctx, "email", emailCodePtr, "en",
			[]string{user.Email}, nil, nil, subject, emailBody, vars, nil, &user.ID, nil,
		); err != nil {
			log.Printf("[EscalationService] Policy EMAIL failed for %s: %v", user.Email, err)
			if result != nil && result.SentLog != nil {
				_ = s.notificationService.SetIncidentIDOnLogs(ctx, []uuid.UUID{result.SentLog.ID}, incident.ID)
			}
		} else {
			sent = append(sent, "EMAIL")
			if result != nil && result.SentLog != nil {
				_ = s.notificationService.SetIncidentIDOnLogs(ctx, []uuid.UUID{result.SentLog.ID}, incident.ID)
			}
		}
	}

	if sendSMS && user.Phone != "" {
		if result, err := s.notificationService.SendNotification(ctx, "sms", smsCodePtr, "en",
			[]string{user.Phone}, nil, nil, "", smsBody, vars, nil, &user.ID, nil,
		); err != nil {
			log.Printf("[EscalationService] Policy SMS failed for %s: %v", user.Phone, err)
			if result != nil && result.SentLog != nil {
				_ = s.notificationService.SetIncidentIDOnLogs(ctx, []uuid.UUID{result.SentLog.ID}, incident.ID)
			}
		} else {
			sent = append(sent, "SMS")
			if result != nil && result.SentLog != nil {
				_ = s.notificationService.SetIncidentIDOnLogs(ctx, []uuid.UUID{result.SentLog.ID}, incident.ID)
			}
		}
	}

	return sent
}

// sendNotifications dispatches EMAIL and SMS for one SLA breach and returns the list of

func (s *EscalationService) sendNotifications(
	ctx context.Context,
	incident models.Incident,
	state *models.WorkflowState,
	transitionName string,
	user models.User,
	hoursInState float64,
) []string {
	sent := make([]string, 0)

	incidentURL := fmt.Sprintf("%s/incidents/%s", s.frontendURL, incident.ID)

	vars := BuildIncidentVariables(&incident, nil, nil)
	vars["first_name"] = user.FirstName
	vars["last_name"] = user.LastName
	vars["state_name"] = state.Name
	vars["current_state"] = state.Name
	vars["hours_in_state"] = fmt.Sprintf("%.1f", hoursInState)
	vars["sla_hours"] = fmt.Sprintf("%d", *state.SLAHours)
	vars["transition_name"] = transitionName
	vars["incident_url"] = incidentURL
	vars["sla_page_url"] = fmt.Sprintf("%s/incidents?sla_breached=true", s.frontendURL)
	vars["hours_in_breach"] = fmt.Sprintf("%.1f", hoursInState-float64(*state.SLAHours))

	// Templates configured from admin UI (action_type = "escalation") — no hardcoded codes.
	if user.Email != "" {
		if err := s.notificationService.SendByActionTypeForIncident(
			ctx, models.TemplateActionEscalation, "email", "ar",
			[]string{user.Email}, vars, nil, incident.ID,
		); err != nil {
			log.Printf("[EscalationService] EMAIL failed for %s: %v", user.Email, err)
		} else {
			sent = append(sent, "EMAIL")
		}
	}

	if user.Phone != "" {
		if err := s.notificationService.SendByActionTypeForIncident(
			ctx, models.TemplateActionEscalation, "sms", "ar",
			[]string{user.Phone}, vars, nil, incident.ID,
		); err != nil {
			log.Printf("[EscalationService] SMS failed for %s: %v", user.Phone, err)
		} else {
			sent = append(sent, "SMS")
		}
	}

	return sent
}

// ProcessGlobalSLABreaches fires once per incident when its global SLA deadline is crossed
// (sla_breached = true).  It notifies the primary assignee and reporter (if different) via
// EMAIL and/or SMS, then stores an escalation_sla record with state_id = NULL so the same
// incident is never notified twice.
func (s *EscalationService) ProcessGlobalSLABreaches(ctx context.Context) error {
	log.Println("[EscalationService] Running global SLA breach notification check ...")

	incidents, err := s.incidentRepo.GetNewlyBreachedIncidents(ctx)
	if err != nil {
		return fmt.Errorf("GetNewlyBreachedIncidents: %w", err)
	}

	if len(incidents) == 0 {
		log.Println("[EscalationService] No newly globally-breached incidents — nothing to do.")
		return nil
	}

	log.Printf("[EscalationService] %d incident(s) newly globally SLA breached.", len(incidents))

	for _, incident := range incidents {
		// Build a deduplicated set of system users to notify (assignee + reporter)
		uniqueUsers := make(map[uuid.UUID]models.User)
		if incident.Assignee != nil {
			uniqueUsers[incident.Assignee.ID] = *incident.Assignee
		}
		if incident.Reporter != nil {
			if _, exists := uniqueUsers[incident.Reporter.ID]; !exists {
				uniqueUsers[incident.Reporter.ID] = *incident.Reporter
			}
		}

		if len(uniqueUsers) == 0 {
			log.Printf("[EscalationService] Incident %s: no system users (assignee/reporter) for global SLA breach notification", incident.IncidentNumber)
			continue
		}

		// Calculate SLA context values for the breach record
		totalHoursOpen := time.Since(incident.CreatedAt).Hours()
		slaHoursAllowed := 0
		if incident.SLADeadline != nil && !incident.SLADeadline.IsZero() {
			slaHoursAllowed = int(incident.SLADeadline.Sub(incident.CreatedAt).Hours())
		}

		for _, user := range uniqueUsers {
			// Deduplication — skip if already notified for the global breach of this incident
			already, err := s.repo.HasGlobalBreachNotification(ctx, incident.ID, user.ID)
			if err != nil {
				log.Printf("[EscalationService] HasGlobalBreachNotification error for user %s: %v", user.Email, err)
				continue
			}
			if already {
				log.Printf("[EscalationService] Global breach already notified for user %s / incident %s — skipping", user.Email, incident.IncidentNumber)
				continue
			}

			// Send EMAIL / SMS (best-effort; failures are logged, not fatal)
			sentChannels := s.sendGlobalBreachNotification(ctx, incident, user, totalHoursOpen, slaHoursAllowed)
			actions := sentChannels
			if actions == nil {
				actions = []string{}
			}
			now := time.Now()
			breach := &models.EscalationSLA{
				IncidentID:      &incident.ID,
				StateID:         nil,
				TransitionID:    nil,
				NotifiedUserID:  &user.ID,
				Email:           user.Email,
				Phone:           user.Phone,
				Actions:         actions,
				SLAHoursAllowed: slaHoursAllowed,
				HoursInState:    totalHoursOpen,
				NotifiedAt:      &now,
			}
			if err := s.repo.Create(ctx, breach); err != nil {
				log.Printf("[EscalationService] Failed to store global breach log for user %s / incident %s: %v",
					user.Email, incident.IncidentNumber, err)
			} else {
				log.Printf("[EscalationService] Global breach log stored — incident %s, user %s, channels %v, id %s",
					incident.IncidentNumber, user.Email, sentChannels, breach.ID)
			}
		}
	}

	return nil
}

// sendGlobalBreachNotification dispatches EMAIL and/or SMS for a global SLA deadline breach.
func (s *EscalationService) sendGlobalBreachNotification(
	ctx context.Context,
	incident models.Incident,
	user models.User,
	totalHoursOpen float64,
	slaHoursAllowed int,
) []string {
	sent := make([]string, 0)

	incidentURL := fmt.Sprintf("%s/incidents/%s", s.frontendURL, incident.ID)

	stateName := "Unknown"
	if incident.CurrentState != nil {
		stateName = incident.CurrentState.Name
	}

	subject := fmt.Sprintf(
		"SLA Deadline Breached: Incident %s has exceeded its SLA deadline",
		incident.IncidentNumber,
	)

	emailBody := fmt.Sprintf(
		"Dear %s %s,\n\n"+
			"This is an automated SLA deadline breach notification.\n\n"+
			"Incident %s - \"%s\" has exceeded its SLA deadline. "+
			"The incident has been open for %f hours against an allowed SLA of %d hours.\n\n"+
			"Incident Details:\n"+
			"  Number       : %s\n"+
			"  Title        : %s\n"+
			"  Current State: %s\n"+
			"View Incident: %s\n\n"+
			"Please log in to the system and take immediate action to resolve this incident.",
		user.FirstName, user.LastName,
		incident.IncidentNumber, incident.Title,
		totalHoursOpen, slaHoursAllowed,
		incident.IncidentNumber,
		incident.Title,
		stateName,
		// // totalHoursOpen,
		// slaHoursAllowed,
		incidentURL,
	)

	smsBody := fmt.Sprintf(
		"SLA BREACHED: Incident %s (\"%s\") has been open %.1fh (SLA: %dh). Immediate action required. View: %s",
		incident.IncidentNumber,
		incident.Title,
		totalHoursOpen,
		slaHoursAllowed,
		incidentURL,
	)

	// Start from the full incident variable set, then override/add global-breach-specific values.
	vars := BuildIncidentVariables(&incident, nil, nil)
	vars["first_name"] = user.FirstName
	vars["last_name"] = user.LastName
	vars["state_name"] = stateName
	vars["current_state"] = stateName
	vars["hours_open"] = fmt.Sprintf("%.1f", totalHoursOpen)
	vars["hours_in_state"] = fmt.Sprintf("%.1f", totalHoursOpen)
	vars["sla_hours"] = fmt.Sprintf("%d", slaHoursAllowed)
	vars["incident_url"] = incidentURL
	vars["sla_page_url"] = fmt.Sprintf("%s/incidents?sla_breached=true", s.frontendURL)
	vars["hours_in_breach"] = fmt.Sprintf("%.1f", totalHoursOpen-float64(slaHoursAllowed))

	emailCode := "SLA_GLOBAL_BREACH"
	smsCode := "SLA_GLOBAL_BREACH_SMS"

	if user.Email != "" {
		result, err := s.notificationService.SendNotification(
			ctx, "email", &emailCode, "en",
			[]string{user.Email}, nil, nil,
			subject, emailBody,
			vars, nil, &user.ID, nil,
		)
		if result != nil && result.SentLog != nil {
			_ = s.notificationService.SetIncidentIDOnLogs(ctx, []uuid.UUID{result.SentLog.ID}, incident.ID)
		}
		if err != nil {
			log.Printf("[EscalationService] Global breach EMAIL failed for %s: %v", user.Email, err)
		} else {
			sent = append(sent, "EMAIL")
		}
	}

	if user.Phone != "" {
		result, err := s.notificationService.SendNotification(
			ctx, "sms", &smsCode, "en",
			[]string{user.Phone}, nil, nil,
			"", smsBody,
			vars, nil, &user.ID, nil,
		)
		if result != nil && result.SentLog != nil {
			_ = s.notificationService.SetIncidentIDOnLogs(ctx, []uuid.UUID{result.SentLog.ID}, incident.ID)
		}
		if err != nil {
			log.Printf("[EscalationService] Global breach SMS failed for %s: %v", user.Phone, err)
		} else {
			sent = append(sent, "SMS")
		}
	}

	return sent
}

// hoursInCurrentState calculates how many hours the incident has been in its current state.
// It queries the transition history; if no history exists, it falls back to created_at.
func (s *EscalationService) hoursInCurrentState(ctx context.Context, incident models.Incident) float64 {
	history, err := s.incidentRepo.GetTransitionHistory(ctx, incident.ID)
	if err != nil || len(history) == 0 {
		return time.Since(incident.CreatedAt).Hours()
	}

	// Find the most recent entry whose ToState matches the current state
	var latestEntry *time.Time
	for i := range history {
		h := &history[i]
		if h.ToStateID == incident.CurrentStateID {
			if latestEntry == nil || h.TransitionedAt.After(*latestEntry) {
				latestEntry = &h.TransitionedAt
			}
		}
	}

	if latestEntry == nil {
		return time.Since(incident.CreatedAt).Hours()
	}

	return time.Since(*latestEntry).Hours()
}
