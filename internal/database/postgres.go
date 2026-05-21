package database

import (
	"fmt"
	"log"
	"strings"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/database/migrations"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/pkg/utils"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var DB *gorm.DB

func Connect(cfg *config.DatabaseConfig) (*gorm.DB, error) {
	dsn := fmt.Sprintf(
		"host=%s user=%s password=%s dbname=%s port=%s sslmode=%s",
		cfg.Host, cfg.User, cfg.Password, cfg.DBName, cfg.Port, cfg.SSLMode,
	)

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	DB = db
	log.Println("Database connected successfully")
	return db, nil
}

func Migrate(db *gorm.DB) error {
	log.Println("Running database migrations...")
	// Manually create the ENUM type for call_status if it doesn't exist
	createEnumQuery := `
        DO $$
        BEGIN
            IF NOT EXISTS (SELECT 1 FROM pg_type WHERE typname = 'user_call_status') THEN
                CREATE TYPE user_call_status AS ENUM ('offline', 'online', 'busy', 'in_call', 'available');
            END IF;
        END $$;`

	if err := db.Exec(createEnumQuery).Error; err != nil {
		return fmt.Errorf("failed to create enum type: %w", err)
	}

	// License table schema migration: the old single-PEM public_key column has
	// been replaced by a JWKS JSON blob. Dropping here is idempotent — AutoMigrate
	// will add the new `jwks` column on the next step. Any pre-existing license row
	// becomes non-verifiable; LoadFromDB gracefully falls back to cached state.
	if err := db.Exec(`ALTER TABLE IF EXISTS licenses DROP COLUMN IF EXISTS public_key`).Error; err != nil {
		log.Printf("Warning: failed to drop legacy licenses.public_key column: %v", err)
	}

	// Use session that disables FK constraints during migration to prevent auto-FK issues
	migrationDB := db.Session(&gorm.Session{})
	migrationDB.Config.DisableForeignKeyConstraintWhenMigrating = true

	err := migrationDB.AutoMigrate(
		&models.Permission{},
		&models.Role{},
		&models.Category{},
		&models.Classification{},
		&models.ClassificationType{},
		&models.ClassificationCriticality{},
		&models.Location{},
		&models.Department{},
		&models.User{},
		&models.ActionLog{},
		&models.CallLog{},
		&models.CallLogAttachment{},
		&models.NotificationTemplate{},
		&models.NotificationLog{},
		&models.EscalationSLA{},
		// IncidentMerge MUST come before Incident (foreign key dependency)
		&models.IncidentMerge{},
		// Lookup models
		&models.LookupCategory{},
		&models.LookupValue{},
		// Workflow models
		&models.Workflow{},
		&models.WorkflowState{},
		&models.WorkflowTransition{},
		&models.TransitionRequirement{},
		&models.TransitionAction{},
		&models.TransitionFieldChange{},
		&models.CommentTemplate{},
		&models.FeedbackTemplate{},
		// Incident models
		&models.Incident{},
		&models.IncidentComment{},
		&models.IncidentAttachment{},
		&models.IncidentFeedback{},
		&models.IncidentTransitionHistory{},
		&models.IncidentRevision{},
		&models.IncidentRejectionLog{},
		// Report models
		&models.Report{},
		&models.ReportExecution{},
		&models.ReportTemplate{},
		// Application Links
		&models.ApplicationLink{},
		// Settings
		&models.Settings{},
		// Escalation Policies
		&models.EscalationPolicy{},
		&models.EscalationPolicyStep{},
		&models.EscalationPolicyStepTarget{},
		// Custom Escalation Groups
		&models.EscalationGroup{},
		&models.EscalationGroupTarget{},
		// Ready-to-Close expiry tracking
		&models.IncidentReadyToCloseEntry{},
		&models.DeviceToken{},
		&models.CallerSentiment{},
		// Goal Management models
		&models.Goal{},
		&models.GoalMetric{},
		&models.MetricHistory{},
		&models.GoalCollaborator{},
		&models.Evidence{},
		&models.EvidenceTransitionHistory{},
		&models.GoalCheckIn{},
		// Goal Metric approval workflow
		&models.GoalMetricValueChange{},
		&models.MetricTransitionHistory{},
		&models.MetricValueChangeTransitionHistory{},
		// Goal Templates
		&models.GoalTemplate{},
		// Metric Import Batch models
		&models.MetricImportBatch{},
		&models.MetricImportItem{},
		&models.MetricImportBatchTransitionHistory{},
		// Goal Comments
		&models.GoalComment{},
		// Performance Review models
		&models.ReviewCycle{},
		&models.ReviewAssignment{},
		&models.GoalScore{},
		// AI Quality Feedback
		&models.AIQualityFeedback{},
		// License Management
		&models.License{},
		// External Integration
		&models.IntegrationVariable{},
		&models.IntegrationScript{},
		&models.WorkflowStateTrigger{},
		&models.WorkflowTransitionTrigger{},
		&models.IntegrationExecutionLog{},
		&models.IncidentBridge{},
		&models.WebhookCallbackConfig{},
	)
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	// Drop problematic foreign key constraints on incidents table
	// Merge relationships are tracked via incident_merges table, not direct FK
	log.Println("Cleaning up auto-generated foreign key constraints...")
	db.Exec("ALTER TABLE incidents DROP CONSTRAINT IF EXISTS fk_incident_merges_master_incident")
	db.Exec("ALTER TABLE incidents DROP CONSTRAINT IF EXISTS incident_merges_master_incident_id_fkey")
	db.Exec("ALTER TABLE incidents DROP CONSTRAINT IF EXISTS fk_incidents_master_incident")

	// Drop FK constraints on performed_by_id so system-triggered actions (uuid.Nil) are allowed.
	// System actions (e.g. Ready-to-Close auto-revert) do not have a real user performer.
	db.Exec("ALTER TABLE incident_transition_histories DROP CONSTRAINT IF EXISTS fk_incident_transition_histories_performed_by")
	db.Exec("ALTER TABLE incident_transition_histories DROP CONSTRAINT IF EXISTS incident_transition_histories_performed_by_id_fkey")
	db.Exec("ALTER TABLE incident_revisions DROP CONSTRAINT IF EXISTS fk_incident_revisions_performed_by")
	db.Exec("ALTER TABLE incident_revisions DROP CONSTRAINT IF EXISTS incident_revisions_performed_by_id_fkey")
	db.Exec("ALTER TABLE evidence_transition_histories DROP CONSTRAINT IF EXISTS fk_evidence_transition_histories_performed_by")
	db.Exec("ALTER TABLE evidence_transition_histories DROP CONSTRAINT IF EXISTS evidence_transition_histories_performed_by_id_fkey")
	db.Exec("ALTER TABLE metric_import_batch_transition_histories DROP CONSTRAINT IF EXISTS fk_metric_import_batch_transition_histories_performed_by")
	db.Exec("ALTER TABLE metric_import_batch_transition_histories DROP CONSTRAINT IF EXISTS metric_import_batch_transition_histories_performed_by_id_fkey")
	db.Exec("ALTER TABLE metric_transition_histories DROP CONSTRAINT IF EXISTS fk_metric_transition_histories_performed_by")
	db.Exec("ALTER TABLE metric_transition_histories DROP CONSTRAINT IF EXISTS metric_transition_histories_performed_by_id_fkey")
	db.Exec("ALTER TABLE metric_value_change_transition_histories DROP CONSTRAINT IF EXISTS fk_metric_value_change_transition_histories_performed_by")
	db.Exec("ALTER TABLE metric_value_change_transition_histories DROP CONSTRAINT IF EXISTS metric_value_change_transition_histories_performed_by_id_fkey")

	db.Exec("ALTER TABLE lookup_categories ADD COLUMN IF NOT EXISTS redirect_url VARCHAR(500)")

	// Notification template enhancements: add categorisation and bilingual-linking columns.
	db.Exec("ALTER TABLE notification_templates ADD COLUMN IF NOT EXISTS name VARCHAR(200)")
	db.Exec("ALTER TABLE notification_templates ADD COLUMN IF NOT EXISTS module_type VARCHAR(50)")
	db.Exec("ALTER TABLE notification_templates ADD COLUMN IF NOT EXISTS action_type VARCHAR(50)")
	db.Exec("ALTER TABLE notification_templates ADD COLUMN IF NOT EXISTS variables TEXT")
	db.Exec("ALTER TABLE notification_templates ADD COLUMN IF NOT EXISTS transition_id UUID REFERENCES workflow_transitions(id) ON DELETE SET NULL")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_notification_templates_module_type ON notification_templates(module_type)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_notification_templates_action_type ON notification_templates(action_type)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_notification_templates_transition_id ON notification_templates(transition_id)")

	// Bilingual redesign: add new columns, migrate existing data, drop old single-language columns.
	db.Exec("ALTER TABLE notification_templates ADD COLUMN IF NOT EXISTS subject_en TEXT")
	db.Exec("ALTER TABLE notification_templates ADD COLUMN IF NOT EXISTS body_en    TEXT")
	db.Exec("ALTER TABLE notification_templates ADD COLUMN IF NOT EXISTS subject_ar TEXT")
	db.Exec("ALTER TABLE notification_templates ADD COLUMN IF NOT EXISTS body_ar    TEXT")
	db.Exec("UPDATE notification_templates SET subject_en = subject, body_en = body WHERE language = 'en' AND body_en IS NULL")
	db.Exec(`UPDATE notification_templates t SET subject_ar = ar.subject, body_ar = ar.body FROM notification_templates ar WHERE ar.language = 'ar' AND ar.code = t.code AND ar.channel = t.channel AND t.language = 'en'`)
	db.Exec("DELETE FROM notification_templates WHERE language = 'ar'")
	db.Exec("ALTER TABLE notification_templates DROP COLUMN IF EXISTS language")
	db.Exec("ALTER TABLE notification_templates DROP COLUMN IF EXISTS subject")
	db.Exec("ALTER TABLE notification_templates DROP COLUMN IF EXISTS body")
	db.Exec("ALTER TABLE notification_templates DROP COLUMN IF EXISTS description")
	// Enforce uniqueness of (code, channel) for non-deleted templates so duplicate codes
	// are rejected at the DB level in addition to the service-level check.
	db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS uq_notification_templates_code_channel ON notification_templates(code, channel) WHERE deleted_at IS NULL`)

	// Workflow code column was previously VARCHAR(50) which is too short for some existing workflows. Increase to 100 chars. Note: MySQL syntax is ALTER TABLE workflows MODIFY code VARCHAR(100) NOT NULL but Postgres is ALTER TABLE workflows ALTER COLUMN code TYPE VARCHAR(100). GORM's AutoMigrate doesn't handle this edge case, so we run raw SQL. Idempotent: safe to run repeatedly.
	db.Exec("ALTER TABLE workflows ALTER COLUMN code TYPE VARCHAR(100)")
	db.Exec("ALTER TABLE reports ALTER COLUMN timestamp_key TYPE VARCHAR(100) Default 'created_at'")

	// Migrate transition assignment roles (single → many-to-many)
	if err := migrations.MigrateTransitionAssignmentRoles(db); err != nil {
		log.Printf("Warning: transition assignment roles migration failed: %v", err)
	}

	// Migrate state assignment roles (creation-time user assignment join table)
	if err := migrations.MigrateStateAssignmentRoles(db); err != nil {
		log.Printf("Warning: state assignment roles migration failed: %v", err)
	}

	// Migrate closed incident edit tracking
	if err := migrations.MigrateClosedIncidentEditTracking(db); err != nil {
		log.Printf("Warning: closed incident edit tracking migration failed: %v", err)
	}

	// Migrate user mobile verified
	if err := migrations.MigrateUserMobileVerified(db); err != nil {
		log.Printf("Warning: user mobile verified migration failed: %v", err)
	}

	// Migrate AI quality columns
	if err := migrations.MigrateAIQuality(db); err != nil {
		log.Printf("Warning: AI quality migration failed: %v", err)
	}

	// Migrate classification single-type column → classification_types join table
	if err := migrations.MigrateClassificationTypes(db); err != nil {
		log.Printf("Warning: classification types migration failed: %v", err)
	}

	// Migrate user national_id column
	if err := migrations.MigrateUserNationalID(db); err != nil {
		log.Printf("Warning: user national_id migration failed: %v", err)
	}

	// Seed existing free-text goal categories as root Category rows
	// and back-fill goals.category_id. Idempotent: safe to run repeatedly.
	if err := migrateFreeTextGoalCategories(db); err != nil {
		log.Printf("Warning: goal category back-fill migration failed: %v", err)
	}

	// Partial Close feature columns — idempotent, safe to run repeatedly
	db.Exec("ALTER TABLE workflow_states ADD COLUMN IF NOT EXISTS is_partial_close BOOLEAN NOT NULL DEFAULT false")
	db.Exec("ALTER TABLE incidents ADD COLUMN IF NOT EXISTS partial_close_expires_at TIMESTAMPTZ")
	db.Exec("ALTER TABLE incidents ADD COLUMN IF NOT EXISTS partial_close_duration VARCHAR(100) NOT NULL DEFAULT ''")
	db.Exec("ALTER TABLE incidents ADD COLUMN IF NOT EXISTS partial_close_notified BOOLEAN NOT NULL DEFAULT false")

	// AD/LDAP user flag — idempotent
	db.Exec("ALTER TABLE users ADD COLUMN IF NOT EXISTS is_ad_user BOOLEAN NOT NULL DEFAULT false")

	log.Println("Database migrations completed")
	return nil
}

// migrateFreeTextGoalCategories seeds root Category rows from distinct
// non-empty `goals.category` values (where `category_id IS NULL`) and links
// those goals to the new Category rows. Idempotent.
func migrateFreeTextGoalCategories(db *gorm.DB) error {
	// Guard: only run if both columns exist on the goals table. On a fresh DB
	// the goals table may not exist yet during very first boot (it does after
	// AutoMigrate above, but we still double-check defensively).
	var hasTable bool
	if err := db.Raw(
		`SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'goals')`,
	).Scan(&hasTable).Error; err != nil || !hasTable {
		return nil
	}

	type row struct {
		Category string
	}
	var rows []row
	if err := db.Raw(
		`SELECT DISTINCT category FROM goals
		 WHERE category IS NOT NULL AND category <> ''
		   AND category_id IS NULL
		   AND deleted_at IS NULL`,
	).Scan(&rows).Error; err != nil {
		return fmt.Errorf("scan distinct goal categories: %w", err)
	}

	if len(rows) == 0 {
		return nil
	}

	sortOrder := 0
	for _, r := range rows {
		name := r.Category
		code := slugifyCategoryCode(name)
		if code == "" {
			continue
		}

		// Idempotency: check if a category with this code already exists.
		var existing models.Category
		lookup := db.Where("code = ?", code).First(&existing)
		if lookup.Error != nil && lookup.Error != gorm.ErrRecordNotFound {
			log.Printf("category seed lookup failed for %q: %v", code, lookup.Error)
			continue
		}

		var catID uuid.UUID
		if lookup.Error == gorm.ErrRecordNotFound {
			newCat := models.Category{
				Name:      name,
				Code:      code,
				Level:     0,
				Path:      "/" + code,
				IsActive:  true,
				SortOrder: sortOrder,
			}
			if err := db.Create(&newCat).Error; err != nil {
				log.Printf("failed to create category %q from legacy goal value: %v", code, err)
				continue
			}
			catID = newCat.ID
			sortOrder++
		} else {
			catID = existing.ID
		}

		// Back-fill goals.category_id for matching free-text rows.
		if err := db.Exec(
			`UPDATE goals SET category_id = ?
			 WHERE category = ? AND category_id IS NULL AND deleted_at IS NULL`,
			catID, name,
		).Error; err != nil {
			log.Printf("failed to back-fill goals.category_id for %q: %v", name, err)
		}
	}

	return nil
}

// slugifyCategoryCode converts a free-text category name to a lowercase,
// hyphen-separated code suitable for the Category.Code field.
func slugifyCategoryCode(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	prevHyphen := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			prevHyphen = false
		default:
			if !prevHyphen && b.Len() > 0 {
				b.WriteRune('-')
				prevHyphen = true
			}
		}
	}
	out := strings.TrimRight(b.String(), "-")
	if len(out) > 100 {
		out = out[:100]
	}
	return out
}

func Seed(db *gorm.DB) error {
	log.Println("Seeding database...")

	// Seed default permissions
	permissions := []models.Permission{
		// User permissions
		{Name: "View Users", Code: "users:view", Module: "users", Action: "view", Description: "View user list and details"},
		{Name: "Create Users", Code: "users:create", Module: "users", Action: "create", Description: "Create new users"},
		{Name: "Update Users", Code: "users:update", Module: "users", Action: "update", Description: "Update user information"},
		{Name: "Delete Users", Code: "users:delete", Module: "users", Action: "delete", Description: "Delete users"},

		// Role permissions
		{Name: "View Roles", Code: "roles:view", Module: "roles", Action: "view", Description: "View roles list"},
		{Name: "Create Roles", Code: "roles:create", Module: "roles", Action: "create", Description: "Create new roles"},
		{Name: "Update Roles", Code: "roles:update", Module: "roles", Action: "update", Description: "Update roles"},
		{Name: "Delete Roles", Code: "roles:delete", Module: "roles", Action: "delete", Description: "Delete roles"},

		// Permission management
		{Name: "View Permissions", Code: "permissions:view", Module: "permissions", Action: "view", Description: "View permissions"},
		{Name: "Manage Permissions", Code: "permissions:manage", Module: "permissions", Action: "manage", Description: "Manage permissions"},

		// Department permissions
		{Name: "View Departments", Code: "departments:view", Module: "departments", Action: "view", Description: "View departments"},
		{Name: "Create Departments", Code: "departments:create", Module: "departments", Action: "create", Description: "Create departments"},
		{Name: "Update Departments", Code: "departments:update", Module: "departments", Action: "update", Description: "Update departments"},
		{Name: "Delete Departments", Code: "departments:delete", Module: "departments", Action: "delete", Description: "Delete departments"},

		// Location permissions
		{Name: "View Locations", Code: "locations:view", Module: "locations", Action: "view", Description: "View locations"},
		{Name: "Create Locations", Code: "locations:create", Module: "locations", Action: "create", Description: "Create locations"},
		{Name: "Update Locations", Code: "locations:update", Module: "locations", Action: "update", Description: "Update locations"},
		{Name: "Delete Locations", Code: "locations:delete", Module: "locations", Action: "delete", Description: "Delete locations"},

		// Classification permissions
		{Name: "View Classifications", Code: "classifications:view", Module: "classifications", Action: "view", Description: "View classifications"},
		{Name: "Create Classifications", Code: "classifications:create", Module: "classifications", Action: "create", Description: "Create classifications"},
		{Name: "Update Classifications", Code: "classifications:update", Module: "classifications", Action: "update", Description: "Update classifications"},
		{Name: "Delete Classifications", Code: "classifications:delete", Module: "classifications", Action: "delete", Description: "Delete classifications"},

		// Category permissions (hierarchical taxonomy used by goals)
		{Name: "View Categories", Code: "categories:view", Module: "categories", Action: "view", Description: "View categories"},
		{Name: "Create Categories", Code: "categories:create", Module: "categories", Action: "create", Description: "Create categories"},
		{Name: "Update Categories", Code: "categories:update", Module: "categories", Action: "update", Description: "Update categories"},
		{Name: "Delete Categories", Code: "categories:delete", Module: "categories", Action: "delete", Description: "Delete categories"},

		// Settings permissions
		{Name: "View Settings", Code: "settings:view", Module: "settings", Action: "view", Description: "View system settings"},
		{Name: "Update Settings", Code: "settings:update", Module: "settings", Action: "update", Description: "Update system settings"},

		// Workflow permissions
		{Name: "View Workflows", Code: "workflows:view", Module: "workflows", Action: "view", Description: "View workflow templates"},
		{Name: "Create Workflows", Code: "workflows:create", Module: "workflows", Action: "create", Description: "Create workflow templates"},
		{Name: "Update Workflows", Code: "workflows:update", Module: "workflows", Action: "update", Description: "Update workflow templates"},
		{Name: "Delete Workflows", Code: "workflows:delete", Module: "workflows", Action: "delete", Description: "Delete workflow templates"},
		{Name: "Design Workflows", Code: "workflows:design", Module: "workflows", Action: "design", Description: "Access workflow designer"},

		// Incident permissions
		{Name: "View Incidents", Code: "incidents:view", Module: "incidents", Action: "view", Description: "View incidents"},
		{Name: "Create Incidents", Code: "incidents:create", Module: "incidents", Action: "create", Description: "Create new incidents"},
		{Name: "Update Incidents", Code: "incidents:update", Module: "incidents", Action: "update", Description: "Update incident fields"},
		{Name: "Delete Incidents", Code: "incidents:delete", Module: "incidents", Action: "delete", Description: "Delete incidents"},
		{Name: "Transition Incidents", Code: "incidents:transition", Module: "incidents", Action: "transition", Description: "Execute state transitions"},
		{Name: "Assign Incidents", Code: "incidents:assign", Module: "incidents", Action: "assign", Description: "Assign/reassign incidents"},
		{Name: "Comment on Incidents", Code: "incidents:comment", Module: "incidents", Action: "comment", Description: "Add comments to incidents"},
		{Name: "View All Incidents", Code: "incidents:view_all", Module: "incidents", Action: "view_all", Description: "View all incidents regardless of assignment"},
		{Name: "Manage SLA", Code: "incidents:manage_sla", Module: "incidents", Action: "manage_sla", Description: "Override SLA settings"},
		{Name: "Merge Incidents", Code: "incidents:merge", Module: "incidents", Action: "merge", Description: "Merge multiple incidents into one"},
		{Name: "Edit Closed Incidents", Code: "incidents:edit-closed", Module: "incidents", Action: "edit_closed", Description: "Edit summary/description of closed incidents"},
		{Name: "Request Info on Incidents", Code: "incidents:request-info", Module: "incidents", Action: "request_info", Description: "Request additional information from citizens"},

		// Request permissions
		{Name: "View Requests", Code: "requests:view", Module: "requests", Action: "view", Description: "View requests"},
		{Name: "Create Requests", Code: "requests:create", Module: "requests", Action: "create", Description: "Create new requests"},
		{Name: "Update Requests", Code: "requests:update", Module: "requests", Action: "update", Description: "Update request fields"},
		{Name: "Delete Requests", Code: "requests:delete", Module: "requests", Action: "delete", Description: "Delete requests"},
		{Name: "Transition Requests", Code: "requests:transition", Module: "requests", Action: "transition", Description: "Execute request state transitions"},
		{Name: "Assign Requests", Code: "requests:assign", Module: "requests", Action: "assign", Description: "Assign/reassign requests"},
		{Name: "Comment on Requests", Code: "requests:comment", Module: "requests", Action: "comment", Description: "Add comments to requests"},
		{Name: "View All Requests", Code: "requests:view_all", Module: "requests", Action: "view_all", Description: "View all requests regardless of assignment"},

		// Complaint permissions
		{Name: "View Complaints", Code: "complaints:view", Module: "complaints", Action: "view", Description: "View complaints"},
		{Name: "Create Complaints", Code: "complaints:create", Module: "complaints", Action: "create", Description: "Create new complaints"},
		{Name: "Update Complaints", Code: "complaints:update", Module: "complaints", Action: "update", Description: "Update complaint fields"},
		{Name: "Delete Complaints", Code: "complaints:delete", Module: "complaints", Action: "delete", Description: "Delete complaints"},
		{Name: "Transition Complaints", Code: "complaints:transition", Module: "complaints", Action: "transition", Description: "Execute complaint state transitions"},
		{Name: "Assign Complaints", Code: "complaints:assign", Module: "complaints", Action: "assign", Description: "Assign/reassign complaints"},
		{Name: "Comment on Complaints", Code: "complaints:comment", Module: "complaints", Action: "comment", Description: "Add comments to complaints"},
		{Name: "View All Complaints", Code: "complaints:view_all", Module: "complaints", Action: "view_all", Description: "View all complaints regardless of assignment"},

		// Query permissions
		{Name: "View Queries", Code: "queries:view", Module: "queries", Action: "view", Description: "View queries"},
		{Name: "Create Queries", Code: "queries:create", Module: "queries", Action: "create", Description: "Create new queries"},
		{Name: "Update Queries", Code: "queries:update", Module: "queries", Action: "update", Description: "Update query fields"},
		{Name: "Delete Queries", Code: "queries:delete", Module: "queries", Action: "delete", Description: "Delete queries"},
		{Name: "Transition Queries", Code: "queries:transition", Module: "queries", Action: "transition", Description: "Execute query state transitions"},
		{Name: "Assign Queries", Code: "queries:assign", Module: "queries", Action: "assign", Description: "Assign/reassign queries"},
		{Name: "Comment on Queries", Code: "queries:comment", Module: "queries", Action: "comment", Description: "Add comments to queries"},
		{Name: "View All Queries", Code: "queries:view_all", Module: "queries", Action: "view_all", Description: "View all queries regardless of assignment"},

		// Report permissions
		{Name: "View Reports", Code: "reports:view", Module: "reports", Action: "view", Description: "View reports"},
		{Name: "Create Reports", Code: "reports:create", Module: "reports", Action: "create", Description: "Create new reports"},
		{Name: "Update Reports", Code: "reports:update", Module: "reports", Action: "update", Description: "Update reports"},
		{Name: "Delete Reports", Code: "reports:delete", Module: "reports", Action: "delete", Description: "Delete reports"},

		// Action Log permissions
		{Name: "View Action Logs", Code: "action-logs:view", Module: "action-logs", Action: "view", Description: "View action logs"},
		{Name: "Delete Action Logs", Code: "action-logs:delete", Module: "action-logs", Action: "delete", Description: "Delete/cleanup action logs"},

		// Call Log permissions
		{Name: "View Call Logs", Code: "call-logs:view", Module: "call-logs", Action: "view", Description: "View call logs"},
		{Name: "Create Call Logs", Code: "call-logs:create", Module: "call-logs", Action: "create", Description: "Create call logs"},
		{Name: "Update Call Logs", Code: "call-logs:update", Module: "call-logs", Action: "update", Description: "Update call logs"},
		{Name: "Delete Call Logs", Code: "call-logs:delete", Module: "call-logs", Action: "delete", Description: "Delete call logs"},

		// Lookup permissions
		{Name: "View Lookups", Code: "lookups:view", Module: "lookups", Action: "view", Description: "View lookup categories and values"},
		{Name: "Create Lookups", Code: "lookups:create", Module: "lookups", Action: "create", Description: "Create lookup categories and values"},
		{Name: "Update Lookups", Code: "lookups:update", Module: "lookups", Action: "update", Description: "Update lookup categories and values"},
		{Name: "Delete Lookups", Code: "lookups:delete", Module: "lookups", Action: "delete", Description: "Delete lookup categories and values"},

		// Application Links permissions
		{Name: "View Application Links", Code: "application-links:view", Module: "application-links", Action: "view", Description: "View application links"},
		{Name: "Create Application Links", Code: "application-links:create", Module: "application-links", Action: "create", Description: "Create application links"},
		{Name: "Update Application Links", Code: "application-links:update", Module: "application-links", Action: "update", Description: "Update application links"},
		{Name: "Delete Application Links", Code: "application-links:delete", Module: "application-links", Action: "delete", Description: "Delete application links"},
		{Name: "Access Application Links on Dashboard", Code: "application-links:dashboard", Module: "application-links", Action: "dashboard", Description: "See and launch application links from the dashboard"},

		// Notification permissions
		{Name: "View Notifications", Code: "notifications:read", Module: "notifications", Action: "read", Description: "View notification logs"},
		{Name: "Send Notifications", Code: "notifications:send", Module: "notifications", Action: "send", Description: "Send email/SMS notifications"},
		{Name: "Create Draft Notifications", Code: "notifications:create", Module: "notifications", Action: "create", Description: "Create draft notifications"},
		{Name: "Update Draft Notifications", Code: "notifications:update", Module: "notifications", Action: "update", Description: "Update draft notifications"},
		{Name: "Delete Notifications", Code: "notifications:delete", Module: "notifications", Action: "delete", Description: "Delete notification logs"},

		// Template permissions
		{Name: "View Templates", Code: "templates:read", Module: "templates", Action: "read", Description: "View notification templates"},
		{Name: "Create Templates", Code: "templates:create", Module: "templates", Action: "create", Description: "Create notification templates"},
		{Name: "Update Templates", Code: "templates:update", Module: "templates", Action: "update", Description: "Update notification templates"},
		{Name: "Delete Templates", Code: "templates:delete", Module: "templates", Action: "delete", Description: "Delete notification templates"},

		// Escalation Group permissions
		{Name: "View Escalation Groups", Code: "escalation-groups:view", Module: "escalation-groups", Action: "view", Description: "View escalation groups list and details"},
		{Name: "Create Escalation Group", Code: "escalation-groups:create", Module: "escalation-groups", Action: "create", Description: "Create new escalation groups"},
		{Name: "Update Escalation Group", Code: "escalation-groups:update", Module: "escalation-groups", Action: "update", Description: "Update escalation group settings"},
		{Name: "Delete Escalation Group", Code: "escalation-groups:delete", Module: "escalation-groups", Action: "delete", Description: "Delete escalation groups"},
		{Name: "Assign Users to Escalation Group", Code: "escalation-groups:assign_users", Module: "escalation-groups", Action: "assign_users", Description: "Add or remove users from escalation groups"},
		{Name: "Manage Escalation Rules", Code: "escalation-groups:manage_rules", Module: "escalation-groups", Action: "manage_rules", Description: "Configure escalation frequency, channel, and classification rules"},

		// Goal permissions
		{Name: "View Goals", Code: "goals:view", Module: "goals", Action: "view", Description: "View goals"},
		{Name: "Create Goals", Code: "goals:create", Module: "goals", Action: "create", Description: "Create new goals"},
		{Name: "Update Goals", Code: "goals:update", Module: "goals", Action: "update", Description: "Update goals"},
		{Name: "Delete Goals", Code: "goals:delete", Module: "goals", Action: "delete", Description: "Delete goals"},
		{Name: "Assign Goals", Code: "goals:assign", Module: "goals", Action: "assign", Description: "Assign goal collaborators"},
		{Name: "Approve Goals", Code: "goals:approve", Module: "goals", Action: "approve", Description: "Approve/reject goal evidence"},

		// Caller Sentiment permissions
		{Name: "Create Caller Sentiment", Code: "caller-sentiment:create", Module: "caller-sentiment", Action: "create", Description: "Record a sentiment entry after a call"},
		{Name: "View Caller Sentiments", Code: "caller-sentiment:view", Module: "caller-sentiment", Action: "view", Description: "View all caller sentiment records and summaries"},

		// License permissions
		{Name: "View License", Code: "license:view", Module: "license", Action: "view", Description: "View license status and info"},
		{Name: "Manage License", Code: "license:manage", Module: "license", Action: "manage", Description: "Activate, deactivate, and manage license keys"},

		// Dashboard permissions
		{Name: "Goals Dashboard", Code: "dashboard:goals", Module: "dashboard", Action: "goals", Description: "Access goal cards on dashboard"},
		{Name: "Admin Dashboard", Code: "dashboard:admin", Module: "dashboard", Action: "admin", Description: "Access admin section cards on dashboard"},
		{Name: "Incidents Dashboard", Code: "dashboard:incidents", Module: "dashboard", Action: "incidents", Description: "Access incident cards on dashboard"},
		{Name: "Requests Dashboard", Code: "dashboard:requests", Module: "dashboard", Action: "requests", Description: "Access request cards on dashboard"},
		{Name: "Complaints Dashboard", Code: "dashboard:complaints", Module: "dashboard", Action: "complaints", Description: "Access complaint cards on dashboard"},
		{Name: "Queries Dashboard", Code: "dashboard:queries", Module: "dashboard", Action: "queries", Description: "Access query cards on dashboard"},
		{Name: "Workflows Dashboard", Code: "dashboard:workflows", Module: "dashboard", Action: "workflows", Description: "Access workflow cards on dashboard"},
		{Name: "CCM Dashboard", Code: "dashboard:ccm", Module: "dashboard", Action: "ccm", Description: "Access ccm cards on dashboard"},
	}

	for _, perm := range permissions {
		var existing models.Permission
		result := db.Where("code = ?", perm.Code).First(&existing)
		if result.Error == gorm.ErrRecordNotFound {
			if err := db.Create(&perm).Error; err != nil {
				log.Printf("Failed to create permission %s: %v", perm.Code, err)
			}
		}
	}

	// Seed default roles
	var allPerms []models.Permission
	db.Find(&allPerms)

	// Admin role gets all permissions
	var adminRole models.Role
	result := db.Where("code = ?", "admin").First(&adminRole)
	if result.Error == gorm.ErrRecordNotFound {
		adminRole = models.Role{
			Name:        "Administrator",
			Code:        "admin",
			Description: "Full system access",
			IsSystem:    true,
			IsActive:    true,
			Permissions: allPerms,
		}
		db.Create(&adminRole)
	} else {
		// Update existing admin role to have all permissions
		db.Model(&adminRole).Association("Permissions").Replace(allPerms)
	}

	// User role with basic permissions
	var userRole models.Role
	result = db.Where("code = ?", "user").First(&userRole)
	if result.Error == gorm.ErrRecordNotFound {
		var viewPerms []models.Permission
		db.Where("action = ?", "view").Find(&viewPerms)
		userRole = models.Role{
			Name:        "User",
			Code:        "user",
			Description: "Basic user access",
			IsSystem:    true,
			IsActive:    true,
			Permissions: viewPerms,
		}
		db.Create(&userRole)
	} else {
		// Update existing user role to have all view permissions
		var viewPerms []models.Permission
		db.Where("action = ?", "view").Find(&viewPerms)
		db.Model(&userRole).Association("Permissions").Replace(viewPerms)
	}

	// Manager role with broader permissions
	var managerRole models.Role
	result = db.Where("code = ?", "manager").First(&managerRole)
	if result.Error == gorm.ErrRecordNotFound {
		var managerPerms []models.Permission
		db.Where("action IN ?", []string{"view", "create", "update", "delete", "assign", "approve"}).Find(&managerPerms)
		managerRole = models.Role{
			Name:        "Manager",
			Code:        "manager",
			Description: "Department manager access",
			IsSystem:    true,
			IsActive:    true,
			Permissions: managerPerms,
		}
		db.Create(&managerRole)
	} else {
		// Update existing manager role to have full management permissions
		var managerPerms []models.Permission
		db.Where("action IN ?", []string{"view", "create", "update", "delete", "assign", "approve"}).Find(&managerPerms)
		db.Model(&managerRole).Association("Permissions").Replace(managerPerms)
	}

	// Create default super admin user
	var adminUser models.User
	result = db.Where("email = ?", "admin@automax.com").First(&adminUser)
	if result.Error == gorm.ErrRecordNotFound {
		hashedPassword, _ := utils.HashPassword("admin123")
		adminUser = models.User{
			Email:        "admin@automax.com",
			Username:     "admin",
			Password:     hashedPassword,
			FirstName:    "Super",
			LastName:     "Admin",
			IsActive:     true,
			IsSuperAdmin: true,
		}
		db.Create(&adminUser)
		db.Model(&adminUser).Association("Roles").Append(&adminRole)
	}

	// Seed default lookup categories
	seedLookupCategories(db)

	// Seed default evidence approval workflow
	seedEvidenceApprovalWorkflow(db)

	// Seed default metric & metric_value_change approval workflows
	seedMetricApprovalWorkflows(db)

	log.Println("Database seeding completed")
	return nil
}

func seedLookupCategories(db *gorm.DB) {
	// Priority category
	var priorityCategory models.LookupCategory
	result := db.Where("code = ?", "PRIORITY").First(&priorityCategory)
	if result.Error == gorm.ErrRecordNotFound {
		priorityCategory = models.LookupCategory{
			Code:        "PRIORITY",
			Name:        "Priority",
			NameAr:      "الأولوية",
			Description: "Incident priority levels",
			IsSystem:    true,
			IsActive:    true,
		}
		if err := db.Create(&priorityCategory).Error; err != nil {
			log.Printf("Failed to create priority category: %v", err)
		} else {
			// Create priority values
			priorityValues := []models.LookupValue{
				{CategoryID: priorityCategory.ID, Code: "CRITICAL", Name: "Critical", NameAr: "حرج", SortOrder: 1, Color: "#EF4444", IsDefault: false, IsActive: true},
				{CategoryID: priorityCategory.ID, Code: "HIGH", Name: "High", NameAr: "عالي", SortOrder: 2, Color: "#F97316", IsDefault: false, IsActive: true},
				{CategoryID: priorityCategory.ID, Code: "MEDIUM", Name: "Medium", NameAr: "متوسط", SortOrder: 3, Color: "#EAB308", IsDefault: true, IsActive: true},
				{CategoryID: priorityCategory.ID, Code: "LOW", Name: "Low", NameAr: "منخفض", SortOrder: 4, Color: "#3B82F6", IsDefault: false, IsActive: true},
				{CategoryID: priorityCategory.ID, Code: "VERY_LOW", Name: "Very Low", NameAr: "منخفض جداً", SortOrder: 5, Color: "#6B7280", IsDefault: false, IsActive: true},
			}
			for _, v := range priorityValues {
				if err := db.Create(&v).Error; err != nil {
					log.Printf("Failed to create priority value %s: %v", v.Code, err)
				}
			}
		}
	}
}

func seedEvidenceApprovalWorkflow(db *gorm.DB) {
	// Check if workflow already exists
	var existing models.Workflow
	result := db.Where("code = ?", "evidence_approval").First(&existing)
	if result.Error == nil {
		return // Already seeded
	}

	log.Println("Seeding default evidence approval workflow...")

	workflow := models.Workflow{
		Name:        "Goal Evidence Approval",
		Code:        "evidence_approval",
		Description: "Default approval workflow for goal evidence. Supports L1 and optional L2 review.",
		RecordType:  "evidence",
		IsActive:    true,
		IsDefault:   true,
	}
	if err := db.Create(&workflow).Error; err != nil {
		log.Printf("Failed to create evidence approval workflow: %v", err)
		return
	}

	// Create states
	type stateSpec struct {
		Name      string
		Code      string
		StateType string
		Color     string
		SortOrder int
		PosX      int
		PosY      int
	}

	stateSpecs := []stateSpec{
		{"Draft", "draft", "initial", "#94a3b8", 1, 100, 200},
		{"Submitted", "submitted", "normal", "#3b82f6", 2, 300, 200},
		{"L1 Review", "l1_review", "normal", "#f59e0b", 3, 500, 200},
		{"L2 Review", "l2_review", "normal", "#f97316", 4, 700, 200},
		{"Approved", "approved", "terminal", "#22c55e", 5, 700, 400},
		{"Rejected", "rejected", "terminal", "#ef4444", 6, 500, 400},
		{"Changes Requested", "changes_requested", "normal", "#f97316", 7, 300, 400},
	}

	stateMap := make(map[string]models.WorkflowState)
	for _, spec := range stateSpecs {
		state := models.WorkflowState{
			WorkflowID: workflow.ID,
			Name:       spec.Name,
			Code:       spec.Code,
			StateType:  spec.StateType,
			Color:      spec.Color,
			SortOrder:  spec.SortOrder,
			PositionX:  spec.PosX,
			PositionY:  spec.PosY,
			IsActive:   true,
		}
		if err := db.Create(&state).Error; err != nil {
			log.Printf("Failed to create state %s: %v", spec.Code, err)
			return
		}
		stateMap[spec.Code] = state
	}

	// Create transitions
	type transSpec struct {
		Name        string
		Code        string
		From        string
		To          string
		IsRejection bool
		CommentReq  bool // whether comment is mandatory
		SortOrder   int
	}

	transSpecs := []transSpec{
		{"Submit for Review", "submit", "draft", "submitted", false, false, 1},
		{"Assign L1 Reviewer", "assign_l1", "submitted", "l1_review", false, false, 2},
		{"Approve (L1)", "approve_l1", "l1_review", "l2_review", false, false, 3},
		{"Approve (Final)", "approve_l1_final", "l1_review", "approved", false, false, 4},
		{"Request Changes", "request_changes_l1", "l1_review", "changes_requested", false, true, 5},
		{"Reject", "reject_l1", "l1_review", "rejected", true, true, 6},
		{"Approve (L2)", "approve_l2", "l2_review", "approved", false, false, 7},
		{"Request Changes", "request_changes_l2", "l2_review", "changes_requested", false, true, 8},
		{"Reject", "reject_l2", "l2_review", "rejected", true, true, 9},
		{"Resubmit", "resubmit", "changes_requested", "submitted", false, false, 10},
	}

	boolTrue := true
	for _, spec := range transSpecs {
		fromState := stateMap[spec.From]
		toState := stateMap[spec.To]

		transition := models.WorkflowTransition{
			WorkflowID:  workflow.ID,
			Name:        spec.Name,
			Code:        spec.Code,
			FromStateID: fromState.ID,
			ToStateID:   toState.ID,
			IsRejection: spec.IsRejection,
			IsActive:    true,
			SortOrder:   spec.SortOrder,
		}

		if err := db.Create(&transition).Error; err != nil {
			log.Printf("Failed to create transition %s: %v", spec.Code, err)
			return
		}

		// Add comment requirement for reject/request_changes transitions
		if spec.CommentReq {
			requirement := models.TransitionRequirement{
				TransitionID:    transition.ID,
				RequirementType: "comment",
				IsMandatory:     &boolTrue,
				ErrorMessage:    "Comment is required for this action",
			}
			if err := db.Create(&requirement).Error; err != nil {
				log.Printf("Failed to create requirement for %s: %v", spec.Code, err)
			}
		}
	}

	log.Println("Evidence approval workflow seeded successfully")
}

// seedMetricApprovalWorkflows seeds two parallel workflows used by the
// metric approval gate (item #21):
//   - record_type='metric' covers initial metric creation visibility.
//   - record_type='metric_value_change' covers each subsequent value update.
//
// Both have the same shape as the evidence workflow. Idempotent: skips if
// the workflow code already exists.
func seedMetricApprovalWorkflows(db *gorm.DB) {
	specs := []struct {
		Name        string
		Code        string
		Description string
		RecordType  string
	}{
		{
			Name:        "Goal Metric Approval",
			Code:        "metric_approval",
			Description: "Default approval workflow for goal metric definitions. Supports L1 and optional L2 review.",
			RecordType:  "metric",
		},
		{
			Name:        "Goal Metric Value Change Approval",
			Code:        "metric_value_change_approval",
			Description: "Default approval workflow for proposed metric value changes. Supports L1 and optional L2 review.",
			RecordType:  "metric_value_change",
		},
	}

	for _, s := range specs {
		var existing models.Workflow
		if err := db.Where("code = ?", s.Code).First(&existing).Error; err == nil {
			continue // already seeded
		}

		log.Printf("Seeding default %s workflow...", s.RecordType)

		workflow := models.Workflow{
			Name:        s.Name,
			Code:        s.Code,
			Description: s.Description,
			RecordType:  s.RecordType,
			IsActive:    true,
			IsDefault:   true,
		}
		if err := db.Create(&workflow).Error; err != nil {
			log.Printf("Failed to create %s workflow: %v", s.Code, err)
			continue
		}

		type stateSpec struct {
			Name      string
			Code      string
			StateType string
			Color     string
			SortOrder int
			PosX      int
			PosY      int
		}

		stateSpecs := []stateSpec{
			{"Draft", "draft", "initial", "#94a3b8", 1, 100, 200},
			{"Submitted", "submitted", "normal", "#3b82f6", 2, 300, 200},
			{"L1 Review", "l1_review", "normal", "#f59e0b", 3, 500, 200},
			{"L2 Review", "l2_review", "normal", "#f97316", 4, 700, 200},
			{"Approved", "approved", "terminal", "#22c55e", 5, 700, 400},
			{"Rejected", "rejected", "terminal", "#ef4444", 6, 500, 400},
			{"Changes Requested", "changes_requested", "normal", "#f97316", 7, 300, 400},
		}

		stateMap := make(map[string]models.WorkflowState)
		for _, sp := range stateSpecs {
			state := models.WorkflowState{
				WorkflowID: workflow.ID,
				Name:       sp.Name,
				Code:       sp.Code,
				StateType:  sp.StateType,
				Color:      sp.Color,
				SortOrder:  sp.SortOrder,
				PositionX:  sp.PosX,
				PositionY:  sp.PosY,
				IsActive:   true,
			}
			if err := db.Create(&state).Error; err != nil {
				log.Printf("Failed to create state %s for %s: %v", sp.Code, s.Code, err)
				return
			}
			stateMap[sp.Code] = state
		}

		type transSpec struct {
			Name        string
			Code        string
			From        string
			To          string
			IsRejection bool
			CommentReq  bool
			SortOrder   int
		}

		transSpecs := []transSpec{
			{"Submit for Review", "submit", "draft", "submitted", false, false, 1},
			{"Assign L1 Reviewer", "assign_l1", "submitted", "l1_review", false, false, 2},
			{"Approve (L1)", "approve_l1", "l1_review", "l2_review", false, false, 3},
			{"Approve (Final)", "approve_l1_final", "l1_review", "approved", false, false, 4},
			{"Request Changes", "request_changes_l1", "l1_review", "changes_requested", false, true, 5},
			{"Reject", "reject_l1", "l1_review", "rejected", true, true, 6},
			{"Approve (L2)", "approve_l2", "l2_review", "approved", false, false, 7},
			{"Request Changes", "request_changes_l2", "l2_review", "changes_requested", false, true, 8},
			{"Reject", "reject_l2", "l2_review", "rejected", true, true, 9},
			{"Resubmit", "resubmit", "changes_requested", "submitted", false, false, 10},
		}

		boolTrue := true
		for _, sp := range transSpecs {
			fromState := stateMap[sp.From]
			toState := stateMap[sp.To]

			transition := models.WorkflowTransition{
				WorkflowID:  workflow.ID,
				Name:        sp.Name,
				Code:        sp.Code,
				FromStateID: fromState.ID,
				ToStateID:   toState.ID,
				IsRejection: sp.IsRejection,
				IsActive:    true,
				SortOrder:   sp.SortOrder,
			}

			if err := db.Create(&transition).Error; err != nil {
				log.Printf("Failed to create transition %s for %s: %v", sp.Code, s.Code, err)
				return
			}

			if sp.CommentReq {
				requirement := models.TransitionRequirement{
					TransitionID:    transition.ID,
					RequirementType: "comment",
					IsMandatory:     &boolTrue,
					ErrorMessage:    "Comment is required for this action",
				}
				if err := db.Create(&requirement).Error; err != nil {
					log.Printf("Failed to create requirement for %s: %v", sp.Code, err)
				}
			}
		}

		log.Printf("%s workflow seeded successfully", s.Code)
	}
}

func Close(db *gorm.DB) error {
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
