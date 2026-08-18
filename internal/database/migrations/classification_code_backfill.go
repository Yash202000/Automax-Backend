package migrations

import (
	"fmt"
	"log"

	"gorm.io/gorm"
)

// clsCodePrefix mirrors repository.clsCodePrefix — the fixed prefix for the
// auto-generated Classification Code (e.g. cls-000001).
const clsCodePrefix = "cls-"

// MigrateClassificationCodeBackfill assigns a unique, sequential Classification Code
// (cls-000001, cls-000002, …) to every classification row that currently has no code.
// It continues numbering from the current maximum so it composes with codes created
// at runtime. Idempotent: it only touches rows with a NULL/empty code, so re-running
// once every row has a code is a no-op.
func MigrateClassificationCodeBackfill(db *gorm.DB) error {
	var hasTable bool
	if err := db.Raw(
		`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'classifications')`,
	).Scan(&hasTable).Error; err != nil || !hasTable {
		return nil
	}

	var ids []string
	if err := db.Raw(
		`SELECT id FROM classifications WHERE (code IS NULL OR code = '') AND deleted_at IS NULL ORDER BY created_at`,
	).Scan(&ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}

	var maxCode *string
	if err := db.Raw(
		`SELECT MAX(code) FROM classifications WHERE code LIKE ?`, clsCodePrefix+"%",
	).Scan(&maxCode).Error; err != nil {
		return err
	}
	nextSeq := 1
	if maxCode != nil && *maxCode != "" {
		var seq int
		if _, err := fmt.Sscanf(*maxCode, clsCodePrefix+"%d", &seq); err == nil {
			nextSeq = seq + 1
		}
	}

	updated := 0
	for _, id := range ids {
		code := fmt.Sprintf("%s%06d", clsCodePrefix, nextSeq)
		if err := db.Exec(`UPDATE classifications SET code = ? WHERE id = ?`, code, id).Error; err != nil {
			log.Printf("classification code backfill: failed to update %s: %v", id, err)
			continue
		}
		nextSeq++
		updated++
	}
	log.Printf("classification code backfill: assigned codes to %d/%d classifications", updated, len(ids))
	return nil
}
