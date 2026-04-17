package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/validation"
	"github.com/gofiber/fiber/v2"
)

// mockUserService embeds the interface to satisfy all methods (ChangePassword, etc.) automatically.
type mockUserService struct {
	services.UserService
	registerFunc       func(ctx context.Context, req *models.UserRegisterRequest) (*models.AuthResponse, error)
	loginFunc          func(ctx context.Context, req *models.UserLoginRequest) (*models.AuthResponse, error)
	validateMobileFunc func(ctx context.Context, phone string) (*models.UserResponse, error)
	refreshFunc        func(ctx context.Context, refreshToken string) (*models.AuthResponse, error)
}

// Override Register
func (m *mockUserService) Register(ctx context.Context, req *models.UserRegisterRequest) (*models.AuthResponse, error) {
	if m.registerFunc != nil {
		return m.registerFunc(ctx, req)
	}
	return nil, nil
}

// Override Login
func (m *mockUserService) Login(ctx context.Context, req *models.UserLoginRequest) (*models.AuthResponse, error) {
	if m.loginFunc != nil {
		return m.loginFunc(ctx, req)
	}
	return nil, nil
}

// Override ValidateMobile
func (m *mockUserService) ValidateMobileForLogin(ctx context.Context, phone string) (*models.UserResponse, error) {
	if m.validateMobileFunc != nil {
		return m.validateMobileFunc(ctx, phone)
	}
	return nil, nil
}

func (m *mockUserService) RefreshToken(ctx context.Context, refreshToken string) (*models.AuthResponse, error) {
	return m.refreshFunc(ctx, refreshToken)
}

// TestMain runs once to setup global state like validators
func TestMain(m *testing.M) {
	validation.InitValidatorRegistry()
	os.Exit(m.Run())
}

func TestUserHandler(t *testing.T) {
	app := fiber.New()
	mSvc := &mockUserService{}
	h := NewUserHandler(mSvc, nil)

	// Routes
	app.Post("/register", h.Register)
	app.Post("/login", h.Login)
	app.Post("/refresh", h.RefreshToken)

	// --- REGISTER TEST CASES ---
	t.Run("Register_Success", func(t *testing.T) {
		mSvc.registerFunc = func(ctx context.Context, req *models.UserRegisterRequest) (*models.AuthResponse, error) {
			return &models.AuthResponse{Token: "reg-token"}, nil
		}
		payload := models.UserRegisterRequest{
			Email:     "new@test.com",
			Username:  "newuser",
			Password:  "Pass123!",
			FirstName: "John",
			LastName:  "Doe",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/register", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req)
		if resp.StatusCode != fiber.StatusCreated {
			t.Errorf("Register expected 201, got %d", resp.StatusCode)
		}
	})

	t.Run("Register_Validation_Fail", func(t *testing.T) {
		payload := models.UserRegisterRequest{Username: "missing_email"}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/register", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req)
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("Register validation expected 400, got %d", resp.StatusCode)
		}
	})

	// --- LOGIN TEST CASES ---
	t.Run("Login_Email_Success", func(t *testing.T) {
		mSvc.loginFunc = func(ctx context.Context, req *models.UserLoginRequest) (*models.AuthResponse, error) {
			return &models.AuthResponse{Token: "login-token"}, nil
		}
		payload := map[string]string{
			"email":    "user@test.com",
			"password": "password123",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/login", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req)
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("Login expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("Login_Mobile_Validation_Flow", func(t *testing.T) {
		mSvc.validateMobileFunc = func(ctx context.Context, phone string) (*models.UserResponse, error) {
			return &models.UserResponse{Phone: phone}, nil
		}
		// Only providing phone triggers "mobile_validation" logic in handler
		payload := map[string]string{"phone": "+123456789"}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/login", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req)
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("Mobile validation expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("Login_Service_Unauthorized", func(t *testing.T) {
		mSvc.loginFunc = func(ctx context.Context, req *models.UserLoginRequest) (*models.AuthResponse, error) {
			return nil, errors.New("invalid credentials")
		}
		payload := map[string]string{
			"email":    "wrong@test.com",
			"password": "wrong",
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/login", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req)
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Errorf("Login failure expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("Login_Invalid_Payload", func(t *testing.T) {
		// No email, no phone
		payload := map[string]string{"foo": "bar"}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/login", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, _ := app.Test(req)
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("Invalid login payload expected 400, got %d", resp.StatusCode)
		}
	})

	// --- REFRESH TOKEN TESTS ---
	t.Run("Refresh_Success", func(t *testing.T) {
		mSvc.refreshFunc = func(ctx context.Context, token string) (*models.AuthResponse, error) {
			return &models.AuthResponse{Token: "new-access-token"}, nil
		}
		payload := models.RefreshTokenRequest{RefreshToken: "valid-refresh-token"}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/refresh", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		resp, _ := app.Test(req)
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("Refresh expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("Refresh_Invalid_Token_401", func(t *testing.T) {
		mSvc.refreshFunc = func(ctx context.Context, token string) (*models.AuthResponse, error) {
			return nil, errors.New("invalid or expired refresh token")
		}
		payload := models.RefreshTokenRequest{RefreshToken: "expired-token"}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/refresh", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		resp, _ := app.Test(req)
		if resp.StatusCode != fiber.StatusUnauthorized {
			t.Errorf("Refresh failure expected 401, got %d", resp.StatusCode)
		}
	})

	t.Run("Refresh_Validation_Error_400", func(t *testing.T) {
		// Empty payload to trigger validation error
		payload := models.RefreshTokenRequest{RefreshToken: ""}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", "/refresh", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")

		resp, _ := app.Test(req)
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("Empty refresh token expected 400, got %d", resp.StatusCode)
		}
	})

}
