package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/internal/storage"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

type UserHandler struct {
	userService services.UserService
	storage     *storage.MinIOStorage
	validator   *validator.Validate
	redisClient *redis.Client
	cfg         *config.Config
	wsHub       *services.WSHub
}

func NewUserHandler(userService services.UserService, storage *storage.MinIOStorage, redisClient *redis.Client, cfg *config.Config, wsHub *services.WSHub) *UserHandler {
	return &UserHandler{
		userService: userService,
		storage:     storage,
		validator:   validator.New(),
		redisClient: redisClient,
		cfg:         cfg,
		wsHub:       wsHub,
	}
}

func (h *UserHandler) Register(c *fiber.Ctx) error {
	var req models.UserRegisterRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	response, err := h.userService.Register(c.UserContext(), &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "user_registered"), response)
}

func (h *UserHandler) Login(c *fiber.Ctx) error {
	var req models.UserLoginRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	// Validate login request - must have either email+password OR phone (with or without OTP)
	loginType := req.LoginType()
	if loginType == "unknown" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "provide_email_or_phone"))
	}

	// For mobile login, first check if only phone is provided (pre-validation step)
	if loginType == "mobile_validation" {
		// Pre-validation: Check if mobile exists and is verified
		response, err := h.userService.ValidateMobileForLogin(c.UserContext(), req.Phone)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
		}
		return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "mobile_verified_send_otp"), response)
	}

	ctx := c.UserContext()
	identifier := c.Locals("login_user_identifier")
	response, err := h.userService.Login(ctx, &req)
	//response, err := h.userService.Login(c.UserContext(), &req)
	if err != nil {
		if id, ok := identifier.(string); ok && id != "" {
			h.HandleLoginFailure(ctx, id)
		}
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, err.Error())
	}

	//  LOGIN SUCCESS
	if id, ok := identifier.(string); ok && id != "" {
		h.HandleLoginSuccess(ctx, id)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "login_successful"), response)
}

func (h *UserHandler) Logout(c *fiber.Ctx) error {
	if err := h.userService.Logout(c.UserContext()); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_logout"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "logout_successful"), nil)
}

func (h *UserHandler) RefreshToken(c *fiber.Ctx) error {
	var req models.RefreshTokenRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	response, err := h.userService.RefreshToken(c.UserContext(), req.RefreshToken, req.RememberMe)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "token_refreshed"), response)
}

func (h *UserHandler) GetProfile(c *fiber.Ctx) error {
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	response, err := h.userService.GetProfile(c.UserContext(), userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "user_not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "profile_retrieved"), response)
}

func (h *UserHandler) UpdateProfile(c *fiber.Ctx) error {
	var req models.UserUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	response, err := h.userService.UpdateProfile(c.UserContext(), &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "profile_updated"), response)
}

func (h *UserHandler) ChangePassword(c *fiber.Ctx) error {
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	var req models.ChangePasswordRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	if err := h.userService.ChangePassword(c.UserContext(), userID, &req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "password_changed"), nil)
}

func (h *UserHandler) AdminResetPassword(c *fiber.Ctx) error {

	adminUserID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	targetUserIDStr := c.Params("userID")
	targetUserID, err := uuid.Parse(targetUserIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_user_id"))
	}

	var req struct {
		NewPassword string `json:"new_password"`
	}

	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request"))
	}

	err = h.userService.AdminResetPassword(c.UserContext(), adminUserID, targetUserID, req.NewPassword)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "password_reset_successful"), nil)
}

func (h *UserHandler) UploadAvatar(c *fiber.Ctx) error {
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	file, err := c.FormFile("avatar")
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "no_file_uploaded"))
	}

	if file.Size > 5*1024*1024 {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "file_size_5mb"))
	}

	contentType := file.Header.Get("Content-Type")
	if contentType != "image/jpeg" && contentType != "image/png" && contentType != "image/gif" && contentType != "image/webp" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_file_type_avatar"))
	}

	src, err := file.Open()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_open_file"))
	}
	defer src.Close()

	response, err := h.userService.UploadAvatar(c.UserContext(), userID, src, file)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_upload_avatar"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "avatar_uploaded"), response)
}

func (h *UserHandler) DeleteAccount(c *fiber.Ctx) error {
	if err := h.userService.DeleteUser(c.UserContext()); err != nil {
		if errors.Is(err, services.ErrUserAssigned) {
			return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "cannot_delete_user_assigned"))
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_delete_account"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "account_deleted"), nil)
}

