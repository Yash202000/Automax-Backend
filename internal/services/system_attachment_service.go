package services

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	SystemAttachmentFolder = "public"
	// DefaultSystemAttachmentMaxSizeMB is used when no (valid) limit is configured.
	DefaultSystemAttachmentMaxSizeMB = 5
	// DefaultSystemAttachmentMaxFiles is used when no (valid) file-count limit is configured.
	DefaultSystemAttachmentMaxFiles = 10
)

var systemAttachmentAllowedTypes = map[string]struct{}{
	"image/jpg": {}, "image/jpeg": {}, "image/png": {}, "image/webp": {}, "image/gif": {},
}

// SystemAttachmentError is a client-facing failure. Handlers translate Key
// (an i18n key) with Args; Status is the HTTP status to return.
type SystemAttachmentError struct {
	Status int
	Key    string
	Args   []interface{}
}

func (e *SystemAttachmentError) Error() string { return e.Key }

func paErr(status int, key string, args ...interface{}) error {
	return &SystemAttachmentError{Status: status, Key: key, Args: args}
}

type SystemAttachmentService struct {
	db        *gorm.DB
	storage   *storage.MinIOStorage
	maxSizeMB int64
	maxFiles  int
}

// SystemAttachmentURLs returns the download and inline-preview URLs generated from an attachment
// id. baseURL is "protocol://hostname" of the request (see utils.RequestBaseURL); "" gives
// relative paths. Public attachments need no token: the unguessable id is the access key.
func SystemAttachmentURLs(baseURL string, id uuid.UUID) (url, previewURL string) {
	url = fmt.Sprintf("%s/api/v1/system/attachments/%s", baseURL, id)
	return url, url + "/preview"
}

// NewSystemAttachmentService: maxSizeMB is the per-file size limit in MB and
// maxFiles the per-request file count; values <= 0 fall back to the defaults.
func NewSystemAttachmentService(db *gorm.DB, storage *storage.MinIOStorage) *SystemAttachmentService {
	maxFiles := DefaultSystemAttachmentMaxFiles
	maxSizeMB := DefaultSystemAttachmentMaxSizeMB
	return &SystemAttachmentService{db: db, storage: storage, maxSizeMB: int64(maxSizeMB), maxFiles: maxFiles}
}

func sniffImageType(fh *multipart.FileHeader) (string, error) {
	f, err := fh.Open()
	if err != nil {
		return "", paErr(http.StatusBadRequest, "failed_to_open_uploaded_file")
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := io.ReadFull(f, head)
	mt := http.DetectContentType(head[:n])
	if _, ok := systemAttachmentAllowedTypes[mt]; !ok {
		return "", paErr(http.StatusBadRequest, "pubatt_invalid_type_fmt", fh.Filename)
	}
	return mt, nil
}

// UploadImages validates and stores images in one new unique folder
// public/<batch_id>/ and records them under module (see models.SystemAttachment.Module). All-or-nothing: on any failure the
// objects already written are removed.
func (s *SystemAttachmentService) UploadImages(ctx context.Context, module string, files []*multipart.FileHeader, userID uuid.UUID) (uuid.UUID, []models.SystemAttachment, error) {
	if len(files) == 0 {
		return uuid.Nil, nil, paErr(http.StatusBadRequest, "pubatt_no_files")
	}
	if len(files) > s.maxFiles {
		return uuid.Nil, nil, paErr(http.StatusBadRequest, "pubatt_too_many_files_fmt", s.maxFiles)
	}

	// Validate everything before uploading anything.
	types := make([]string, len(files))
	for i, fh := range files {
		if fh.Size > s.maxSizeMB<<20 {
			return uuid.Nil, nil, paErr(http.StatusBadRequest, "pubatt_file_too_large_fmt", fh.Filename, s.maxSizeMB)
		}
		mt, err := sniffImageType(fh)
		if err != nil {
			return uuid.Nil, nil, err
		}
		types[i] = mt
	}

	batchID := uuid.New()
	folder := fmt.Sprintf("%s/%s", SystemAttachmentFolder, batchID)
	records := make([]models.SystemAttachment, 0, len(files))
	uploaded := []string{}
	cleanup := func() {
		for _, p := range uploaded {
			_ = s.storage.DeleteFile(ctx, p)
		}
	}

	for i, fh := range files {
		f, err := fh.Open()
		if err != nil {
			cleanup()
			return uuid.Nil, nil, paErr(http.StatusInternalServerError, "failed_to_open_uploaded_file")
		}
		fh.Header.Set("Content-Type", types[i]) // store the sniffed type, not the client's claim
		path, err := s.storage.UploadFile(ctx, f, fh, folder)
		f.Close()
		if err != nil {
			cleanup()
			return uuid.Nil, nil, paErr(http.StatusInternalServerError, "failed_to_upload_file")
		}
		uploaded = append(uploaded, path)
		records = append(records, models.SystemAttachment{
			ID: uuid.New(), BatchID: batchID, FileName: filepath.Base(fh.Filename),
			FileSize: fh.Size, MimeType: types[i], FilePath: path, Module: module, UploadedByID: userID,
		})
	}

	if err := s.db.WithContext(ctx).Create(&records).Error; err != nil {
		cleanup()
		return uuid.Nil, nil, paErr(http.StatusInternalServerError, "pubatt_save_failed")
	}
	return batchID, records, nil
}

// SystemAttachmentView builds the view of one attachment with relative URLs generated from its id.
// The host is added per response from the request (see RequestBaseURL).
func SystemAttachmentView(a *models.SystemAttachment) models.ConfigAttachmentView {
	url, previewURL := SystemAttachmentURLs("", a.ID)
	return models.ConfigAttachmentView{
		ID: a.ID, FileName: a.FileName, FileSize: a.FileSize, MimeType: a.MimeType,
		URL: url, PreviewURL: previewURL,
	}
}

// Resolve loads the attachments with the given ids in one query and returns their views by id.
// Ids that no longer exist (deleted files) are simply absent from the result.
func (s *SystemAttachmentService) Resolve(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]models.ConfigAttachmentView, error) {
	out := make(map[uuid.UUID]models.ConfigAttachmentView, len(ids))
	if len(ids) == 0 {
		return out, nil
	}
	var rows []models.SystemAttachment
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, err
	}
	for i := range rows {
		out[rows[i].ID] = SystemAttachmentView(&rows[i])
	}
	return out, nil
}

// DeleteByIDs removes the attachments (MinIO objects and rows) with the given ids.
// Unknown ids are ignored. It keeps going after a failure and returns the first error.
func (s *SystemAttachmentService) DeleteByIDs(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	var rows []models.SystemAttachment
	if err := s.db.WithContext(ctx).Where("id IN ?", ids).Find(&rows).Error; err != nil {
		return err
	}
	var firstErr error
	var deleted []uuid.UUID
	for i := range rows {
		if err := s.storage.DeleteFile(ctx, rows[i].FilePath); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		deleted = append(deleted, rows[i].ID)
	}
	if len(deleted) > 0 {
		if err := s.db.WithContext(ctx).Where("id IN ?", deleted).Delete(&models.SystemAttachment{}).Error; err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
