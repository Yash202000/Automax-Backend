package migrations

import (
	"fmt"
	"log"

	"gorm.io/gorm"
)

// roleCodePrefix mirrors repository.roleCodePrefix — the fixed prefix for the
// auto-generated Role Code (e.g. role-000001).
const roleCodePrefix = "role-"

// MigrateRoleCodeBackfill assigns a unique, sequential Role Code (role-000001,
// role-000002, …) to every role row that currently has no code. It continues
// numbering from the current maximum so it composes with codes created at
// runtime. Idempotent: it only touches rows with a NULL/empty code, so re-running
// once every row has a code is a no-op.
func MigrateRoleCodeBackfill(db *gorm.DB) error {
	var hasTable bool
	if err := db.Raw(
		`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'roles')`,
	).Scan(&hasTable).Error; err != nil || !hasTable {
		return nil
	}

	var ids []string
	if err := db.Raw(
		`SELECT id FROM roles WHERE (code IS NULL OR code = '') AND deleted_at IS NULL ORDER BY created_at`,
	).Scan(&ids).Error; err != nil {
		return err
	}
	if len(ids) == 0 {
		return nil
	}

	var maxCode *string
	if err := db.Raw(
		`SELECT MAX(code) FROM roles WHERE code LIKE ?`, roleCodePrefix+"%",
	).Scan(&maxCode).Error; err != nil {
		return err
	}
	nextSeq := 1
	if maxCode != nil && *maxCode != "" {
		var seq int
		if _, err := fmt.Sscanf(*maxCode, roleCodePrefix+"%d", &seq); err == nil {
			nextSeq = seq + 1
		}
	}

	updated := 0
	for _, id := range ids {
		code := fmt.Sprintf("%s%06d", roleCodePrefix, nextSeq)
		if err := db.Exec(`UPDATE roles SET code = ? WHERE id = ?`, code, id).Error; err != nil {
			log.Printf("role code backfill: failed to update %s: %v", id, err)
			continue
		}
		nextSeq++
		updated++
	}
	log.Printf("role code backfill: assigned codes to %d/%d roles", updated, len(ids))
	return nil
}
