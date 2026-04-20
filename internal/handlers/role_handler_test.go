package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type mockRoleRepo struct {
	repository.RoleRepository
	createFunc       func(ctx context.Context, role *models.Role) error
	findByIDFunc    func(ctx context.Context, id uuid.UUID) (*models.Role, error)
	updateFunc      func(ctx context.Context, role *models.Role) error
	deleteFunc      func(ctx context.Context, id uuid.UUID) error
	listFunc        func(ctx context.Context) ([]models.Role, error)
	assignPermsFunc func(ctx context.Context, roleID uuid.UUID, permissionIDs []uuid.UUID) error
	getPermsFunc    func(ctx context.Context, roleID uuid.UUID) ([]models.Permission, error)
	createdRole *models.Role
}

type mockPermRepo struct {
	repository.PermissionRepository
	listFunc func(ctx context.Context) ([]models.Permission, error)
}

func (m *mockRoleRepo) Create(ctx context.Context, role *models.Role) error {
	if m.createFunc != nil {
		return m.createFunc(ctx, role)
	}
	role.ID = uuid.New()
	m.createdRole = role
	return nil
}

func (m *mockRoleRepo) FindByID(ctx context.Context, id uuid.UUID) (*models.Role, error) {
	if m.findByIDFunc != nil {
		return m.findByIDFunc(ctx, id)
	}
	if m.createdRole != nil && m.createdRole.ID == id {
		return m.createdRole, nil
	}
	return nil, nil
}

func (m *mockRoleRepo) Update(ctx context.Context, role *models.Role) error {
	if m.updateFunc != nil {
		return m.updateFunc(ctx, role)
	}
	return nil
}

func (m *mockRoleRepo) Delete(ctx context.Context, id uuid.UUID) error {
	if m.deleteFunc != nil {
		return m.deleteFunc(ctx, id)
	}
	return nil
}

func (m *mockRoleRepo) List(ctx context.Context) ([]models.Role, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx)
	}
	return []models.Role{}, nil
}

func (m *mockRoleRepo) AssignPermissions(ctx context.Context, roleID uuid.UUID, permissionIDs []uuid.UUID) error {
	if m.assignPermsFunc != nil {
		return m.assignPermsFunc(ctx, roleID, permissionIDs)
	}
	return nil
}

func (m *mockRoleRepo) GetPermissions(ctx context.Context, roleID uuid.UUID) ([]models.Permission, error) {
	if m.getPermsFunc != nil {
		return m.getPermsFunc(ctx, roleID)
	}
	return nil, nil
}

func (m *mockPermRepo) List(ctx context.Context) ([]models.Permission, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx)
	}
	return []models.Permission{}, nil
}

func TestRoleHandler(t *testing.T) {
	mRoleRepo := &mockRoleRepo{}
	mPermRepo := &mockPermRepo{}
	h := NewRoleHandler(mRoleRepo, mPermRepo)

	app := fiber.New()
	app.Get("/roles", h.ListRoles)
	app.Post("/roles", h.CreateRole)
	app.Put("/roles/:id", h.UpdateRole)
	app.Delete("/roles/:id", h.DeleteRole)
	app.Post("/roles/:id/permissions", h.AssignPermissions)

	t.Run("ListRoles_Success", func(t *testing.T) {
		mRoleRepo.listFunc = func(ctx context.Context) ([]models.Role, error) {
			return []models.Role{
				{ID: uuid.New(), Name: "Admin", Code: "ADMIN"},
			}, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/roles", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("CreateRole_Success", func(t *testing.T) {
		mRoleRepo.createFunc = func(ctx context.Context, role *models.Role) error {
			role.ID = uuid.New()
			mRoleRepo.createdRole = role
			return nil
		}
		mRoleRepo.findByIDFunc = func(ctx context.Context, id uuid.UUID) (*models.Role, error) {
			return mRoleRepo.createdRole, nil
		}

		payload := map[string]interface{}{
			"name": "Manager",
			"code": "MGR",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/roles", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusCreated {
			t.Errorf("expected 201, got %d", resp.StatusCode)
		}
	})

	t.Run("CreateRole_ValidationFailure", func(t *testing.T) {
		payload := map[string]string{}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/roles", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("UpdateRole_Success", func(t *testing.T) {
		testID := uuid.New()
		mRoleRepo.findByIDFunc = func(ctx context.Context, id uuid.UUID) (*models.Role, error) {
			return &models.Role{ID: id, Name: "Manager"}, nil
		}
		mRoleRepo.updateFunc = func(ctx context.Context, role *models.Role) error {
			return nil
		}

		payload := map[string]string{"name": "Super Admin"}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPut, "/roles/"+testID.String(), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("DeleteRole_Success", func(t *testing.T) {
		testID := uuid.New()
		mRoleRepo.findByIDFunc = func(ctx context.Context, id uuid.UUID) (*models.Role, error) {
			return &models.Role{ID: id, Name: "Manager", IsSystem: false}, nil
		}
		mRoleRepo.deleteFunc = func(ctx context.Context, id uuid.UUID) error {
			return nil
		}

		req := httptest.NewRequest(http.MethodDelete, "/roles/"+testID.String(), nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("DeleteRole_SystemRoleForbidden", func(t *testing.T) {
		testID := uuid.New()
		mRoleRepo.findByIDFunc = func(ctx context.Context, id uuid.UUID) (*models.Role, error) {
			return &models.Role{ID: id, Name: "Admin", IsSystem: true}, nil
		}

		req := httptest.NewRequest(http.MethodDelete, "/roles/"+testID.String(), nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusForbidden {
			t.Errorf("expected 403, got %d", resp.StatusCode)
		}
	})

	t.Run("AssignPermissions_Success", func(t *testing.T) {
		testID := uuid.New()
		mRoleRepo.findByIDFunc = func(ctx context.Context, id uuid.UUID) (*models.Role, error) {
			return &models.Role{ID: id, Name: "Admin"}, nil
		}
		mRoleRepo.assignPermsFunc = func(ctx context.Context, roleID uuid.UUID, permissionIDs []uuid.UUID) error {
			return nil
		}

		payload := map[string][]string{
			"permission_ids": {uuid.New().String()},
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/roles/"+testID.String()+"/permissions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})
}