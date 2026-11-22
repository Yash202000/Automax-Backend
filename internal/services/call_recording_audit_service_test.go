package services

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/google/uuid"
	"gorm.io/datatypes"
)

type fakeAuditRepo struct {
	repository.CallRecordingAuditRepository
	createErr error
	created   []*models.CallRecordingAudit
}

func (f *fakeAuditRepo) Create(_ context.Context, e *models.CallRecordingAudit) error {
	if f.createErr != nil {
		return f.createErr
	}
	f.created = append(f.created, e)
	return nil
}

type fakeCallLogRepo struct {
	repository.CallLogRepository
	logs map[uuid.UUID]*models.CallLog
}

func (f *fakeCallLogRepo) FindByID(_ context.Context, id uuid.UUID) (*models.CallLog, error) {
	if cl, ok := f.logs[id]; ok {
		return cl, nil
	}
	return nil, errors.New("record not found")
}

func newTestAuditService(auditRepo *fakeAuditRepo) (CallRecordingAuditService, *models.CallLog, *models.CallLog) {
	withRec := &models.CallLog{ID: uuid.New(), CallUuid: "call-1", Meta: datatypes.JSON(`{"recording_url":"http://cx/api/v1/cdr/9/recording"}`)}
	noRec := &models.CallLog{ID: uuid.New(), CallUuid: "call-2"}
	clRepo := &fakeCallLogRepo{logs: map[uuid.UUID]*models.CallLog{withRec.ID: withRec, noRec.ID: noRec}}
	return NewCallRecordingAuditService(auditRepo, clRepo, "test-secret"), withRec, noRec
}

func testUser() *models.User {
	return &models.User{
		ID: uuid.New(), Username: "anjaly", FirstName: "Anjaly",
		Roles: []models.Role{{Name: "Agent"}},
	}
}

func TestRequestAccess_SavesEntryThenIssuesGrant(t *testing.T) {
	repo := &fakeAuditRepo{}
	svc, cl, _ := newTestAuditService(repo)
	user := testUser()

	grant, token, err := svc.RequestAccess(context.Background(), user, cl.ID, models.RecordingActivityDownloaded, "10.0.0.1", "ua")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("expected 1 audit entry, got %d", len(repo.created))
	}
	e := repo.created[0]
	if e.Activity != "downloaded" || e.UserID != user.ID || e.UserName != "Anjaly" || e.UserRole != "Agent" || e.CallUuid != "call-1" {
		t.Errorf("unexpected entry: %+v", e)
	}
	if !e.CreatedAt.IsZero() {
		t.Errorf("created_at must be left to the DB clock, got %v", e.CreatedAt)
	}

	got, err := svc.VerifyGrant(token)
	if err != nil {
		t.Fatalf("grant should verify: %v", err)
	}
	if got.AuditID != e.ID || got.CallLogID != cl.ID || got.Activity != "downloaded" || got.UserID != user.ID || !got.ExpiresAt.Equal(grant.ExpiresAt) {
		t.Errorf("grant mismatch: %+v vs %+v", got, grant)
	}
}

func TestRequestAccess_FailedSaveIssuesNoGrant(t *testing.T) {
	repo := &fakeAuditRepo{createErr: errors.New("db down")}
	svc, cl, _ := newTestAuditService(repo)

	grant, token, err := svc.RequestAccess(context.Background(), testUser(), cl.ID, models.RecordingActivityListened, "", "")
	if !errors.Is(err, ErrRecordingAuditFailed) {
		t.Fatalf("want ErrRecordingAuditFailed, got %v", err)
	}
	if grant != nil || token != "" {
		t.Fatalf("no grant may be issued when the audit entry is not saved")
	}
}

func TestRequestAccess_NoRecording(t *testing.T) {
	repo := &fakeAuditRepo{}
	svc, _, noRec := newTestAuditService(repo)

	if _, _, err := svc.RequestAccess(context.Background(), testUser(), noRec.ID, models.RecordingActivityListened, "", ""); !errors.Is(err, ErrRecordingNotFound) {
		t.Fatalf("want ErrRecordingNotFound, got %v", err)
	}
	if _, _, err := svc.RequestAccess(context.Background(), testUser(), uuid.New(), models.RecordingActivityListened, "", ""); !errors.Is(err, ErrRecordingNotFound) {
		t.Fatalf("unknown call: want ErrRecordingNotFound, got %v", err)
	}
	if len(repo.created) != 0 {
		t.Fatalf("nothing should be logged when there is no recording")
	}
}

func TestVerifyGrant_RejectsTamperedExpiredAndForeign(t *testing.T) {
	s := NewCallRecordingAuditService(&fakeAuditRepo{}, &fakeCallLogRepo{}, "test-secret").(*callRecordingAuditService)
	g := &RecordingGrant{AuditID: uuid.New(), CallLogID: uuid.New(), Activity: "listened", UserID: uuid.New(), ExpiresAt: time.Now().Add(time.Minute)}
	token := s.signGrant(g)

	if _, err := s.VerifyGrant(token); err != nil {
		t.Fatalf("valid grant rejected: %v", err)
	}

	// Tamper: flip listened -> downloaded but keep the old signature.
	tampered := *g
	tampered.Activity = "downloaded"
	enc, _, _ := strings.Cut(s.signGrant(&tampered), ".")
	_, sig, _ := strings.Cut(token, ".")
	if _, err := s.VerifyGrant(enc + "." + sig); !errors.Is(err, ErrRecordingGrantInvalid) {
		t.Errorf("tampered grant: want invalid, got %v", err)
	}

	expired := *g
	expired.ExpiresAt = time.Now().Add(-time.Second)
	if _, err := s.VerifyGrant(s.signGrant(&expired)); !errors.Is(err, ErrRecordingGrantExpired) {
		t.Errorf("expired grant: want expired, got %v", err)
	}

	other := NewCallRecordingAuditService(&fakeAuditRepo{}, &fakeCallLogRepo{}, "other-secret")
	if _, err := other.VerifyGrant(token); !errors.Is(err, ErrRecordingGrantInvalid) {
		t.Errorf("grant from another secret: want invalid, got %v", err)
	}

	for _, bad := range []string{"", "garbage", "a.b", token + "x"} {
		if _, err := s.VerifyGrant(bad); err == nil {
			t.Errorf("malformed grant %q accepted", bad)
		}
	}
}
