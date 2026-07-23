package migrations

import (
	"fmt"
	"log"

	"gorm.io/gorm"
)

// orgCodePrefix mirrors repository.orgCodePrefix — the fixed prefix for the
// auto-generated Organization Code (e.g. ORG-000001).
const orgCodePrefix = "ORG-"

// MigrateDepartmentCodeBackfill assigns a unique, sequential Organization Code
// (ORG-000001, ORG-000002, …) to every department row that currently has no code.
// It continues numbering from the current maximum so it composes with codes created
// at runtime. Idempotent: it only touches rows with a NULL/empty code, so re-running
// once every row has a code is a no-op.
func MigrateDepartmentCodeBackfill(db *gorm.DB) error {
	var hasTable bool
	if err := db.Raw(
		`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'departments')`,
	).Scan(&hasTable).Error; err != nil || !hasTable {
		return nil
	}

	// Rows needing a code, ordered so the assignment is deterministic.
	var ids []string
	if err := db.Raw(
		`SELECT id FROM departments WHERE (code IS NULL OR code = '') AND deleted_at IS NULL ORDER BY created_at`,
	).Scan(&ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}

	// Continue numbering from the current maximum ORG- code (including soft-deleted
	// rows via a plain scan of the whole table) so codes are never reused.
	var maxCode *string
	if err := db.Raw(
		`SELECT MAX(code) FROM departments WHERE code LIKE ?`, orgCodePrefix+"%",
	).Scan(&maxCode).Error; err != nil {
		return err
	}
	nextSeq := 1
	if maxCode != nil && *maxCode != "" {
		var seq int
		if _, err := fmt.Sscanf(*maxCode, orgCodePrefix+"%d", &seq); err == nil {
			nextSeq = seq + 1
		}
	}

	updated := 0
	for _, id := range ids {
		code := fmt.Sprintf("%s%06d", orgCodePrefix, nextSeq)
		if err := db.Exec(`UPDATE departments SET code = ? WHERE id = ?`, code, id).Error; err != nil {
			log.Printf("department code backfill: failed to update %s: %v", id, err)
			continue
		}
		nextSeq++
		updated++
	}
	log.Printf("department code backfill: assigned codes to %d/%d departments", updated, len(ids))
	return nil
}
