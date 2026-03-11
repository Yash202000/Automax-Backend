package migrations

import (
	"gorm.io/gorm"
)

// MigrateTransitionAssignmentRoles creates the transition_assignment_roles join table
// and drops the old single assignment_role_id column from workflow_transitions
func MigrateTransitionAssignmentRoles(db *gorm.DB) error {
	migrationSQL := `
	-- Create transition_assignment_roles join table
	CREATE TABLE IF NOT EXISTS transition_assignment_roles (
		workflow_transition_id UUID NOT NULL REFERENCES workflow_transitions(id) ON DELETE CASCADE,
		role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
		PRIMARY KEY (workflow_transition_id, role_id)
	);

	-- Create indexes for performance
	CREATE INDEX IF NOT EXISTS idx_transition_assignment_roles_transition ON transition_assignment_roles(workflow_transition_id);
	CREATE INDEX IF NOT EXISTS idx_transition_assignment_roles_role ON transition_assignment_roles(role_id);

	-- Migrate existing single role assignments to the new join table
	INSERT INTO transition_assignment_roles (workflow_transition_id, role_id)
	SELECT id, assignment_role_id
	FROM workflow_transitions
	WHERE assignment_role_id IS NOT NULL
	ON CONFLICT DO NOTHING;

	-- Drop the old column
	ALTER TABLE workflow_transitions DROP COLUMN IF EXISTS assignment_role_id;
	`

	return db.Exec(migrationSQL).Error
}
