package storage

import (
	"context"
	"io"
)

// ════════════════════════════════════════════════════
// DMS Response Types
// ════════════════════════════════════════════════════

// DmsFile represents a file or folder in the Documenta DMS.
// JSON tags are the snake_case contract Automax presents to its own frontend.
// Fields are filled by the HTTP client from upstream MyDocs responses
// (which use camelCase — see ListFiles/GetFileInfo for the mapping).
type DmsFile struct {
	UUID       string            `json:"uuid"`
	Name       string            `json:"name"`
	Type       string            `json:"type"` // "file" or "folder"
	Size       int64             `json:"size"`
	MimeType   string            `json:"mime_type"`
	Parent     string            `json:"parent"`      // legacy alias; mirrors ParentUUID
	ParentUUID string            `json:"parent_uuid"` // direct parent folder UUID, empty for workspace root
	Path       string            `json:"path"`        // absolute path, e.g. /Goal Management/Safety/file.pdf
	CreatedAt  string            `json:"created_at"`
	UpdatedAt  string            `json:"updated_at"`
	Metadata   map[string]string `json:"metadata"`
}

// DmsBreadcrumbEntry is one step in a file's folder chain (workspace root → parent folder).
type DmsBreadcrumbEntry struct {
	UUID string `json:"uuid"`
	Name string `json:"name"`
}

// DmsComment represents a comment on a file.
type DmsComment struct {
	ID        string `json:"id"`
	FileID    string `json:"file_id"`
	Author    string `json:"author"`
	Content   string `json:"content"`
	CreatedAt string `json:"created_at"`
}

// DmsListResult is the response from listing folder contents.
type DmsListResult struct {
	Files []DmsFile `json:"files"`
}

// DmsSearchResult is the response from searching files.
type DmsSearchResult struct {
	Files []DmsFile `json:"files"`
	Total int       `json:"total"`
}

// DmsVersion represents a version of a file in the Documenta DMS.
type DmsVersion struct {
	UUID          string `json:"uuid"`
	NodeUUID      string `json:"node_uuid"`
	VersionNumber int    `json:"version_number"`
	Size          int64  `json:"size"`
	Description   string `json:"description"`
	Source        string `json:"source"`
	CreatedBy     string `json:"created_by"`
	CreatedByName string `json:"created_by_name"`
	CreatedAt     string `json:"created_at"`
	IsCurrent     bool   `json:"is_current"`
}

// ════════════════════════════════════════════════════
// DocumentaClient Interface
// ════════════════════════════════════════════════════

// DocumentaClient defines the interface for interacting with the Documenta/MyDocs DMS API.
type DocumentaClient interface {
	// ── Folder & File Operations ──

	// CreateFolder creates a folder in Documenta, optionally nested under a parent.
	// Pass empty parentID for workspace root.
	CreateFolder(ctx context.Context, workspaceName string, parentID string, name string) (folderID string, err error)

	// EnsureFolder idempotently ensures a named folder exists under a parent.
	// Uses Redis cache → list parent contents → create if missing. Returns folder UUID.
	EnsureFolder(ctx context.Context, workspaceName string, parentID string, name string) (folderID string, err error)

	// UploadFile uploads a file into a Documenta folder with metadata tags.
	UploadFile(ctx context.Context, folderID, fileName string, fileData io.Reader, fileSize int64, metadata map[string]string) (fileID string, err error)

	// GetPreviewURL returns a URL that can be used to preview a file in the browser.
	GetPreviewURL(ctx context.Context, fileID string) (previewURL string, err error)

	// GetDownloadURL returns a URL that can be used to download a file.
	GetDownloadURL(ctx context.Context, fileID string) (downloadURL string, err error)

	// DownloadFile streams the raw bytes of a file from MyDocs. Returns the
	// response body (caller must Close), and file metadata (name, mime type, size)
	// so the caller can set Content-Disposition / Content-Type correctly.
	DownloadFile(ctx context.Context, fileID string) (io.ReadCloser, *DmsFile, error)

	// UpdateMetadata updates the metadata tags on a file in Documenta.
	UpdateMetadata(ctx context.Context, fileID string, metadata map[string]string) error

	// DeleteFile removes a file from Documenta.
	DeleteFile(ctx context.Context, fileID string) error

	// ── Browsing & Search ──

	// ListFiles lists files and folders within a workspace/parent folder.
	ListFiles(ctx context.Context, workspaceSlug string, parentID string) (*DmsListResult, error)

	// SearchFiles searches for files across the workspace.
	SearchFiles(ctx context.Context, workspaceSlug string, query string) (*DmsSearchResult, error)

	// SearchFilesWithTags searches by query text and/or metadata tag filters.
	SearchFilesWithTags(ctx context.Context, workspaceSlug string, query string, tags map[string]string) (*DmsSearchResult, error)

	// GetFileInfo returns metadata for a single file.
	GetFileInfo(ctx context.Context, fileID string) (*DmsFile, error)

	// GetFileBreadcrumb returns the folder chain from workspace root down to
	// (but excluding) the file itself. Each entry is (uuid, name). Used by the
	// frontend Documents page to expand the folder tree when deep-linking to a file.
	GetFileBreadcrumb(ctx context.Context, fileID string) ([]DmsBreadcrumbEntry, error)

	// ── Comments ──

	// GetComments returns comments on a file.
	GetComments(ctx context.Context, fileID string) ([]DmsComment, error)

	// AddComment adds a comment to a file.
	AddComment(ctx context.Context, fileID string, content string) error

	// ── Tags ──

	// GetTags returns the metadata tags on a file.
	GetTags(ctx context.Context, fileID string) (map[string]string, error)

	// SetTags sets metadata tags on a file (replaces existing).
	SetTags(ctx context.Context, fileID string, tags map[string]string) error

	// ── Versions ──

	// ListVersions returns all versions of a file.
	ListVersions(ctx context.Context, fileID string) ([]DmsVersion, error)

	// UploadVersion uploads a new version of a file.
	UploadVersion(ctx context.Context, fileID string, fileName string, fileData io.Reader, fileSize int64, description string) (*DmsVersion, error)

	// DownloadVersion returns a reader for a specific version's content.
	DownloadVersion(ctx context.Context, versionUUID string) (io.ReadCloser, string, error)

	// RollbackVersion restores a previous version of a file.
	RollbackVersion(ctx context.Context, fileID string, versionUUID string) (*DmsVersion, error)
}
