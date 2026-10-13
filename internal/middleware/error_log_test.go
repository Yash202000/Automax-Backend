package middleware

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

// fakeErrorLogRepo captures created logs so the test can inspect them.
type fakeErrorLogRepo struct{ created chan *models.ErrorLog }

func (f *fakeErrorLogRepo) Create(_ context.Context, e *models.ErrorLog) error {
	f.created <- e
	return nil
}
func (f *fakeErrorLogRepo) List(context.Context, models.ErrorLogFilter) ([]models.ErrorLog, int64, error) {
	return nil, 0, nil
}
func (f *fakeErrorLogRepo) FindByID(context.Context, uuid.UUID) (*models.ErrorLog, error) {
	return nil, nil
}
func (f *fakeErrorLogRepo) ListForExport(context.Context, models.ErrorLogFilter, int) ([]models.ErrorLog, error) {
	return nil, nil
}
func (f *fakeErrorLogRepo) UpdateStatus(context.Context, uuid.UUID, string, string, *uuid.UUID, string) error {
	return nil
}
func (f *fakeErrorLogRepo) DeleteOlderThan(context.Context, time.Time) (int64, error) { return 0, nil }

func TestErrorLogRules(t *testing.T) {
	repo := &fakeErrorLogRepo{created: make(chan *models.ErrorLog, 4)}
	services.DefaultErrorLog = services.NewErrorLogService(repo)
	defer func() { services.DefaultErrorLog = nil }()

	incidentID := uuid.NewString()
	cases := []struct {
		name       string
		method     string
		path       string
		status     int
		wantLogged bool
		wantModule string
		wantAction string
		message    string
		wantSev    string
	}{
		{"incident create 500", "POST", "/api/v1/incidents", 500, true, models.ErrorModuleIncident, "Create incident", "", ""},
		{"incident update 500", "PUT", "/api/v1/incidents/" + incidentID, 500, true, models.ErrorModuleIncident, "Update incident", "", ""},
		{"status update 500", "POST", "/api/v1/incidents/" + incidentID + "/transition", 500, true, models.ErrorModuleIncident, "Update incident status", "", ""},
		{"incident validation 400 not logged", "POST", "/api/v1/incidents", 400, false, "", "", "", ""},
		{"incident GET 500 not logged", "GET", "/api/v1/incidents/" + incidentID, 500, false, "", "", "", ""},
		{"login 500 is critical", "POST", "/api/v1/auth/login", 500, true, models.ErrorModuleAuth, "Login", "boom", models.ErrorSeverityCritical},
		{"storage failure is critical", "POST", "/api/v1/incidents/" + incidentID + "/attachments", 500, true, models.ErrorModuleAttachment, "Upload attachment", "failed to upload file", models.ErrorSeverityCritical},
		{"incident 500 is high", "POST", "/api/v1/incidents", 500, true, models.ErrorModuleIncident, "Create incident", "boom", models.ErrorSeverityHigh},
		{"wrong password 401 not logged", "POST", "/api/v1/auth/login", 401, false, "", "", "", ""},
		{"ldap login 500", "POST", "/api/v1/ldap/login", 500, true, models.ErrorModuleAuth, "LDAP login", "", ""},
		{"attachment 500", "POST", "/api/v1/incidents/" + incidentID + "/attachments", 500, true, models.ErrorModuleAttachment, "Upload attachment", "", ""},
		{"attachment 413", "POST", "/api/v1/complaints/" + incidentID + "/attachments", 413, true, models.ErrorModuleAttachment, "Upload attachment", "", ""},
		{"unrelated route 500 not logged", "GET", "/api/v1/departments", 500, false, "", "", "", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Use(ErrorLog())
			app.Add(tc.method, tc.path, func(c *fiber.Ctx) error {
				msg := tc.message
				if msg == "" {
					msg = "boom"
				}
				return utils.ErrorResponse(c, tc.status, msg)
			})
			resp, err := app.Test(httptest.NewRequest(tc.method, tc.path, nil))
			if err != nil || resp.StatusCode != tc.status {
				t.Fatalf("request failed: err=%v status=%v", err, resp)
			}
			select {
			case got := <-repo.created:
				if !tc.wantLogged {
					t.Fatalf("expected no log, got %+v", got)
				}
				if tc.wantSev != "" && got.Severity != tc.wantSev {
					t.Errorf("got severity=%q, want %q", got.Severity, tc.wantSev)
				}
				if got.Module != tc.wantModule || got.Action != tc.wantAction {
					t.Errorf("got module=%q action=%q, want module=%q action=%q", got.Module, got.Action, tc.wantModule, tc.wantAction)
				}
			case <-time.After(300 * time.Millisecond):
				if tc.wantLogged {
					t.Fatal("expected a log entry, got none")
				}
			}
		})
	}
}

func (f *fakeErrorLogRepo) ListUsers(context.Context) ([]models.ErrorLogUser, error) { return nil, nil }
