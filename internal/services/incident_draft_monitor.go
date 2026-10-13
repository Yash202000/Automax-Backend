package services

import (
	"context"
	"log"
	"time"

	"github.com/automax/backend/internal/config"
)

// IncidentDraftMonitor periodically hard-deletes expired drafts.
type IncidentDraftMonitor interface {
	Start(ctx context.Context)
	Stop()
}

type incidentDraftMonitor struct {
	service  IncidentDraftService
	cfg      config.IncidentDraftConfig
	stopChan chan struct{}
	running  bool
}

func NewIncidentDraftMonitor(service IncidentDraftService, cfg config.IncidentDraftConfig) IncidentDraftMonitor {
	return &incidentDraftMonitor{service: service, cfg: cfg, stopChan: make(chan struct{})}
}

func (m *incidentDraftMonitor) Start(ctx context.Context) {
	if m.running {
		return
	}
	if m.cfg.TTLDays <= 0 || m.cfg.PruneIntervalMinutes <= 0 {
		log.Println("Incident Draft Monitor disabled: INCIDENT_DRAFT_TTL_DAYS / INCIDENT_DRAFT_PRUNE_INTERVAL_MINUTES not positive")
		return
	}

	m.running = true
	interval := time.Duration(m.cfg.PruneIntervalMinutes) * time.Minute
	log.Printf("Incident Draft Monitor started: ttl=%dd interval=%v", m.cfg.TTLDays, interval)

	go func() {
		m.prune(ctx)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.prune(ctx)
			case <-m.stopChan:
				log.Println("Incident Draft Monitor stopped")
				return
			case <-ctx.Done():
				log.Println("Incident Draft Monitor context cancelled")
				return
			}
		}
	}()
}

// prune is idempotent, so running it on several replicas at once is harmless.
func (m *incidentDraftMonitor) prune(ctx context.Context) {
	n, err := m.service.PruneExpired(ctx)
	if err != nil {
		log.Printf("[IncidentDraftMonitor] prune failed: %v", err)
		return
	}
	if n > 0 {
		log.Printf("[IncidentDraftMonitor] pruned %d expired drafts", n)
	}
}

func (m *incidentDraftMonitor) Stop() {
	if !m.running {
		return
	}
	m.running = false
	close(m.stopChan)
}
