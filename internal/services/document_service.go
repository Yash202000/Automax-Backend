package services

import (
	"context"
	"fmt"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/storage"
)

// ════════════════════════════════════════════════════
// DocumentService Interface
// ════════════════════════════════════════════════════

type DocumentService interface {
	ListFiles(ctx context.Context, parentID string, userEmail string) (*storage.DmsListResult, error)
	SearchFiles(ctx context.Context, query string, userEmail string) (*storage.DmsSearchResult, error)
	SearchFilesWithTags(ctx context.Context, query string, tags map[string]string, userEmail string) (*storage.DmsSearchResult, error)
	GetFileInfo(ctx context.Context, fileID string, userEmail string) (*storage.DmsFile, error)
	GetPreviewURL(ctx context.Context, fileID string, userEmail string) (string, error)
	GetDownloadURL(ctx context.Context, fileID string, userEmail string) (string, error)
	GetComments(ctx context.Context, fileID string, userEmail string) ([]storage.DmsComment, error)
	AddComment(ctx context.Context, fileID string, content string, userEmail string) error
	GetTags(ctx context.Context, fileID string, userEmail string) (map[string]string, error)
	SetTags(ctx context.Context, fileID string, tags map[string]string, userEmail string) error
}

// ════════════════════════════════════════════════════
// Implementation
// ════════════════════════════════════════════════════

type documentService struct {
	client        storage.DocumentaClient
	workspaceSlug string
}

func NewDocumentService(client storage.DocumentaClient, cfg config.DocumentaConfig) DocumentService {
	return &documentService{
		client:        client,
		workspaceSlug: cfg.WorkspaceName,
	}
}

func (s *documentService) withUser(ctx context.Context, email string) context.Context {
	return storage.ContextWithOnBehalf(ctx, email)
}

func (s *documentService) ListFiles(ctx context.Context, parentID string, userEmail string) (*storage.DmsListResult, error) {
	result, err := s.client.ListFiles(s.withUser(ctx, userEmail), s.workspaceSlug, parentID)
	if err != nil {
		return nil, fmt.Errorf("list files: %w", err)
	}
	return result, nil
}

func (s *documentService) SearchFiles(ctx context.Context, query string, userEmail string) (*storage.DmsSearchResult, error) {
	result, err := s.client.SearchFiles(s.withUser(ctx, userEmail), s.workspaceSlug, query)
	if err != nil {
		return nil, fmt.Errorf("search files: %w", err)
	}
	return result, nil
}

func (s *documentService) SearchFilesWithTags(ctx context.Context, query string, tags map[string]string, userEmail string) (*storage.DmsSearchResult, error) {
	result, err := s.client.SearchFilesWithTags(s.withUser(ctx, userEmail), s.workspaceSlug, query, tags)
	if err != nil {
		return nil, fmt.Errorf("search files with tags: %w", err)
	}
	return result, nil
}

func (s *documentService) GetFileInfo(ctx context.Context, fileID string, userEmail string) (*storage.DmsFile, error) {
	result, err := s.client.GetFileInfo(s.withUser(ctx, userEmail), fileID)
	if err != nil {
		return nil, fmt.Errorf("get file info: %w", err)
	}
	return result, nil
}

func (s *documentService) GetPreviewURL(ctx context.Context, fileID string, userEmail string) (string, error) {
	url, err := s.client.GetPreviewURL(s.withUser(ctx, userEmail), fileID)
	if err != nil {
		return "", fmt.Errorf("get preview url: %w", err)
	}
	return url, nil
}

func (s *documentService) GetDownloadURL(ctx context.Context, fileID string, userEmail string) (string, error) {
	url, err := s.client.GetDownloadURL(s.withUser(ctx, userEmail), fileID)
	if err != nil {
		return "", fmt.Errorf("get download url: %w", err)
	}
	return url, nil
}

func (s *documentService) GetComments(ctx context.Context, fileID string, userEmail string) ([]storage.DmsComment, error) {
	comments, err := s.client.GetComments(s.withUser(ctx, userEmail), fileID)
	if err != nil {
		return nil, fmt.Errorf("get comments: %w", err)
	}
	return comments, nil
}

func (s *documentService) AddComment(ctx context.Context, fileID string, content string, userEmail string) error {
	if err := s.client.AddComment(s.withUser(ctx, userEmail), fileID, content); err != nil {
		return fmt.Errorf("add comment: %w", err)
	}
	return nil
}

func (s *documentService) GetTags(ctx context.Context, fileID string, userEmail string) (map[string]string, error) {
	tags, err := s.client.GetTags(s.withUser(ctx, userEmail), fileID)
	if err != nil {
		return nil, fmt.Errorf("get tags: %w", err)
	}
	return tags, nil
}

func (s *documentService) SetTags(ctx context.Context, fileID string, tags map[string]string, userEmail string) error {
	if err := s.client.SetTags(s.withUser(ctx, userEmail), fileID, tags); err != nil {
		return fmt.Errorf("set tags: %w", err)
	}
	return nil
}
