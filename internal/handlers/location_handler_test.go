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

type mockLocationRepo struct {
	repository.LocationRepository
	createFunc       func(ctx context.Context, location *models.Location) error
	findByIDFunc    func(ctx context.Context, id uuid.UUID) (*models.Location, error)
	updateFunc      func(ctx context.Context, location *models.Location) error
	deleteFunc      func(ctx context.Context, id uuid.UUID) error
	listFunc        func(ctx context.Context) ([]models.Location, error)
	getTreeFunc     func(ctx context.Context) ([]models.Location, error)
	getByParentIDFunc func(ctx context.Context, parentID *uuid.UUID) ([]models.Location, error)
	createdLocation *models.Location
}

func (m *mockLocationRepo) Create(ctx context.Context, location *models.Location) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, location)
	}
	location.ID = uuid.New()
	m.createdLocation = location
	return nil
}

func (m *mockLocationRepo) FindByID(ctx context.Context, id uuid.UUID) (*models.Location, error) {
	if m.findByIDFunc != nil {
		return m.findByIDFunc(ctx, id)
	}
	if m.createdLocation != nil && m.createdLocation.ID == id {
		return m.createdLocation, nil
	}
	return nil, errors.New("not found")
}

func (m *mockLocationRepo) Update(ctx context.Context, location *models.Location) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, location)
	}
	return nil
}

func (m *mockLocationRepo) Delete(ctx context.Context, id uuid.UUID) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

func (m *mockLocationRepo) List(ctx context.Context) ([]models.Location, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx)
	}
	return []models.Location{}, nil
}

func (m *mockLocationRepo) GetTree(ctx context.Context) ([]models.Location, error) {
	if m.getTreeFunc != nil {
		return m.getTreeFunc(ctx)
	}
	return []models.Location{}, nil
}

func (m *mockLocationRepo) GetByParentID(ctx context.Context, parentID *uuid.UUID) ([]models.Location, error) {
	if m.getByParentIDFunc != nil {
		return m.getByParentIDFunc(ctx, parentID)
	}
	return []models.Location{}, nil
}

func TestLocationHandler(t *testing.T) {
	mRepo := &mockLocationRepo{}
	h := NewLocationHandler(mRepo)

	app := fiber.New()
	app.Get("/locations", h.List)
	app.Get("/locations/tree", h.GetTree)
	app.Post("/locations", h.Create)
	app.Put("/locations/:id", h.Update)
	app.Delete("/locations/:id", h.Delete)

	t.Run("List_Success", func(t *testing.T) {
		mRepo.listFunc = func(ctx context.Context) ([]models.Location, error) {
			return []models.Location{
				{ID: uuid.New(), Name: "New York", Code: "NY"},
			}, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/locations", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("GetTree_Success", func(t *testing.T) {
		mRepo.getTreeFunc = func(ctx context.Context) ([]models.Location, error) {
			return []models.Location{
				{ID: uuid.New(), Name: "USA"},
			}, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/locations/tree", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("Create_Success", func(t *testing.T) {
		mRepo.createFunc = func(ctx context.Context, location *models.Location) error {
			location.ID = uuid.New()
			mRepo.createdLocation = location
			return nil
		}
		mRepo.findByIDFunc = func(ctx context.Context, id uuid.UUID) (*models.Location, error) {
			return mRepo.createdLocation, nil
		}

		payload := map[string]interface{}{
			"name": "New Location",
			"code": "NL",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/locations", bytes.NewBuffer(body))
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
		req := httptest.NewRequest(http.MethodPost, "/locations", bytes.NewBuffer(body))
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
		mRepo.findByIDFunc = func(ctx context.Context, id uuid.UUID) (*models.Location, error) {
			return &models.Location{ID: id, Name: "Test"}, nil
		}
		mRepo.updateFunc = func(ctx context.Context, location *models.Location) error {
			return nil
		}

		payload := map[string]string{"name": "Updated Location"}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPut, "/locations/"+testID.String(), bytes.NewBuffer(body))
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

		req := httptest.NewRequest(http.MethodDelete, "/locations/"+testID.String(), nil)
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
			return errors.New("location has children")
		}

		req := httptest.NewRequest(http.MethodDelete, "/locations/"+uuid.New().String(), nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Errorf("expected 500, got %d", resp.StatusCode)
		}
	})
}