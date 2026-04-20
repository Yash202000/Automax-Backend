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

type mockLookupRepo struct {
	repository.LookupRepository
	createCategoryFunc     func(ctx context.Context, category *models.LookupCategory) error
	findCategoryByIDFunc    func(ctx context.Context, id uuid.UUID) (*models.LookupCategory, error)
	updateCategoryFunc     func(ctx context.Context, category *models.LookupCategory) error
	deleteCategoryFunc     func(ctx context.Context, id uuid.UUID) error
	listCategoriesFunc     func(ctx context.Context) ([]models.LookupCategory, error)
	listValuesByCategoryCodeFunc func(ctx context.Context, code string) ([]models.LookupValue, error)
}

func (m *mockLookupRepo) CreateCategory(ctx context.Context, category *models.LookupCategory) error {
	if m.createCategoryFunc != nil {
		return m.createCategoryFunc(ctx, category)
	}
	return nil
}

func (m *mockLookupRepo) FindCategoryByID(ctx context.Context, id uuid.UUID) (*models.LookupCategory, error) {
	if m.findCategoryByIDFunc != nil {
		return m.findCategoryByIDFunc(ctx, id)
	}
	return nil, nil
}

func (m *mockLookupRepo) UpdateCategory(ctx context.Context, category *models.LookupCategory) error {
	if m.updateCategoryFunc != nil {
		return m.updateCategoryFunc(ctx, category)
	}
	return nil
}

func (m *mockLookupRepo) DeleteCategory(ctx context.Context, id uuid.UUID) error {
	if m.deleteCategoryFunc != nil {
		return m.deleteCategoryFunc(ctx, id)
	}
	return nil
}

func (m *mockLookupRepo) ListCategories(ctx context.Context) ([]models.LookupCategory, error) {
	if m.listCategoriesFunc != nil {
		return m.listCategoriesFunc(ctx)
	}
	return []models.LookupCategory{}, nil
}

func (m *mockLookupRepo) ListValuesByCategoryCode(ctx context.Context, code string) ([]models.LookupValue, error) {
	if m.listValuesByCategoryCodeFunc != nil {
		return m.listValuesByCategoryCodeFunc(ctx, code)
	}
	return []models.LookupValue{}, nil
}

func TestLookupHandler(t *testing.T) {
	mRepo := &mockLookupRepo{}
	h := NewLookupHandler(mRepo)

	app := fiber.New()
	app.Get("/lookups/categories", h.ListCategories)
	app.Get("/lookups/values/:code", h.GetValuesByCategoryCode)
	app.Post("/lookups/categories", h.CreateCategory)
	app.Put("/lookups/categories/:id", h.UpdateCategory)
	app.Delete("/lookups/categories/:id", h.DeleteCategory)

	t.Run("ListCategories_Success", func(t *testing.T) {
		mRepo.listCategoriesFunc = func(ctx context.Context) ([]models.LookupCategory, error) {
			return []models.LookupCategory{
				{ID: uuid.New(), Code: "priority", Name: "Priority"},
			}, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/lookups/categories", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("GetValuesByCategoryCode_Success", func(t *testing.T) {
		mRepo.listValuesByCategoryCodeFunc = func(ctx context.Context, code string) ([]models.LookupValue, error) {
			return []models.LookupValue{
				{CategoryID: uuid.New(), Code: "HIGH", Name: "High"},
			}, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/lookups/values/priority", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("CreateCategory_Success", func(t *testing.T) {
		mRepo.createCategoryFunc = func(ctx context.Context, category *models.LookupCategory) error {
			category.ID = uuid.New()
			return nil
		}

		payload := map[string]interface{}{
			"code": "priority",
			"name": "Priority",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/lookups/categories", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusCreated {
			t.Errorf("expected 201, got %d", resp.StatusCode)
		}
	})

	t.Run("CreateCategory_ValidationFailure", func(t *testing.T) {
		payload := map[string]string{}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/lookups/categories", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("UpdateCategory_Success", func(t *testing.T) {
		testID := uuid.New()
		mRepo.findCategoryByIDFunc = func(ctx context.Context, id uuid.UUID) (*models.LookupCategory, error) {
			return &models.LookupCategory{ID: id, Code: "priority"}, nil
		}
		mRepo.updateCategoryFunc = func(ctx context.Context, category *models.LookupCategory) error {
			return nil
		}

		payload := map[string]string{"name": "Updated"}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPut, "/lookups/categories/"+testID.String(), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("DeleteCategory_Success", func(t *testing.T) {
		testID := uuid.New()
		mRepo.deleteCategoryFunc = func(ctx context.Context, id uuid.UUID) error {
			return nil
		}

		req := httptest.NewRequest(http.MethodDelete, "/lookups/categories/"+testID.String(), nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("DeleteCategory_Error", func(t *testing.T) {
		mRepo.deleteCategoryFunc = func(ctx context.Context, id uuid.UUID) error {
			return errors.New("category in use")
		}

		req := httptest.NewRequest(http.MethodDelete, "/lookups/categories/"+uuid.New().String(), nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Errorf("expected 500, got %d", resp.StatusCode)
		}
	})
}