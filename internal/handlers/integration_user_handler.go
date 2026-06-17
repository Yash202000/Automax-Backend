package handlers

import (
	"strings"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type IntegrationUserHandler struct {
	userService        services.UserService
	roleRepo           repository.RoleRepository
	departmentRepo     repository.DepartmentRepository
	locationRepo       repository.LocationRepository
	classificationRepo repository.ClassificationRepository
}

func NewIntegrationUserHandler(
	userService services.UserService,
	roleRepo repository.RoleRepository,
	departmentRepo repository.DepartmentRepository,
	locationRepo repository.LocationRepository,
	classificationRepo repository.ClassificationRepository,
) *IntegrationUserHandler {
	return &IntegrationUserHandler{
		userService:        userService,
		roleRepo:           roleRepo,
		departmentRepo:     departmentRepo,
		locationRepo:       locationRepo,
		classificationRepo: classificationRepo,
	}
}

type IntegrationCreateUserRequest struct {
	Email     string   `json:"email" validate:"required,email"`
	Username  string   `json:"username" validate:"required,min=3,max=50"`
	Password  string   `json:"password" validate:"required,min=6"`
	FirstName string   `json:"first_name" validate:"max=100"`
	LastName  string   `json:"last_name" validate:"max=100"`
	Phone     string   `json:"phone" validate:"max=20"`
	Roles     []string `json:"roles" validate:"required,min=1"`
}

type IntegrationCreateUserResponse struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Username  string    `json:"username"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Phone     string    `json:"phone"`
	IsActive  bool      `json:"is_active"`
	Roles     []string  `json:"roles"`
}

// CreateUser creates a user with the specified roles and automatically assigns
// all departments, locations, and classifications in the system.
func (h *IntegrationUserHandler) CreateUser(c *fiber.Ctx) error {
	var req IntegrationCreateUserRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if len(req.Roles) == 0 {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "At least one role is required")
	}

	// Resolve role codes to IDs
	var roleIDs []uuid.UUID
	var roleNames []string
	for _, roleCode := range req.Roles {
		code := strings.TrimSpace(strings.ToLower(roleCode))
		if code == "" {
			continue
		}
		role, err := h.roleRepo.FindByCode(c.UserContext(), code)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "Role not found: "+roleCode)
		}
		if !role.IsActive {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "Role is inactive: "+roleCode)
		}
		roleIDs = append(roleIDs, role.ID)
		roleNames = append(roleNames, role.Name)
	}

	// Fetch all departments
	departments, err := h.departmentRepo.List(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to fetch departments")
	}
	departmentIDs := make([]uuid.UUID, len(departments))
	for i, d := range departments {
		departmentIDs[i] = d.ID
	}

	// Fetch all locations
	locations, err := h.locationRepo.List(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to fetch locations")
	}
	locationIDs := make([]uuid.UUID, len(locations))
	for i, l := range locations {
		locationIDs[i] = l.ID
	}

	// Fetch all classifications
	classifications, err := h.classificationRepo.List(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to fetch classifications")
	}
	classificationIDs := make([]uuid.UUID, len(classifications))
	for i, cl := range classifications {
		classificationIDs[i] = cl.ID
	}

	// Build the register request with all access
	registerReq := &models.UserRegisterRequest{
		Email:             req.Email,
		Username:          req.Username,
		Password:          req.Password,
		FirstName:         req.FirstName,
		LastName:          req.LastName,
		Phone:             req.Phone,
		RoleIDs:           roleIDs,
		DepartmentIDs:     departmentIDs,
		LocationIDs:       locationIDs,
		ClassificationIDs: classificationIDs,
	}

	authResp, err := h.userService.Register(c.UserContext(), registerReq)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "already in use") {
			return utils.ErrorResponse(c, fiber.StatusConflict, err.Error())
		}
		if strings.Contains(err.Error(), "user_limit_reached") {
			return utils.ErrorResponse(c, fiber.StatusForbidden, err.Error())
		}
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	resp := IntegrationCreateUserResponse{
		ID:        authResp.User.ID,
		Email:     authResp.User.Email,
		Username:  authResp.User.Username,
		FirstName: authResp.User.FirstName,
		LastName:  authResp.User.LastName,
		Phone:     authResp.User.Phone,
		IsActive:  authResp.User.IsActive,
		Roles:     roleNames,
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "User created successfully", resp)
}
