package migrations

import (
	"gorm.io/gorm"
)

// MigrateIncidentMerges creates the incident_merges table for tracking merge relationships
func MigrateIncidentMerges(db *gorm.DB) error {
	migrationSQL := `
	-- Create incident_merges table to track merge relationships
	CREATE TABLE IF NOT EXISTS incident_merges (
		id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
		master_incident_id UUID NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
		related_incident_id UUID NOT NULL REFERENCES incidents(id) ON DELETE CASCADE,
		merged_by_user_id UUID REFERENCES users(id),
		merged_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
		unmerged_by_user_id UUID REFERENCES users(id),
		unmerged_at TIMESTAMP WITH TIME ZONE,
		is_active BOOLEAN DEFAULT TRUE,
		notes TEXT,
		UNIQUE(master_incident_id, related_incident_id)
	);

	-- Create indexes for performance
	CREATE INDEX IF NOT EXISTS idx_incident_merges_master ON incident_merges(master_incident_id);
	CREATE INDEX IF NOT EXISTS idx_incident_merges_related ON incident_merges(related_incident_id);
	CREATE INDEX IF NOT EXISTS idx_incident_merges_active ON incident_merges(is_active) WHERE is_active = TRUE;
	`

	return db.Exec(migrationSQL).Error
}
