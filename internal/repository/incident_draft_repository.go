package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Sentinels carry their i18n key as the message so handlers can translate them directly.
var (
	ErrDraftNotFound           = errors.New("incident_draft_not_found")
	ErrDraftVersionConflict    = errors.New("incident_draft_version_conflict")
	ErrDraftPublished          = errors.New("incident_draft_already_published")
	ErrDraftAttachmentNotFound = errors.New("incident_draft_attachment_not_found")
)

// DraftNumberSequence is the Postgres sequence behind DRF-xxxxx; created in database.Migrate.
const DraftNumberSequence = "incident_draft_seq"

type IncidentDraftRepository interface {
	Create(ctx context.Context, draft *models.IncidentDraft) error
	// FindByID returns the owner's unpublished draft, or ErrDraftNotFound (also for other
	// users' drafts and for published ones).
	FindByID(ctx context.Context, id, ownerID uuid.UUID) (*models.IncidentDraft, error)
	// FindByIDForUpdate takes a row lock and also returns published drafts, so publish, upload and
	// delete can tell "already published" (409) from "gone" (404); call it on a repository obtained from Transaction.
	FindByIDForUpdate(ctx context.Context, id, ownerID uuid.UUID) (*models.IncidentDraft, error)
	List(ctx context.Context, ownerID uuid.UUID, filter *models.IncidentDraftFilter) ([]models.IncidentDraft, int64, error)
	Count(ctx context.Context, ownerID uuid.UUID) (int64, error)
	// UpdateWithVersion replaces the draft if version still matches, else ErrDraftVersionConflict.
	// keep is written only when non-nil.
	UpdateWithVersion(ctx context.Context, draft *models.IncidentDraft, expectedVersion int, keep *bool) error
	// Delete removes the owner's draft and its attachment rows permanently and returns the
	// storage keys of the files so the caller can remove the objects.
	Delete(ctx context.Context, id, ownerID uuid.UUID) ([]string, error)
	// MarkPublished stamps published_at on a draft that became an incident. It also clears
	// keep so the row is pruned with the others once it ages out.
	MarkPublished(ctx context.Context, id, ownerID uuid.UUID) error
	// ReferencedPaths returns which of the storage keys an incident attachment (including a
	// soft-deleted one) still points at, so their objects must not be removed.
	ReferencedPaths(ctx context.Context, paths []string) (map[string]bool, error)

	// PruneExpired permanently deletes drafts (including published ones)
	// last updated before cutoff, except those flagged keep, together with their attachment
	// rows. It returns how many drafts went, their ids (for cache invalidation) and the storage
	// keys left to remove.
	PruneExpired(ctx context.Context, cutoff time.Time) (int64, []uuid.UUID, []string, error)

	// Touch bumps updated_at (resetting expiry) without changing version.
	Touch(ctx context.Context, id, ownerID uuid.UUID) error
	CreateAttachment(ctx context.Context, a *models.IncidentDraftAttachment) error
	ListAttachments(ctx context.Context, draftID uuid.UUID) ([]models.IncidentDraftAttachment, error)
	FindAttachment(ctx context.Context, draftID, attachmentID uuid.UUID) (*models.IncidentDraftAttachment, error)
	DeleteAttachment(ctx context.Context, draftID, attachmentID uuid.UUID) error
	// DeleteAttachmentsByID removes the rows only (their files now belong to an incident).
	DeleteAttachmentsByID(ctx context.Context, ids []uuid.UUID) error
	// Transaction runs fn with a repository bound to a single transaction.
	Transaction(ctx context.Context, fn func(tx IncidentDraftRepository) error) error
}

type incidentDraftRepository struct {
	db *gorm.DB
}

func NewIncidentDraftRepository(db *gorm.DB) IncidentDraftRepository {
	return &incidentDraftRepository{db: db}
}

func (r *incidentDraftRepository) Create(ctx context.Context, draft *models.IncidentDraft) error {
	var seq int64
	if err := r.db.WithContext(ctx).Raw(fmt.Sprintf("SELECT nextval('%s')", DraftNumberSequence)).Scan(&seq).Error; err != nil {
		return err
	}
	draft.DraftNumber = fmt.Sprintf("DRF-%05d", seq)
	if draft.Version == 0 {
		draft.Version = 1
	}
	return r.db.WithContext(ctx).Create(draft).Error
}

