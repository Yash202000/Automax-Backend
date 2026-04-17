package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/automax/backend/internal/database"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// 1. Create a mock for ActionLogService to prevent nil pointer panics
type mockActionLogService struct {
	ActionLogService // Embedding satisfy interface
}

func (m *mockActionLogService) LogAction(ctx context.Context, params *LogActionParams) error {
	return nil // Do nothing during tests
}

// Add mock for SessionStore
type mockSessionStore struct {
	database.SessionStore
	blacklistFunc func(token string) error
}

func (m *mockSessionStore) BlacklistToken(ctx context.Context, token string, expiration time.Duration) error {
	if m.blacklistFunc != nil {
		return m.blacklistFunc(token)
	}
	return nil
}

// 2. Create a mock for UserRepo
type mockUserRepo struct {
	repository.UserRepository
	existsByEmailFunc    func(email string) (bool, error)
	existsByUsernameFunc func(username string) (bool, error)
	findByEmailFunc      func(email string) (*models.User, error)
	findByPhoneFunc      func(phone string) (*models.User, error)
	findByIDFunc         func(id uuid.UUID) (*models.User, error)
}

func (m *mockUserRepo) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	return m.existsByEmailFunc(email)
}
func (m *mockUserRepo) ExistsByUsername(ctx context.Context, username string) (bool, error) {
	if m.existsByUsernameFunc != nil {
		return m.existsByUsernameFunc(username)
	}
	return false, nil
}
func (m *mockUserRepo) FindByEmailWithRelations(ctx context.Context, email string) (*models.User, error) {
	return m.findByEmailFunc(email)
}
func (m *mockUserRepo) FindByPhoneWithRelations(ctx context.Context, phone string) (*models.User, error) {
	return m.findByPhoneFunc(phone)
}
func (m *mockUserRepo) FindByIDWithRelations(ctx context.Context, id uuid.UUID) (*models.User, error) {
	return m.findByIDFunc(id)
}

func TestUserService_Logic(t *testing.T) {
	repo := &mockUserRepo{}
	logSvc := &mockActionLogService{} // Initialize the mock

	// 3. Inject the mock logSvc into the userService
	svc := &userService{
		userRepo:         repo,
		actionLogService: logSvc, // This prevents the panic
	}

	// --- REGISTER LOGIC TESTS ---
	t.Run("Register_Fail_DuplicateEmail", func(t *testing.T) {
		repo.existsByEmailFunc = func(email string) (bool, error) {
			return true, nil
		}
		req := &models.UserRegisterRequest{Email: "taken@test.com"}
		_, err := svc.Register(context.Background(), req)
		if err == nil || err.Error() != "email already exists" {
			t.Errorf("Expected email exists error, got %v", err)
		}
	})

	t.Run("Register_Fail_DuplicateUsername", func(t *testing.T) {
		repo.existsByEmailFunc = func(email string) (bool, error) { return false, nil }
		repo.existsByUsernameFunc = func(username string) (bool, error) { return true, nil }
		req := &models.UserRegisterRequest{Email: "new@test.com", Username: "taken"}
		_, err := svc.Register(context.Background(), req)
		if err == nil || err.Error() != "username already exists" {
			t.Errorf("Expected username exists error, got %v", err)
		}
	})

	// --- LOGIN LOGIC TESTS ---
	t.Run("Login_Fail_UserNotFound", func(t *testing.T) {
		repo.findByEmailFunc = func(email string) (*models.User, error) {
			return nil, gorm.ErrRecordNotFound
		}
		req := &models.UserLoginRequest{Email: "missing@test.com", Password: "any"}
		_, err := svc.Login(context.Background(), req)
		if err == nil || err.Error() != "invalid credentials" {
			t.Errorf("Expected invalid credentials, got %v", err)
		}
	})

	t.Run("Login_Fail_DeactivatedAccount", func(t *testing.T) {
		repo.findByEmailFunc = func(email string) (*models.User, error) {
			return &models.User{
				ID:       uuid.New(),
				Email:    email,
				Password: "hashed_dummy_password",
				IsActive: false,
			}, nil
		}
		req := &models.UserLoginRequest{Email: "inactive@test.com", Password: "any"}
		_, err := svc.Login(context.Background(), req)
		if err == nil {
			t.Errorf("Expected error for deactivated account, got nil")
		}
	})

	t.Run("MobileValidation_Fail_NotVerified", func(t *testing.T) {
		repo.findByPhoneFunc = func(phone string) (*models.User, error) {
			return &models.User{
				Phone:          phone,
				MobileVerified: false,
			}, nil
		}
		_, err := svc.ValidateMobileForLogin(context.Background(), "+123456789")
		if err == nil || err.Error() != "mobile number is not verified. Please contact administrator to verify your mobile number" {
			t.Errorf("Expected verification error, got %v", err)
		}
	})

	// --- REFRESH TOKEN TESTS ---
	t.Run("RefreshToken_Fail_UserNotFound", func(t *testing.T) {
		repo.findByIDFunc = func(id uuid.UUID) (*models.User, error) {
			return nil, errors.New("user not found")
		}
		_, err := svc.RefreshToken(context.Background(), "some-token")
		if err == nil {
			t.Error("Expected error for non-existent user, got nil")
		}
	})
}
