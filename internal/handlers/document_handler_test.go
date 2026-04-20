package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/internal/storage"
	"github.com/gofiber/fiber/v2"
)

type mockDocumentService struct {
	services.DocumentService
	listFilesFunc            func(ctx context.Context, parentID string, userEmail string) (*storage.DmsListResult, error)
	searchFilesFunc          func(ctx context.Context, query string, userEmail string) (*storage.DmsSearchResult, error)
	searchFilesWithTagsFunc  func(ctx context.Context, query string, tags map[string]string, userEmail string) (*storage.DmsSearchResult, error)
	getFileInfoFunc          func(ctx context.Context, fileID string, userEmail string) (*storage.DmsFile, error)
	getPreviewURLFunc        func(ctx context.Context, fileID string, userEmail string) (string, error)
	getDownloadURLFunc       func(ctx context.Context, fileID string, userEmail string) (string, error)
	getCommentsFunc          func(ctx context.Context, fileID string, userEmail string) ([]storage.DmsComment, error)
	addCommentFunc           func(ctx context.Context, fileID string, content string, userEmail string) error
	getTagsFunc              func(ctx context.Context, fileID string, userEmail string) (map[string]string, error)
	setTagsFunc              func(ctx context.Context, fileID string, tags map[string]string, userEmail string) error
	listVersionsFunc         func(ctx context.Context, fileID string, userEmail string) ([]storage.DmsVersion, error)
}

func (m *mockDocumentService) ListFiles(ctx context.Context, parentID string, userEmail string) (*storage.DmsListResult, error) {
	if m.listFilesFunc != nil {
		return m.listFilesFunc(ctx, parentID, userEmail)
	}
	return &storage.DmsListResult{Files: []storage.DmsFile{}}, nil
}

func (m *mockDocumentService) SearchFiles(ctx context.Context, query string, userEmail string) (*storage.DmsSearchResult, error) {
	if m.searchFilesFunc != nil {
		return m.searchFilesFunc(ctx, query, userEmail)
	}
	return &storage.DmsSearchResult{Files: []storage.DmsFile{}, Total: 0}, nil
}

func (m *mockDocumentService) SearchFilesWithTags(ctx context.Context, query string, tags map[string]string, userEmail string) (*storage.DmsSearchResult, error) {
	if m.searchFilesWithTagsFunc != nil {
		return m.searchFilesWithTagsFunc(ctx, query, tags, userEmail)
	}
	return &storage.DmsSearchResult{Files: []storage.DmsFile{}, Total: 0}, nil
}

func (m *mockDocumentService) GetFileInfo(ctx context.Context, fileID string, userEmail string) (*storage.DmsFile, error) {
	if m.getFileInfoFunc != nil {
		return m.getFileInfoFunc(ctx, fileID, userEmail)
	}
	return nil, nil
}

func (m *mockDocumentService) GetPreviewURL(ctx context.Context, fileID string, userEmail string) (string, error) {
	if m.getPreviewURLFunc != nil {
		return m.getPreviewURLFunc(ctx, fileID, userEmail)
	}
	return "", nil
}

func (m *mockDocumentService) GetDownloadURL(ctx context.Context, fileID string, userEmail string) (string, error) {
	if m.getDownloadURLFunc != nil {
		return m.getDownloadURLFunc(ctx, fileID, userEmail)
	}
	return "", nil
}

func (m *mockDocumentService) GetComments(ctx context.Context, fileID string, userEmail string) ([]storage.DmsComment, error) {
	if m.getCommentsFunc != nil {
		return m.getCommentsFunc(ctx, fileID, userEmail)
	}
	return []storage.DmsComment{}, nil
}

func (m *mockDocumentService) AddComment(ctx context.Context, fileID string, content string, userEmail string) error {
	if m.addCommentFunc != nil {
		return m.addCommentFunc(ctx, fileID, content, userEmail)
	}
	return nil
}

func (m *mockDocumentService) GetTags(ctx context.Context, fileID string, userEmail string) (map[string]string, error) {
	if m.getTagsFunc != nil {
		return m.getTagsFunc(ctx, fileID, userEmail)
	}
	return map[string]string{}, nil
}

func (m *mockDocumentService) SetTags(ctx context.Context, fileID string, tags map[string]string, userEmail string) error {
	if m.setTagsFunc != nil {
		return m.setTagsFunc(ctx, fileID, tags, userEmail)
	}
	return nil
}

func (m *mockDocumentService) ListVersions(ctx context.Context, fileID string, userEmail string) ([]storage.DmsVersion, error) {
	if m.listVersionsFunc != nil {
		return m.listVersionsFunc(ctx, fileID, userEmail)
	}
	return []storage.DmsVersion{}, nil
}