// AdminDeleteUser handles DELETE /admin/users/:id (admin deletes another user)
func (h *UserHandler) AdminDeleteUser(c *fiber.Ctx) error {
	userIDStr := c.Params("id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_user_id"))
	}

	if err := h.userService.AdminDeleteUser(c.UserContext(), userID); err != nil {
		if errors.Is(err, services.ErrUserAssigned) {
			return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "cannot_delete_user_assigned"))
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_delete_user"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "user_deleted"), nil)
}

func parseUUIDList(raw string) []uuid.UUID {
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	result := make([]uuid.UUID, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if id, err := uuid.Parse(p); err == nil {
			result = append(result, id)
		}
	}
	return result
}

func (h *UserHandler) ListUsers(c *fiber.Ctx) error {
	page, _ := strconv.Atoi(c.Query("page", "1"))
	limit, _ := strconv.Atoi(c.Query("limit", "10"))
	search := c.Query("search", "")
	phone := strings.TrimSpace(c.Query("phone", ""))
	extension := strings.TrimSpace(c.Query("extension", ""))
	callStatus := strings.TrimSpace(c.Query("call_status", ""))

	roleIDs := parseUUIDList(c.Query("role_ids", ""))
	departmentIDs := parseUUIDList(c.Query("department_ids", ""))
	locationIDs := parseUUIDList(c.Query("location_ids", ""))
	classificationIDs := parseUUIDList(c.Query("classification_ids", ""))

	// Department-scoped: users with view_department_only see only their department's users.
	// When ENFORCE_USER_ACCESS_SCOPE=true, uses M2M relationships for all three dimensions.
	// Otherwise falls back to legacy single DeptManager fields.
	user, _ := c.Locals(constants.ContextKeys.User).(*models.User)
	if user != nil && !user.IsSuperAdmin && user.HasPermission("users:view_department_only") {
		enforceAccessScope := strings.EqualFold(strings.TrimSpace(os.Getenv("ENFORCE_USER_ACCESS_SCOPE")), "true")

		if enforceAccessScope {
			// Load full user with M2M relations
			fullUser, err := h.userService.FindByIDWithRelations(c.UserContext(), user.ID)
			if err == nil && fullUser != nil {
				// Departments: use ScopeDepartmentIDs (priority chain)
				scopeDeptIDs := fullUser.ScopeDepartmentIDs()
				if len(scopeDeptIDs) > 0 {
					departmentIDs = scopeDeptIDs
				}
				// Classifications: M2M (empty = no restriction)
				if len(fullUser.Classifications) > 0 {
					classIDs := make([]uuid.UUID, 0, len(fullUser.Classifications))
					for _, c := range fullUser.Classifications {
						classIDs = append(classIDs, c.ID)
					}
					classificationIDs = classIDs
				}
				// Locations: M2M (empty = no restriction)
				if len(fullUser.Locations) > 0 {
					locIDs := make([]uuid.UUID, 0, len(fullUser.Locations))
					for _, l := range fullUser.Locations {
						locIDs = append(locIDs, l.ID)
					}
					locationIDs = locIDs
				}
			}
		} else {
			// Legacy: single DeptManager fields
			scopeDeptID := user.ScopeDepartmentID()
			if scopeDeptID != nil {
				departmentIDs = []uuid.UUID{*scopeDeptID}
			}
			if user.DeptManagerClassificationID != nil {
				classificationIDs = []uuid.UUID{*user.DeptManagerClassificationID}
			}
			if user.DeptManagerLocationID != nil {
				locationIDs = []uuid.UUID{*user.DeptManagerLocationID}
			}
		}
	}

	page = max(page, 1)
	if limit < 1 {
		limit = 10
	} else if limit > 1000 {
		limit = 1000
	}

	users, total, err := h.userService.ListUsers(c.UserContext(), page, limit, search, phone, extension, callStatus, roleIDs, departmentIDs, locationIDs, classificationIDs, false)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_fetch_users"))
	}

	return utils.PaginatedSuccessResponse(c, users, page, limit, total)
}

func (h *UserHandler) GetUser(c *fiber.Ctx) error {
	userIDStr := c.Params("id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_user_id"))
	}

	response, err := h.userService.GetUserByID(c.UserContext(), userID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "user_not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "user_retrieved"), response)
}