func (r *incidentDraftRepository) find(ctx context.Context, id, ownerID uuid.UUID, lock, unpublishedOnly bool) (*models.IncidentDraft, error) {
	q := r.db.WithContext(ctx)
	if unpublishedOnly {
		q = q.Where("published_at IS NULL")
	}
	if lock {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var d models.IncidentDraft
	if err := q.Where("id = ? AND created_by = ?", id, ownerID).First(&d).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDraftNotFound
		}
		return nil, err
	}
	return &d, nil
}

func (r *incidentDraftRepository) FindByID(ctx context.Context, id, ownerID uuid.UUID) (*models.IncidentDraft, error) {
	return r.find(ctx, id, ownerID, false, true)
}

func (r *incidentDraftRepository) FindByIDForUpdate(ctx context.Context, id, ownerID uuid.UUID) (*models.IncidentDraft, error) {
	return r.find(ctx, id, ownerID, true, false)
}

func (r *incidentDraftRepository) List(ctx context.Context, ownerID uuid.UUID, f *models.IncidentDraftFilter) ([]models.IncidentDraft, int64, error) {
	q := r.db.WithContext(ctx).Model(&models.IncidentDraft{}).Where("created_by = ? AND published_at IS NULL", ownerID)

	if s := strings.TrimSpace(f.Search); s != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s) + "%"
		q = q.Where(`draft_number ILIKE ? OR title ILIKE ? OR description ILIKE ? OR caller_info->>'phone' ILIKE ?
			OR concat_ws(' ', caller_info->>'first_name', caller_info->>'middle_name', caller_info->>'last_name') ILIKE ?`,
			like, like, like, like, like)
	}
	if f.ClassificationID != "" {
		q = q.Where("classification_id = ?", f.ClassificationID)
	}
	if f.RecordType != "" {
		q = q.Where("record_type = ?", f.RecordType)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	page, limit := f.Page, f.Limit
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	var drafts []models.IncidentDraft
	err := q.Preload("Classification").Preload("Location").
		Order("updated_at DESC").Offset((page - 1) * limit).Limit(limit).Find(&drafts).Error
	return drafts, total, err
}

func (r *incidentDraftRepository) Count(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	var n int64
	err := r.db.WithContext(ctx).Model(&models.IncidentDraft{}).Where("created_by = ? AND published_at IS NULL", ownerID).Count(&n).Error
	return n, err
}

func (r *incidentDraftRepository) UpdateWithVersion(ctx context.Context, d *models.IncidentDraft, expectedVersion int, keep *bool) error {
	updates := map[string]interface{}{
		"title":             d.Title,
		"source":            d.Source,
		"lookup_value_ids":  d.LookupValueIDs,
		"record_type":       d.RecordType,
		"classification_id": d.ClassificationID,
		"location_id":       d.LocationID,
		"description":       d.Description,
		"latitude":          d.Latitude,
		"longitude":         d.Longitude,
		"caller_info":       d.CallerInfo,
		"version":           expectedVersion + 1,
		"updated_at":        time.Now(),
	}
	if keep != nil {
		updates["keep"] = *keep
		d.Keep = *keep
	}

	res := r.db.WithContext(ctx).Model(&models.IncidentDraft{}).
		Where("id = ? AND created_by = ? AND version = ? AND published_at IS NULL", d.ID, d.CreatedBy, expectedVersion).
		Updates(updates)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		// Tell a missing/foreign draft, a published one and a stale version apart.
		var cur models.IncidentDraft
		if err := r.db.WithContext(ctx).Select("id", "published_at").
			Where("id = ? AND created_by = ?", d.ID, d.CreatedBy).First(&cur).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDraftNotFound
			}
			return err
		}
		if cur.PublishedAt != nil {
			return ErrDraftPublished
		}
		return ErrDraftVersionConflict
	}
	d.Version = expectedVersion + 1
	return nil
}

func (r *incidentDraftRepository) Delete(ctx context.Context, id, ownerID uuid.UUID) ([]string, error) {
	var paths []string
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ? AND created_by = ?", id, ownerID).Delete(&models.IncidentDraft{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrDraftNotFound
		}
		return deleteAttachmentsForDrafts(tx, []uuid.UUID{id}, &paths)
	})
	return paths, err
}

