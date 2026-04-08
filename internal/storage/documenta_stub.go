package storage

import (
	"context"
	"io"
	"log"
	"strings"

	"github.com/google/uuid"
)

// StubDocumentaClient is a placeholder implementation that logs all calls
// and returns mock data. Used when DOCUMENTA_ENABLED=false.
type StubDocumentaClient struct{}

func NewStubDocumentaClient() DocumentaClient {
	return &StubDocumentaClient{}
}

// ── Folder & File Operations ──

func (s *StubDocumentaClient) CreateFolder(ctx context.Context, workspaceName string, parentID string, name string) (string, error) {
	id := "stub-folder-" + uuid.New().String()
	log.Printf("[DOCUMENTA STUB] CreateFolder: workspace=%s, parent=%s, name=%s → folderID=%s", workspaceName, parentID, name, id)
	return id, nil
}

func (s *StubDocumentaClient) EnsureFolder(ctx context.Context, workspaceName string, parentID string, name string) (string, error) {
	id := "stub-folder-" + uuid.New().String()
	log.Printf("[DOCUMENTA STUB] EnsureFolder: workspace=%s, parent=%s, name=%s → folderID=%s", workspaceName, parentID, name, id)
	return id, nil
}

func (s *StubDocumentaClient) UploadFile(ctx context.Context, folderID, fileName string, fileData io.Reader, fileSize int64, metadata map[string]string) (string, error) {
	id := "stub-file-" + uuid.New().String()
	log.Printf("[DOCUMENTA STUB] UploadFile: folder=%s, file=%s, size=%d → fileID=%s", folderID, fileName, fileSize, id)
	return id, nil
}

func (s *StubDocumentaClient) GetPreviewURL(ctx context.Context, fileID string) (string, error) {
	url := "https://documenta.stub/preview/" + fileID
	log.Printf("[DOCUMENTA STUB] GetPreviewURL: fileID=%s → %s", fileID, url)
	return url, nil
}

func (s *StubDocumentaClient) GetDownloadURL(ctx context.Context, fileID string) (string, error) {
	url := "https://documenta.stub/download/" + fileID
	log.Printf("[DOCUMENTA STUB] GetDownloadURL: fileID=%s → %s", fileID, url)
	return url, nil
}

func (s *StubDocumentaClient) UpdateMetadata(ctx context.Context, fileID string, metadata map[string]string) error {
	log.Printf("[DOCUMENTA STUB] UpdateMetadata: fileID=%s, metadata=%v", fileID, metadata)
	return nil
}

func (s *StubDocumentaClient) DeleteFile(ctx context.Context, fileID string) error {
	log.Printf("[DOCUMENTA STUB] DeleteFile: fileID=%s", fileID)
	return nil
}

// ── Browsing & Search ──

func (s *StubDocumentaClient) ListFiles(ctx context.Context, workspaceSlug string, parentID string) (*DmsListResult, error) {
	log.Printf("[DOCUMENTA STUB] ListFiles: workspace=%s, parent=%s", workspaceSlug, parentID)
	return &DmsListResult{
		Files: []DmsFile{
			{UUID: "stub-folder-goal-mgmt", Name: "Goal Management", Type: "folder", CreatedAt: "2025-01-01T00:00:00Z", UpdatedAt: "2025-01-01T00:00:00Z"},
			{UUID: "stub-file-readme", Name: "README.md", Type: "file", Size: 1024, MimeType: "text/markdown", CreatedAt: "2025-01-01T00:00:00Z", UpdatedAt: "2025-01-01T00:00:00Z"},
		},
	}, nil
}

func (s *StubDocumentaClient) SearchFiles(ctx context.Context, workspaceSlug string, query string) (*DmsSearchResult, error) {
	log.Printf("[DOCUMENTA STUB] SearchFiles: workspace=%s, query=%s", workspaceSlug, query)
	return &DmsSearchResult{
		Files: []DmsFile{
			{UUID: "stub-file-search-1", Name: "search-result.pdf", Type: "file", Size: 2048, MimeType: "application/pdf", CreatedAt: "2025-01-01T00:00:00Z", UpdatedAt: "2025-01-01T00:00:00Z"},
		},
		Total: 1,
	}, nil
}

