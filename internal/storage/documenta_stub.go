package storage

import (
	"context"
	"io"
	"log"

	"github.com/google/uuid"
)

// StubDocumentaClient is a placeholder implementation that logs all calls
// and returns mock IDs. Replace with the real Documenta HTTP client when
// the API documentation is available.
type StubDocumentaClient struct{}

func NewStubDocumentaClient() DocumentaClient {
	return &StubDocumentaClient{}
}

func (s *StubDocumentaClient) CreateFolder(ctx context.Context, workspaceName, goalTitle string) (string, error) {
	id := "stub-folder-" + uuid.New().String()
	log.Printf("[DOCUMENTA STUB] CreateFolder: workspace=%s, goal=%s → folderID=%s", workspaceName, goalTitle, id)
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
