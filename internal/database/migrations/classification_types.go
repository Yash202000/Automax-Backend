package migrations

import (
	"log"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// classificationRow is a minimal struct for reading the old type column via raw SQL
type classificationRow struct {
	ID   string `gorm:"column:id"`
	Type string `gorm:"column:type"`
}

// classificationTypeRow is a minimal struct for inserting into classification_types
type classificationTypeRow struct {
	ID               string    `gorm:"column:id"`
	ClassificationID string    `gorm:"column:classification_id"`
	Type             string    `gorm:"column:type"`
	CreatedAt        time.Time `gorm:"column:created_at"`
}

// MigrateClassificationTypes migrates the single `type` column on classifications
// into the new `classification_types` join table, preserving all existing data.
//
// Mapping rules:
//   - 'both'  → ["incident", "request"]
//   - 'all'   → ["incident", "request", "complaint", "query", "mobile", "ivr"]
//   - any other single value → [value]
func MigrateClassificationTypes(db *gorm.DB) error {
	// Check if the old type column still exists — if not, migration already done or table is fresh
	if !db.Migrator().HasColumn(&struct {
		ID   string `gorm:"table:classifications"`
		Type string `gorm:"column:type"`
	}{}, "type") {
		log.Println("classification_types migration: old type column not found, skipping")
		return nil
	}

	// Check if classification_types table exists and already has data
	var existingCount int64
	if err := db.Raw("SELECT COUNT(*) FROM classification_types").Scan(&existingCount).Error; err != nil {
		// Table might not exist yet — AutoMigrate runs before this, so this shouldn't happen
		log.Printf("classification_types migration: could not count rows: %v", err)
		return nil
	}
	if existingCount > 0 {
		log.Println("classification_types migration: table already populated, skipping")
		return nil
	}

	// Read all non-deleted classifications with their old type value
	var rows []classificationRow
	if err := db.Raw("SELECT id, COALESCE(type, 'incident') as type FROM classifications WHERE deleted_at IS NULL").Scan(&rows).Error; err != nil {
		return err
	}

	if len(rows) == 0 {
		log.Println("classification_types migration: no classifications found, skipping")
		return nil
	}

	typeMapping := map[string][]string{
		"incident":  {"incident"},
		"request":   {"request"},
		"complaint": {"complaint"},
		"query":     {"query"},
		"mobile":    {"mobile"},
		"ivr":       {"ivr"},
		"both":      {"incident", "request"},
		"all":       {"incident", "request", "complaint", "query", "mobile", "ivr"},
	}

	now := time.Now()
	var toInsert []classificationTypeRow
	for _, row := range rows {
		types, ok := typeMapping[row.Type]
		if !ok {
			// Unknown value — fall back to incident+request
			log.Printf("classification_types migration: unknown type %q for classification %s, defaulting to incident+request", row.Type, row.ID)
			types = []string{"incident", "request"}
		}
		for _, t := range types {
			toInsert = append(toInsert, classificationTypeRow{
				ID:               uuid.New().String(),
				ClassificationID: row.ID,
				Type:             t,
				CreatedAt:        now,
			})
		}
	}

	// Insert inside a transaction for safety
	return db.Transaction(func(tx *gorm.DB) error {
		for _, ct := range toInsert {
			if err := tx.Exec(
				"INSERT INTO classification_types (id, classification_id, type, created_at) VALUES (?, ?, ?, ?) ON CONFLICT DO NOTHING",
				ct.ID, ct.ClassificationID, ct.Type, ct.CreatedAt,
			).Error; err != nil {
				return err
			}
		}
		log.Printf("classification_types migration: inserted %d type rows for %d classifications", len(toInsert), len(rows))
		return nil
	})
}
