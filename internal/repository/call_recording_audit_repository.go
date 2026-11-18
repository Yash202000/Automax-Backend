package repository

import (
	"context"
	"strings"

	"github.com/automax/backend/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// returningCreatedAt reads the DB-stamped created_at back into the entry.
var returningCreatedAt = clause.Returning{Columns: []clause.Column{{Name: "created_at"}}}

// escapeLike escapes LIKE/ILIKE wildcards so user search text matches literally.
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(s)
}

// CallRecordingAuditRepository is append-only by design: it exposes Create and
// reads, and nothing that could change or remove an entry.
type CallRecordingAuditRepository interface {
	Create(ctx context.Context, entry *models.CallRecordingAudit) error
	List(ctx context.Context, filter *models.CallRecordingAuditFilter) ([]models.CallRecordingAuditItem, int64, error)
	Counts(ctx context.Context, filter *models.CallRecordingAuditFilter) (*models.CallRecordingAuditCounts, error)
}

type callRecordingAuditRepository struct {
	db *gorm.DB
}

func NewCallRecordingAuditRepository(db *gorm.DB) CallRecordingAuditRepository {
	return &callRecordingAuditRepository{db: db}
}

// Create inserts the entry. created_at is left to the database default (now())
// so every timestamp comes from the one server clock; Omit keeps GORM from
// sending the Go-side zero/now value, and RETURNING hands the DB value back.
func (r *callRecordingAuditRepository) Create(ctx context.Context, entry *models.CallRecordingAudit) error {
	return r.db.WithContext(ctx).
		Omit("CreatedAt").
		Clauses(returningCreatedAt).
		Create(entry).Error
}

// base builds the filtered FROM clause shared by List and Counts. The incident
// is resolved per row through incidents.call_uuid (first non-deleted incident
// raised from that call).
func (r *callRecordingAuditRepository) base(ctx context.Context, filter *models.CallRecordingAuditFilter, withActivity bool) *gorm.DB {
	q := r.db.WithContext(ctx).
		Table("call_recording_audits AS a").
		Joins(`LEFT JOIN LATERAL (
			SELECT i.id, i.incident_number FROM incidents i
			WHERE i.call_uuid = a.call_uuid AND a.call_uuid <> '' AND i.deleted_at IS NULL
			ORDER BY i.created_at ASC LIMIT 1
		) inc ON TRUE`)

	if withActivity && filter.Activity != "" {
		q = q.Where("a.activity = ?", filter.Activity)
	}
	if filter.UserID != nil {
		q = q.Where("a.user_id = ?", *filter.UserID)
	}
	if filter.CallLogID != nil {
		q = q.Where("a.call_log_id = ?", *filter.CallLogID)
	}
	if filter.StartDate != nil {
		q = q.Where("a.created_at >= ?", *filter.StartDate)
	}
	if filter.EndDate != nil {
		q = q.Where("a.created_at <= ?", *filter.EndDate)
	}
	if s := strings.TrimSpace(filter.Search); s != "" {
		like := "%" + escapeLike(s) + "%"
		q = q.Where(`(a.call_uuid ILIKE ? OR CAST(a.call_log_id AS text) ILIKE ?
			OR inc.incident_number ILIKE ? OR a.user_name ILIKE ? OR a.username ILIKE ?)`,
			like, like, like, like, like)
	}
	return q
}

func (r *callRecordingAuditRepository) List(ctx context.Context, filter *models.CallRecordingAuditFilter) ([]models.CallRecordingAuditItem, int64, error) {
	var total int64
	if err := r.base(ctx, filter, true).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	items := []models.CallRecordingAuditItem{}
	err := r.base(ctx, filter, true).
		Select(`a.id, a.call_log_id, a.call_uuid, inc.id AS incident_id, inc.incident_number,
			a.activity, a.user_id, a.user_name, a.username, a.user_role, a.created_at`).
		Order("a.created_at DESC, a.id DESC").
		Offset((filter.Page - 1) * filter.Limit).
		Limit(filter.Limit).
		Scan(&items).Error
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

func (r *callRecordingAuditRepository) Counts(ctx context.Context, filter *models.CallRecordingAuditFilter) (*models.CallRecordingAuditCounts, error) {
	var counts models.CallRecordingAuditCounts
	err := r.base(ctx, filter, false).
		Select(`COUNT(*) AS "all",
			COUNT(*) FILTER (WHERE a.activity = 'listened') AS listened,
			COUNT(*) FILTER (WHERE a.activity = 'downloaded') AS downloaded`).
		Scan(&counts).Error
	if err != nil {
		return nil, err
	}
	return &counts, nil
}
