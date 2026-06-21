package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type DepartmentHandler struct {
	repo      repository.DepartmentRepository
	userRepo  repository.UserRepository
	validator *validator.Validate
}

func NewDepartmentHandler(repo repository.DepartmentRepository, userRepo repository.UserRepository) *DepartmentHandler {
	return &DepartmentHandler{
		repo:      repo,
		userRepo:  userRepo,
		validator: validator.New(),
	}
}

func (h *DepartmentHandler) Create(c *fiber.Ctx) error {
	var req models.DepartmentCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	req.Name = strings.TrimSpace(req.Name)
	req.NameAr = strings.TrimSpace(req.NameAr)

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	existing, err := h.repo.FindByNameOrNameAr(c.UserContext(), req.Name, req.NameAr)
	if err == nil && existing != nil {
		msg := fmt.Sprintf("Department '%s' already exists", existing.Name)
		return utils.ErrorResponse(c, fiber.StatusConflict, msg)
	}

	deptType := req.Type
	if deptType == "" {
		deptType = "internal"
	}
	department := &models.Department{
		Name:          req.Name,
		NameAr:        req.NameAr,
		Code:          req.Code,
		Description:   req.Description,
		DescriptionAr: req.DescriptionAr,
		Type:          deptType,
		ParentID:      req.ParentID,
		ManagerID:     req.ManagerID,
		SortOrder:     req.SortOrder,
		IsActive:      true,
	}

	if err := h.repo.Create(c.UserContext(), department); err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "department_code_exists"))
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Assign locations, classifications, and roles if provided
	if len(req.LocationIDs) > 0 {
		h.repo.AssignLocations(c.UserContext(), department.ID, req.LocationIDs)
	}
	if len(req.ClassificationIDs) > 0 {
		h.repo.AssignClassifications(c.UserContext(), department.ID, req.ClassificationIDs)
	}
	if len(req.RoleIDs) > 0 {
		h.repo.AssignRoles(c.UserContext(), department.ID, req.RoleIDs)
	}

	// Reload with associations
	department, _ = h.repo.FindByID(c.UserContext(), department.ID)

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "department_created"), models.ToDepartmentResponse(department))
}

// cascadeToUsers syncs department location/classification/role assignments to all users in this department
func (h *DepartmentHandler) cascadeToUsers(ctx context.Context, departmentID uuid.UUID) {
	users, err := h.userRepo.FindByDepartmentID(ctx, departmentID)
	if err != nil || len(users) == 0 {
		return
	}

	for _, user := range users {
		// Collect classifications from all user's departments
		classIDMap := make(map[uuid.UUID]bool)
		locIDMap := make(map[uuid.UUID]bool)
		roleIDMap := make(map[uuid.UUID]bool)

		for _, dept := range user.Departments {
			fullDept, err := h.repo.FindByID(ctx, dept.ID)
			if err != nil {
				continue
			}
			for _, c := range fullDept.Classifications {
				classIDMap[c.ID] = true
			}
			for _, l := range fullDept.Locations {
				locIDMap[l.ID] = true
			}
			for _, r := range fullDept.Roles {
				roleIDMap[r.ID] = true
			}
		}

		classIDs := make([]uuid.UUID, 0, len(classIDMap))
		for id := range classIDMap {
			classIDs = append(classIDs, id)
		}
		locIDs := make([]uuid.UUID, 0, len(locIDMap))
		for id := range locIDMap {
			locIDs = append(locIDs, id)
		}
		roleIDs := make([]uuid.UUID, 0, len(roleIDMap))
		for id := range roleIDMap {
			roleIDs = append(roleIDs, id)
		}

		_ = h.userRepo.AssignClassifications(ctx, user.ID, classIDs)
		_ = h.userRepo.AssignLocations(ctx, user.ID, locIDs)
		_ = h.userRepo.AssignRoles(ctx, user.ID, roleIDs)
	}
}

func (h *DepartmentHandler) GetByID(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	department, err := h.repo.FindByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "department_not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "department_retrieved"), models.ToDepartmentResponse(department))
}

