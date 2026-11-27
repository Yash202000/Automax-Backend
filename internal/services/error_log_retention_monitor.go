package services

import (
	"context"
	"log"
	"time"

	"github.com/automax/backend/internal/repository"
)

// ErrorLogRetentionMonitor periodically deletes error logs older than the retention
// period configured in System Settings.
type ErrorLogRetentionMonitor struct {
	service      *ErrorLogService
	settingsRepo repository.SettingsRepository
	interval     time.Duration
	stopChan     chan struct{}
}

func NewErrorLogRetentionMonitor(service *ErrorLogService, settingsRepo repository.SettingsRepository, interval time.Duration) *ErrorLogRetentionMonitor {
	return &ErrorLogRetentionMonitor{service: service, settingsRepo: settingsRepo, interval: interval, stopChan: make(chan struct{})}
}

func (m *ErrorLogRetentionMonitor) RunOnce(ctx context.Context) {
	settings, err := m.settingsRepo.GetOrCreate(ctx)
	if err != nil {
		log.Printf("[ErrorLogRetention] could not load settings: %v", err)
		return
	}
	deleted, err := m.service.PurgeExpired(ctx, settings)
	if err != nil {
		log.Printf("[ErrorLogRetention] purge failed: %v", err)
		return
	}
	if deleted > 0 {
		log.Printf("[ErrorLogRetention] deleted %d expired error log(s)", deleted)
	}
}

func (m *ErrorLogRetentionMonitor) Start(ctx context.Context) {
	log.Printf("Error Log Retention Monitor started: interval=%v", m.interval)
	go func() {
		m.RunOnce(ctx)
		ticker := time.NewTicker(m.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				m.RunOnce(ctx)
			case <-m.stopChan:
				return
			case <-ctx.Done():
				return
			}
		}
	}()
}

func (m *ErrorLogRetentionMonitor) Stop() {
	close(m.stopChan)
}
