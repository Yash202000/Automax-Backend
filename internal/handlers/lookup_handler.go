package handlers

import (
	"strings"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/utils"
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

// Category handlers

func (h *LookupHandler) CreateCategory(c *fiber.Ctx) error {
	var req models.LookupCategoryCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.validator.Struct(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}

	// Normalize code to uppercase
	req.Code = strings.ToUpper(req.Code)

	category := &models.LookupCategory{
		Code:              req.Code,
		Name:              req.Name,
		NameAr:            req.NameAr,
		Description:       req.Description,
		IsSystem:          false,
		IsActive:          true,
		AddToIncidentForm: false,
	}

	if req.IsActive != nil {
		category.IsActive = *req.IsActive
	}
	if req.AddToIncidentForm != nil {
		category.AddToIncidentForm = *req.AddToIncidentForm
	}

	if err := h.repo.CreateCategory(c.Context(), category); err != nil {
		if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "unique") {
			return utils.ErrorResponse(c, fiber.StatusConflict, "Category with this code already exists")
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Category created", models.ToLookupCategoryResponse(category))
}

func (h *LookupHandler) GetCategoryByID(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	category, err := h.repo.FindCategoryByID(c.Context(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Category not found")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Category retrieved", models.ToLookupCategoryResponse(category))
}

func (h *LookupHandler) UpdateCategory(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	var req models.LookupCategoryUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	category, err := h.repo.FindCategoryByID(c.Context(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Category not found")
	}

	// System categories can only have limited updates (no code/isActive changes)
	if category.IsSystem {
		// Only allow updating name, name_ar, description, add_to_incident_form for system categories
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
	}

	if err := h.repo.UpdateCategory(c.Context(), category); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Category updated", models.ToLookupCategoryResponse(category))
}

func (h *LookupHandler) DeleteCategory(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	category, err := h.repo.FindCategoryByID(c.Context(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Category not found")
	}

	// System categories cannot be deleted
	if category.IsSystem {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "System categories cannot be deleted")
	}

	if err := h.repo.DeleteCategory(c.Context(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Category deleted", nil)
}

func (h *LookupHandler) ListCategories(c *fiber.Ctx) error {
	categories, err := h.repo.ListCategories(c.Context())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	responses := make([]models.LookupCategoryResponse, len(categories))
	for i, cat := range categories {
		responses[i] = models.ToLookupCategoryResponse(&cat)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Categories retrieved", responses)
}

// Value handlers

func (h *LookupHandler) CreateValue(c *fiber.Ctx) error {
	categoryIDStr := c.Params("id")
	categoryID, err := uuid.Parse(categoryIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid category ID")
	}

	// Verify category exists
	category, err := h.repo.FindCategoryByID(c.Context(), categoryID)
	if err != nil && category == nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Category not found")
	}

	var req models.LookupValueCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if err := h.validator.Struct(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
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
		if err := h.repo.ClearDefaultForCategory(c.Context(), categoryID); err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to clear existing defaults")
		}
	}

	if err := h.repo.CreateValue(c.Context(), value); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Reload to get the updated category values count
	category, _ = h.repo.FindCategoryByID(c.Context(), categoryID)

	return utils.SuccessResponse(c, fiber.StatusCreated, "Value created", models.ToLookupValueResponse(value))
}

func (h *LookupHandler) GetValueByID(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	value, err := h.repo.FindValueByID(c.Context(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Value not found")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Value retrieved", models.ToLookupValueResponse(value))
}

func (h *LookupHandler) UpdateValue(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	var req models.LookupValueUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	value, err := h.repo.FindValueByID(c.Context(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Value not found")
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
			if err := h.repo.ClearDefaultForCategory(c.Context(), value.CategoryID); err != nil {
				return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to clear existing defaults")
			}
		}
		value.IsDefault = *req.IsDefault
	}
	if req.IsActive != nil {
		value.IsActive = *req.IsActive
	}

	if err := h.repo.UpdateValue(c.Context(), value); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Value updated", models.ToLookupValueResponse(value))
}

func (h *LookupHandler) DeleteValue(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	_, err = h.repo.FindValueByID(c.Context(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Value not found")
	}

	if err := h.repo.DeleteValue(c.Context(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Value deleted", nil)
}

func (h *LookupHandler) ListValuesByCategory(c *fiber.Ctx) error {
	categoryIDStr := c.Params("id")
	categoryID, err := uuid.Parse(categoryIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid category ID")
	}

	values, err := h.repo.ListValuesByCategory(c.Context(), categoryID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	responses := make([]models.LookupValueResponse, len(values))
	for i, v := range values {
		responses[i] = models.ToLookupValueResponse(&v)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Values retrieved", responses)
}

// Public endpoint - Get values by category code
func (h *LookupHandler) GetValuesByCategoryCode(c *fiber.Ctx) error {
	code := strings.ToUpper(c.Params("code"))

	values, err := h.repo.ListValuesByCategoryCode(c.Context(), code)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	responses := make([]models.LookupValueResponse, len(values))
	for i, v := range values {
		responses[i] = models.ToLookupValueResponse(&v)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Values retrieved", responses)
}
