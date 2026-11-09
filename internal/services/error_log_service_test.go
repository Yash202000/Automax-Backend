package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
)

type captureErrorLogRepo struct{ created chan *models.ErrorLog }

func (f *captureErrorLogRepo) Create(_ context.Context, e *models.ErrorLog) error {
	f.created <- e
	return nil
}
func (f *captureErrorLogRepo) List(context.Context, models.ErrorLogFilter) ([]models.ErrorLog, int64, error) {
	return nil, 0, nil
}
func (f *captureErrorLogRepo) FindByID(context.Context, uuid.UUID) (*models.ErrorLog, error) {
	return nil, nil
}
func (f *captureErrorLogRepo) ListForExport(context.Context, models.ErrorLogFilter, int) ([]models.ErrorLog, error) {
	return nil, nil
}
func (f *captureErrorLogRepo) UpdateStatus(context.Context, uuid.UUID, string, string, *uuid.UUID, string) error {
	return nil
}
func (f *captureErrorLogRepo) DeleteOlderThan(context.Context, time.Time) (int64, error) {
	return 0, nil
}

func TestLogIntegrationFailureTypes(t *testing.T) {
	repo := &captureErrorLogRepo{created: make(chan *models.ErrorLog, 1)}
	DefaultErrorLog = NewErrorLogService(repo)
	defer func() { DefaultErrorLog = nil }()

	cases := []struct {
		err      error
		status   int
		wantType string
	}{
		{errors.New("request failed: Get \"http://x\": context deadline exceeded (Client.Timeout exceeded)"), 0, models.ErrorTypeTimeout},
		{errors.New("request failed: dial tcp: connection refused"), 0, models.ErrorTypeConnectionFailed},
		{errors.New("external API returned 502: bad gateway"), 502, models.ErrorTypeInvalidResponse},
		{errors.New("unknown script_type: x"), 0, models.ErrorTypeUnexpected},
	}
	for _, tc := range cases {
		LogIntegrationFailure("Sync", uuid.NewString(), uuid.New(), "INC-1", tc.status, tc.err)
		select {
		case got := <-repo.created:
			if got.ErrorType != tc.wantType || got.Module != models.ErrorModuleIntegration {
				t.Errorf("err=%q: got type=%q module=%q, want type=%q", tc.err, got.ErrorType, got.Module, tc.wantType)
			}
		case <-time.After(time.Second):
			t.Fatalf("no log for %q", tc.err)
		}
	}
}

func TestLogAssignmentFailure(t *testing.T) {
	repo := &captureErrorLogRepo{created: make(chan *models.ErrorLog, 1)}
	DefaultErrorLog = NewErrorLogService(repo)
	defer func() { DefaultErrorLog = nil }()

	id := uuid.New()
	LogAssignmentFailure(id, "INC-9", errors.New("db down"))
	select {
	case got := <-repo.created:
		if got.Module != models.ErrorModuleIncident || got.ErrorType != models.ErrorTypeAssignmentFailed || got.RelatedID != id.String() {
			t.Errorf("unexpected entry: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("no log entry")
	}
}

func TestLogContactFailureMasksPhone(t *testing.T) {
	repo := &captureErrorLogRepo{created: make(chan *models.ErrorLog, 1)}
	DefaultErrorLog = NewErrorLogService(repo)
	defer func() { DefaultErrorLog = nil }()

	LogContactFailure("Create contact from incident", "", "+971501234567", errors.New("insert failed for phone +971501234567"))
	select {
	case got := <-repo.created:
		if got.Module != models.ErrorModuleContact || got.RelatedLabel != "+971 50 *** 4567" {
			t.Errorf("unexpected entry: module=%q label=%q", got.Module, got.RelatedLabel)
		}
		if strings.Contains(got.TechnicalDetails, "1234567") {
			t.Errorf("phone not masked in details: %s", got.TechnicalDetails)
		}
	case <-time.After(time.Second):
		t.Fatal("no log entry")
	}
}

type alertCall struct {
	code  string
	count int
}

func newTestAlerter(window time.Duration) (*ErrorLogAlerter, chan alertCall) {
	calls := make(chan alertCall, 10)
	a := NewErrorLogAlerter(nil, nil, window)
	a.deliver = func(e *models.ErrorLog, count int) { calls <- alertCall{e.ErrorCode, count} }
	return a, calls
}

func TestCriticalAlertIsGrouped(t *testing.T) {
	a, calls := newTestAlerter(150 * time.Millisecond)
	e := &models.ErrorLog{ErrorCode: "ERR-000001", Fingerprint: "fp1", Severity: models.ErrorSeverityCritical}

	// Same error 5 times quickly: 1 immediate alert, then 1 grouped alert (count 5).
	for i := 0; i < 5; i++ {
		a.Alert(e)
	}
	first := <-calls
	if first.count != 1 {
		t.Fatalf("first alert count = %d, want 1", first.count)
	}
	select {
	case second := <-calls:
		if second.count != 5 {
			t.Fatalf("grouped alert count = %d, want 5", second.count)
		}
	case <-time.After(time.Second):
		t.Fatal("no grouped alert after the window")
	}
	select {
	case extra := <-calls:
		t.Fatalf("unexpected extra alert: %+v", extra)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestSingleCriticalErrorSendsOneAlertOnly(t *testing.T) {
	a, calls := newTestAlerter(100 * time.Millisecond)
	a.Alert(&models.ErrorLog{ErrorCode: "ERR-000002", Fingerprint: "fp2"})
	<-calls
	select {
	case extra := <-calls:
		t.Fatalf("no grouped alert expected for a single error, got %+v", extra)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestOnlyCriticalErrorsAreAlerted(t *testing.T) {
	repo := &captureErrorLogRepo{created: make(chan *models.ErrorLog, 4)}
	svc := NewErrorLogService(repo)
	a, calls := newTestAlerter(time.Minute)
	svc.SetAlerter(a)

	svc.Log(models.ErrorLogEntry{Module: models.ErrorModuleAuth, Action: "Login", Severity: models.ErrorSeverityHigh, Message: "x"})
	<-repo.created
	select {
	case c := <-calls:
		t.Fatalf("High error must not alert, got %+v", c)
	case <-time.After(200 * time.Millisecond):
	}

	svc.Log(models.ErrorLogEntry{Module: models.ErrorModuleAuth, Action: "Login", Severity: models.ErrorSeverityCritical, Message: "x"})
	<-repo.created
	select {
	case <-calls:
	case <-time.After(time.Second):
		t.Fatal("Critical error must alert")
	}
}

func (f *captureErrorLogRepo) ListUsers(context.Context) ([]models.ErrorLogUser, error) {
	return nil, nil
}
