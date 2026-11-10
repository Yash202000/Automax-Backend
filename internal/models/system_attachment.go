package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// SystemAttachment is a file uploaded by an authenticated user that may be
// previewed/downloaded by anyone (no auth). Files uploaded in the same request
// share a BatchID and a unique MinIO folder: public/<batch_id>/.
type SystemAttachment struct {
	ID       uuid.UUID `gorm:"type:uuid;primary_key" json:"id"`
	BatchID  uuid.UUID `gorm:"type:uuid;index;not null" json:"batch_id"`
	FileName string    `gorm:"size:255;not null" json:"file_name"`
	FileSize int64     `json:"file_size"`
	MimeType string    `gorm:"size:100" json:"mime_type"`
	FilePath string    `gorm:"size:500;not null" json:"-"`
	// Module is the config module the file belongs to (e.g. "settings" for settings-attachments/logo),
	// or SystemAttachmentDefaultModule for uploads that aren't tied to a config module.
	Module string `gorm:"size:100;not null;default:system;index" json:"module"`

	UploadedByID uuid.UUID `gorm:"type:uuid;index;not null" json:"uploaded_by_id"`

	CreatedAt time.Time      `json:"created_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// SystemAttachmentDefaultModule is the module recorded for files uploaded through the generic
// system-attachments endpoint. It is reserved: config attachment modules cannot use it.
const SystemAttachmentDefaultModule = "system"

type SystemAttachmentResponse struct {
	ID         uuid.UUID `json:"id"`
	Module     string    `json:"module"`
	BatchID    uuid.UUID `json:"batch_id"`
	FileName   string    `json:"file_name"`
	FileSize   int64     `json:"file_size"`
	MimeType   string    `json:"mime_type"`
	URL        string    `json:"url"`
	PreviewURL string    `json:"preview_url"`
	CreatedAt  time.Time `json:"created_at"`
}
