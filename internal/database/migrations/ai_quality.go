package migrations

import (
	"gorm.io/gorm"
)

// MigrateAIQuality adds the is_ai_verified column to incidents and the is_ai_qa column to workflow_states.
func MigrateAIQuality(db *gorm.DB) error {
	migrationSQL := `
	-- Add is_ai_verified to incidents
	DO $$
	BEGIN
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'incidents' AND column_name = 'is_ai_verified') THEN
			ALTER TABLE incidents ADD COLUMN is_ai_verified BOOLEAN NOT NULL DEFAULT FALSE;
		END IF;
	END $$;

	-- Add is_ai_qa to workflow_states
	DO $$
	BEGIN
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'workflow_states' AND column_name = 'is_ai_qa') THEN
			ALTER TABLE workflow_states ADD COLUMN is_ai_qa BOOLEAN NOT NULL DEFAULT FALSE;
		END IF;
	END $$;

	-- Index to speed up the monitor query
	CREATE INDEX IF NOT EXISTS idx_incidents_is_ai_verified ON incidents(is_ai_verified) WHERE is_ai_verified = TRUE;
	CREATE INDEX IF NOT EXISTS idx_workflow_states_is_ai_qa ON workflow_states(is_ai_qa) WHERE is_ai_qa = TRUE;
	`

	return db.Exec(migrationSQL).Error
}
