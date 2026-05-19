package migrations

import (
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func MigrateUserNationalID(db *gorm.DB) error {
	addColumnSQL := `ALTER TABLE users ADD COLUMN IF NOT EXISTS national_id VARCHAR(255) NOT NULL DEFAULT '';`
	if err := db.Exec(addColumnSQL).Error; err != nil {
		return fmt.Errorf("add national_id column: %w", err)
	}

	var emptyIDs []string
	if err := db.Table("users").
		Where("national_id = '' OR national_id IS NULL").
		Pluck("id", &emptyIDs).Error; err != nil {
		return fmt.Errorf("find empty national_id rows: %w", err)
	}
	for _, id := range emptyIDs {
		uid := uuid.New().String()
		if err := db.Exec(`UPDATE users SET national_id = ? WHERE id = ?`, uid, id).Error; err != nil {
			return fmt.Errorf("backfill national_id for user %s: %w", id, err)
		}
	}

	db.Exec(`DROP INDEX IF EXISTS idx_users_national_id`)

	createIndexSQL := `CREATE UNIQUE INDEX IF NOT EXISTS idx_users_national_id ON users(national_id) WHERE national_id <> '';`
	return db.Exec(createIndexSQL).Error
}