func (h *DepartmentHandler) Update(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.DepartmentUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	req.Name = strings.TrimSpace(req.Name)
	req.NameAr = strings.TrimSpace(req.NameAr)

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	department, err := h.repo.FindByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "department_not_found"))
	}

	checkName := req.Name
	if checkName == "" {
		checkName = department.Name
	}
	checkNameAr := req.NameAr
	if checkNameAr == "" {
		checkNameAr = department.NameAr
	}

	if (req.Name != "" && req.Name != department.Name) || (req.NameAr != "" && req.NameAr != department.NameAr) {
		existing, err := h.repo.FindByNameOrNameAr(c.UserContext(), checkName, checkNameAr)
		if err == nil && existing != nil && existing.ID != id {
			msg := fmt.Sprintf("Department '%s' already exists", existing.Name)
			return utils.ErrorResponse(c, fiber.StatusConflict, msg)
		}
	}

	if req.Name != "" {
		department.Name = req.Name
	}
	if req.NameAr != "" {
		department.NameAr = req.NameAr
	}
	if req.Code != "" {
		department.Code = req.Code
	}
	if req.Description != "" {
		department.Description = req.Description
	}
	if req.DescriptionAr != "" {
		department.DescriptionAr = req.DescriptionAr
	}
	if req.Type != "" {
		department.Type = req.Type
	}
	if req.ManagerID != nil {
		department.ManagerID = req.ManagerID
	}
	if req.IsActive != nil {
		department.IsActive = *req.IsActive
	}
	if req.SortOrder != nil {
		department.SortOrder = *req.SortOrder
	}

	if err := h.repo.Update(c.UserContext(), department); err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "department_code_exists"))
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Update associations if provided
	associationsChanged := false
	if req.LocationIDs != nil {
		h.repo.AssignLocations(c.UserContext(), department.ID, req.LocationIDs)
		associationsChanged = true
	}
	if req.ClassificationIDs != nil {
		h.repo.AssignClassifications(c.UserContext(), department.ID, req.ClassificationIDs)
		associationsChanged = true
	}
	if req.RoleIDs != nil {
		h.repo.AssignRoles(c.UserContext(), department.ID, req.RoleIDs)
		associationsChanged = true
	}

	// Cascade changes to all users in this department
	if associationsChanged {
		h.cascadeToUsers(c.UserContext(), department.ID)
	}

	// Reload with associations
	department, _ = h.repo.FindByID(c.UserContext(), department.ID)

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "department_updated"), models.ToDepartmentResponse(department))
}

func (h *DepartmentHandler) Delete(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	if err := h.repo.Delete(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "department_deleted"), nil)
}

func (h *DepartmentHandler) List(c *fiber.Ctx) error {
	departments, err := h.repo.List(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	responses := make([]models.DepartmentResponse, len(departments))
	for i, dept := range departments {
		responses[i] = models.ToDepartmentResponse(&dept)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "departments_retrieved"), responses)
}

func (h *DepartmentHandler) GetTree(c *fiber.Ctx) error {
	tree, err := h.repo.GetTree(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	responses := make([]models.DepartmentResponse, len(tree))
	for i, dept := range tree {
		responses[i] = models.ToDepartmentResponse(&dept)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "department_tree_retrieved"), responses)
}

func (h *DepartmentHandler) GetChildren(c *fiber.Ctx) error {
	parentIDStr := c.Query("parent_id")

	var children []models.Department
	var err error

	if parentIDStr == "" {
		children, err = h.repo.GetByParentID(c.UserContext(), nil)
	} else {
		parentID, parseErr := uuid.Parse(parentIDStr)
		if parseErr != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_parent_id"))
		}
		children, err = h.repo.GetByParentID(c.UserContext(), &parentID)
	}

	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	responses := make([]models.DepartmentResponse, len(children))
	for i, dept := range children {
		responses[i] = models.ToDepartmentResponse(&dept)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "children_retrieved"), responses)
}

// MatchDepartment finds departments that match the given classification and/or location
func (h *DepartmentHandler) MatchDepartment(c *fiber.Ctx) error {
	var req models.DepartmentMatchRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	var classificationID, locationID *uuid.UUID

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

	departments, err := h.repo.FindMatching(c.UserContext(), classificationID, locationID, req.DepartmentType)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	responses := make([]models.DepartmentResponse, len(departments))
	for i, dept := range departments {
		responses[i] = models.ToDepartmentResponse(&dept)
	}

	// Build match response
	matchResponse := models.DepartmentMatchResponse{
		Departments: responses,
		SingleMatch: len(departments) == 1,
	}

	if len(departments) == 1 {
		idStr := departments[0].ID.String()
		matchResponse.MatchedDepartmentID = &idStr
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "departments_matched"), matchResponse)
}

