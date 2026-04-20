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

type mockIncidentMergeService struct {
	services.IncidentMergeService
	validateMergeFunc      func(ctx context.Context, incidentIDs []string) (*models.IncidentMergeValidationResponse, error)
	mergeIncidentsFunc     func(ctx context.Context, req *models.IncidentMergeRequest, userID uuid.UUID, userRoleIDs []uuid.UUID) (*models.IncidentMergeResponse, error)
	unmergeIncidentFunc    func(ctx context.Context, req *models.IncidentUnmergeRequest, userID uuid.UUID, userRoleIDs []uuid.UUID) (*models.IncidentUnmergeResponse, error)
	bulkUnmergeIncidentsFunc func(ctx context.Context, req *models.IncidentBulkUnmergeRequest, userID uuid.UUID, userRoleIDs []uuid.UUID) (*models.IncidentBulkUnmergeResponse, error)
	getMergedIncidentsFunc func(ctx context.Context, masterIncidentID string) ([]models.IncidentResponse, error)
	canUserMergeFunc       func(ctx context.Context, workflowID uuid.UUID, userRoleIDs []uuid.UUID) (bool, error)
}

func (m *mockIncidentMergeService) ValidateMerge(ctx context.Context, incidentIDs []string) (*models.IncidentMergeValidationResponse, error) {
	if m.validateMergeFunc != nil {
		return m.validateMergeFunc(ctx, incidentIDs)
	}
	return &models.IncidentMergeValidationResponse{CanMerge: true}, nil
}

func (m *mockIncidentMergeService) MergeIncidents(ctx context.Context, req *models.IncidentMergeRequest, userID uuid.UUID, userRoleIDs []uuid.UUID) (*models.IncidentMergeResponse, error) {
	if m.mergeIncidentsFunc != nil {
		return m.mergeIncidentsFunc(ctx, req, userID, userRoleIDs)
	}
	return &models.IncidentMergeResponse{}, nil
}

func (m *mockIncidentMergeService) UnmergeIncident(ctx context.Context, req *models.IncidentUnmergeRequest, userID uuid.UUID, userRoleIDs []uuid.UUID) (*models.IncidentUnmergeResponse, error) {
	if m.unmergeIncidentFunc != nil {
		return m.unmergeIncidentFunc(ctx, req, userID, userRoleIDs)
	}
	return &models.IncidentUnmergeResponse{}, nil
}

func (m *mockIncidentMergeService) BulkUnmergeIncidents(ctx context.Context, req *models.IncidentBulkUnmergeRequest, userID uuid.UUID, userRoleIDs []uuid.UUID) (*models.IncidentBulkUnmergeResponse, error) {
	if m.bulkUnmergeIncidentsFunc != nil {
		return m.bulkUnmergeIncidentsFunc(ctx, req, userID, userRoleIDs)
	}
	return &models.IncidentBulkUnmergeResponse{}, nil
}

func (m *mockIncidentMergeService) GetMergedIncidents(ctx context.Context, masterIncidentID string) ([]models.IncidentResponse, error) {
	if m.getMergedIncidentsFunc != nil {
		return m.getMergedIncidentsFunc(ctx, masterIncidentID)
	}
	return []models.IncidentResponse{}, nil
}

func (m *mockIncidentMergeService) CanUserMerge(ctx context.Context, workflowID uuid.UUID, userRoleIDs []uuid.UUID) (bool, error) {
	if m.canUserMergeFunc != nil {
		return m.canUserMergeFunc(ctx, workflowID, userRoleIDs)
	}
	return true, nil
}

type mockUserRepoForMerge struct {
	repository.UserRepository
	getUserRolesFunc func(ctx context.Context, userID uuid.UUID) ([]models.Role, error)
}

func (m *mockUserRepoForMerge) GetUserRoles(ctx context.Context, userID uuid.UUID) ([]models.Role, error) {
	if m.getUserRolesFunc != nil {
		return m.getUserRolesFunc(ctx, userID)
	}
	return []models.Role{}, nil
}

