package migrations

import (
	"gorm.io/gorm"
)

// MigrateStateAssignmentRoles creates the state_assignment_roles join table
// which links WorkflowState to Role for creation-time user assignment.
func MigrateStateAssignmentRoles(db *gorm.DB) error {
	migrationSQL := `
	CREATE TABLE IF NOT EXISTS state_assignment_roles (
		workflow_state_id UUID NOT NULL REFERENCES workflow_states(id) ON DELETE CASCADE,
		role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
		PRIMARY KEY (workflow_state_id, role_id)
	);

	CREATE INDEX IF NOT EXISTS idx_state_assignment_roles_state ON state_assignment_roles(workflow_state_id);
	CREATE INDEX IF NOT EXISTS idx_state_assignment_roles_role ON state_assignment_roles(role_id);
	`

	return db.Exec(migrationSQL).Error
}
