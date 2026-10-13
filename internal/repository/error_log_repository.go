package repository

import (
	"context"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type ErrorLogRepository interface {
	Create(ctx context.Context, e *models.ErrorLog) error
	List(ctx context.Context, f models.ErrorLogFilter) ([]models.ErrorLog, int64, error)
	FindByID(ctx context.Context, id uuid.UUID) (*models.ErrorLog, error)
	ListForExport(ctx context.Context, f models.ErrorLogFilter, max int) ([]models.ErrorLog, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, toStatus, note string, userID *uuid.UUID, userName string) error
	DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
	ListUsers(ctx context.Context) ([]models.ErrorLogUser, error)
}

type errorLogRepository struct {
	db *gorm.DB
}

func NewErrorLogRepository(db *gorm.DB) ErrorLogRepository {
	return &errorLogRepository{db: db}
}

func (r *errorLogRepository) Create(ctx context.Context, e *models.ErrorLog) error {
	return r.db.WithContext(ctx).Create(e).Error
}

func (r *errorLogRepository) ApplyFilter(q *gorm.DB, f models.ErrorLogFilter) *gorm.DB {
	if s := strings.TrimSpace(f.Search); s != "" {
		like := "%" + strings.ToLower(s) + "%"
		q = q.Where(`LOWER(error_code) LIKE ? OR LOWER(message) LIKE ? OR LOWER(user_name) LIKE ?
			OR LOWER(related_id) LIKE ? OR LOWER(related_label) LIKE ?`, like, like, like, like, like)
	}
	if f.Module != "" {
		q = q.Where("module = ?", f.Module)
	}
	if f.Severity != "" {
		q = q.Where("severity = ?", f.Severity)
	}
	if f.Status != "" {
		q = q.Where("status = ?", f.Status)
	}
	if f.UserID != nil {
		q = q.Where("user_id = ?", *f.UserID)
	}
	if t, err := time.Parse("2006-01-02", f.StartDate); err == nil {
		q = q.Where("occurred_at >= ?", t)
	}
	if t, err := time.Parse("2006-01-02", f.EndDate); err == nil {
		q = q.Where("occurred_at < ?", t.AddDate(0, 0, 1))
	}
	return q
}

func (r *errorLogRepository) List(ctx context.Context, f models.ErrorLogFilter) ([]models.ErrorLog, int64, error) {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 200 {
		f.Limit = 20
	}

	var total int64
	if err := r.ApplyFilter(r.db.WithContext(ctx).Model(&models.ErrorLog{}), f).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var logs []models.ErrorLog
	err := r.ApplyFilter(r.db.WithContext(ctx), f).
		Order("occurred_at DESC").
		Offset((f.Page - 1) * f.Limit).Limit(f.Limit).
		Find(&logs).Error
	return logs, total, err
}

func (r *errorLogRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.ErrorLog, error) {
	var e models.ErrorLog
	err := r.db.WithContext(ctx).
		Preload("History", func(db *gorm.DB) *gorm.DB { return db.Order("changed_at DESC") }).
		First(&e, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (r *errorLogRepository) ListForExport(ctx context.Context, f models.ErrorLogFilter, max int) ([]models.ErrorLog, error) {
	var logs []models.ErrorLog
	err := r.ApplyFilter(r.db.WithContext(ctx), f).Order("occurred_at DESC").Limit(max).Find(&logs).Error
	return logs, err
}

// UpdateStatus changes the status and records the change in the history, atomically.
func (r *errorLogRepository) UpdateStatus(ctx context.Context, id uuid.UUID, toStatus, note string, userID *uuid.UUID, userName string) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var e models.ErrorLog
		if err := tx.First(&e, "id = ?", id).Error; err != nil {
			return err
		}
		history := models.ErrorLogHistory{
			ErrorLogID:  e.ID,
			FromStatus:  e.Status,
			ToStatus:    toStatus,
			Note:        note,
			ChangedByID: userID,
			ChangedBy:   userName,
			ChangedAt:   time.Now(),
		}
		if err := tx.Create(&history).Error; err != nil {
			return err
		}
		return tx.Model(&models.ErrorLog{}).Where("id = ?", e.ID).Update("status", toStatus).Error
	})
}

// DeleteOlderThan removes logs (and, by cascade, their history) older than the cutoff.
func (r *errorLogRepository) DeleteOlderThan(ctx context.Context, cutoff time.Time) (int64, error) {
	var deleted int64
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// FK constraints are disabled in migrations, so remove history explicitly.
		old := tx.Model(&models.ErrorLog{}).Select("id").Where("occurred_at < ?", cutoff)
		if err := tx.Where("error_log_id IN (?)", old).Delete(&models.ErrorLogHistory{}).Error; err != nil {
			return err
		}
		res := tx.Where("occurred_at < ?", cutoff).Delete(&models.ErrorLog{})
		deleted = res.RowsAffected
		return res.Error
	})
	return deleted, err
}

// ListUsers returns the distinct users that appear in the error log (for the User filter).
func (r *errorLogRepository) ListUsers(ctx context.Context) ([]models.ErrorLogUser, error) {
	var users []models.ErrorLogUser
	err := r.db.WithContext(ctx).Model(&models.ErrorLog{}).
		Select("user_id AS id, MAX(user_name) AS name").
		Where("user_id IS NOT NULL").
		Group("user_id").
		Order("name ASC").
		Scan(&users).Error
	return users, err
}