func TestDocumentHandler(t *testing.T) {
	mSvc := &mockDocumentService{}
	h := NewDocumentHandler(mSvc)

	app := fiber.New()
	app.Get("/documents/files", h.ListFiles)
	app.Post("/documents/search", h.SearchFiles)
	app.Get("/documents/files/:id/info", h.GetFileInfo)
	app.Get("/documents/files/:id/preview", h.GetPreviewURL)
	app.Get("/documents/files/:id/download", h.GetDownloadURL)
	app.Get("/documents/files/:id/comments", h.GetComments)
	app.Post("/documents/files/:id/comments", h.AddComment)
	app.Get("/documents/files/:id/tags", h.GetTags)
	app.Put("/documents/files/:id/tags", h.SetTags)
	app.Get("/documents/files/:id/versions", h.ListVersions)

	t.Run("ListFiles_Success", func(t *testing.T) {
		mSvc.listFilesFunc = func(ctx context.Context, parentID string, userEmail string) (*storage.DmsListResult, error) {
			return &storage.DmsListResult{
				Files: []storage.DmsFile{
					{UUID: "123", Name: "test.pdf", Type: "file"},
				},
			}, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/documents/files", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("ListFiles_WithParent", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/documents/files?parent=abc-123", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("SearchFiles_Success", func(t *testing.T) {
		mSvc.searchFilesFunc = func(ctx context.Context, query string, userEmail string) (*storage.DmsSearchResult, error) {
			return &storage.DmsSearchResult{
				Files: []storage.DmsFile{
					{UUID: "123", Name: "report.pdf"},
				},
				Total: 1,
			}, nil
		}

		payload := map[string]string{"query": "report"}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/documents/search", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("SearchFiles_EmptyQuery", func(t *testing.T) {
		payload := map[string]string{}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/documents/search", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusBadRequest {
			t.Errorf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("SearchFiles_WithTags", func(t *testing.T) {
		mSvc.searchFilesWithTagsFunc = func(ctx context.Context, query string, tags map[string]string, userEmail string) (*storage.DmsSearchResult, error) {
			return &storage.DmsSearchResult{
				Files: []storage.DmsFile{{UUID: "123", Name: "tagged.pdf"}},
				Total: 1,
			}, nil
		}

		payload := map[string]interface{}{
			"query": "",
			"tags":  map[string]string{"department": "IT"},
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/documents/search", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("GetFileInfo_Success", func(t *testing.T) {
		mSvc.getFileInfoFunc = func(ctx context.Context, fileID string, userEmail string) (*storage.DmsFile, error) {
			return &storage.DmsFile{
				UUID:  fileID,
				Name:  "test.pdf",
				Type:  "file",
				Size:  1024,
			}, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/documents/files/abc-123/info", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("GetFileInfo_Error", func(t *testing.T) {
		mSvc.getFileInfoFunc = func(ctx context.Context, fileID string, userEmail string) (*storage.DmsFile, error) {
			return nil, errors.New("file not found")
		}

		req := httptest.NewRequest(http.MethodGet, "/documents/files/invalid/info", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusInternalServerError {
			t.Errorf("expected 500, got %d", resp.StatusCode)
		}
	})

	t.Run("GetPreviewURL_Success", func(t *testing.T) {
		mSvc.getPreviewURLFunc = func(ctx context.Context, fileID string, userEmail string) (string, error) {
			return "https://preview.url/file.pdf", nil
		}

		req := httptest.NewRequest(http.MethodGet, "/documents/files/abc-123/preview", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("GetDownloadURL_Success", func(t *testing.T) {
		mSvc.getDownloadURLFunc = func(ctx context.Context, fileID string, userEmail string) (string, error) {
			return "https://download.url/file.pdf", nil
		}

		req := httptest.NewRequest(http.MethodGet, "/documents/files/abc-123/download", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("GetComments_Success", func(t *testing.T) {
		mSvc.getCommentsFunc = func(ctx context.Context, fileID string, userEmail string) ([]storage.DmsComment, error) {
			return []storage.DmsComment{
				{ID: "1", FileID: fileID, Author: "user@test.com", Content: "Good doc"},
			}, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/documents/files/abc-123/comments", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("AddComment_Success", func(t *testing.T) {
		mSvc.addCommentFunc = func(ctx context.Context, fileID string, content string, userEmail string) error {
			return nil
		}

		payload := map[string]string{"content": "New comment"}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPost, "/documents/files/abc-123/comments", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusCreated {
			t.Errorf("expected 201, got %d", resp.StatusCode)
		}
	})

	t.Run("GetTags_Success", func(t *testing.T) {
		mSvc.getTagsFunc = func(ctx context.Context, fileID string, userEmail string) (map[string]string, error) {
			return map[string]string{"department": "IT", "project": "Alpha"}, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/documents/files/abc-123/tags", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("SetTags_Success", func(t *testing.T) {
		mSvc.setTagsFunc = func(ctx context.Context, fileID string, tags map[string]string, userEmail string) error {
			return nil
		}

		payload := map[string]string{"department": "HR"}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest(http.MethodPut, "/documents/files/abc-123/tags", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("ListVersions_Success", func(t *testing.T) {
		mSvc.listVersionsFunc = func(ctx context.Context, fileID string, userEmail string) ([]storage.DmsVersion, error) {
			return []storage.DmsVersion{
				{UUID: "v1", NodeUUID: fileID, VersionNumber: 1, Size: 1024},
			}, nil
		}

		req := httptest.NewRequest(http.MethodGet, "/documents/files/abc-123/versions", nil)
		resp, err := app.Test(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		if resp.StatusCode != fiber.StatusOK {
			t.Errorf("expected 200, got %d", resp.StatusCode)
		}
	})
}