func (h *UserHandler) GetManagerScope(c *fiber.Ctx) error {
	userLocal, _ := c.Locals(constants.ContextKeys.User).(*models.User)
	if userLocal == nil {
		return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "unauthorized"))
	}

	// Reload with all scope relations for complete response
	user, err := h.userService.GetUserByID(c.UserContext(), userLocal.ID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "user_not_found"))
	}

	if !userLocal.HasPermission("users:view_department_only") {
		return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(c.UserContext(), "not_department_manager"))
	}

	scope := map[string]interface{}{
		"is_department_scoped": true,
	}
	if user.DeptManagerDepartmentID != nil {
		scope["department_id"] = user.DeptManagerDepartmentID
		scope["department"] = user.DeptManagerDepartment
	}
	if user.DeptManagerClassificationID != nil {
		scope["classification_id"] = user.DeptManagerClassificationID
		scope["classification"] = user.DeptManagerClassification
	}
	if user.DeptManagerLocationID != nil {
		scope["location_id"] = user.DeptManagerLocationID
		scope["location"] = user.DeptManagerLocation
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "scope_retrieved"), scope)
}

func (h *UserHandler) AdminCreateUser(c *fiber.Ctx) error {
	var req models.UserRegisterRequest

	contentType := string(c.Request().Header.ContentType())

	// Check if it's multipart form data (with file attachment)
	if strings.Contains(contentType, "multipart/form-data") {
		// Parse form fields manually for multipart
		req.Email = c.FormValue("email")
		req.Username = c.FormValue("username")
		req.Password = c.FormValue("password")
		req.FirstName = c.FormValue("first_name")
		req.LastName = c.FormValue("last_name")
		req.Phone = c.FormValue("phone")
		req.Extension = c.FormValue("extension")

		// Parse optional UUID fields
		if deptID := c.FormValue("department_id"); deptID != "" {
			if id, err := uuid.Parse(deptID); err == nil {
				req.DepartmentID = &id
			}
		}
		if locID := c.FormValue("location_id"); locID != "" {
			if id, err := uuid.Parse(locID); err == nil {
				req.LocationID = &id
			}
		}

		// Parse array fields (JSON format in form)
		if roleIDsStr := c.FormValue("role_ids"); roleIDsStr != "" {
			var roleIDStrings []string
			if err := json.Unmarshal([]byte(roleIDsStr), &roleIDStrings); err == nil {
				for _, idStr := range roleIDStrings {
					if id, err := uuid.Parse(idStr); err == nil {
						req.RoleIDs = append(req.RoleIDs, id)
					}
				}
			}
		}
		if deptIDsStr := c.FormValue("department_ids"); deptIDsStr != "" {
			var deptIDStrings []string
			if err := json.Unmarshal([]byte(deptIDsStr), &deptIDStrings); err == nil {
				for _, idStr := range deptIDStrings {
					if id, err := uuid.Parse(idStr); err == nil {
						req.DepartmentIDs = append(req.DepartmentIDs, id)
					}
				}
			}
		}
		if locIDsStr := c.FormValue("location_ids"); locIDsStr != "" {
			var locIDStrings []string
			if err := json.Unmarshal([]byte(locIDsStr), &locIDStrings); err == nil {
				for _, idStr := range locIDStrings {
					if id, err := uuid.Parse(idStr); err == nil {
						req.LocationIDs = append(req.LocationIDs, id)
					}
				}
			}
		}
		if classIDsStr := c.FormValue("classification_ids"); classIDsStr != "" {
			var classIDStrings []string
			if err := json.Unmarshal([]byte(classIDsStr), &classIDStrings); err == nil {
				for _, idStr := range classIDStrings {
					if id, err := uuid.Parse(idStr); err == nil {
						req.ClassificationIDs = append(req.ClassificationIDs, id)
					}
				}
			}
		}

	} else {
		// Regular JSON body
		if err := c.BodyParser(&req); err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
		}
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	response, err := h.userService.Register(c.UserContext(), &req)
	if err != nil {
		if errors.Is(err, services.ErrUserLimitReached) {
			return c.Status(fiber.StatusForbidden).JSON(fiber.Map{
				"success": false,
				"error":   "user_limit_reached",
				"message": err.Error(),
			})
		}
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	// Handle avatar upload if provided (multipart form data)
	if strings.Contains(contentType, "multipart/form-data") {
		file, err := c.FormFile("avatar")
		if err == nil && file != nil {
			// Open the file
			src, err := file.Open()
			if err == nil {
				defer src.Close()

				// Upload to storage
				folder := fmt.Sprintf("avatars/%s", response.User.ID)
				filePath, err := h.storage.UploadFile(c.UserContext(), src, file, folder)
				if err == nil {
					// Update user's avatar URL
					avatarURL := filePath
					if updateErr := h.userService.UpdateAvatar(c.UserContext(), response.User.ID, avatarURL); updateErr == nil {
						response.User.Avatar = avatarURL
					}
				}
			}
		}
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "user_created"), response)
}