// Export exports all departments as JSON
func (h *DepartmentHandler) Export(c *fiber.Ctx) error {
	departments, err := h.repo.List(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Filter out invalid records (with corrupted paths or invalid UUIDs)
	validDepartments := make([]models.Department, 0)
	invalidUUID := "00000000-0000-0000-0000-000000000000"
	for _, dept := range departments {
		if dept.ID.String() == invalidUUID {
			continue
		}
		validDepartments = append(validDepartments, dept)
	}
	// Convert to export format
	exportData := make([]map[string]interface{}, len(validDepartments))
	for i, dept := range validDepartments {
		exportData[i] = map[string]interface{}{
			"id":          dept.ID,
			"name":        dept.Name,
			"code":        dept.Code,
			"description": dept.Description,
			"parent_id":   dept.ParentID,
			"level":       dept.Level,
			"path":        dept.Path,
			"manager_id":  dept.ManagerID,
			"is_active":   dept.IsActive,
			"sort_order":  dept.SortOrder,
		}
	}

	c.Set("Content-Type", "application/json")
	c.Set("Content-Disposition", "attachment; filename=departments_export.json")
	return c.JSON(exportData)
}

// Import imports departments from JSON
func (h *DepartmentHandler) Import(c *fiber.Ctx) error {
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
		ID          uuid.UUID  `json:"id"`
		Name        string     `json:"name"`
		Code        string     `json:"code"`
		Description string     `json:"description"`
		ParentID    *uuid.UUID `json:"parent_id"`
		Level       int        `json:"level"`
		Path        string     `json:"path"`
		ManagerID   *uuid.UUID `json:"manager_id"`
		IsActive    bool       `json:"is_active"`
		SortOrder   int        `json:"sort_order"`
	}

	// Parse JSON from file
	decoder := json.NewDecoder(fileContent)
	if err := decoder.Decode(&importData); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.Tf(c.UserContext(), "invalid_json_format_detail", err.Error()))
	}

	// Sort by level to ensure parents are imported before children
	sort.Slice(importData, func(i, j int) bool {
		return importData[i].Level < importData[j].Level
	})

	// Create a map from old IDs to new IDs for maintaining parent-child relationships
	idMapping := make(map[uuid.UUID]uuid.UUID)
	imported := 0
	skipped := 0
	errors := []string{}

	// Import all departments in level order
	for _, data := range importData {
		var newParentID *uuid.UUID

		// If has parent, get the new parent ID from mapping
		if data.ParentID != nil {
			mappedParentID, exists := idMapping[*data.ParentID]
			if exists {
				newParentID = &mappedParentID
			} else {
				// Parent not found in import data, import as root node
				newParentID = nil
			}
		}

		// Check if department already exists with same name and parent
		existingDepartment, err := h.repo.FindByNameAndParent(c.UserContext(), data.Name, newParentID)
		if err == nil && existingDepartment != nil {
			// Department already exists, use existing ID
			skipped++
			errors = append(errors, data.Name+" (Level "+fmt.Sprintf("%d", data.Level)+") - Already exists, skipped")
			idMapping[data.ID] = existingDepartment.ID
			continue
		}

		// Create new department
		newID := uuid.New()
		department := &models.Department{
			ID:          newID,
			Name:        data.Name,
			Code:        data.Code,
			Description: data.Description,
			ParentID:    newParentID,
			ManagerID:   data.ManagerID,
			IsActive:    data.IsActive,
			SortOrder:   data.SortOrder,
		}

		if err := h.repo.Create(c.UserContext(), department); err != nil {
			skipped++
			errors = append(errors, data.Name+" (Level "+fmt.Sprintf("%d", data.Level)+") - "+err.Error())
		} else {
			imported++
			idMapping[data.ID] = newID
		}
	}

	result := map[string]interface{}{
		"imported": imported,
		"skipped":  skipped,
		"errors":   errors,
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "import_completed"), result)
}
