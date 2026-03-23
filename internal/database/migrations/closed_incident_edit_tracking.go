package migrations

import (
	"gorm.io/gorm"
)

// MigrateClosedIncidentEditTracking adds columns for tracking closed incident edits
func MigrateClosedIncidentEditTracking(db *gorm.DB) error {
	migrationSQL := `
	-- Add closed_at column if it doesn't exist
	DO $$ 
	BEGIN 
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'incidents' AND column_name = 'closed_at') THEN
			ALTER TABLE incidents ADD COLUMN closed_at TIMESTAMP WITH TIME ZONE;
		END IF;
	END $$;

	-- Add closed_by column if it doesn't exist
	DO $$ 
	BEGIN 
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'incidents' AND column_name = 'closed_by') THEN
			ALTER TABLE incidents ADD COLUMN closed_by UUID REFERENCES users(id);
		END IF;
	END $$;

	-- Add post_closure_edits column (JSONB) if it doesn't exist
	DO $$ 
	BEGIN 
		IF NOT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name = 'incidents' AND column_name = 'post_closure_edits') THEN
			ALTER TABLE incidents ADD COLUMN post_closure_edits JSONB DEFAULT '[]'::jsonb;
		END IF;
	END $$;

	-- Create index on closed_at for performance
	CREATE INDEX IF NOT EXISTS idx_incidents_closed_at ON incidents(closed_at) WHERE closed_at IS NOT NULL;

	-- Create index on closed_by for performance
	CREATE INDEX IF NOT EXISTS idx_incidents_closed_by ON incidents(closed_by) WHERE closed_by IS NOT NULL;
	`

	return db.Exec(migrationSQL).Error
}