func (h *UserHandler) AdminUpdateUser(c *fiber.Ctx) error {
	userIDStr := c.Params("id")
	userID, err := uuid.Parse(userIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_user_id"))
	}

	var req models.UserUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	response, err := h.userService.UpdateAdminProfile(c.UserContext(), userID, &req)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	// If the admin changed this user's roles, nudge their live clients to refresh
	// permissions so the UI reflects the new access without a manual re-login.
	if req.RoleIDs != nil && h.wsHub != nil {
		h.wsHub.BroadcastToUser(userID, "permissions_changed", fiber.Map{"reason": "roles_updated"})
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "user_updated"), response)
}

// MatchUsers finds users that match the given criteria (role, classification, location, department)
func (h *UserHandler) MatchUsers(c *fiber.Ctx) error {
	var req models.UserMatchRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	var classificationID, locationID, departmentID, excludeUserID *uuid.UUID
	var roleIDs []uuid.UUID

	for _, idStr := range req.RoleIDs {
		if idStr == "" {
			continue
		}
		id, err := uuid.Parse(idStr)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.Tf(c.UserContext(), "invalid_role_id_detail", idStr))
		}
		roleIDs = append(roleIDs, id)
	}

	if req.ClassificationID != nil && *req.ClassificationID != "" {
		id, err := uuid.Parse(*req.ClassificationID)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_classification_id"))
		}
		classificationID = &id
	}

	if req.LocationID != nil && *req.LocationID != "" {
		id, err := uuid.Parse(*req.LocationID)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_location_id"))
		}
		locationID = &id
	}

	if req.DepartmentID != nil && *req.DepartmentID != "" {
		id, err := uuid.Parse(*req.DepartmentID)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_department_id"))
		}
		departmentID = &id
	}

	if req.ExcludeUserID != nil && *req.ExcludeUserID != "" {
		id, err := uuid.Parse(*req.ExcludeUserID)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_exclude_user_id"))
		}
		excludeUserID = &id
	}

	users, err := h.userService.FindMatchingUsers(c.UserContext(), roleIDs, classificationID, locationID, departmentID, excludeUserID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Build match response
	matchResponse := models.UserMatchResponse{
		Users:       users,
		SingleMatch: len(users) == 1,
	}

	if len(users) == 1 {
		idStr := users[0].ID.String()
		matchResponse.MatchedUserID = &idStr
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "users_matched"), matchResponse)
}

func (h *UserHandler) UpdateUserCallStatus(c *fiber.Ctx) error {
	// Get Extension from Params
	userExt := c.Params("userExtID")
	var req struct {
		Status string `json:"call_status"`
	}
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	validStatuses := map[string]bool{
		"available": true,
		"online":    true,
		"in_call":   true,
		"offline":   true,
	}
	if !validStatuses[req.Status] {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.Tf(c.UserContext(), "invalid_status", req.Status))
	}

	if strings.EqualFold(req.Status, "available") {
		req.Status = string(models.CallStatusOnline) // "online"
	}
	// Call Service
	resp, err := h.userService.UpdateUserCallStatus(c.UserContext(), userExt, req.Status)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Broadcast the availability change to all broadcast clients (e.g. incident
	// assignee dropdowns) so they can reflect live agent status. The payload map
	// returned by the service already contains id, extension, call_status and
	// updated_at, matching the identifiers the ["admin","users"] list uses.
	if h.wsHub != nil {
		h.wsHub.BroadcastToAll("user_status_changed", resp)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "user_call_status_updated"), resp)
}