func (s *StubDocumentaClient) SearchFilesWithTags(ctx context.Context, workspaceSlug string, query string, tags map[string]string) (*DmsSearchResult, error) {
	log.Printf("[DOCUMENTA STUB] SearchFilesWithTags: workspace=%s, query=%s, tags=%v", workspaceSlug, query, tags)
	return &DmsSearchResult{Files: []DmsFile{}, Total: 0}, nil
}

func (s *StubDocumentaClient) GetFileInfo(ctx context.Context, fileID string) (*DmsFile, error) {
	log.Printf("[DOCUMENTA STUB] GetFileInfo: fileID=%s", fileID)
	return &DmsFile{
		UUID:      fileID,
		Name:      "stub-file.pdf",
		Type:      "file",
		Size:      4096,
		MimeType:  "application/pdf",
		CreatedAt: "2025-01-01T00:00:00Z",
		UpdatedAt: "2025-01-01T00:00:00Z",
		Metadata:  map[string]string{"source_system": "automax"},
	}, nil
}

// ── Comments ──

func (s *StubDocumentaClient) GetComments(ctx context.Context, fileID string) ([]DmsComment, error) {
	log.Printf("[DOCUMENTA STUB] GetComments: fileID=%s", fileID)
	return []DmsComment{
		{ID: "stub-comment-1", FileID: fileID, Author: "admin@example.com", Content: "Stub comment", CreatedAt: "2025-01-01T00:00:00Z"},
	}, nil
}

func (s *StubDocumentaClient) AddComment(ctx context.Context, fileID string, content string) error {
	log.Printf("[DOCUMENTA STUB] AddComment: fileID=%s, content=%s", fileID, content)
	return nil
}

// ── Tags ──

func (s *StubDocumentaClient) GetTags(ctx context.Context, fileID string) (map[string]string, error) {
	log.Printf("[DOCUMENTA STUB] GetTags: fileID=%s", fileID)
	return map[string]string{"source_system": "automax", "evidence_type": "Report"}, nil
}

func (s *StubDocumentaClient) SetTags(ctx context.Context, fileID string, tags map[string]string) error {
	log.Printf("[DOCUMENTA STUB] SetTags: fileID=%s, tags=%v", fileID, tags)
	return nil
}

// ── Versions ──

func (s *StubDocumentaClient) ListVersions(ctx context.Context, fileID string) ([]DmsVersion, error) {
	log.Printf("[DOCUMENTA STUB] ListVersions: fileID=%s", fileID)
	return []DmsVersion{
		{UUID: "stub-version-1", NodeUUID: fileID, VersionNumber: 1, Size: 1024, Description: "Initial version", Source: "upload", CreatedBy: "admin@example.com", CreatedByName: "Admin", CreatedAt: "2025-01-01T00:00:00Z", IsCurrent: true},
	}, nil
}

func (s *StubDocumentaClient) UploadVersion(ctx context.Context, fileID string, fileName string, fileData io.Reader, fileSize int64, description string) (*DmsVersion, error) {
	id := uuid.New().String()
	log.Printf("[DOCUMENTA STUB] UploadVersion: fileID=%s, fileName=%s, size=%d → versionID=%s", fileID, fileName, fileSize, id)
	return &DmsVersion{
		UUID:          id,
		NodeUUID:      fileID,
		VersionNumber: 2,
		Size:          fileSize,
		Description:   description,
		Source:        "upload",
		CreatedBy:     "admin@example.com",
		CreatedByName: "Admin",
		CreatedAt:     "2025-01-01T00:00:00Z",
		IsCurrent:     true,
	}, nil
}

func (s *StubDocumentaClient) DownloadVersion(ctx context.Context, versionUUID string) (io.ReadCloser, string, error) {
	log.Printf("[DOCUMENTA STUB] DownloadVersion: versionUUID=%s", versionUUID)
	return io.NopCloser(strings.NewReader("stub version content")), "text/plain", nil
}

func (s *StubDocumentaClient) RollbackVersion(ctx context.Context, fileID string, versionUUID string) (*DmsVersion, error) {
	log.Printf("[DOCUMENTA STUB] RollbackVersion: fileID=%s, versionUUID=%s", fileID, versionUUID)
	return &DmsVersion{
		UUID:          versionUUID,
		NodeUUID:      fileID,
		VersionNumber: 1,
		Size:          1024,
		Description:   "Rolled back version",
		Source:        "rollback",
		CreatedBy:     "admin@example.com",
		CreatedByName: "Admin",
		CreatedAt:     "2025-01-01T00:00:00Z",
		IsCurrent:     true,
	}, nil
}
