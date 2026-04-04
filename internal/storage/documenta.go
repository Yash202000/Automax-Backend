package storage

import (
	"context"
	"io"
)

// ════════════════════════════════════════════════════
// DMS Response Types
// ════════════════════════════════════════════════════

// DmsFile represents a file or folder in the Documenta DMS.
type DmsFile struct {
	UUID      string            `json:"uuid"`
	Name      string            `json:"name"`
	Type      string            `json:"type"` // "file" or "folder"
	Size      int64             `json:"size"`
	MimeType  string            `json:"mime_type"`
	Parent    string            `json:"parent"`
	CreatedAt string            `json:"created_at"`
	UpdatedAt string            `json:"updated_at"`
	Metadata  map[string]string `json:"metadata"`
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
}
