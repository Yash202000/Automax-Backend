package services

import (
	"context"
	"log"
	"time"

	"github.com/automax/backend/internal/repository"
)

// SLAMonitor handles background SLA breach detection
type SLAMonitor interface {
	Start(ctx context.Context)
	Stop()
	CheckSLABreaches(ctx context.Context) error
}

type slaMonitor struct {
	incidentRepo           repository.IncidentRepository
	escalationService      *EscalationService
	escalationGroupService *EscalationGroupService
	interval               time.Duration
	stopChan               chan struct{}
	running                bool
}

// NewSLAMonitor creates a new SLA monitor
func NewSLAMonitor(incidentRepo repository.IncidentRepository, escalationService *EscalationService, escalationGroupService *EscalationGroupService, checkInterval time.Duration) SLAMonitor {
	if checkInterval == 0 {
		checkInterval = 5 * time.Minute // Default to 5 minutes
	}
	return &slaMonitor{
		incidentRepo:           incidentRepo,
		escalationService:      escalationService,
		escalationGroupService: escalationGroupService,
		interval:               checkInterval,
		stopChan:               make(chan struct{}),
	}
}

// Start begins the background SLA monitoring
func (m *slaMonitor) Start(ctx context.Context) {
	if m.running {
		return
	}

	m.running = true
	log.Printf("SLA Monitor started with interval: %v", m.interval)

	go func() {
		// Initial check on startup
		if err := m.CheckSLABreaches(ctx); err != nil {
			log.Printf("Initial SLA check failed: %v", err)
		}

		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := m.CheckSLABreaches(ctx); err != nil {
					log.Printf("SLA check failed: %v", err)
				}
			case <-m.stopChan:
				log.Println("SLA Monitor stopped")
				return
			case <-ctx.Done():
				log.Println("SLA Monitor context cancelled")
				return
			}
		}
	}()
}

// Stop halts the background monitoring
func (m *slaMonitor) Stop() {
	if !m.running {
		return
	}

	m.running = false
	close(m.stopChan)
}

// CheckSLABreaches checks all incidents for SLA breaches and sends notifications. Runs five steps every interval:Few steps on hold for now can be used in next phase.....
//  1. Mark incidents whose global SLA deadline has passed (sla_breached = true).
//  2. Log current stats.
//  3. Send one-time global breach notifications (assignee + reporter) for incidents
//     newly marked as breached — stored with state_id = NULL in escalation_sla.
//  4. Send per-state SLA breach notifications to users responsible for the next
//     transition — stored with state_id set in escalation_sla.
//  5. Send custom escalation-group notifications (daily/weekly grouped summaries
//     per classification) to all configured escalation-group users.
func (m *slaMonitor) CheckSLABreaches(ctx context.Context) error {

	// Mark incidents whose global SLA deadline has passed
	breachedCount, err := m.incidentRepo.MarkSLABreached(ctx)
	if err != nil {
		return err
	}
	if breachedCount > 0 {
		log.Printf("Marked %d incident(s) as globally SLA breached", breachedCount)
	}

	// Log current stats
	stats, err := m.incidentRepo.GetStats(ctx, nil)
	if err != nil {
		log.Printf("Failed to get stats: %v", err)
	} else {
		log.Printf("SLA Status — Total: %d, Open: %d, In Progress: %d, Breached: %d",
			stats.Total, stats.Open, stats.InProgress, stats.SLABreached)
	}

	//  Send one-time notifications for incidents that just hit the global SLA deadline
	// if err := m.escalationService.ProcessGlobalSLABreaches(ctx); err != nil {
	// 	log.Printf("Global SLA breach notifications failed: %v", err)
	// }

	//Send per-state SLA breach notifications to users responsible for next transitions this features is on hold  (can use into next phase)
	// if err := m.escalationService.ProcessTransitionSLAAlerts(ctx); err != nil {
	// 	log.Printf("Transition SLA alert notifications failed: %v", err)
	// }

	// Send custom escalation-group grouped summaries (daily/weekly, per classification)
	if err := m.escalationGroupService.ProcessGroupEscalations(ctx); err != nil {
		log.Printf("Escalation group notifications failed: %v", err)
	}

	return nil
}
