package handlers

import (
	"encoding/json"
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

type LookupHandler struct {
	repo      repository.LookupRepository
	validator *validator.Validate
}

func NewLookupHandler(repo repository.LookupRepository) *LookupHandler {
	return &LookupHandler{
		repo:      repo,
		validator: validator.New(),
	}
}

// validateFieldTypeAndRules validates field type and validation rules JSON structure
func validateFieldTypeAndRules(fieldType, validationRules string) error {
	if fieldType == "" {
		fieldType = "select"
	}

	// Validate field type
	validTypes := []string{"text", "number", "date", "select", "multiselect", "checkbox", "textarea"}
	isValid := false
	for _, vt := range validTypes {
		if fieldType == vt {
			isValid = true
			break
		}
	}
	if !isValid {
		return fiber.NewError(fiber.StatusBadRequest, "Invalid field_type. Must be one of: text, number, date, select, multiselect, checkbox, textarea")
	}

	// Validate validation_rules JSON if provided
	if validationRules != "" {
		var rules map[string]interface{}
		if err := json.Unmarshal([]byte(validationRules), &rules); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "Invalid validation_rules JSON format")
		}
	}

	return nil
}

// Category handlers

func (h *LookupHandler) CreateCategory(c *fiber.Ctx) error {
	var req models.LookupCategoryCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	// Normalize code to uppercase
	req.Code = strings.ToUpper(req.Code)

	// Set default field type if not provided
	if req.FieldType == "" {
		req.FieldType = "select"
	}

	// Validate field type and validation rules
	if err := validateFieldTypeAndRules(req.FieldType, req.ValidationRules); err != nil {
		return err
	}

	category := &models.LookupCategory{
		Code:              req.Code,
		Name:              req.Name,
		NameAr:            req.NameAr,
		Description:       req.Description,
		IsSystem:          false,
		IsActive:          true,
		AddToIncidentForm: false,
		FieldType:         req.FieldType,
		ValidationRules:   req.ValidationRules,
	}

	if req.IsActive != nil {
		category.IsActive = *req.IsActive
	}
	if req.AddToIncidentForm != nil {
		category.AddToIncidentForm = *req.AddToIncidentForm
	}

	if err := h.repo.CreateCategory(c.UserContext(), category); err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "category_code_exists"))
		}
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "category_created"), models.ToLookupCategoryResponse(category))
}

func (h *LookupHandler) GetCategoryByID(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	category, err := h.repo.FindCategoryByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "category_not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "category_retrieved"), models.ToLookupCategoryResponse(category))
}

func (h *LookupHandler) UpdateCategory(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.LookupCategoryUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	category, err := h.repo.FindCategoryByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "category_not_found"))
	}

	// Validate field type and validation rules if provided
	if req.FieldType != "" || req.ValidationRules != "" {
		fieldType := req.FieldType
		if fieldType == "" {
			fieldType = category.FieldType
		}
		if err := validateFieldTypeAndRules(fieldType, req.ValidationRules); err != nil {
			return err
		}
	}

	// System categories can only have limited updates (no code/isActive/field_type changes)
	if category.IsSystem {
		// Only allow updating name, name_ar, description, add_to_incident_form, validation_rules for system categories
		if req.Name != "" {
			category.Name = req.Name
		}
		if req.NameAr != "" {
			category.NameAr = req.NameAr
		}
		if req.Description != "" {
			category.Description = req.Description
		}
		if req.AddToIncidentForm != nil {
			category.AddToIncidentForm = *req.AddToIncidentForm
		}
		if req.ValidationRules != "" {
			category.ValidationRules = req.ValidationRules
		}
	} else {
		if req.Code != "" {
			category.Code = strings.ToUpper(req.Code)
		}
		if req.Name != "" {
			category.Name = req.Name
		}
		if req.NameAr != "" {
			category.NameAr = req.NameAr
		}
		if req.Description != "" {
			category.Description = req.Description
		}
		if req.IsActive != nil {
			category.IsActive = *req.IsActive
		}
		if req.AddToIncidentForm != nil {
			category.AddToIncidentForm = *req.AddToIncidentForm
		}
		if req.FieldType != "" {
			category.FieldType = req.FieldType
		}
		if req.ValidationRules != "" {
			category.ValidationRules = req.ValidationRules
		}

		if req.RedirectURL != "" {
			category.RedirectURL = req.RedirectURL
		}
		// If code is being updated, ensure it's unique
		if req.Code != "" && !category.IsSystem {
			existing, err := h.repo.FindCategoryByCode(c.UserContext(), category.Code)
			if err == nil && existing.ID != category.ID {
				return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "another_category_exists"))
			}
		}

		// If setting as inactive, ensure no active values exist
		if req.IsActive != nil && !*req.IsActive {
			values, err := h.repo.ListValuesByCategory(c.UserContext(), category.ID)
			if err != nil {
				return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_check_category"))
			}
			for _, v := range values {
				if v.IsActive {
					return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "cannot_deactivate_category"))
				}
			}
		}
	}

	if err := h.repo.UpdateCategory(c.UserContext(), category); err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "category_updated"), models.ToLookupCategoryResponse(category))
}

