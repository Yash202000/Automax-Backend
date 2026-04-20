package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type mockIncidentService struct {
	services.IncidentService
	listIncidentsFunc      func(ctx context.Context, filter *models.IncidentFilter) ([]models.IncidentResponse, int64, error)
	getIncidentFunc        func(ctx context.Context, id uuid.UUID) (*models.IncidentDetailResponse, error)
	createIncidentFunc     func(ctx context.Context, req *models.IncidentCreateRequest, reporterID uuid.UUID) (*models.IncidentResponse, error)
	updateIncidentFunc     func(ctx context.Context, id uuid.UUID, req *models.IncidentUpdateRequest, userID uuid.UUID, userRoleIDs []uuid.UUID) (*models.IncidentResponse, error)
	deleteIncidentFunc     func(ctx context.Context, id uuid.UUID) error
	getStatsFunc           func(ctx context.Context, filter *models.IncidentFilter) (*models.IncidentStatsResponse, error)
}

func (m *mockIncidentService) ListIncidents(ctx context.Context, filter *models.IncidentFilter) ([]models.IncidentResponse, int64, error) {
	if m.listIncidentsFunc != nil {
		return m.listIncidentsFunc(ctx, filter)
	}
	return []models.IncidentResponse{}, 0, nil
}

func (m *mockIncidentService) GetIncident(ctx context.Context, id uuid.UUID) (*models.IncidentDetailResponse, error) {
	if m.getIncidentFunc != nil {
		return m.getIncidentFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockIncidentService) CreateIncident(ctx context.Context, req *models.IncidentCreateRequest, reporterID uuid.UUID) (*models.IncidentResponse, error) {
	if m.createIncidentFunc != nil {
		return m.createIncidentFunc(ctx, req, reporterID)
	}
	return nil, nil
}

func (m *mockIncidentService) UpdateIncident(ctx context.Context, id uuid.UUID, req *models.IncidentUpdateRequest, userID uuid.UUID, userRoleIDs []uuid.UUID) (*models.IncidentResponse, error) {
	if m.updateIncidentFunc != nil {
		return m.updateIncidentFunc(ctx, id, req, userID, userRoleIDs)
	}
	return nil, nil
}

func (m *mockIncidentService) DeleteIncident(ctx context.Context, id uuid.UUID) error {
	if m.deleteIncidentFunc != nil {
		return m.deleteIncidentFunc(ctx, id)
	}
	return nil
}

func (m *mockIncidentService) GetStats(ctx context.Context, filter *models.IncidentFilter) (*models.IncidentStatsResponse, error) {
	if m.getStatsFunc != nil {
		return m.getStatsFunc(ctx, filter)
	}
	return nil, nil
}

type mockUserRepo struct {
	repository.UserRepository
	getUserRolesFunc func(ctx context.Context, userID uuid.UUID) ([]models.Role, error)
}

func (m *mockUserRepo) GetUserRoles(ctx context.Context, userID uuid.UUID) ([]models.Role, error) {
	if m.getUserRolesFunc != nil {
		return m.getUserRolesFunc(ctx, userID)
	}
	return []models.Role{}, nil
}

type mockIncidentRepo struct {
	repository.IncidentRepository
}

type mockPresenceService struct {
	services.PresenceService
}

type mockReadyToCloseService struct {
	services.ReadyToCloseService
}

func TestIncidentHandler(t *testing.T) {
	mSvc := &mockIncidentService{}
	mUserRepo := &mockUserRepo{
		getUserRolesFunc: func(ctx context.Context, userID uuid.UUID) ([]models.Role, error) {
			return []models.Role{}, nil
		},
	}
	mIncidentRepo := &mockIncidentRepo{}
	mPresenceSvc := &mockPresenceService{}
	mReadyToCloseSvc := &mockReadyToCloseService{}

	h := NewIncidentHandler(mSvc, nil, mUserRepo, mIncidentRepo, nil, mPresenceSvc)
	h.SetReadyToCloseService(mReadyToCloseSvc)

	testUserID := uuid.New()

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(constants.ContextKeys.UserID, testUserID)
		c.Locals(constants.ContextKeys.UserName, "testuser")
		c.Locals(constants.ContextKeys.UserEmail, "test@example.com")
		return c.Next()
	})
	app.Get("/incidents", h.ListIncidents)
	app.Get("/incidents/:id", h.GetIncident)
	app.Post("/incidents", h.CreateIncident)
	app.Put("/incidents/:id", h.UpdateIncident)
	app.Delete("/incidents/:id", h.DeleteIncident)

	t.Run("ListIncidents_Success", func(t *testing.T) {
		testID := uuid.New()
		mSvc.listIncidentsFunc = func(ctx context.Context, filter *models.IncidentFilter) ([]models.IncidentResponse, int64, error) {
			return []models.IncidentResponse{
				{ID: testID, Title: "Test Incident"},
			}, 1, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/incidents", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("ListIncidents_WithFilters", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/incidents?record_type=incident&status=open", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("GetIncident_Success", func(t *testing.T) {
		testID := uuid.New()
		mSvc.getIncidentFunc = func(ctx context.Context, id uuid.UUID) (*models.IncidentDetailResponse, error) {
			return &models.IncidentDetailResponse{
				IncidentResponse: models.IncidentResponse{
					ID:    id,
					Title: "Test Incident",
				},
			}, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/incidents/"+testID.String(), nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("GetIncident_NotFound", func(t *testing.T) {
		mSvc.getIncidentFunc = func(ctx context.Context, id uuid.UUID) (*models.IncidentDetailResponse, error) {
			return nil, errors.New("not found")
		}

		req := httptest.NewRequest(http.MethodGet, "/incidents/"+uuid.New().String(), nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("CreateIncident_Success", func(t *testing.T) {
		t.Skip("Requires additional mock setup for user repo")
	})

	t.Run("CreateIncident_ValidationFailure", func(t *testing.T) {
		payload := map[string]string{}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/incidents", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("UpdateIncident_Success", func(t *testing.T) {
		t.Skip("Requires additional mock setup for validation")
	})

	t.Run("DeleteIncident_Success", func(t *testing.T) {
		testID := uuid.New()
		mSvc.deleteIncidentFunc = func(ctx context.Context, id uuid.UUID) error {
			return nil
		}

		req := httptest.NewRequest(http.MethodDelete, "/incidents/"+testID.String(), nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("DeleteIncident_Error", func(t *testing.T) {
		mSvc.deleteIncidentFunc = func(ctx context.Context, id uuid.UUID) error {
			return errors.New("cannot delete")
		}

		req := httptest.NewRequest(http.MethodDelete, "/incidents/"+uuid.New().String(), nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Errorf("expected 500, got %d", resp.StatusCode)
		}
	})
}