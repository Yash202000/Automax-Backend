package migrations

import (
	"gorm.io/gorm"
)

// MigrateWorkflowMergeRoles creates the workflow_merge_allowed_roles join table
// for configuring which roles can merge incidents per workflow
func MigrateWorkflowMergeRoles(db *gorm.DB) error {
	migrationSQL := `
	-- Create workflow_merge_allowed_roles join table
	CREATE TABLE IF NOT EXISTS workflow_merge_allowed_roles (
		workflow_id UUID NOT NULL REFERENCES workflows(id) ON DELETE CASCADE,
		role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
		created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
		PRIMARY KEY (workflow_id, role_id)
	);

	-- Create indexes for performance
	CREATE INDEX IF NOT EXISTS idx_workflow_merge_roles_workflow ON workflow_merge_allowed_roles(workflow_id);
	CREATE INDEX IF NOT EXISTS idx_workflow_merge_roles_role ON workflow_merge_allowed_roles(role_id);
	`

	return db.Exec(migrationSQL).Error
}
