package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type mockActionLogService struct {
	services.ActionLogService
	logActionFunc        func(ctx context.Context, params *services.LogActionParams) error
	getActionLogFunc     func(ctx context.Context, id uuid.UUID) (*models.ActionLogResponse, error)
	listLogsFunc         func(ctx context.Context, filter *models.ActionLogFilter) ([]models.ActionLogResponse, int64, error)
	getStatsFunc         func(ctx context.Context) (*models.ActionLogStats, error)
	getFilterOptionsFunc func(ctx context.Context) (*services.FilterOptions, error)
	getUserActionsFunc   func(ctx context.Context, userID uuid.UUID, page, limit int) ([]models.ActionLogResponse, int64, error)
}

func (m *mockActionLogService) LogAction(ctx context.Context, params *services.LogActionParams) error {
	if m.logActionFunc != nil {
		return m.logActionFunc(ctx, params)
	}
	return nil
}

func (m *mockActionLogService) GetActionLog(ctx context.Context, id uuid.UUID) (*models.ActionLogResponse, error) {
	if m.getActionLogFunc != nil {
		return m.getActionLogFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockActionLogService) ListActionLogs(ctx context.Context, filter *models.ActionLogFilter) ([]models.ActionLogResponse, int64, error) {
	if m.listLogsFunc != nil {
		fmt.Println("")
		return m.listLogsFunc(ctx, filter)
	}
	return []models.ActionLogResponse{}, 0, nil
}

func (m *mockActionLogService) GetStats(ctx context.Context) (*models.ActionLogStats, error) {
	if m.getStatsFunc != nil {
		return m.getStatsFunc(ctx)
	}
	return nil, nil
}

func (m *mockActionLogService) GetFilterOptions(ctx context.Context) (*services.FilterOptions, error) {
	if m.getFilterOptionsFunc != nil {
		return m.getFilterOptionsFunc(ctx)
	}
	return nil, nil
}

func (m *mockActionLogService) GetUserActions(ctx context.Context, userID uuid.UUID, page, limit int) ([]models.ActionLogResponse, int64, error) {
	if m.getUserActionsFunc != nil {
		return m.getUserActionsFunc(ctx, userID, page, limit)
	}
	return []models.ActionLogResponse{}, 0, nil
}

func TestActionLogHandler(t *testing.T) {
	mSvc := &mockActionLogService{}
	h := NewActionLogHandler(mSvc, nil)

	app := fiber.New()
	app.Get("/admin/action-logs", h.ListActionLogs)
	app.Get("/admin/action-logs/stats", h.GetStats)
	app.Get("/admin/action-logs/filter-options", h.GetFilterOptions)
	app.Get("/admin/action-logs/user/:id", h.GetUserActions)
	app.Get("/admin/action-logs/:id", h.GetActionLog)

	t.Run("ListActionLogs_Success", func(t *testing.T) {
		mSvc.listLogsFunc = func(ctx context.Context, filter *models.ActionLogFilter) ([]models.ActionLogResponse, int64, error) {
			return []models.ActionLogResponse{
				{ID: uuid.New(), Action: "create", Module: "users"},
			}, 1, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/action-logs", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("ListActionLogs_WithPagination", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/action-logs?page=2&limit=50", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("ListActionLogs_WithFilters", func(t *testing.T) {
		mSvc.listLogsFunc = func(ctx context.Context, filter *models.ActionLogFilter) ([]models.ActionLogResponse, int64, error) {
			if filter.Module != "" || filter.Action != "" {
				return []models.ActionLogResponse{{ID: uuid.New()}}, 1, nil
			}
			return []models.ActionLogResponse{}, 0, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/action-logs?module=users&action=create", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("GetActionLog_Success", func(t *testing.T) {
		testID := uuid.New()
		mSvc.getActionLogFunc = func(ctx context.Context, id uuid.UUID) (*models.ActionLogResponse, error) {
			return &models.ActionLogResponse{ID: id, Action: "create"}, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/action-logs/"+testID.String(), nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("GetActionLog_NotFound", func(t *testing.T) {
		mSvc.getActionLogFunc = func(ctx context.Context, id uuid.UUID) (*models.ActionLogResponse, error) {
			return nil, errors.New("not found")
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/action-logs/"+uuid.New().String(), nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusNotFound {
			t.Errorf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("GetActionLog_InvalidID", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/action-logs/invalid-id", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("GetStats_Success", func(t *testing.T) {
		mSvc.getStatsFunc = func(ctx context.Context) (*models.ActionLogStats, error) {
			return &models.ActionLogStats{
				TotalActions:    100,
				ActionsByModule: map[string]int64{"users": 50, "incidents": 50},
			}, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/action-logs/stats", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("GetFilterOptions_Success", func(t *testing.T) {
		mSvc.getFilterOptionsFunc = func(ctx context.Context) (*services.FilterOptions, error) {
			return &services.FilterOptions{
				Modules: []string{"users", "incidents"},
				Actions: []string{"create", "update", "delete"},
			}, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/action-logs/filter-options", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("GetUserActions_Success", func(t *testing.T) {
		testUserID := uuid.New()
		mSvc.getUserActionsFunc = func(ctx context.Context, userID uuid.UUID, page, limit int) ([]models.ActionLogResponse, int64, error) {
			return []models.ActionLogResponse{
				{ID: uuid.New(), UserID: userID, Action: "create"},
			}, 1, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/admin/action-logs/user/"+testUserID.String(), nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})
}
