package services

import (
	"context"
	"log"
	"time"

	"github.com/automax/backend/internal/config"
)

// AutoAssignMonitor periodically assigns unassigned incidents to online agents.
type AutoAssignMonitor interface {
	Start(ctx context.Context)
	Stop()
}

type autoAssignMonitor struct {
	incidentService IncidentService
	cfg             config.AutoAssignConfig
	stopChan        chan struct{}
	running         bool
}

// NewAutoAssignMonitor creates a new AutoAssignMonitor.
func NewAutoAssignMonitor(incidentService IncidentService, cfg config.AutoAssignConfig) AutoAssignMonitor {
	return &autoAssignMonitor{
		incidentService: incidentService,
		cfg:             cfg,
		stopChan:        make(chan struct{}),
	}
}

func (m *autoAssignMonitor) Start(ctx context.Context) {
	if m.running {
		return
	}

	if m.cfg.StateCode == "" {
		log.Println("Auto-Assign Monitor disabled: AUTO_ASSIGN_STATE_CODE not set")
		return
	}

	m.running = true
	interval := time.Duration(m.cfg.IntervalMinutes) * time.Minute
	log.Printf("Auto-Assign Monitor started: state_code=%q interval=%v", m.cfg.StateCode, interval)

	go func() {
		if err := m.incidentService.AutoAssignUnassigned(ctx); err != nil {
			log.Printf("[AutoAssignMonitor] Initial check failed: %v", err)
		}

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ticker.C:
				if err := m.incidentService.AutoAssignUnassigned(ctx); err != nil {
					log.Printf("[AutoAssignMonitor] Check failed: %v", err)
				}
			case <-m.stopChan:
				log.Println("Auto-Assign Monitor stopped")
				return
			case <-ctx.Done():
				log.Println("Auto-Assign Monitor context cancelled")
				return
			}
		}
	}()
}

func (m *autoAssignMonitor) Stop() {
	if !m.running {
		return
	}
	m.running = false
	close(m.stopChan)
}