// deleteAttachmentsForDrafts collects the storage keys of the drafts' attachments and
// deletes the rows.
func deleteAttachmentsForDrafts(tx *gorm.DB, draftIDs []uuid.UUID, paths *[]string) error {
	if len(draftIDs) == 0 {
		return nil
	}
	if err := tx.Model(&models.IncidentDraftAttachment{}).Where("draft_id IN ?", draftIDs).Pluck("file_path", paths).Error; err != nil {
		return err
	}
	return tx.Where("draft_id IN ?", draftIDs).Delete(&models.IncidentDraftAttachment{}).Error
}

func (r *incidentDraftRepository) MarkPublished(ctx context.Context, id, ownerID uuid.UUID) error {
	now := time.Now()
	res := r.db.WithContext(ctx).Model(&models.IncidentDraft{}).
		Where("id = ? AND created_by = ?", id, ownerID).
		Updates(map[string]interface{}{"published_at": now, "keep": false})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrDraftNotFound
	}
	return nil
}

func (r *incidentDraftRepository) PruneExpired(ctx context.Context, cutoff time.Time) (int64, []uuid.UUID, []string, error) {
	var (
		count int64
		ids   []uuid.UUID
		paths []string
	)
	err := r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Lock the expired rows first so a concurrent edit or publish cannot slip between
		// reading the attachment keys and deleting the drafts.
		if err := tx.Model(&models.IncidentDraft{}).Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("keep = ? AND updated_at < ?", false, cutoff).Pluck("id", &ids).Error; err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}
		if err := deleteAttachmentsForDrafts(tx, ids, &paths); err != nil {
			return err
		}
		res := tx.Where("id IN ?", ids).Delete(&models.IncidentDraft{})
		count = res.RowsAffected
		return res.Error
	})
	if err != nil {
		return 0, nil, nil, err
	}
	return count, ids, paths, nil
}

func (r *incidentDraftRepository) ReferencedPaths(ctx context.Context, paths []string) (map[string]bool, error) {
	out := make(map[string]bool, len(paths))
	if len(paths) == 0 {
		return out, nil
	}
	var used []string
	if err := r.db.WithContext(ctx).Unscoped().Model(&models.IncidentAttachment{}).
		Where("file_path IN ?", paths).Distinct().Pluck("file_path", &used).Error; err != nil {
		return nil, err
	}
	for _, p := range used {
		out[p] = true
	}
	return out, nil
}

func (r *incidentDraftRepository) Touch(ctx context.Context, id, ownerID uuid.UUID) error {
	res := r.db.WithContext(ctx).Model(&models.IncidentDraft{}).
		Where("id = ? AND created_by = ?", id, ownerID).Update("updated_at", time.Now())
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrDraftNotFound
	}
	return nil
}

func (r *incidentDraftRepository) CreateAttachment(ctx context.Context, a *models.IncidentDraftAttachment) error {
	return r.db.WithContext(ctx).Create(a).Error
}

func (r *incidentDraftRepository) ListAttachments(ctx context.Context, draftID uuid.UUID) ([]models.IncidentDraftAttachment, error) {
	var list []models.IncidentDraftAttachment
	err := r.db.WithContext(ctx).Where("draft_id = ?", draftID).Order("created_at ASC").Find(&list).Error
	return list, err
}

func (r *incidentDraftRepository) FindAttachment(ctx context.Context, draftID, attachmentID uuid.UUID) (*models.IncidentDraftAttachment, error) {
	var a models.IncidentDraftAttachment
	if err := r.db.WithContext(ctx).Where("id = ? AND draft_id = ?", attachmentID, draftID).First(&a).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDraftAttachmentNotFound
		}
		return nil, err
	}
	return &a, nil
}

func (r *incidentDraftRepository) DeleteAttachment(ctx context.Context, draftID, attachmentID uuid.UUID) error {
	res := r.db.WithContext(ctx).Where("id = ? AND draft_id = ?", attachmentID, draftID).Delete(&models.IncidentDraftAttachment{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrDraftAttachmentNotFound
	}
	return nil
}

func (r *incidentDraftRepository) DeleteAttachmentsByID(ctx context.Context, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.WithContext(ctx).Where("id IN ?", ids).Delete(&models.IncidentDraftAttachment{}).Error
}

func (r *incidentDraftRepository) Transaction(ctx context.Context, fn func(tx IncidentDraftRepository) error) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return fn(&incidentDraftRepository{db: tx})
	})
}