func (h *LookupHandler) DeleteCategory(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	category, err := h.repo.FindCategoryByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "category_not_found"))
	}

	// System categories cannot be deleted
	if category.IsSystem {
		return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(c.UserContext(), "system_categories_no_delete"))
	}

	if err := h.repo.DeleteCategory(c.UserContext(), id); err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "category_deleted"), nil)
}

func (h *LookupHandler) ListCategories(c *fiber.Ctx) error {
	categories, err := h.repo.ListCategories(c.UserContext())
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	responses := make([]models.LookupCategoryResponse, len(categories))
	for i, cat := range categories {
		responses[i] = models.ToLookupCategoryResponse(&cat)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "categories_retrieved"), responses)
}

// Value handlers

func (h *LookupHandler) CreateValue(c *fiber.Ctx) error {
	categoryIDStr := c.Params("id")
	categoryID, err := uuid.Parse(categoryIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_category_id"))
	}

	// Verify category exists
	category, err := h.repo.FindCategoryByID(c.UserContext(), categoryID)
	if err != nil && category == nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "category_not_found"))
	}

	var req models.LookupValueCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	// Normalize code to uppercase
	req.Code = strings.ToUpper(req.Code)

	value := &models.LookupValue{
		CategoryID:  categoryID,
		Code:        req.Code,
		Name:        req.Name,
		NameAr:      req.NameAr,
		Description: req.Description,
		SortOrder:   req.SortOrder,
		Color:       req.Color,
		IsDefault:   req.IsDefault,
		IsActive:    true,
	}

	if req.IsActive != nil {
		value.IsActive = *req.IsActive
	}

	// If this is set as default, clear other defaults for this category
	if value.IsDefault {
		if err := h.repo.ClearDefaultForCategory(c.UserContext(), categoryID); err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_clear_defaults"))
		}
	}

	if err := h.repo.CreateValue(c.UserContext(), value); err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	// Reload to get the updated category values count
	category, _ = h.repo.FindCategoryByID(c.UserContext(), categoryID)

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "value_created"), models.ToLookupValueResponse(value))
}

func (h *LookupHandler) GetValueByID(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	value, err := h.repo.FindValueByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "value_not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "value_retrieved"), models.ToLookupValueResponse(value))
}

func (h *LookupHandler) UpdateValue(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.LookupValueUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	value, err := h.repo.FindValueByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "value_not_found"))
	}

	if req.Code != "" {
		value.Code = strings.ToUpper(req.Code)
	}
	if req.Name != "" {
		value.Name = req.Name
	}
	if req.NameAr != "" {
		value.NameAr = req.NameAr
	}
	if req.Description != "" {
		value.Description = req.Description
	}
	if req.SortOrder != nil {
		value.SortOrder = *req.SortOrder
	}
	if req.Color != "" {
		value.Color = req.Color
	}
	if req.IsDefault != nil {
		// If setting as default, clear other defaults first
		if *req.IsDefault && !value.IsDefault {
			if err := h.repo.ClearDefaultForCategory(c.UserContext(), value.CategoryID); err != nil {
				return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_clear_defaults"))
			}
		}
		value.IsDefault = *req.IsDefault
	}
	if req.IsActive != nil {
		value.IsActive = *req.IsActive
	}

	if err := h.repo.UpdateValue(c.UserContext(), value); err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "value_updated"), models.ToLookupValueResponse(value))
}

func (h *LookupHandler) DeleteValue(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	_, err = h.repo.FindValueByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "value_not_found"))
	}

	if err := h.repo.DeleteValue(c.UserContext(), id); err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "value_deleted"), nil)
}

func (h *LookupHandler) ListValuesByCategory(c *fiber.Ctx) error {
	categoryIDStr := c.Params("id")
	categoryID, err := uuid.Parse(categoryIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_category_id"))
	}

	values, err := h.repo.ListValuesByCategory(c.UserContext(), categoryID)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	responses := make([]models.LookupValueResponse, len(values))
	for i, v := range values {
		responses[i] = models.ToLookupValueResponse(&v)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "values_retrieved"), responses)
}

// Public endpoint - Get values by category code
func (h *LookupHandler) GetValuesByCategoryCode(c *fiber.Ctx) error {
	code := strings.ToUpper(c.Params("code"))

	values, err := h.repo.ListValuesByCategoryCode(c.UserContext(), code)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	responses := make([]models.LookupValueResponse, len(values))
	for i, v := range values {
		responses[i] = models.ToLookupValueResponse(&v)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "values_retrieved"), responses)
}