// Export exports all users as JSON
func (h *UserHandler) Export(c *fiber.Ctx) error {
	// Department-scoped: users with view_department_only can only export their own department's users.
	// Mirrors the same restriction applied in ListUsers.
	var departmentIDs []uuid.UUID
	user, _ := c.Locals(constants.ContextKeys.User).(*models.User)
	if user != nil && !user.IsSuperAdmin && user.HasPermission("users:view_department_only") {
		scopeDeptID := user.ScopeDepartmentID()
		if scopeDeptID != nil {
			departmentIDs = []uuid.UUID{*scopeDeptID}
		}
	}

	// Get all users without pagination
	users, _, err := h.userService.ListUsers(c.UserContext(), 1, 10000, "", "", "", "", nil, departmentIDs, nil, nil)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Convert to export format
	exportData := make([]map[string]interface{}, len(users))
	for i, user := range users {
		// Extract role IDs
		roleIDs := make([]string, len(user.Roles))
		for j, role := range user.Roles {
			roleIDs[j] = role.ID.String()
		}

		// Extract department IDs
		departmentIDs := make([]string, len(user.Departments))
		for j, dept := range user.Departments {
			departmentIDs[j] = dept.ID.String()
		}

		// Extract location IDs
		locationIDs := make([]string, len(user.Locations))
		for j, loc := range user.Locations {
			locationIDs[j] = loc.ID.String()
		}

		// Extract classification IDs
		classificationIDs := make([]string, len(user.Classifications))
		for j, cls := range user.Classifications {
			classificationIDs[j] = cls.ID.String()
		}

		exportData[i] = map[string]interface{}{
			"id":                 user.ID,
			"username":           user.Username,
			"email":              user.Email,
			"first_name":         user.FirstName,
			"last_name":          user.LastName,
			"phone":              user.Phone,
			"department_id":      user.DepartmentID,
			"location_id":        user.LocationID,
			"role_ids":           roleIDs,
			"department_ids":     departmentIDs,
			"location_ids":       locationIDs,
			"classification_ids": classificationIDs,
			"is_active":          user.IsActive,
			"is_super_admin":     user.IsSuperAdmin,
		}
	}

	c.Set("Content-Type", "application/json")
	c.Set("Content-Disposition", "attachment; filename=users_export.json")
	return c.JSON(exportData)
}

// defaultImportPassword is assigned to every user created through Import; the import payload
// carries no credentials.
const defaultImportPassword = "DefaultPassword123!"

