package migrations

import (
	"fmt"
	"log"
	"strings"

	"gorm.io/gorm"
)

// MigrateUserPhoneUnique creates the partial unique index on users.phone that
// user_service.Register already assumes exists — it maps a 23505 on
// "idx_users_phone" to a "phone number already in use" message, but the index
// was never actually created. Until now phone uniqueness was enforced only by an
// application-level ExistsByPhone check, which is racy under concurrent creates.
//
// The index is partial on two conditions:
//   - a non-blank phone — phone is optional, so blank values must not collide with
//     each other (same shape as idx_users_national_id).
//   - deleted_at IS NULL — Postgres knows nothing about GORM's soft deletes, and
//     ExistsByPhone only looks at live rows; without this a soft-deleted user
//     would keep its number reserved forever while the app reported it as free.
//
// Idempotent: safe to run on every startup.
func MigrateUserPhoneUnique(db *gorm.DB) error {
	if !hasTable(db, "users") {
		return nil
	}

	if err := db.Exec(`
		CREATE UNIQUE INDEX IF NOT EXISTS idx_users_phone
		ON users (phone)
		WHERE phone <> '' AND deleted_at IS NULL
	`).Error; err != nil {
		// Don't block startup, and don't silently blank someone's phone number to
		// force the index through. Report the offending numbers so they can be
		// cleaned up by hand; meanwhile ExistsByPhone still guards new writes.
		log.Printf("Warning: failed to create unique index on users.phone: %v", err)
		logDuplicateUserPhones(db)
	}

	return nil
}

// logDuplicateUserPhones lists the phone numbers that are blocking the unique index.
func logDuplicateUserPhones(db *gorm.DB) {
	var duplicates []struct {
		Phone string
		Count int64
	}

	err := db.Table("users").
		Select("phone, COUNT(*) AS count").
		Where("phone <> '' AND deleted_at IS NULL").
		Group("phone").
		Having("COUNT(*) > 1").
		Order("count DESC").
		Scan(&duplicates).Error
	if err != nil {
		log.Printf("Warning: could not list duplicate user phone numbers: %v", err)
		return
	}

	parts := make([]string, 0, len(duplicates))
	for _, d := range duplicates {
		parts = append(parts, fmt.Sprintf("%s (%d users)", d.Phone, d.Count))
	}
	log.Printf("users.phone is not unique yet - resolve these duplicates and restart: %s", strings.Join(parts, ", "))
}
