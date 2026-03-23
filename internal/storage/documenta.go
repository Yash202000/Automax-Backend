package storage

import (
	"context"
	"io"
)

// DocumentaClient defines the interface for interacting with the Documenta DMS API.
// This will be replaced with a real implementation when API docs are provided.
type DocumentaClient interface {
	// CreateFolder creates a folder structure in Documenta for a goal.
	// Returns the folder ID assigned by Documenta.
	CreateFolder(ctx context.Context, workspaceName, goalTitle string) (folderID string, err error)

	// UploadFile uploads a file into a Documenta folder with metadata tags.
	// Returns the file ID assigned by Documenta.
	UploadFile(ctx context.Context, folderID, fileName string, fileData io.Reader, fileSize int64, metadata map[string]string) (fileID string, err error)

	// GetPreviewURL returns a URL that can be used to preview a file in the browser.
	GetPreviewURL(ctx context.Context, fileID string) (previewURL string, err error)

	// GetDownloadURL returns a URL that can be used to download a file.
	GetDownloadURL(ctx context.Context, fileID string) (downloadURL string, err error)

	// UpdateMetadata updates the metadata tags on a file in Documenta.
	UpdateMetadata(ctx context.Context, fileID string, metadata map[string]string) error

	// DeleteFile removes a file from Documenta.
	DeleteFile(ctx context.Context, fileID string) error
}