// parseImportUUIDs converts import-payload string IDs to UUIDs, dropping malformed values.
func parseImportUUIDs(values []string) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(values))
	for _, v := range values {
		if id, err := uuid.Parse(strings.TrimSpace(v)); err == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// formatValidationErrors flattens the field->message map from validation.ValidateStruct into a
// single line. Fields are sorted so the same bad row always produces the same message.
func formatValidationErrors(validationErrors map[string]string) string {
	fields := make([]string, 0, len(validationErrors))
	for field := range validationErrors {
		fields = append(fields, field)
	}
	sort.Strings(fields)

	parts := make([]string, 0, len(fields))
	for _, field := range fields {
		parts = append(parts, field+": "+validationErrors[field])
	}
	return strings.Join(parts, "; ")
}

// importRowLabel identifies a row in the Import error list. Email is the natural identifier, but a
// row can fail precisely because its email is blank, so fall back to the username and then to the
// row's 1-based position in the file - an error nobody can trace back to a row is not actionable.
func importRowLabel(email, username string, index int) string {
	switch {
	case email != "":
		return email
	case username != "":
		return username
	default:
		return "row " + strconv.Itoa(index+1)
	}
}

// Import imports users from JSON
func (h *UserHandler) Import(c *fiber.Ctx) error {
	file, err := c.FormFile("file")
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "no_file_uploaded"))
	}

	// Open and read file
	fileContent, err := file.Open()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_read_file"))
	}
	defer fileContent.Close()

	// Read file content
	var importData []struct {
		// ID        uuid.UUID `json:"id"`
		Username  string `json:"username"`
		Email     string `json:"email"`
		FirstName string `json:"first_name"`
		LastName  string `json:"last_name"`
		Phone     string `json:"phone"`
		Extension string `json:"extension"`
		// DepartmentID      *uuid.UUID `json:"department_id"`
		// LocationID        *uuid.UUID `json:"location_id"`
		// RoleIDs           []string   `json:"role_ids"`
		// DepartmentIDs     []string   `json:"department_ids"`
		// LocationIDs       []string   `json:"location_ids"`
		// ClassificationIDs []string   `json:"classification_ids"`
		// IsActive          bool       `json:"is_active"`
		// IsSuperAdmin      bool       `json:"is_super_admin"`
	}

	// Parse JSON from file
	decoder := json.NewDecoder(fileContent)
	if err := decoder.Decode(&importData); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.Tf(c.UserContext(), "invalid_json_format_detail", err.Error()))
	}

	// Normalise the rows and collect their identifiers so every duplicate in the file can be
	// resolved with one narrow query instead of two full-row lookups per row.
	emails := make([]string, 0, len(importData))
	usernames := make([]string, 0, len(importData))
	phones := make([]string, 0, len(importData))
	for i := range importData {
		importData[i].Email = strings.ToLower(strings.TrimSpace(importData[i].Email))
		importData[i].Username = strings.TrimSpace(importData[i].Username)
		importData[i].Phone = strings.TrimSpace(importData[i].Phone)

		if importData[i].Email != "" {
			emails = append(emails, importData[i].Email)
		}
		if importData[i].Username != "" {
			usernames = append(usernames, importData[i].Username)
		}
		if importData[i].Phone != "" {
			phones = append(phones, importData[i].Phone)
		}
	}

	existing, err := h.userService.CheckExistingIdentifiers(c.UserContext(), emails, usernames, phones)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	imported := 0
	skipped := 0
	errors := []string{}

	// Import all users
	for i, data := range importData {
		label := importRowLabel(data.Email, data.Username, i)

		req := &models.UserRegisterRequest{
			Username:  data.Username,
			Email:     data.Email,
			Password:  defaultImportPassword,
			FirstName: data.FirstName,
			LastName:  data.LastName,
			Phone:     data.Phone,
			// Extension: data.Extension,
			// DepartmentID:      data.DepartmentID,
			// LocationID:        data.LocationID,
			// RoleIDs:           parseImportUUIDs(data.RoleIDs),
			// DepartmentIDs:     parseImportUUIDs(data.DepartmentIDs),
			// LocationIDs:       parseImportUUIDs(data.LocationIDs),
			// ClassificationIDs: parseImportUUIDs(data.ClassificationIDs),
		}

		// Import is deliberately stricter than UserRegisterRequest, which tags Phone `omitempty`:
		// every imported row must carry a valid mobile number, so a blank one fails the row here
		// rather than passing the struct tags. IsValidMobile is the same function backing the
		// `mobile` tag, so checking it directly cannot diverge from what Register will accept - it
		// just lets the message name the number that was rejected.
		switch {
		case data.Phone == "":
			skipped++
			errors = append(errors, label+" - Phone is required.")
			continue
		case !validation.IsValidMobile(data.Phone):
			skipped++
			errors = append(errors, label+" - Phone "+data.Phone+" is not a valid mobile number.")
			continue
		}

		// Format is checked before the duplicate lookups: a malformed email that happens to collide
		// with an existing user has to be reported as malformed, not as "already exists". The struct
		// tags own the email and username rules, so an import accepts exactly what
		// POST /admin/users accepts.
		if validationErrors := validation.ValidateStruct(c.UserContext(), req); len(validationErrors) != 0 {
			skipped++
			errors = append(errors, label+" - "+formatValidationErrors(validationErrors))
			continue
		}

		if existing.HasEmail(data.Email) {
			skipped++
			errors = append(errors, label+" - User already exists with this email, skipped")
			continue
		}

		if existing.HasUsername(data.Username) {
			skipped++
			errors = append(errors, label+" - User already exists with this username, skipped")
			continue
		}

		if existing.HasPhone(data.Phone) {
			skipped++
			errors = append(errors, label+" - Phone "+data.Phone+" already in use.")
			continue
		}

		// Register user
		if _, err := h.userService.Register(c.UserContext(), req); err != nil {
			skipped++
			errors = append(errors, label+" - "+err.Error())
			continue
		}

		// Claim these identifiers so a later row in the same file cannot reuse them
		existing.Add(data.Email, data.Username, data.Phone)
		imported++
	}

	result := map[string]interface{}{
		"imported": imported,
		"skipped":  skipped,
		"errors":   errors,
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "import_completed"), result)
}

