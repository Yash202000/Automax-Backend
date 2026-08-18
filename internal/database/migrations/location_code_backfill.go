package migrations

import (
	"fmt"
	"log"

	"gorm.io/gorm"
)

// locCodePrefix mirrors repository.locCodePrefix — the fixed prefix for the
// auto-generated Location Code (e.g. loc-000001).
const locCodePrefix = "loc-"

// MigrateLocationCodeBackfill assigns a unique, sequential Location Code (loc-000001,
// loc-000002, …) to every location row that currently has no code. It continues
// numbering from the current maximum so it composes with codes created at
// runtime. Idempotent: it only touches rows with a NULL/empty code, so re-running
// once every row has a code is a no-op.
func MigrateLocationCodeBackfill(db *gorm.DB) error {
	var hasTable bool
	if err := db.Raw(
		`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'locations')`,
	).Scan(&hasTable).Error; err != nil || !hasTable {
		return nil
	}

	var ids []string
	if err := db.Raw(
		`SELECT id FROM locations WHERE (code IS NULL OR code = '') AND deleted_at IS NULL ORDER BY created_at`,
	).Scan(&ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}

	var maxCode *string
	if err := db.Raw(
		`SELECT MAX(code) FROM locations WHERE code LIKE ?`, locCodePrefix+"%",
	).Scan(&maxCode).Error; err != nil {
		return err
	}
	nextSeq := 1
	if maxCode != nil && *maxCode != "" {
		var seq int
		if _, err := fmt.Sscanf(*maxCode, locCodePrefix+"%d", &seq); err == nil {
			nextSeq = seq + 1
		}
	}

	updated := 0
	for _, id := range ids {
		code := fmt.Sprintf("%s%06d", locCodePrefix, nextSeq)
		if err := db.Exec(`UPDATE locations SET code = ? WHERE id = ?`, code, id).Error; err != nil {
			log.Printf("location code backfill: failed to update %s: %v", id, err)
			continue
		}
		nextSeq++
		updated++
	}
	log.Printf("location code backfill: assigned codes to %d/%d locations", updated, len(ids))
	return nil
}
