package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"path/filepath"
	"time"

	"github.com/automax/backend/internal/config"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinIOStorage struct {
	client     *minio.Client
	bucketName string
}

var ErrObjectNotFound = errors.New("object not found in storage")

var Storage *MinIOStorage

func NewMinIOStorage(cfg *config.MinIOConfig) (*MinIOStorage, error) {
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create MinIO client: %w", err)
	}

	ctx := context.Background()
	exists, err := client.BucketExists(ctx, cfg.BucketName)
	if err != nil {
		return nil, fmt.Errorf("failed to check bucket existence: %w", err)
	}

	if !exists {
		err = client.MakeBucket(ctx, cfg.BucketName, minio.MakeBucketOptions{})
		if err != nil {
			return nil, fmt.Errorf("failed to create bucket: %w", err)
		}
		log.Printf("Bucket '%s' created successfully\n", cfg.BucketName)

		policy := fmt.Sprintf(`{
			"Version": "2012-10-17",
			"Statement": [
				{
					"Effect": "Allow",
					"Principal": {"AWS": ["*"]},
					"Action": ["s3:GetObject"],
					"Resource": ["arn:aws:s3:::%s/avatars/*"]
				}
			]
		}`, cfg.BucketName)
		err = client.SetBucketPolicy(ctx, cfg.BucketName, policy)
		if err != nil {
			log.Printf("Warning: failed to set bucket policy: %v\n", err)
		}
	}

	storage := &MinIOStorage{
		client:     client,
		bucketName: cfg.BucketName,
	}
	Storage = storage
	log.Println("MinIO storage connected successfully")
	return storage, nil
}

func (s *MinIOStorage) UploadFile(ctx context.Context, file multipart.File, header *multipart.FileHeader, folder string) (string, error) {
	ext := filepath.Ext(header.Filename)
	filename := fmt.Sprintf("%s/%s%s", folder, uuid.New().String(), ext)

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	_, err := s.client.PutObject(ctx, s.bucketName, filename, file, header.Size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload file: %w", err)
	}

	return filename, nil
}

// UploadBytes uploads file content from bytes
func (s *MinIOStorage) UploadBytes(ctx context.Context, data []byte, filename, contentType, folder string) (string, error) {
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// Create unique object name
	objectName := fmt.Sprintf("%s/%s", folder, filename)

	reader := bytes.NewReader(data)
	_, err := s.client.PutObject(ctx, s.bucketName, objectName, reader, int64(len(data)), minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload file: %w", err)
	}

	return objectName, nil
}

func (s *MinIOStorage) UploadAvatar(ctx context.Context, file multipart.File, header *multipart.FileHeader, userID string) (string, error) {
	ext := filepath.Ext(header.Filename)
	filename := fmt.Sprintf("avatars/%s%s", userID, ext)

	contentType := header.Header.Get("Content-Type")
	if contentType == "" {
		contentType = "image/jpeg"
	}

	_, err := s.client.PutObject(ctx, s.bucketName, filename, file, header.Size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload avatar: %w", err)
	}

	return filename, nil
}

func (s *MinIOStorage) GetFileURL(ctx context.Context, objectName string) (string, error) {
	presignedURL, err := s.client.PresignedGetObject(ctx, s.bucketName, objectName, time.Hour*24, nil)
	if err != nil {
		return "", fmt.Errorf("failed to get presigned URL: %w", err)
	}
	return presignedURL.String(), nil
}

func (s *MinIOStorage) GetPublicURL(objectName string, endpoint string) string {
	return fmt.Sprintf("http://%s/%s/%s", endpoint, s.bucketName, objectName)
}

func (s *MinIOStorage) DeleteFile(ctx context.Context, objectName string) error {
	err := s.client.RemoveObject(ctx, s.bucketName, objectName, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}
	return nil
}

func (s *MinIOStorage) GetFile(ctx context.Context, objectName string) (io.ReadCloser, error) {
	object, err := s.client.GetObject(ctx, s.bucketName, objectName, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get file: %w", err)
	}
	// Eagerly verify the key exists. MinIO's GetObject is lazy — it only makes
	// the HTTP request on the first Read(), so a missing key would otherwise
	// surface as a confusing "copy file data: key does not exist" error in
	// callers that pipe the reader into io.Copy.
	if _, err := object.Stat(); err != nil {
		object.Close()
		return nil, fmt.Errorf("object not found in storage (key=%q): %w", objectName, err)
	}

	return object, nil
}

func (s *MinIOStorage) FileExists(ctx context.Context, objectName string) (bool, error) {
	_, err := s.client.StatObject(ctx, s.bucketName, objectName, minio.StatObjectOptions{})
	if err != nil {
		errResponse := minio.ToErrorResponse(err)
		if errResponse.Code == "NoSuchKey" {
			return false, nil
		}
		return false, err
	}
	return true, nil
}