func (h *UserHandler) HandleLoginFailure(ctx context.Context, user string) {

	key := "login_attempts:user:" + user
	count, err := h.redisClient.Incr(ctx, key).Result()
	if err != nil {
		return
	}

	// set expiry only first time
	if count == 1 {
		h.redisClient.Expire(ctx, key, time.Duration(h.cfg.LoginRateLimit.BlockDuration)*time.Minute)
	}

	// block user if limit reached
	if int(count) >= h.cfg.LoginRateLimit.MaxAttempts {
		blockKey := "login_block:user:" + user
		h.redisClient.Set(ctx, blockKey, "1", time.Duration(h.cfg.LoginRateLimit.BlockDuration)*time.Minute)
	}
}

func (h *UserHandler) HandleLoginSuccess(ctx context.Context, user string) {

	h.redisClient.Del(ctx, "login_attempts:user:"+user)
	h.redisClient.Del(ctx, "login_block:user:"+user)
}

func (h *UserHandler) UnblockUser(c *fiber.Ctx) error {

	type Request struct {
		Identifier string `json:"identifier"` // email OR phone
	}

	var req Request
	if err := c.BodyParser(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Invalid request",
		})
	}

	if req.Identifier == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "Identifier is required",
		})
	}

	//  INLINE buildLoginKey
	identifier := strings.TrimSpace(req.Identifier)
	var key string
	if strings.Contains(identifier, "@") {
		key = "email:" + strings.ToLower(identifier)
	} else {
		key = "phone:" + identifier
	}

	ctx := c.UserContext()

	blockKey := "login_block:user:" + key
	attemptKey := "login_attempts:user:" + key

	// Check if blocked
	blockExists, err := h.redisClient.Exists(ctx, blockKey).Result()
	if err != nil {
		return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
			"error": "Redis error",
		})
	}

	if blockExists == 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "User is not blocked",
		})
	}

	// Delete keys
	h.redisClient.Del(ctx, blockKey)
	h.redisClient.Del(ctx, attemptKey)

	return c.JSON(fiber.Map{
		"message": "User unblocked successfully",
	})
}

func (h *UserHandler) ForgotPassword(c *fiber.Ctx) error {
	var req models.ForgotPasswordRequest

	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, 400, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if strings.TrimSpace(req.Value) == "" || strings.TrimSpace(req.Channel) == "" {
		return utils.ErrorResponse(c, 400, i18n.T(c.UserContext(), "channel_value_required"))
	}
	sessionID, err := h.userService.ForgotPassword(c.UserContext(), &req)

	if err != nil {
		log.Println("ForgotPassword ERROR:", err)
		return utils.ErrorResponse(c, 400, err.Error())
	}

	return utils.SuccessResponse(c, 200, i18n.T(c.UserContext(), "otp_sent"), fiber.Map{
		"sessionID": sessionID,
	})
}

func (h *UserHandler) VerifyOTPForReset(c *fiber.Ctx) error {
	var req models.VerifyOTPRequest

	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, 400, i18n.T(c.UserContext(), "invalid_request"))
	}

	resetToken, err := h.userService.VerifyOTPForReset(c.UserContext(), &req)
	if err != nil {
		return utils.ErrorResponse(c, 400, err.Error())
	}

	return utils.SuccessResponse(c, 200, i18n.T(c.UserContext(), "otp_verified"), fiber.Map{
		"resetToken": resetToken,
	})
}

func (h *UserHandler) ResetPassword(c *fiber.Ctx) error {
	var req struct {
		ResetToken  string `json:"resetToken"`
		NewPassword string `json:"newPassword"`
	}

	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, 400, i18n.T(c.UserContext(), "invalid_request"))
	}

	if req.ResetToken == "" || req.NewPassword == "" {
		return utils.ErrorResponse(c, 400, i18n.T(c.UserContext(), "missing_required_fields"))
	}

	err := h.userService.ResetPassword(
		c.UserContext(),
		req.ResetToken,
		req.NewPassword,
	)
	if err != nil {
		return utils.ErrorResponse(c, 400, err.Error())
	}

	return utils.SuccessResponse(c, 200, i18n.T(c.UserContext(), "password_reset_ok"), nil)
}
