package services

import (
	"context"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/google/uuid"
)

type ApplicationLinkService interface {
	CreateLink(ctx context.Context, req *models.ApplicationLinkCreateRequest) (*models.ApplicationLinkResponse, error)
	GetLink(ctx context.Context, id uuid.UUID) (*models.ApplicationLinkResponse, error)
	ListLinks(ctx context.Context) ([]models.ApplicationLinkResponse, error)
	ListActiveLinks(ctx context.Context) ([]models.ApplicationLinkResponse, error)
	UpdateLink(ctx context.Context, id uuid.UUID, req *models.ApplicationLinkUpdateRequest) (*models.ApplicationLinkResponse, error)
	DeleteLink(ctx context.Context, id uuid.UUID) error
}

type applicationLinkService struct {
	linkRepo repository.ApplicationLinkRepository
}

func NewApplicationLinkService(linkRepo repository.ApplicationLinkRepository) ApplicationLinkService {
	return &applicationLinkService{
		linkRepo: linkRepo,
	}
}

func (s *applicationLinkService) CreateLink(ctx context.Context, req *models.ApplicationLinkCreateRequest) (*models.ApplicationLinkResponse, error) {
	link := &models.ApplicationLink{
		Name:        req.Name,
		Description: req.Description,
		URL:         req.URL,
		Icon:        req.Icon,
		ImageURL:    req.ImageURL,
		Color:       req.Color,
		SortOrder:   req.SortOrder,
		IsActive:    req.IsActive,
	}

	// Set defaults if not provided
	if link.Icon == "" {
		link.Icon = "ExternalLink"
	}
	if link.Color == "" {
		link.Color = "blue"
	}

	if err := s.linkRepo.Create(ctx, link); err != nil {
		return nil, err
	}

	response := models.ToApplicationLinkResponse(link)
	return &response, nil
}

func (s *applicationLinkService) GetLink(ctx context.Context, id uuid.UUID) (*models.ApplicationLinkResponse, error) {
	link, err := s.linkRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	response := models.ToApplicationLinkResponse(link)
	return &response, nil
}

func (s *applicationLinkService) ListLinks(ctx context.Context) ([]models.ApplicationLinkResponse, error) {
	links, err := s.linkRepo.List(ctx)
	if err != nil {
		return nil, err
	}

	responses := make([]models.ApplicationLinkResponse, len(links))
	for i, link := range links {
		responses[i] = models.ToApplicationLinkResponse(&link)
	}

	return responses, nil
}

func (s *applicationLinkService) ListActiveLinks(ctx context.Context) ([]models.ApplicationLinkResponse, error) {
	links, err := s.linkRepo.ListActive(ctx)
	if err != nil {
		return nil, err
	}

	responses := make([]models.ApplicationLinkResponse, len(links))
	for i, link := range links {
		responses[i] = models.ToApplicationLinkResponse(&link)
	}

	return responses, nil
}

func (s *applicationLinkService) UpdateLink(ctx context.Context, id uuid.UUID, req *models.ApplicationLinkUpdateRequest) (*models.ApplicationLinkResponse, error) {
	link, err := s.linkRepo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Update fields if provided
	if req.Name != "" {
		link.Name = req.Name
	}
	if req.Description != "" {
		link.Description = req.Description
	}
	if req.URL != "" {
		link.URL = req.URL
	}
	if req.Icon != "" {
		link.Icon = req.Icon
	}
	if req.ImageURL != "" {
		link.ImageURL = req.ImageURL
	}
	if req.Color != "" {
		link.Color = req.Color
	}
	if req.SortOrder != nil {
		link.SortOrder = *req.SortOrder
	}
	if req.IsActive != nil {
		link.IsActive = *req.IsActive
	}

	if err := s.linkRepo.Update(ctx, link); err != nil {
		return nil, err
	}

	response := models.ToApplicationLinkResponse(link)
	return &response, nil
}

func (s *applicationLinkService) DeleteLink(ctx context.Context, id uuid.UUID) error {
	return s.linkRepo.Delete(ctx, id)
}
