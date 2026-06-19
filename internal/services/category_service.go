package services

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/i18n"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MaxCategoryDepth caps the depth of the Category tree. Roots are Level 0.
// Enforced at the service layer on Create.
const MaxCategoryDepth = 5

type CategoryService interface {
	Create(ctx context.Context, req *models.CategoryCreateRequest) (*models.CategoryResponse, error)
	Update(ctx context.Context, id uuid.UUID, req *models.CategoryUpdateRequest) (*models.CategoryResponse, error)
	Get(ctx context.Context, id uuid.UUID) (*models.CategoryResponse, error)
	List(ctx context.Context, includeInactive bool) ([]models.CategoryResponse, error)
	GetTree(ctx context.Context) ([]models.CategoryResponse, error)
	Delete(ctx context.Context, id uuid.UUID) error
}

type categoryService struct {
	repo repository.CategoryRepository
}

func NewCategoryService(repo repository.CategoryRepository) CategoryService {
	return &categoryService{repo: repo}
}

func (s *categoryService) Create(ctx context.Context, req *models.CategoryCreateRequest) (*models.CategoryResponse, error) {
	code := strings.TrimSpace(strings.ToLower(req.Code))
	if code == "" {
		return nil, fmt.Errorf("code is required")
	}

	// Uniqueness check (active or soft-deleted)
	if existing, err := s.repo.FindByCode(ctx, code); err == nil && existing != nil {
		return nil, fmt.Errorf("category code %q already exists", code)
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("code lookup failed: %w", err)
	}

	level := 0
	path := "/" + code

	if req.ParentID != nil {
		parent, err := s.repo.FindByID(ctx, *req.ParentID)
		if err != nil {
			return nil, fmt.Errorf("parent category not found: %w", err)
		}
		level = parent.Level + 1
		if level >= MaxCategoryDepth {
			return nil, fmt.Errorf("maximum category depth of %d exceeded", MaxCategoryDepth)
		}
		path = parent.Path + "/" + code
	}

	category := &models.Category{
		Name:          strings.TrimSpace(req.Name),
		NameAr:        req.NameAr,
		Code:          code,
		Description:   req.Description,
		DescriptionAr: req.DescriptionAr,
		ParentID:      req.ParentID,
		Level:         level,
		Path:          path,
		SortOrder:     req.SortOrder,
		IsActive:      true,
	}

	if err := s.repo.Create(ctx, category); err != nil {
		return nil, fmt.Errorf("failed to create category: %w", err)
	}

	resp := models.ToCategoryResponse(category)
	return &resp, nil
}

func (s *categoryService) Update(ctx context.Context, id uuid.UUID, req *models.CategoryUpdateRequest) (*models.CategoryResponse, error) {
	category, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(ctx, "category_not_found"), err)
	}

	if req.Name != nil {
		n := strings.TrimSpace(*req.Name)
		if n == "" {
			return nil, fmt.Errorf("name cannot be empty")
		}
		category.Name = n
	}
	if req.NameAr != nil {
		category.NameAr = *req.NameAr
	}
	if req.Description != nil {
		category.Description = *req.Description
	}
	if req.DescriptionAr != nil {
		category.DescriptionAr = *req.DescriptionAr
	}
	if req.IsActive != nil {
		category.IsActive = *req.IsActive
	}
	if req.SortOrder != nil {
		category.SortOrder = *req.SortOrder
	}

	if err := s.repo.Update(ctx, category); err != nil {
		return nil, fmt.Errorf("failed to update category: %w", err)
	}

	resp := models.ToCategoryResponse(category)
	return &resp, nil
}

func (s *categoryService) Get(ctx context.Context, id uuid.UUID) (*models.CategoryResponse, error) {
	category, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(ctx, "category_not_found"), err)
	}
	resp := models.ToCategoryResponse(category)
	return &resp, nil
}

func (s *categoryService) List(ctx context.Context, includeInactive bool) ([]models.CategoryResponse, error) {
	categories, err := s.repo.List(ctx, includeInactive)
	if err != nil {
		return nil, fmt.Errorf("failed to list categories: %w", err)
	}
	out := make([]models.CategoryResponse, len(categories))
	for i := range categories {
		out[i] = models.ToCategoryResponse(&categories[i])
	}
	return out, nil
}

func (s *categoryService) GetTree(ctx context.Context) ([]models.CategoryResponse, error) {
	tree, err := s.repo.GetTree(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(ctx, "failed_to_load_category_tree"), err)
	}
	out := make([]models.CategoryResponse, len(tree))
	for i := range tree {
		out[i] = models.ToCategoryResponse(&tree[i])
	}
	return out, nil
}

func (s *categoryService) Delete(ctx context.Context, id uuid.UUID) error {
	if _, err := s.repo.FindByID(ctx, id); err != nil {
		return fmt.Errorf("%s: %w", i18n.T(ctx, "category_not_found"), err)
	}
	return s.repo.Delete(ctx, id)
}
