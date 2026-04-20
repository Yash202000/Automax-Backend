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

type mockDepartmentRepo struct {
	repository.DepartmentRepository
	createFunc         func(ctx context.Context, department *models.Department) error
	findByIDFunc       func(ctx context.Context, id uuid.UUID) (*models.Department, error)
	updateFunc        func(ctx context.Context, department *models.Department) error
	deleteFunc        func(ctx context.Context, id uuid.UUID) error
	listFunc          func(ctx context.Context) ([]models.Department, error)
	getTreeFunc       func(ctx context.Context) ([]models.Department, error)
	getByParentIDFunc func(ctx context.Context, parentID *uuid.UUID) ([]models.Department, error)
	createdDepartment *models.Department
}

func (m *mockDepartmentRepo) Create(ctx context.Context, department *models.Department) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, department)
	}
	department.ID = uuid.New()
	m.createdDepartment = department
	return nil
}

func (m *mockDepartmentRepo) FindByID(ctx context.Context, id uuid.UUID) (*models.Department, error) {
	if m.findByIDFunc != nil {
		return m.findByIDFunc(ctx, id)
	}
	if m.createdDepartment != nil && m.createdDepartment.ID == id {
		return m.createdDepartment, nil
	}
	return nil, errors.New("not found")
}

func (m *mockDepartmentRepo) Update(ctx context.Context, department *models.Department) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, department)
	}
	return nil
}

func (m *mockDepartmentRepo) Delete(ctx context.Context, id uuid.UUID) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

func (m *mockDepartmentRepo) List(ctx context.Context) ([]models.Department, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx)
	}
	return []models.Department{}, nil
}

func (m *mockDepartmentRepo) GetTree(ctx context.Context) ([]models.Department, error) {
	if m.getTreeFunc != nil {
		return m.getTreeFunc(ctx)
	}
	return []models.Department{}, nil
}

func (m *mockDepartmentRepo) GetByParentID(ctx context.Context, parentID *uuid.UUID) ([]models.Department, error) {
	if m.getByParentIDFunc != nil {
		return m.getByParentIDFunc(ctx, parentID)
	}
	return []models.Department{}, nil
}

func TestDepartmentHandler(t *testing.T) {
	mRepo := &mockDepartmentRepo{}
	h := NewDepartmentHandler(mRepo)

	app := fiber.New()
	app.Get("/departments", h.List)
	app.Get("/departments/tree", h.GetTree)
	app.Post("/departments", h.Create)
	app.Put("/departments/:id", h.Update)
	app.Delete("/departments/:id", h.Delete)

	t.Run("List_Success", func(t *testing.T) {
		mRepo.listFunc = func(ctx context.Context) ([]models.Department, error) {
			return []models.Department{
				{ID: uuid.New(), Name: "IT Department", Code: "IT"},
			}, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/departments", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("GetTree_Success", func(t *testing.T) {
		mRepo.getTreeFunc = func(ctx context.Context) ([]models.Department, error) {
			return []models.Department{
				{ID: uuid.New(), Name: "Root Department"},
			}, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/departments/tree", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("Create_Success", func(t *testing.T) {
		mRepo.createFunc = func(ctx context.Context, department *models.Department) error {
			department.ID = uuid.New()
			mRepo.createdDepartment = department
			return nil
		}
		mRepo.findByIDFunc = func(ctx context.Context, id uuid.UUID) (*models.Department, error) {
			return mRepo.createdDepartment, nil
		}

		payload := map[string]interface{}{
			"name": "New Department",
			"code": "NEW",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/departments", bytes.NewBuffer(body))
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
		req := httptest.NewRequest(http.MethodPost, "/departments", bytes.NewBuffer(body))
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
		mRepo.findByIDFunc = func(ctx context.Context, id uuid.UUID) (*models.Department, error) {
			return &models.Department{ID: id, Name: "Test"}, nil
		}
		mRepo.updateFunc = func(ctx context.Context, department *models.Department) error {
			return nil
		}

		payload := map[string]string{"name": "Updated Department"}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPut, "/departments/"+testID.String(), bytes.NewBuffer(body))
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

		req := httptest.NewRequest(http.MethodDelete, "/departments/"+testID.String(), nil)
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
			return errors.New("department has users")
		}

		req := httptest.NewRequest(http.MethodDelete, "/departments/"+uuid.New().String(), nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Errorf("expected 500, got %d", resp.StatusCode)
		}
	})
}