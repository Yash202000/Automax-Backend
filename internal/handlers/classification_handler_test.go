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
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type mockClassificationRepo struct {
	repository.ClassificationRepository
	createFunc         func(ctx context.Context, classification *models.Classification) error
	findByIDFunc       func(ctx context.Context, id uuid.UUID) (*models.Classification, error)
	updateFunc         func(ctx context.Context, classification *models.Classification) error
	deleteFunc         func(ctx context.Context, id uuid.UUID) error
	listFunc           func(ctx context.Context) ([]models.Classification, error)
	listByTypeFunc     func(ctx context.Context, types []string) ([]models.Classification, error)
	getTreeFunc        func(ctx context.Context) ([]models.Classification, error)
	getByParentIDFunc  func(ctx context.Context, parentID *uuid.UUID) ([]models.Classification, error)
	createdClassification *models.Classification
}

func (m *mockClassificationRepo) Create(ctx context.Context, classification *models.Classification) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, classification)
	}
	classification.ID = uuid.New()
	m.createdClassification = classification
	return nil
}

func (m *mockClassificationRepo) FindByID(ctx context.Context, id uuid.UUID) (*models.Classification, error) {
	if m.findByIDFunc != nil {
		return m.findByIDFunc(ctx, id)
	}
	if m.createdClassification != nil && m.createdClassification.ID == id {
		return m.createdClassification, nil
	}
	return nil, errors.New("not found")
}

func (m *mockClassificationRepo) Update(ctx context.Context, classification *models.Classification) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, classification)
	}
	return nil
}

func (m *mockClassificationRepo) Delete(ctx context.Context, id uuid.UUID) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

func (m *mockClassificationRepo) List(ctx context.Context) ([]models.Classification, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx)
	}
	return []models.Classification{}, nil
}

func (m *mockClassificationRepo) ListByType(ctx context.Context, types []string) ([]models.Classification, error) {
	if m.listByTypeFunc != nil {
		return m.listByTypeFunc(ctx, types)
	}
	return []models.Classification{}, nil
}

func (m *mockClassificationRepo) GetTree(ctx context.Context) ([]models.Classification, error) {
	if m.getTreeFunc != nil {
		return m.getTreeFunc(ctx)
	}
	return []models.Classification{}, nil
}

func (m *mockClassificationRepo) GetByParentID(ctx context.Context, parentID *uuid.UUID) ([]models.Classification, error) {
	if m.getByParentIDFunc != nil {
		return m.getByParentIDFunc(ctx, parentID)
	}
	return []models.Classification{}, nil
}

func TestClassificationHandler(t *testing.T) {
	mRepo := &mockClassificationRepo{}
	h := NewClassificationHandler(mRepo)

	app := fiber.New()
	app.Get("/classifications", h.List)
	app.Get("/classifications/tree", h.GetTree)
	app.Get("/classifications/children", h.GetChildren)
	app.Post("/classifications", h.Create)
	app.Put("/classifications/:id", h.Update)
	app.Delete("/classifications/:id", h.Delete)

	t.Run("List_Success", func(t *testing.T) {
		mRepo.listFunc = func(ctx context.Context) ([]models.Classification, error) {
			return []models.Classification{
				{ID: uuid.New(), Name: "Test Classification"},
			}, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/classifications", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("List_WithTypeFilter", func(t *testing.T) {
		mRepo.listByTypeFunc = func(ctx context.Context, types []string) ([]models.Classification, error) {
			return []models.Classification{
				{ID: uuid.New(), Name: "Incident Classification"},
			}, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/classifications?type=incident", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("GetTree_Success", func(t *testing.T) {
		mRepo.getTreeFunc = func(ctx context.Context) ([]models.Classification, error) {
			return []models.Classification{
				{ID: uuid.New(), Name: "Root"},
			}, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/classifications/tree", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("GetChildren_WithParentID", func(t *testing.T) {
		parentID := uuid.New()
		mRepo.getByParentIDFunc = func(ctx context.Context, pid *uuid.UUID) ([]models.Classification, error) {
			if pid != nil {
				return []models.Classification{{ID: *pid, Name: "Child"}}, nil
			}
			return []models.Classification{}, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/classifications/children?parent_id="+parentID.String(), nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("Create_Success", func(t *testing.T) {
		mRepo.createFunc = func(ctx context.Context, classification *models.Classification) error {
			classification.ID = uuid.New()
			mRepo.createdClassification = classification
			return nil
		}
		mRepo.findByIDFunc = func(ctx context.Context, id uuid.UUID) (*models.Classification, error) {
			return mRepo.createdClassification, nil
		}

		payload := map[string]interface{}{
			"name":        "New Classification",
			"description": "Description",
			"types":       []string{"incident", "request"},
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/classifications", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusCreated {
			t.Errorf("expected 201, got %d", resp.StatusCode)
		}
	})

	t.Run("Create_ValidationFailure", func(t *testing.T) {
		payload := map[string]string{}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/classifications", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("Update_Success", func(t *testing.T) {
		testID := uuid.New()
		mRepo.findByIDFunc = func(ctx context.Context, id uuid.UUID) (*models.Classification, error) {
			return &models.Classification{ID: id, Name: "Test"}, nil
		}
		mRepo.updateFunc = func(ctx context.Context, classification *models.Classification) error {
			return nil
		}

		payload := map[string]string{"name": "Updated Classification"}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPut, "/classifications/"+testID.String(), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("Delete_Success", func(t *testing.T) {
		testID := uuid.New()
		mRepo.deleteFunc = func(ctx context.Context, id uuid.UUID) error {
			return nil
		}

		req := httptest.NewRequest(http.MethodDelete, "/classifications/"+testID.String(), nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("Delete_Error", func(t *testing.T) {
		mRepo.deleteFunc = func(ctx context.Context, id uuid.UUID) error {
			return errors.New("cannot delete: has children")
		}

		req := httptest.NewRequest(http.MethodDelete, "/classifications/"+uuid.New().String(), nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Errorf("expected 500, got %d", resp.StatusCode)
		}
	})
}