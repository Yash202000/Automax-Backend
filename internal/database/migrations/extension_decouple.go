package migrations

import (
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// MigrateExtensionDecouple moves the extension off the users table into the
// dedicated extension_assignments (current) table and drops users.extension.
//
// It is idempotent and safe to run repeatedly:
//   - Backfills each user's non-empty users.extension into extension_assignments
//     (self-assigned) if the column still exists and no current row exists yet.
//   - Drops leftover columns from the earlier history-shaped table.
//   - Drops the users.extension column.
//
// Must run AFTER AutoMigrate so extension_assignments already exists.
func MigrateExtensionDecouple(db *gorm.DB) error {
	// Remove columns left over from the earlier history-shaped extension_assignments
	// FIRST — they carry NOT NULL constraints that would break the current-table insert.
	db.Exec(`ALTER TABLE extension_assignments DROP COLUMN IF EXISTS action`)
	db.Exec(`ALTER TABLE extension_assignments DROP COLUMN IF EXISTS previous_user_id`)

	// Only backfill while the source column still exists.
	if db.Migrator().HasColumn("users", "extension") {
		type extRow struct {
			ID        string
			Extension string
		}
		var rows []extRow
		if err := db.Table("users").
			Select("id", "extension").
			Where("extension IS NOT NULL AND extension <> ''").
			Where("deleted_at IS NULL").
			Scan(&rows).Error; err != nil {
			return fmt.Errorf("read users.extension for backfill: %w", err)
		}

		for _, r := range rows {
			// Skip if this extension or this user already has a current row
			// (handles reruns and any prior partial migration).
			var count int64
			if err := db.Table("extension_assignments").
				Where("extension = ? OR user_id = ?", r.Extension, r.ID).
				Count(&count).Error; err != nil {
				return fmt.Errorf("check existing assignment: %w", err)
			}
			if count > 0 {
				continue
			}
			if err := db.Exec(
				`INSERT INTO extension_assignments (id, extension, user_id, assigned_by, note, created_at, updated_at)
				 VALUES (?, ?, ?, ?, ?, NOW(), NOW())`,
				uuid.New(), r.Extension, r.ID, r.ID, "backfilled from users.extension",
			).Error; err != nil {
				return fmt.Errorf("backfill assignment for user %s: %w", r.ID, err)
			}
		}
	}

	// Drop the now-unused users.extension column.
	if err := db.Exec(`ALTER TABLE users DROP COLUMN IF EXISTS extension`).Error; err != nil {
		return fmt.Errorf("drop users.extension: %w", err)
	}

	return nil
}
