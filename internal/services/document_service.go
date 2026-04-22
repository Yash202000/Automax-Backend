package services

import (
	"context"
	"fmt"
	"io"

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
	GetFileBreadcrumb(ctx context.Context, fileID string, userEmail string) ([]storage.DmsBreadcrumbEntry, error)
	GetPreviewURL(ctx context.Context, fileID string, userEmail string) (string, error)
	GetDownloadURL(ctx context.Context, fileID string, userEmail string) (string, error)
	// DownloadFile streams the raw bytes of a DMS file. Caller owns the returned
	// reader and must Close it.
	DownloadFile(ctx context.Context, fileID string, userEmail string) (io.ReadCloser, *storage.DmsFile, error)
	GetComments(ctx context.Context, fileID string, userEmail string) ([]storage.DmsComment, error)
	AddComment(ctx context.Context, fileID string, content string, userEmail string) error
	GetTags(ctx context.Context, fileID string, userEmail string) (map[string]string, error)
	SetTags(ctx context.Context, fileID string, tags map[string]string, userEmail string) error
	ListVersions(ctx context.Context, fileID string, userEmail string) ([]storage.DmsVersion, error)
	UploadVersion(ctx context.Context, fileID string, fileName string, fileData io.Reader, fileSize int64, description string, userEmail string) (*storage.DmsVersion, error)
	DownloadVersion(ctx context.Context, versionUUID string, userEmail string) (io.ReadCloser, string, error)
	RollbackVersion(ctx context.Context, fileID string, versionUUID string, userEmail string) (*storage.DmsVersion, error)
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

func (s *documentService) GetFileBreadcrumb(ctx context.Context, fileID string, userEmail string) ([]storage.DmsBreadcrumbEntry, error) {
	chain, err := s.client.GetFileBreadcrumb(s.withUser(ctx, userEmail), fileID)
	if err != nil {
		return nil, fmt.Errorf("get file breadcrumb: %w", err)
	}
	return chain, nil
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

func (s *documentService) DownloadFile(ctx context.Context, fileID string, userEmail string) (io.ReadCloser, *storage.DmsFile, error) {
	reader, info, err := s.client.DownloadFile(s.withUser(ctx, userEmail), fileID)
	if err != nil {
		return nil, nil, fmt.Errorf("download file: %w", err)
	}
	return reader, info, nil
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

func (s *documentService) ListVersions(ctx context.Context, fileID string, userEmail string) ([]storage.DmsVersion, error) {
	versions, err := s.client.ListVersions(s.withUser(ctx, userEmail), fileID)
	if err != nil {
		return nil, fmt.Errorf("list versions: %w", err)
	}
	return versions, nil
}

func (s *documentService) UploadVersion(ctx context.Context, fileID string, fileName string, fileData io.Reader, fileSize int64, description string, userEmail string) (*storage.DmsVersion, error) {
	version, err := s.client.UploadVersion(s.withUser(ctx, userEmail), fileID, fileName, fileData, fileSize, description)
	if err != nil {
		return nil, fmt.Errorf("upload version: %w", err)
	}
	return version, nil
}

func (s *documentService) DownloadVersion(ctx context.Context, versionUUID string, userEmail string) (io.ReadCloser, string, error) {
	reader, contentType, err := s.client.DownloadVersion(s.withUser(ctx, userEmail), versionUUID)
	if err != nil {
		return nil, "", fmt.Errorf("download version: %w", err)
	}
	return reader, contentType, nil
}

func (s *documentService) RollbackVersion(ctx context.Context, fileID string, versionUUID string, userEmail string) (*storage.DmsVersion, error) {
	version, err := s.client.RollbackVersion(s.withUser(ctx, userEmail), fileID, versionUUID)
	if err != nil {
		return nil, fmt.Errorf("rollback version: %w", err)
	}
	return version, nil
}