func TestIncidentMergeHandler(t *testing.T) {
	mSvc := &mockIncidentMergeService{}
	mUserRepo := &mockUserRepoForMerge{
		getUserRolesFunc: func(ctx context.Context, userID uuid.UUID) ([]models.Role, error) {
			return []models.Role{}, nil
		},
	}

	h := NewIncidentMergeHandler(mSvc, mUserRepo)

	testUserID := uuid.New()

	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals(constants.ContextKeys.UserID, testUserID)
		return c.Next()
	})
	app.Post("/incidents/merge/validate", h.ValidateMerge)
	app.Post("/incidents/merge", h.MergeIncidents)
	app.Post("/incidents/unmerge", h.UnmergeIncident)
	app.Post("/incidents/bulk-unmerge", h.BulkUnmergeIncidents)
	app.Get("/incidents/:id/merged", h.GetMergedIncidents)
	app.Get("/incidents/can-merge", h.CanMerge)

	t.Run("ValidateMerge_Success", func(t *testing.T) {
		mSvc.validateMergeFunc = func(ctx context.Context, incidentIDs []string) (*models.IncidentMergeValidationResponse, error) {
			return &models.IncidentMergeValidationResponse{
				CanMerge: true,
				Errors:   []string{},
			}, nil
		}

		payload := map[string]interface{}{
			"incident_ids": []string{uuid.New().String(), uuid.New().String()},
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/incidents/merge/validate", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("ValidateMerge_ValidationFailure", func(t *testing.T) {
		payload := map[string]string{}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/incidents/merge/validate", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("ValidateMerge_CannotMerge", func(t *testing.T) {
		mSvc.validateMergeFunc = func(ctx context.Context, incidentIDs []string) (*models.IncidentMergeValidationResponse, error) {
			return &models.IncidentMergeValidationResponse{
				CanMerge: false,
				Errors:   []string{"Cannot merge incidents from different workflows"},
			}, nil
		}

		payload := map[string]interface{}{
			"incident_ids": []string{uuid.New().String(), uuid.New().String()},
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/incidents/merge/validate", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("MergeIncidents_Success", func(t *testing.T) {
		mSvc.mergeIncidentsFunc = func(ctx context.Context, req *models.IncidentMergeRequest, userID uuid.UUID, userRoleIDs []uuid.UUID) (*models.IncidentMergeResponse, error) {
			return &models.IncidentMergeResponse{
				MergedCount: 2,
			}, nil
		}

		masterID := uuid.New().String()
		payload := map[string]interface{}{
			"incident_ids":       []string{uuid.New().String(), uuid.New().String()},
			"master_incident_id": masterID,
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/incidents/merge", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("MergeIncidents_ValidationFailure", func(t *testing.T) {
		payload := map[string]string{}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/incidents/merge", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("MergeIncidents_Error", func(t *testing.T) {
		mSvc.mergeIncidentsFunc = func(ctx context.Context, req *models.IncidentMergeRequest, userID uuid.UUID, userRoleIDs []uuid.UUID) (*models.IncidentMergeResponse, error) {
			return nil, errors.New("merge failed")
		}

		masterID := uuid.New().String()
		payload := map[string]interface{}{
			"incident_ids":       []string{uuid.New().String(), uuid.New().String()},
			"master_incident_id": masterID,
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/incidents/merge", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("UnmergeIncident_Success", func(t *testing.T) {
		mSvc.unmergeIncidentFunc = func(ctx context.Context, req *models.IncidentUnmergeRequest, userID uuid.UUID, userRoleIDs []uuid.UUID) (*models.IncidentUnmergeResponse, error) {
			return &models.IncidentUnmergeResponse{
				Message: "Unmerged successfully",
			}, nil
		}

		payload := map[string]interface{}{
			"incident_id": uuid.New().String(),
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/incidents/unmerge", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("UnmergeIncident_ValidationFailure", func(t *testing.T) {
		payload := map[string]string{}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/incidents/unmerge", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("BulkUnmergeIncidents_Success", func(t *testing.T) {
		mSvc.bulkUnmergeIncidentsFunc = func(ctx context.Context, req *models.IncidentBulkUnmergeRequest, userID uuid.UUID, userRoleIDs []uuid.UUID) (*models.IncidentBulkUnmergeResponse, error) {
			return &models.IncidentBulkUnmergeResponse{
				UnmergedCount: len(req.IncidentIDs),
			}, nil
		}

		payload := map[string]interface{}{
			"incident_ids": []string{uuid.New().String(), uuid.New().String(), uuid.New().String()},
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/incidents/bulk-unmerge", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("BulkUnmergeIncidents_ValidationFailure", func(t *testing.T) {
		payload := map[string]string{}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/incidents/bulk-unmerge", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("GetMergedIncidents_Success", func(t *testing.T) {
		mSvc.getMergedIncidentsFunc = func(ctx context.Context, masterIncidentID string) ([]models.IncidentResponse, error) {
			return []models.IncidentResponse{
				{ID: uuid.New()},
				{ID: uuid.New()},
			}, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/incidents/"+uuid.New().String()+"/merged", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("GetMergedIncidents_MissingID", func(t *testing.T) {
		t.Skip("Fiber treats empty param as 404 not 400")
	})

	t.Run("GetMergedIncidents_InvalidID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/incidents/invalid-uuid/merged", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("CanMerge_Success", func(t *testing.T) {
		mSvc.canUserMergeFunc = func(ctx context.Context, workflowID uuid.UUID, userRoleIDs []uuid.UUID) (bool, error) {
			return true, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/incidents/can-merge?workflow_id="+uuid.New().String(), nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("CanMerge_MissingWorkflowID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/incidents/can-merge", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("CanMerge_InvalidWorkflowID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/incidents/can-merge?workflow_id=invalid", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})
}
