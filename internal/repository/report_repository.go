package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/utils"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TransitionPair represents a from→to state name pair for transition lookups.
// TransitionPair represents a from→to state name pair loaded from the DB.
// FromStateType ("initial", "normal", "terminal") lets callers split pairs
// by semantic category (e.g. reopen = terminal → Under Resolution).
// Roles is the comma-split list of role names allowed to execute this transition.
type TransitionPair struct {
	Name          string
	Code          string // wt.code — used to build dynamic field names like {code}_at
	From          string
	To            string
	FromStateType string
	Roles         []string
}

type TransitionLinkedResult struct {
	IncidentID  string
	CreatedBy   string
	Comment     string
	CreatedAt   string
	ToStateName string
}

// TransitionEnrichment holds all per-incident enrichment for one workflow transition.
// Keyed by incident_id → transition_code in the map returned by fetchTransitionEnrichments.
type TransitionEnrichment struct {
	PerformedBy    string
	TransitionedAt string
	Comment        string
	Feedback       string
	Attachments    []string
}

type ReportRepository interface {
	// Report CRUD
	Create(ctx context.Context, report *models.Report) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.Report, error)
	FindByIDWithRelations(ctx context.Context, id uuid.UUID) (*models.Report, error)
	List(ctx context.Context, filter *models.ReportFilter) ([]models.Report, int64, error)
	Update(ctx context.Context, report *models.Report) error
	Delete(ctx context.Context, id uuid.UUID) error

	// Report Execution
	CreateExecution(ctx context.Context, execution *models.ReportExecution) error
	FindExecutionByID(ctx context.Context, id uuid.UUID) (*models.ReportExecution, error)
	ListExecutions(ctx context.Context, reportID uuid.UUID, page, limit int) ([]models.ReportExecution, int64, error)
	UpdateExecution(ctx context.Context, execution *models.ReportExecution) error

	// Data queries for report execution
	ExecuteIncidentQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error)
	ExecuteRequestQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error)
	// GetTransitionUserNames returns a map of incident_id → full name of the user
	// who performed the most-recent status transition TO newStateName for each
	// incident in incidentIDs. Queries incident_revisions joined with users.
	// GetTransitionUserNames(ctx context.Context, newStateName string, incidentIDs []string) (map[string]string, error)
	ExecuteUserQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error)
	ExecuteUserPerformanceQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error)
	ExecuteWorkflowQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error)
	ExecuteDepartmentQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error)
	ExecuteLocationQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error)
	ExecuteClassificationQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error)
	ExecuteActionLogQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error)
	// Count-based group queries
	ExecuteLocationCountQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error)
	ExecuteLocationCountByStatusQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error)
	ExecuteClassificationCountQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error)
	ExecuteClassificationCountByStatusQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error)
	ExecuteDepartmentCountQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error)
	ExecuteDepartmentCountByStatusQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error)
}

type reportRepository struct {
	db *gorm.DB
}

func NewReportRepository(db *gorm.DB) ReportRepository {
	return &reportRepository{db: db}
}

// Report CRUD

func (r *reportRepository) Create(ctx context.Context, report *models.Report) error {
	return r.db.WithContext(ctx).Create(report).Error
}

func (r *reportRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.Report, error) {
	var report models.Report
	err := r.db.WithContext(ctx).First(&report, "id = ?", id).Error
	return &report, err
}

func (r *reportRepository) FindByIDWithRelations(ctx context.Context, id uuid.UUID) (*models.Report, error) {
	var report models.Report
	err := r.db.WithContext(ctx).
		Preload("CreatedBy").
		First(&report, "id = ?", id).Error
	return &report, err
}

func (r *reportRepository) List(ctx context.Context, filter *models.ReportFilter) ([]models.Report, int64, error) {
	var reports []models.Report
	var total int64

	query := r.db.WithContext(ctx).Model(&models.Report{})

	if filter.DataSource != nil && *filter.DataSource != "" {
		query = query.Where("data_source = ?", *filter.DataSource)
	}
	if filter.CreatedByID != nil {
		query = query.Where("created_by_id = ?", *filter.CreatedByID)
	}
	if filter.IsPublic != nil {
		query = query.Where("is_public = ?", *filter.IsPublic)
	}
	if filter.Search != "" {
		search := "%" + filter.Search + "%"
		query = query.Where("name ILIKE ? OR description ILIKE ?", search, search)
	}

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (filter.Page - 1) * filter.Limit
	err := query.
		Preload("CreatedBy").
		Order("created_at DESC").
		Offset(offset).
		Limit(filter.Limit).
		Find(&reports).Error

	return reports, total, err
}

func (r *reportRepository) Update(ctx context.Context, report *models.Report) error {
	return r.db.WithContext(ctx).Save(report).Error
}

func (r *reportRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.Report{}, "id = ?", id).Error
}

// Report Execution

func (r *reportRepository) CreateExecution(ctx context.Context, execution *models.ReportExecution) error {
	return r.db.WithContext(ctx).Create(execution).Error
}

func (r *reportRepository) FindExecutionByID(ctx context.Context, id uuid.UUID) (*models.ReportExecution, error) {
	var execution models.ReportExecution
	err := r.db.WithContext(ctx).
		Preload("ExecutedBy").
		First(&execution, "id = ?", id).Error
	return &execution, err
}

func (r *reportRepository) ListExecutions(ctx context.Context, reportID uuid.UUID, page, limit int) ([]models.ReportExecution, int64, error) {
	var executions []models.ReportExecution
	var total int64

	query := r.db.WithContext(ctx).Model(&models.ReportExecution{}).Where("report_id = ?", reportID)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	offset := (page - 1) * limit
	err := query.
		Preload("ExecutedBy").
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&executions).Error

	return executions, total, err
}

func (r *reportRepository) UpdateExecution(ctx context.Context, execution *models.ReportExecution) error {
	return r.db.WithContext(ctx).Save(execution).Error
}

// Data query helpers

// filterFieldMap is a map of logical field name → qualified SQL column name.
// Presence in the map (with a non-empty value) is the sole allowed-field check.
var incidentFilterFields = map[string]string{
	// ── Direct incidents columns ──────────────────────────────────────────────
	"id":                        "incidents.id",
	"incident_number":           "incidents.incident_number",
	"title":                     "incidents.title",
	"description":               "incidents.description",
	"classification_id":         "incidents.classification_id",
	"workflow_id":               "incidents.workflow_id",
	"current_state_id":          "incidents.current_state_id",
	"priority":                  "incidents.priority",
	"severity":                  "incidents.severity",
	"assignee_id":               "incidents.assignee_id",
	"department_id":             "incidents.department_id",
	"location_id":               "incidents.location_id",
	"latitude":                  "incidents.latitude",
	"longitude":                 "incidents.longitude",
	"due_date":                  "incidents.due_date",
	"resolved_at":               "incidents.resolved_at",
	"closed_at":                 "incidents.closed_at",
	"sla_breached":              "incidents.sla_breached",
	"sla_deadline":              "incidents.sla_deadline",
	"reporter_id":               "incidents.reporter_id",
	"reporter_email":            "incidents.reporter_email",
	"reporter_name":             "incidents.reporter_name",
	"custom_fields":             "incidents.custom_fields",
	"created_at":                "incidents.created_at",
	"updated_at":                "incidents.updated_at",
	"deleted_at":                "incidents.deleted_at",
	"record_type":               "incidents.record_type",
	"source_incident_id":        "incidents.source_incident_id",
	"converted_request_id":      "incidents.converted_request_id",
	"channel":                   "incidents.channel",
	"created_by_name":           "incidents.created_by_name",
	"created_by_mobile":         "incidents.created_by_mobile",
	"evaluation_count":          "incidents.evaluation_count",
	"address":                   "incidents.address",
	"city":                      "incidents.city",
	"country":                   "incidents.country",
	"postal_code":               "incidents.postal_code",
	"source":                    "incidents.source",
	"version":                   "incidents.version",
	"master_incident_id":        "incidents.master_incident_id",
	"is_merged":                 "incidents.is_merged",
	"merged_at":                 "incidents.merged_at",
	"merged_by_user_id":         "incidents.merged_by_user_id",
	"source_incident_ids":       "incidents.source_incident_ids",
	"ready_to_close_expires_at": "incidents.ready_to_close_expires_at",
	"ready_to_close_duration":   "incidents.ready_to_close_duration",
	"ready_to_close_notified":   "incidents.ready_to_close_notified",
	// ── Via JOIN: workflow_states ─────────────────────────────────────────────
	"current_state_name": "workflow_states.name",
	"current_state_type": "workflow_states.state_type",
	// ── Via JOIN: classifications ─────────────────────────────────────────────
	"classification_name": "classifications.name",
	// ── Via JOIN: departments ─────────────────────────────────────────────────
	"department_name": "departments.name",
	// ── Via JOIN: locations ───────────────────────────────────────────────────
	"location_name": "locations.name",
	// ── Via JOIN: workflows ───────────────────────────────────────────────────
	"workflow_name": "workflows.name",
	// ── Via JOIN: reporters (users aliased as reporters) ──────────────────────
	"reporter_username":   "reporters.username",
	"reporter_first_name": "reporters.first_name",
	"reporter_last_name":  "reporters.last_name",
	// ── Via JOIN: assignees (users aliased as assignees) ──────────────────────
	"assignee_email":      "assignees.email",
	"assignee_username":   "assignees.username",
	"assignee_first_name": "assignees.first_name",
	"assignee_last_name":  "assignees.last_name",
	// ── Subquery-handled (empty col = silently skipped by applyFilters) ───────
	"workflow_transition_id": "", // handled via IN-subquery in ExecuteIncidentQuery
	"workflow_transition_at": "", // handled via IN-subquery in ExecuteIncidentQuery
}

// requestFilterFields reuses the incident columns (same table, filtered by record_type).
var requestFilterFields = map[string]string{
	// ── Direct incidents columns ──────────────────────────────────────────────
	"id":                   "incidents.id",
	"incident_number":      "incidents.incident_number",
	"title":                "incidents.title",
	"description":          "incidents.description",
	"classification_id":    "incidents.classification_id",
	"workflow_id":          "incidents.workflow_id",
	"current_state_id":     "incidents.current_state_id",
	"status_id":            "incidents.current_state_id",
	"workflow_state_id":    "incidents.current_state_id",
	"priority":             "incidents.priority",
	"severity":             "incidents.severity",
	"assignee_id":          "incidents.assignee_id",
	"department_id":        "incidents.department_id",
	"location_id":          "incidents.location_id",
	"latitude":             "incidents.latitude",
	"longitude":            "incidents.longitude",
	"due_date":             "incidents.due_date",
	"resolved_at":          "incidents.resolved_at",
	"closed_at":            "incidents.closed_at",
	"sla_breached":         "incidents.sla_breached",
	"sla_deadline":         "incidents.sla_deadline",
	"reporter_id":          "incidents.reporter_id",
	"created_at":           "incidents.created_at",
	"updated_at":           "incidents.updated_at",
	"deleted_at":           "incidents.deleted_at",
	"record_type":          "incidents.record_type",
	"source_incident_id":   "incidents.source_incident_id",
	"converted_request_id": "incidents.converted_request_id",
	"channel":              "incidents.source",
	"source":               "incidents.source",
}

var userFilterFields = map[string]string{
	"id":             "users.id",
	"email":          "users.email",
	"username":       "users.username",
	"first_name":     "users.first_name",
	"last_name":      "users.last_name",
	"phone":          "users.phone",
	"is_active":      "users.is_active",
	"is_super_admin": "users.is_super_admin",
	"department_id":  "users.department_id",
	"location_id":    "users.location_id",
	"created_at":     "users.created_at",
	"updated_at":     "users.updated_at",
}

var workflowFilterFields = map[string]string{
	"id":          "workflows.id",
	"name":        "workflows.name",
	"description": "workflows.description",
	"is_active":   "workflows.is_active",
	"created_at":  "workflows.created_at",
	"updated_at":  "workflows.updated_at",
}

var departmentFilterFields = map[string]string{
	"id":          "departments.id",
	"name":        "departments.name",
	"description": "departments.description",
	"parent_id":   "departments.parent_id",
	"manager_id":  "departments.manager_id",
	"created_at":  "departments.created_at",
	"updated_at":  "departments.updated_at",
}

var locationFilterFields = map[string]string{
	"id":          "locations.id",
	"name":        "locations.name",
	"description": "locations.description",
	"parent_id":   "locations.parent_id",
	"created_at":  "locations.created_at",
	"updated_at":  "locations.updated_at",
}

var classificationFilterFields = map[string]string{
	"id":          "classifications.id",
	"name":        "classifications.name",
	"description": "classifications.description",
	"parent_id":   "classifications.parent_id",
	"created_at":  "classifications.created_at",
	"updated_at":  "classifications.updated_at",
}

var actionLogFilterFields = map[string]string{
	"id":          "action_logs.id",
	"user_id":     "action_logs.user_id",
	"action":      "action_logs.action",
	"module":      "action_logs.module",
	"resource_id": "action_logs.resource_id",
	"description": "action_logs.description",
	"status":      "action_logs.status",
	"ip_address":  "action_logs.ip_address",
	"duration":    "action_logs.duration",
	"created_at":  "action_logs.created_at",
}

// mergeFilterFields merges one or more filter field maps into a new map.
// Later maps override earlier ones on key collision.
func mergeFilterFields(maps ...map[string]string) map[string]string {
	out := make(map[string]string)
	for _, m := range maps {
		for k, v := range m {
			out[k] = v
		}
	}
	return out
}

// incidentCountFilterFields contains the common incident-level filters shared across all
// count-group queries (location/classification/department × with/without status).
var incidentCountFilterFields = map[string]string{
	"record_type":         "incidents.record_type",
	"incident_created_at": "incidents.created_at",
	"workflow_id":         "incidents.workflow_id",
	"assignee_id":         "incidents.assignee_id",
	"sla_breached":        "incidents.sla_breached",
	"reporter_id":         "incidents.reporter_id",
	"channel":             "incidents.source",
	"source":              "incidents.source",
	"created_at":          "incidents.created_at",
}

// The 6 count-group filter maps are populated by init() via mergeFilterFields so that
// incidentCountFilterFields acts as a true shared base with no duplication.
var (
	locationCountFilterFields               map[string]string
	locationCountByStatusFilterFields       map[string]string
	classificationCountFilterFields         map[string]string
	classificationCountByStatusFilterFields map[string]string
	departmentCountFilterFields             map[string]string
	departmentCountByStatusFilterFields     map[string]string
)

func init() {
	statusFields := map[string]string{
		"current_state_id": "incidents.current_state_id",
		"state_name":       "workflow_states.name",
	}

	locationSpecific := map[string]string{
		"location_id":         "locations.id",
		"location_name":       "locations.name",
		"parent_id":           "locations.parent_id",
		"classification_id":   "incidents.classification_id",
		"classification_name": "class.name",
		"department_id":       "incidents.department_id",
		"status":              "workflow_states.id",
		"status_name":         "workflow_states.name",
	}
	locationCountFilterFields = mergeFilterFields(incidentCountFilterFields, locationSpecific)
	locationCountByStatusFilterFields = mergeFilterFields(incidentCountFilterFields, locationSpecific, statusFields)

	classificationSpecific := map[string]string{
		"classification_id":   "classifications.id",
		"classification_name": "classifications.name",
		"parent_id":           "classifications.parent_id",
		"location_id":         "incidents.location_id",
		"location_name":       "loc.name",
		"department_id":       "incidents.department_id",
		"status":              "workflow_states.id",
		"status_name":         "workflow_states.name",
	}
	classificationCountFilterFields = mergeFilterFields(incidentCountFilterFields, classificationSpecific)
	classificationCountByStatusFilterFields = mergeFilterFields(incidentCountFilterFields, classificationSpecific, statusFields)

	departmentSpecific := map[string]string{
		"department_id":     "departments.id",
		"department_name":   "departments.name",
		"parent_id":         "departments.parent_id",
		"location_id":       "incidents.location_id",
		"classification_id": "incidents.classification_id",
		"status":            "workflow_states.id",
		"status_name":       "workflow_states.name",
	}
	departmentCountFilterFields = mergeFilterFields(incidentCountFilterFields, departmentSpecific)
	departmentCountByStatusFilterFields = mergeFilterFields(incidentCountFilterFields, departmentSpecific, statusFields)

	// dataSourceFilterFields is a var literal initialized before init() runs, so the
	// count filter maps (populated above) are nil in the map at that point.
	// Fix: overwrite those entries now that the maps are fully built.
	dataSourceFilterFields["locations_by_count"] = locationCountFilterFields
	dataSourceFilterFields["locations_by_status"] = locationCountByStatusFilterFields
	dataSourceFilterFields["classifications_by_count"] = classificationCountFilterFields
	dataSourceFilterFields["classifications_by_status"] = classificationCountByStatusFilterFields
	dataSourceFilterFields["departments_by_count"] = departmentCountFilterFields
	dataSourceFilterFields["departments_by_status"] = departmentCountByStatusFilterFields
}

// userPerformanceFilterFields covers the joined tables used by ExecuteUserPerformanceQuery.
var userPerformanceFilterFields = map[string]string{
	// incident_transition_histories
	"performed_by_id": "incident_transition_histories.performed_by_id",
	"transitioned_at": "incident_transition_histories.transitioned_at",
	// incidents
	"incident_id":       "incidents.id",
	"incident_number":   "incidents.incident_number",
	"classification_id": "incidents.classification_id",
	"location_id":       "incidents.location_id",
	"record_type":       "incidents.record_type",
	// joined tables
	"classification_name": "classifications.name",
	"to_state_name":       "workflow_states.name",
	"state_name":          "workflow_states.name",
	"location_name":       "locations.name",
	"user_email":          "perf_users.email",
	"user_first_name":     "perf_users.first_name",
	"user_last_name":      "perf_users.last_name",
}

// dataSourceFilterFields maps a data source name to its allowed filter fields.
var dataSourceFilterFields = map[string]map[string]string{
	"incidents":                 incidentFilterFields,
	"request":                   requestFilterFields,
	"requests":                  requestFilterFields,
	"users":                     userFilterFields,
	"workflows":                 workflowFilterFields,
	"departments":               departmentFilterFields,
	"locations":                 locationFilterFields,
	"classifications":           classificationFilterFields,
	"action_logs":               actionLogFilterFields,
	"users_performance":         userPerformanceFilterFields,
	"locations_by_count":        locationCountFilterFields,
	"locations_by_status":       locationCountByStatusFilterFields,
	"classifications_by_count":  classificationCountFilterFields,
	"classifications_by_status": classificationCountByStatusFilterFields,
	"departments_by_count":      departmentCountFilterFields,
	"departments_by_status":     departmentCountByStatusFilterFields,
}

// hasFilter returns true if any filter in the slice targets the given field name.
func hasFilter(filters []models.ReportFilterConfig, field string) bool {
	for _, f := range filters {
		if f.Field == field && f.Value != nil {
			return true
		}
	}
	return false
}

// needsStatusJoin reports whether any filter requires workflow_states to be joined.
func needsStatusJoin(filters []models.ReportFilterConfig) bool {
	return hasFilter(filters, "status_name") || hasFilter(filters, "status") || hasFilter(filters, "current_state_id") || hasFilter(filters, "state_name")
}

// add this helper alongside your repo functions
func isSlice(v interface{}) bool {
	if v == nil {
		return false
	}
	return reflect.TypeOf(v).Kind() == reflect.Slice
}

// applyFilters applies ReportFilterConfig entries to the query. It reads the
// active data source from ctx (set by WithReportDataSource) to select the
// correct allowed-fields map. Each logical field name is mapped to a qualified
// SQL column in a single lookup; unknown fields are silently skipped.
func (r *reportRepository) applyFilters(ctx context.Context, query *gorm.DB, filters []models.ReportFilterConfig) *gorm.DB {
	if len(filters) == 0 {
		return query
	}

	dataSource, _ := ctx.Value(constants.ContextKeys.REPORT_DATA_SOURCE).(string)
	fieldMap := dataSourceFilterFields[dataSource]
	if fieldMap == nil {
		// Fallback to incident fields for backwards compatibility.
		fieldMap = incidentFilterFields
	}

	// Resolve user timezone for interpreting datetime-local filter values
	tzStr, _ := ctx.Value(constants.ContextKeys.REPORT_TIMEZONE).(string)
	loc := utils.ResolveTimezone(tzStr)

	for _, f := range filters {
		log.Printf("field %s datasource %s value %s", f.Field, dataSource, f.Value)
		col, ok := fieldMap[f.Field]
		if !ok {
			log.Println("skipping unknown filter field:", f.Field, "for data source:", dataSource)
			continue
		}
		if col == "" {
			continue // silently skip; handled by the query function directly (e.g. subquery filters)
		}
		if f.Value == nil {
			log.Println("skipping filter with nil value for field:", f.Field)
			continue
		}
		switch f.Operator {
		case "equals":
			if isSlice(f.Value) {
				query = query.Where(col+" IN ?", f.Value)
			} else {
				query = query.Where(col+" = ?", f.Value)
			}
		case "not_equals":
			query = query.Where(col+" != ?", f.Value)
		case "contains":
			query = query.Where(col+" ILIKE ?", "%"+f.Value.(string)+"%")
		case "starts_with":
			query = query.Where(col+" ILIKE ?", f.Value.(string)+"%")
		case "ends_with":
			query = query.Where(col+" ILIKE ?", "%"+f.Value.(string))
		case "gt":
			query = query.Where(col+" > ?", parseDateOrPassthrough(f.Value, loc))
		case "lt":
			query = query.Where(col+" < ?", parseDateOrPassthrough(f.Value, loc))
		case "gte":
			query = query.Where(col+" >= ?", parseDateOrPassthrough(f.Value, loc))
		case "lte":
			query = query.Where(col+" <= ?", parseDateOrPassthrough(f.Value, loc))
		case "in":
			query = query.Where(col+" IN ?", f.Value)
		case "is_null":
			query = query.Where(col + " IS NULL")
		case "is_not_null":
			query = query.Where(col + " IS NOT NULL")
		case "between":
			if m, ok := f.Value.(map[string]interface{}); ok {
				fromStr, _ := m["from"].(string)
				toStr, _ := m["to"].(string)
				from, err1 := parseDateFlexible(fromStr, loc)
				to, err2 := parseDateFlexible(toStr, loc)
				if err1 == nil && err2 == nil {
					query = query.Where(col+" BETWEEN ? AND ?", from, to)
				}
			}
		}
	}
	return query
}

// parseDateOrPassthrough tries to parse a string value as a date/datetime.
// If successful, returns the parsed time.Time (timezone-aware). Otherwise
// returns the original value unchanged (for non-date comparisons like numbers).
func parseDateOrPassthrough(v interface{}, loc *time.Location) interface{} {
	s, ok := v.(string)
	if !ok {
		return v
	}
	if t, err := parseDateFlexible(s, loc); err == nil {
		return t
	}
	return v
}

// parseDateFlexible parses date strings in multiple formats:
// RFC3339 (has timezone offset), datetime-local (from HTML input, no timezone),
// and plain date. For formats without timezone info, the value is interpreted
// in the provided location (user's timezone) so that filter queries match the
// user's intended local time.
func parseDateFlexible(s string, loc *time.Location) (time.Time, error) {
	// Formats with timezone info — parse as-is
	tzFormats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05.999999999Z07:00",
	}
	for _, f := range tzFormats {
		if t, err := time.Parse(f, s); err == nil {
			return t, nil
		}
	}

	// Formats without timezone info — interpret in user's timezone
	if loc == nil {
		loc = time.UTC
	}
	localFormats := []string{
		"2006-01-02T15:04:05",
		"2006-01-02T15:04",
		"2006-01-02",
	}
	for _, f := range localFormats {
		if t, err := time.ParseInLocation(f, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported date format: %s", s)
}

func (r *reportRepository) applySorting(query *gorm.DB, sorting *models.ReportSortConfig) *gorm.DB {
	if sorting != nil && sorting.Field != "" {
		direction := "ASC"
		if sorting.Direction == "desc" {
			direction = "DESC"
		}
		query = query.Order(sorting.Field + " " + direction)
	}
	return query
}

// Data queries for report execution
func (r *reportRepository) ExecuteRequestQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error) {
	var total int64
	var results []map[string]interface{}

	// Extract requested columns from context for dynamic row construction.
	reqColumns, _ := ctx.Value(constants.ContextKeys.REPORT_COLUMNS).([]models.ColumnField)

	query := r.db.WithContext(ctx).Model(&models.Incident{}).
		Where("incidents.record_type = ?", "request")
	query = r.applyFilters(ctx, query, filters)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	query = r.applySorting(query, sorting)
	if sorting == nil {
		query = query.Order("incidents.created_at DESC")
	}

	offset := (page - 1) * limit
	rows, err := query.Debug().
		Select(
			"incidents.incident_number as request_number, " +
				// "incidents.created_by_mobile as created_by_mobile, " +
				"reporters.first_name as reporter_first_name, reporters.last_name as reporter_last_name, " +
				"TRIM(reporters.first_name || ' ' || reporters.last_name) as reporter_name, " +
				"reporters.phone as reporter_phone, " +
				"classifications.name as classification_name, " +
				"locations.name as location_name, " +
				"incidents.title as title, " +
				// Conditional: if source_incident_id IS NULL → fetch reporter's role, else mark as converted
				"CASE " +
				"  WHEN incidents.source_incident_id IS NULL THEN roles.name " +
				"  ELSE 'Converted from Incident' " +
				"END as request_source, " +
				"incidents.created_at as created_at").
		Joins("LEFT JOIN users as reporters ON incidents.reporter_id = reporters.id").
		Joins("LEFT JOIN classifications ON incidents.classification_id = classifications.id").
		Joins("LEFT JOIN locations ON incidents.location_id = locations.id").
		// Join user_roles and roles only for reporter
		Joins("LEFT JOIN user_roles ON user_roles.user_id = reporters.id").
		Joins("LEFT JOIN roles ON roles.id = user_roles.role_id AND roles.deleted_at IS NULL AND roles.is_active = true").
		Offset(offset).
		Limit(limit).
		Rows()
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	for rows.Next() {
		columns := make([]interface{}, len(cols))
		columnPointers := make([]interface{}, len(cols))
		for i := range columns {
			columnPointers[i] = &columns[i]
		}
		if err := rows.Scan(columnPointers...); err != nil {
			continue
		}

		rawRow := make(map[string]interface{})
		for i, colName := range cols {
			val := columns[i]
			if b, ok := val.([]byte); ok {
				rawRow[colName] = string(b)
			} else {
				rawRow[colName] = val
			}
		}

		row := make(map[string]interface{})
		if len(reqColumns) > 0 {
			for _, col := range reqColumns {
				row[col.Label] = rawRow[col.Field]
			}
		} else {
			for k, v := range rawRow {
				row[k] = v
			}
		}

		results = append(results, row)
	}

	return results, total, nil
}

// Data queries for report execution
func (r *reportRepository) ExecuteIncidentQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error) {
	var total int64
	var results []map[string]interface{}

	// buildBase returns a fresh *gorm.DB with all filter-relevant JOINs applied so that:
	// (a) join-based filter fields (e.g. workflow_states.name) work on the COUNT query, and
	// (b) GORM statement accumulation does not duplicate JOINs across Count + Rows calls.
	buildBase := func() *gorm.DB {
		q := r.db.WithContext(ctx).Model(&models.Incident{}).Debug().
			Joins("LEFT JOIN users as reporters ON incidents.reporter_id = reporters.id").
			Joins("LEFT JOIN users as assignees ON incidents.assignee_id = assignees.id").
			Joins("LEFT JOIN workflow_states ON incidents.current_state_id = workflow_states.id").
			Joins("LEFT JOIN classifications ON incidents.classification_id = classifications.id").
			Joins("LEFT JOIN departments ON incidents.department_id = departments.id").
			Joins("LEFT JOIN locations ON incidents.location_id = locations.id").
			Joins("LEFT JOIN workflows ON incidents.workflow_id = workflows.id")

		// workflow_transition_id: optional filter — restricts to incidents that have
		// passed through at least one matching transition (name or code match).
		// Uses an IN subquery instead of a JOIN to avoid row fan-out.
		const transSubq = `incidents.id IN (
			SELECT ith.incident_id
			FROM incident_transition_histories ith
			INNER JOIN workflow_transitions wt ON wt.id = ith.transition_id AND wt.deleted_at IS NULL
			WHERE 1=1
			`
		for _, f := range filters {
			if f.Field != "workflow_transition_id" || f.Value == nil {
				continue
			}
			switch f.Operator {
			case "equals":
				if isSlice(f.Value) {
					q = q.Where(transSubq+" AND wt.id IN (?))", f.Value)
				} else {
					q = q.Where(transSubq+" AND wt.id = ?)", f.Value)
				}
			case "in":
				q = q.Where(transSubq+" AND wt.id IN (?))", f.Value)
			case "contains":
				q = q.Where(transSubq+" AND wt.id ILIKE ?)", "%"+f.Value.(string)+"%")
			case "starts_with":
				q = q.Where(transSubq+" AND wt.id ILIKE ?)", f.Value.(string)+"%")
			case "ends_with":
				q = q.Where(transSubq+" AND wt.id ILIKE ?)", "%"+f.Value.(string))
			}
		}

		for _, f := range filters {
			if f.Field != "workflow_transition_at" || f.Value == nil {
				continue
			}
			switch f.Operator {
			case "between":
				if m, ok := f.Value.(map[string]interface{}); ok {
					fromStr, _ := m["from"].(string)
					toStr, _ := m["to"].(string)
					from, err1 := time.Parse(time.RFC3339, fromStr)
					to, err2 := time.Parse(time.RFC3339, toStr)
					if err1 == nil && err2 == nil {
						q = q.Where(transSubq+" AND ith.transitioned_at BETWEEN ? AND ?)", from, to)
					}
				}
			case "equals":
				t, err := time.Parse(time.RFC3339, fmt.Sprint(f.Value))
				if err == nil {
					q = q.Where(transSubq+" AND ith.transitioned_at = ?)", t)
				}
			case "gt":
				t, err := time.Parse(time.RFC3339, fmt.Sprint(f.Value))
				if err == nil {
					q = q.Where(transSubq+" AND ith.transitioned_at > ?)", t)
				}
			case "lt":
				t, err := time.Parse(time.RFC3339, fmt.Sprint(f.Value))
				if err == nil {
					q = q.Where(transSubq+" AND ith.transitioned_at < ?)", t)
				}
			}
		}
		return r.applyFilters(ctx, q, filters)
	}

	if err := buildBase().Count(&total).Error; err != nil {
		return nil, 0, err
	}

	dataQuery := buildBase()
	dataQuery = r.applySorting(dataQuery, sorting)
	if sorting == nil {
		dataQuery = dataQuery.Order("incidents.created_at DESC")
	}
	offset := (page - 1) * limit
	rows, err := dataQuery.
		Select("incidents.*, " +
			"reporters.email as reporter_email, reporters.first_name as reporter_first_name, reporters.last_name as reporter_last_name, " +
			"TRIM(reporters.first_name || ' ' || reporters.last_name) as reporter_name, " +
			"reporters.username as reporter_username, " + "reporters.phone as reporter_phone, " +
			"assignees.email as assignee_email, assignees.first_name as assignee_first_name, assignees.last_name as assignee_last_name, " +
			"assignees.username as assignee_username, " +
			"workflow_states.name as current_state_name, workflow_states.state_type as current_state_state_type, " +
			"classifications.name as classification_name, " +
			"departments.name as department_name, " +
			"locations.name as location_name, " +
			"workflows.name as workflow_name").
		Offset(offset).
		Limit(limit).
		Rows()
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	// Extract URL-construction context values once, before iterating rows.
	hostname, _ := ctx.Value(constants.ContextKeys.HOSTNAME).(string)
	protocol, _ := ctx.Value(constants.ContextKeys.PROTOCOL).(string)
	token, _ := ctx.Value(constants.ContextKeys.Token).(string)

	// Extract requested columns for dynamic row construction and enrichment filtering.
	reqColumns, _ := ctx.Value(constants.ContextKeys.REPORT_COLUMNS).([]models.ColumnField)
	// colFieldSet: keyed by col.Field — used by hasCol to detect which enrichments to run.
	// fieldToLabel: maps col.Field → col.Label — used to write enriched values under the
	// caller-supplied label rather than a hardcoded display name.
	colFieldSet := make(map[string]bool, len(reqColumns))
	fieldToLabel := make(map[string]string, len(reqColumns))
	for _, col := range reqColumns {
		colFieldSet[col.Field] = true
		fieldToLabel[col.Field] = col.Label
	}

	cols, _ := rows.Columns()
	for rows.Next() {
		columns := make([]interface{}, len(cols))
		columnPointers := make([]interface{}, len(cols))
		for i := range columns {
			columnPointers[i] = &columns[i]
		}
		if err := rows.Scan(columnPointers...); err != nil {
			continue
		}
		// Build raw row data
		rawRow := make(map[string]interface{})
		for i, colName := range cols {
			val := columns[i]
			if b, ok := val.([]byte); ok {
				rawRow[colName] = string(b)
			} else {
				rawRow[colName] = val
			}
		}
		// log.Printf("rawRow keys: %v", func() []string {
		// 	keys := make([]string, 0, len(rawRow))
		// 	for k := range rawRow {
		// 		keys = append(keys, k)
		// 	}
		// 	return keys
		// }())
		// Build row dynamically from the requested columns.
		// col.Field is the SQL alias present in rawRow; col.Label is the output key.
		row := make(map[string]interface{})
		if len(reqColumns) > 0 {

			for _, col := range reqColumns {
				row[col.Label] = rawRow[col.Field]
			}
			// Always carry the internal "id" field so enrichment can key results.
			if _, ok := row["id"]; !ok {
				row["id"] = rawRow["id"]
				row["created_at"] = rawRow["created_at"] // also carry created_at for aging calculations in enrichment
				row["classification_id"] = rawRow["classification_id"]
				row["location_id"] = rawRow["location_id"]
				row["closed_at"] = rawRow["closed_at"]
			}
			var createdAt string
			switch v := rawRow["created_at"].(type) {
			case string:
				createdAt = v
			case time.Time:
				createdAt = v.Format(time.RFC3339Nano)
			}
			if createdAt != "" {
				row["created_at"] = createdAt
			}

			// task_id column: show "Done" when custom_fields contains a task_id entry.
			for _, col := range reqColumns {
				if col.Field != "task_id" && col.Label != "task_id" && col.Label != "Task ID" && col.Label != "TaskID" {
					continue
				}
				cfRaw, _ := rawRow["custom_fields"].(string)
				taskDone := ""
				if cfRaw != "" {
					var cf map[string]struct {
						FieldType string `json:"field_type"`
						Value     string `json:"value"`
					}
					if json.Unmarshal([]byte(cfRaw), &cf) == nil {
						for _, entry := range cf {
							if entry.FieldType == "task_id" && entry.Value != "" {
								taskDone = "Done"
								break
							}
						}
					}
				}
				row[col.Label] = taskDone
			}

		} else {
			for k, v := range rawRow {
				row[k] = v
			}
		}

		results = append(results, row)
	}

	// ── Bulk enrichment from related tables ──────────────────────────────────
	if len(results) > 0 {
		incidentIDs := make([]string, 0, len(results))
		locationIDs := make([]string, 0, len(results))
		classificationIDs := make([]string, 0, len(results))
		for _, row := range results {
			if id := IDStr(row["id"]); id != "" {
				incidentIDs = append(incidentIDs, id)
			}
			if id := IDStr(row["location_id"]); id != "" {
				locationIDs = append(locationIDs, id)
			}
			if id := IDStr(row["classification_id"]); id != "" {
				classificationIDs = append(classificationIDs, id)
			}
		}

		// hasCol returns true when at least one of the given field names was
		// requested by the caller. If no columns were injected via context
		// (e.g. direct service calls), all enrichment fetches run.
		hasCol := func(fields ...string) bool {
			if len(colFieldSet) == 0 {
				return true // no filter → fetch everything
			}
			for _, f := range fields {
				if colFieldSet[f] {
					return true
				}
			}
			return false
		}
		// setField writes value into row under the label the caller assigned to fieldName.
		// If fieldName was not requested, the write is a no-op.
		setField := func(row map[string]interface{}, fieldName string, value interface{}) {
			if label, ok := fieldToLabel[fieldName]; ok {
				row[label] = value
			}
		}

		// Load workflow transition pairs (with from-state type and allowed roles) from
		// the DB once per report run. Keys are to-state names (e.g. "Rejected", "Closed").
		// "Under Resolution" pairs are split into:
		//   underResPairs  – originate from non-terminal states (initial assignment / approval)
		//   reopenPairs    – originate from terminal states (Ready To Close, Closed → reopen)
		dynPairs, _ := r.loadDynamicTransitionPairs(ctx)
		var underResPairs, reopenPairs []TransitionPair
		for _, p := range dynPairs["Under Resolution"] {
			if p.FromStateType == "terminal" {
				reopenPairs = append(reopenPairs, p)
			} else {
				underResPairs = append(underResPairs, p)
			}
		}

		// Build by-code index across all to-states for dynamic {code}_* field detection.
		byCode := map[string][]TransitionPair{}
		for _, pairs := range dynPairs {
			for _, p := range pairs {
				if p.Code != "" {
					byCode[p.Code] = append(byCode[p.Code], p)
				}
			}
		}
		dynSuffixes := []string{"_at", "_by", "_comment", "_feedback", "_attachment", "_attachments"}
		neededCodes := map[string]bool{}
		for _, col := range reqColumns {
			for code := range byCode {
				for _, suf := range dynSuffixes {
					if col.Field == code+suf {
						neededCodes[code] = true
					}
				}
			}
		}
		var transEnrichMap map[string]map[string]TransitionEnrichment
		if len(neededCodes) > 0 {
			transEnrichMap, _ = r.fetchTransitionEnrichments(ctx, incidentIDs, protocol, hostname, token)
		}

		// 1. Per-status transition data – only fetched when the matching
		//    By/Date column is actually requested.
		var rejectedNames, rejectedDates map[string]string
		if hasCol("rejected_by", "rejected_date", "rejected_at") {
			rejectedNames, rejectedDates, _ = r.fetchMultiPairTransitionData(ctx, dynPairs["Rejected"], incidentIDs)
		}

		var underResNames, underResDates map[string]string
		if hasCol("under_resolution_by", "under_resolution_date", "approved_by", "approved_at", "approved_time") {
			underResNames, underResDates, _ = r.fetchMultiPairTransitionData(ctx, underResPairs, incidentIDs)
		}

		var readyToCloseNames, readyToCloseDates map[string]string
		if hasCol("ready_to_close_by", "ready_to_close_date") {
			readyToCloseNames, readyToCloseDates, _ = r.fetchMultiPairTransitionData(ctx, dynPairs["Ready To Close"], incidentIDs)
		}

		var closedNames, closedDates map[string]string
		if hasCol("closed_by", "closed_date", "contractor", "contractor_user", "solved_by", "total_closing_duration") {
			closedNames, closedDates, _ = r.fetchMultiPairTransitionData(ctx, dynPairs["Closed"], incidentIDs)
		}

		// 2. Reopened By + Reopen Date
		var reopenedByNames, reopenDates map[string]string
		if hasCol("reopened_by", "reopen_date") {
			reopenedByNames, reopenDates, _ = r.fetchMultiPairTransitionData(ctx, reopenPairs, incidentIDs)
		}

		// 3. Escalation Date
		var escalationDates map[string]string
		if hasCol("escalation_date") {
			escalationDates, _ = r.fetchEscalationDates(ctx, incidentIDs)
		}

		// 4. General comments
		// var commentsMap map[string][]map[string]interface{}
		// if hasCol("comments", "comments_array") {
		// 	commentsMap, _ = r.fetchGeneralComments(ctx, incidentIDs)
		// }

		// 5. Contractor comment
		var contractorCommentsMap map[string]string
		if hasCol("contractor_comment") {
			contractorCommentsMap, _ = r.fetchContractorComments(ctx, incidentIDs)
		}

		// 6. Before attachments
		var beforeAttachMap map[string][]string
		if hasCol("before_attachments", "before_attachment_array") {
			beforeAttachMap, _ = r.fetchBeforeAttachments(ctx, incidentIDs, protocol, hostname, token)
		}

		// 7. After attachments
		var afterAttachMap map[string][]string
		if hasCol("after_attachments", "after_attachment_array") {
			afterAttachMap, _ = r.fetchAfterAttachments(ctx, incidentIDs, protocol, hostname, token)
		}

		// 8. All attachments
		var allAttachMap map[string][]string
		if hasCol("attachments") {
			allAttachMap, _ = r.fetchAllAttachments(ctx, incidentIDs, protocol, hostname, token)
		}

		// 9. Feedback
		// in the bulk enrichment block, alongside other hasCol checks
		var feedbackMap map[string][]TransitionLinkedResult
		if hasCol("feedbacks", "feedback_array") {
			feedbackMap, _ = r.fetchFeedbackData(ctx, incidentIDs)
		}

		// Closed feedback
		var closedFeedbackMap map[string][]TransitionLinkedResult
		if hasCol("closed_feedback") {
			closedFeedbackMap, _ = r.fetchFeedbackDataByState(ctx, "Closed", incidentIDs)
		}

		// Rejected feedback
		var rejectedFeedbackMap map[string][]TransitionLinkedResult
		if hasCol("rejected_feedback") {
			rejectedFeedbackMap, _ = r.fetchFeedbackDataByState(ctx, "Rejected", incidentIDs)
		}

		// Under Resolution feedback
		var underResFeedbackMap map[string][]TransitionLinkedResult
		if hasCol("under_resolution_feedback") {
			underResFeedbackMap, _ = r.fetchFeedbackDataByState(ctx, "Under Resolution", incidentIDs)
		}

		// Ready To Close feedback
		var readyToCloseFeedbackMap map[string][]TransitionLinkedResult
		if hasCol("ready_to_close_feedback") {
			readyToCloseFeedbackMap, _ = r.fetchFeedbackDataByState(ctx, "Ready To Close", incidentIDs)
		}

		// Reopened feedback — reopen transitions land in "Under Resolution"
		var reopenedFeedbackMap map[string][]TransitionLinkedResult
		if hasCol("reopened_feedback") {
			reopenedFeedbackMap, _ = r.fetchFeedbackDataByState(ctx, "Under Resolution", incidentIDs)
		}
		// 10. Comment
		// in the bulk enrichment block, alongside other hasCol checks
		var commentsMap map[string][]TransitionLinkedResult
		if hasCol("comments", "comment_array") {
			commentsMap, _ = r.fetchCommentData(ctx, incidentIDs)
		}

		// Closed feedback
		var closedCommentMap map[string][]TransitionLinkedResult
		if hasCol("closed_comment") {
			closedCommentMap, _ = r.fetchCommentDataByState(ctx, "Closed", incidentIDs)
		}

		// Rejected feedback
		var rejectedCommentMap map[string][]TransitionLinkedResult
		if hasCol("rejected_comment") {
			rejectedCommentMap, _ = r.fetchCommentDataByState(ctx, "Rejected", incidentIDs)
		}

		// Under Resolution feedback
		var underResCommentMap map[string][]TransitionLinkedResult
		if hasCol("under_resolution_comment") {
			underResCommentMap, _ = r.fetchCommentDataByState(ctx, "Under Resolution", incidentIDs)
		}

		// In Progress feedback
		var inProgressCommentMap map[string][]TransitionLinkedResult
		if hasCol("in_progress_comment") {
			inProgressCommentMap, _ = r.fetchCommentDataByState(ctx, "In Progress", incidentIDs)
		}

		// Ready To Close feedback
		var readyToCloseCommentMap map[string][]TransitionLinkedResult
		if hasCol("ready_to_close_comment") {
			readyToCloseCommentMap, _ = r.fetchCommentDataByState(ctx, "Ready To Close", incidentIDs)
		}

		// Reopened comment — reopen transitions land in "Under Resolution"
		var reopenedCommentMap map[string][]TransitionLinkedResult
		if hasCol("reopened_comment") {
			reopenedCommentMap, _ = r.fetchCommentDataByState(ctx, "Under Resolution", incidentIDs)
		}

		// @todo urgent update the incident id to location/classification id
		// 11. Location Full path
		var locationFullPathMap map[string]string
		if hasCol("full_location") {
			locationFullPathMap, err = r.fetchLocationPaths(ctx, locationIDs)
			if err != nil {
				log.Print("error fetching location full name", locationFullPathMap)
			}
		}

		// 11. Classification Full path
		var classificationFullPathMap map[string]string
		if hasCol("full_classification") {
			classificationFullPathMap, err = r.fetchClassificationPaths(ctx, classificationIDs)
			if err != nil {
				log.Print("error fetching classification full name", classificationFullPathMap)
			}
		}

		for i, row := range results {
			incidentID := IDStr(row["id"])
			if incidentID == "" {
				continue
			}
			locationID := IDStr(row["location_id"])
			classificationID := IDStr(row["classification_id"])
			// ── Per-status By / Date ─────────────────────────────────────────────

			// Closed
			if name := closedNames[incidentID]; name != "" {
				setField(results[i], "closed_by", name)
				setField(results[i], "contractor", name)
				setField(results[i], "contractor_user", name)
				setField(results[i], "solved_by", name)
			}
			if date := closedDates[incidentID]; date != "" {
				setField(results[i], "closed_date", date)
			}

			// approved_by — only from UnderResolution (Approve transition)
			if name := underResNames[incidentID]; name != "" {
				setField(results[i], "approved_by", name)
			}
			if date := underResDates[incidentID]; date != "" {
				setField(results[i], "approved_time", date)
				setField(results[i], "approved_at", date)
			}

			// total_closing_duration
			closedDate := closedDates[incidentID]
			// fetchMultiPairTransitionData may join multiple matches with " | "; use only the first.
			if idx := strings.Index(closedDate, " | "); idx >= 0 {
				closedDate = closedDate[:idx]
			}
			if closedDate != "" {
				if createdAt, ok := row["created_at"].(string); ok && createdAt != "" {
					if dur, err := utils.CalculateDuration(createdAt, closedDate); err == nil {
						setField(results[i], "total_closing_duration", dur)
					} else {
						setField(results[i], "total_closing_duration", nil)
					}
				}
			} else {
				setField(results[i], "total_closing_duration", nil)
			}

			// Rejected
			if name := rejectedNames[incidentID]; name != "" {
				setField(results[i], "rejected_by", name)
			}
			if date := rejectedDates[incidentID]; date != "" {
				setField(results[i], "rejected_date", date)
				setField(results[i], "rejected_at", date)
			}

			// Under Resolution
			// if name := underResNames[incidentID]; name != "" {
			// 	setField(results[i], "under_resolution_by", name)
			// 	setField(results[i], "approved_by", name)
			// }
			// if date := underResDates[incidentID]; date != "" {
			// 	setField(results[i], "under_resolution_date", date)
			// 	setField(results[i], "approved_time", date)
			// 	setField(results[i], "approved_at", date)
			// }

			// In Progress
			// if name := inProgressNames[incidentID]; name != "" {
			// 	setField(results[i], "in_progress_by", name)
			// }
			// if date := inProgressDates[incidentID]; date != "" {
			// 	setField(results[i], "in_progress_date", date)
			// }

			// Ready To Close
			if name := readyToCloseNames[incidentID]; name != "" {
				setField(results[i], "ready_to_close_by", name)
			}
			if date := readyToCloseDates[incidentID]; date != "" {
				setField(results[i], "ready_to_close_date", date)
			}

			// Reopened By + Reopen Date from incident_revisions
			if rn := reopenedByNames[incidentID]; rn != "" {
				setField(results[i], "reopened_by", rn)
			}
			if rd := reopenDates[incidentID]; rd != "" {
				setField(results[i], "reopen_date", rd)
			}

			// Escalation Date
			if ed := escalationDates[incidentID]; ed != "" {
				setField(results[i], "escalation_date", ed)
			}

			// General comments
			// if comments, ok := commentsMap[incidentID]; ok {
			// 	parts := make([]string, 0, len(comments))
			// 	for _, c := range comments {
			// 		parts = append(parts, fmt.Sprintf("%s", c["content"]))
			// 	}
			// 	setField(results[i], "comments", strings.Join(parts, " | "))
			// 	setField(results[i], "comments_array", comments)
			// }

			// Contractor comment (first/latest comment from a transition)
			if cc := contractorCommentsMap[incidentID]; cc != "" {
				setField(results[i], "contractor_comment", cc)
			}

			// Before attachments (creator/agent uploads)
			if urls := beforeAttachMap[incidentID]; len(urls) > 0 {
				setField(results[i], "before_attachments", strings.Join(urls, " | "))
				setField(results[i], "before_attachment_array", urls)
			}

			// After attachments (contractor uploads during transitions)
			if urls := afterAttachMap[incidentID]; len(urls) > 0 {
				setField(results[i], "after_attachments", strings.Join(urls, " | "))
				setField(results[i], "after_attachment_array", urls)
			}

			// All attachments
			if urls := allAttachMap[incidentID]; len(urls) > 0 {
				setField(results[i], "attachments", strings.Join(urls, " | "))
			}

			// Closed feedback
			if feedbacks, ok := closedFeedbackMap[incidentID]; ok && len(feedbacks) > 0 {
				setField(results[i], "closed_feedback", feedbacks[0].Comment)
			}

			// Rejected feedback
			if feedbacks, ok := rejectedFeedbackMap[incidentID]; ok && len(feedbacks) > 0 {
				setField(results[i], "rejected_feedback", feedbacks[0].Comment)
			}

			// Under Resolution feedback
			if feedbacks, ok := underResFeedbackMap[incidentID]; ok && len(feedbacks) > 0 {
				setField(results[i], "under_resolution_feedback", feedbacks[0].Comment)
			}

			// Ready To Close feedback
			if feedbacks, ok := readyToCloseFeedbackMap[incidentID]; ok && len(feedbacks) > 0 {
				setField(results[i], "ready_to_close_feedback", feedbacks[0].Comment)
			}

			// Reopened feedback
			if feedbacks, ok := reopenedFeedbackMap[incidentID]; ok && len(feedbacks) > 0 {
				setField(results[i], "reopened_feedback", feedbacks[0].Comment)
			}

			if feedbacks, ok := feedbackMap[incidentID]; ok {
				// flat joined string for simple columns
				parts := make([]string, 0, len(feedbacks))
				for _, fb := range feedbacks {
					parts = append(parts, fmt.Sprintf(" %s", fb.Comment))
				}
				setField(results[i], "feedbacks", strings.Join(parts, " | "))

				// raw slice for array column (e.g. JSON export)
				raw := make([]map[string]interface{}, 0, len(feedbacks))
				for _, fb := range feedbacks {
					entry := map[string]interface{}{
						"created_by":    fb.CreatedBy,
						"comment":       fb.Comment,
						"created_at":    fb.CreatedAt,
						"to_state_name": fb.ToStateName,
					}

					raw = append(raw, entry)
				}

				setField(results[i], "feedback_array", raw)
			}

			// 11. Comments
			// Closed comment
			if comments, ok := closedCommentMap[incidentID]; ok && len(comments) > 0 {
				setField(results[i], "closed_comment", comments[0].Comment)
			}

			// Rejected comment
			if comments, ok := rejectedCommentMap[incidentID]; ok && len(comments) > 0 {
				setField(results[i], "rejected_comment", comments[0].Comment)
			}

			// Under Resolution comment
			if comments, ok := underResCommentMap[incidentID]; ok && len(comments) > 0 {
				setField(results[i], "under_resolution_comment", comments[0].Comment)
			}

			// In Progress comment
			if comments, ok := inProgressCommentMap[incidentID]; ok && len(comments) > 0 {
				setField(results[i], "in_progress_comment", comments[0].Comment)
			}

			// Ready To Close comment
			if comments, ok := readyToCloseCommentMap[incidentID]; ok && len(comments) > 0 {
				setField(results[i], "ready_to_close_comment", comments[0].Comment)
			}

			// Reopened comment
			if comments, ok := reopenedCommentMap[incidentID]; ok && len(comments) > 0 {
				setField(results[i], "reopened_comment", comments[0].Comment)
			}

			// Classification Full path
			if class, ok := classificationFullPathMap[classificationID]; ok && len(class) > 0 {
				setField(results[i], "full_classification", class)
			}

			// Location Full path
			if loc, ok := locationFullPathMap[locationID]; ok && len(loc) > 0 {
				setField(results[i], "full_location", loc)
			}

			if comments, ok := commentsMap[incidentID]; ok {
				// flat joined string for simple columns
				parts := make([]string, 0, len(comments))
				for _, fb := range comments {
					parts = append(parts, fmt.Sprintf(" %s", fb.Comment))
				}
				setField(results[i], "comments", strings.Join(parts, " | "))

				// raw slice for array column (e.g. JSON export)
				raw := make([]map[string]interface{}, 0, len(comments))
				for _, fb := range comments {
					entry := map[string]interface{}{
						"created_by":    fb.CreatedBy,
						"comment":       fb.Comment,
						"created_at":    fb.CreatedAt,
						"to_state_name": fb.ToStateName,
					}

					raw = append(raw, entry)
				}

				setField(results[i], "comment_array", raw)
			}

			// Dynamic {code}_* enrichments from fetchTransitionEnrichments
			if transEnrichMap != nil {
				if codeMap, ok := transEnrichMap[incidentID]; ok {
					for code, enrich := range codeMap {
						if !neededCodes[code] {
							continue
						}
						if enrich.PerformedBy != "" {
							setField(results[i], code+"_by", enrich.PerformedBy)
						}
						if enrich.TransitionedAt != "" {
							setField(results[i], code+"_at", enrich.TransitionedAt)
							setField(results[i], code+"_date", enrich.TransitionedAt)
						}
						if enrich.Comment != "" {
							log.Println("code for comment", code, enrich.Comment)
							setField(results[i], code+"_comment", enrich.Comment)
						}
						if enrich.Feedback != "" {
							log.Println("code for feedback ", code, enrich.Feedback)
							setField(results[i], code+"_feedback", enrich.Feedback)
						}
						if len(enrich.Attachments) > 0 {
							setField(results[i], code+"_attachment", strings.Join(enrich.Attachments, " | "))
							setField(results[i], code+"_attachments", enrich.Attachments)
						}
					}
				}
			}

			_ = row
		}
	}

	return results, total, nil
}

// GetTransitionUserNames returns incident_id → performer full name for the
// most-recent status_changed revision where new_value = newStateName.
// Delegates to fetchStatusTransitionData (names only).
//
//	func (r *reportRepository) GetTransitionUserNames0(ctx context.Context, newStateName string, incidentIDs []string) (map[string]string, error) {
//		names, _, err := r.fetchStatusTransitionData(ctx, models.IncidentRevisionStatus(newStateName), incidentIDs)
//		return names, err
//	}
func (r *reportRepository) fetchFeedbackDataByState(
	ctx context.Context,
	toStateName string,
	incidentIDs []string,
) (map[string][]TransitionLinkedResult, error) {
	return r.fetchTransitionLinkedData(ctx, "incident_feedbacks", "created_by_id", toStateName, incidentIDs)
}

func (r *reportRepository) fetchCommentDataByState(
	ctx context.Context,
	toStateName string,
	incidentIDs []string,
) (map[string][]TransitionLinkedResult, error) {
	return r.fetchTransitionLinkedData(ctx, "incident_comments", "author_id", toStateName, incidentIDs)
}

func (r *reportRepository) fetchTransitionLinkedData(
	ctx context.Context,
	table,
	userIDCol,
	toStateName string,
	incidentIDs []string,
) (map[string][]TransitionLinkedResult, error) {
	result := map[string][]TransitionLinkedResult{}
	if len(incidentIDs) == 0 {
		return result, nil
	}

	// is_internal filter only applies to incident_comments
	internalFilter := ""
	// if table == "incident_comments" {
	// 	internalFilter = "AND f.is_internal = false"
	// }

	// incident_comments uses content column, feedbacks use comment
	commentCol := "comment"
	if table == "incident_comments" {
		commentCol = "content"
	}

	query := `
        SELECT
            f.incident_id::text,
            TRIM(COALESCE(u.first_name,'') || ' ' || COALESCE(u.last_name,'')) AS full_name,
            COALESCE(f.` + commentCol + `, '')                                  AS comment,
            f.created_at::text,
            COALESCE(tws.name, '')                                              AS to_state_name
        FROM ` + table + ` f
        LEFT JOIN users u ON u.id = f.` + userIDCol + `
        LEFT JOIN incident_transition_histories tr ON tr.id = f.transition_history_id
        LEFT JOIN workflow_states tws ON tws.id = tr.to_state_id
        WHERE f.incident_id::text IN (?)
          AND tws.name = ?
          ` + internalFilter + `
        ORDER BY f.incident_id, f.created_at ASC`

	rows, err := r.db.WithContext(ctx).Raw(query, incidentIDs, toStateName).Rows()
	if err != nil {
		return nil, fmt.Errorf("fetchTransitionLinkedData(%s->%s): %w", table, toStateName, err)
	}
	defer rows.Close()
	for rows.Next() {
		var fb TransitionLinkedResult
		if err := rows.Scan(
			&fb.IncidentID,
			&fb.CreatedBy,
			&fb.Comment,
			&fb.CreatedAt,
			&fb.ToStateName,
		); err != nil {
			continue
		}
		result[fb.IncidentID] = append(result[fb.IncidentID], fb)
	}
	return result, nil
}

// fetchStatusTransitionData is the single generic query behind all per-status
// enrichment helpers. It returns the most-recent performer (full name) and the
// transition timestamp for each incident where the status changed TO `status`.
//
// The query reads incident_revisions, unnests the changes JSONB array, and
// filters on:
//
//	action_type   = 'status_changed'
//	field_name    = 'current_state_id'   (set by incident_service.ExecuteTransition)
//	new_value     = string(status)
func (r *reportRepository) fetchStatusTransitionData(
	ctx context.Context,
	fromState, toState string,
	incidentIDs []string,
) (names map[string]string, dates map[string]string, err error) {
	names = map[string]string{}
	dates = map[string]string{}
	if len(incidentIDs) == 0 {
		return
	}

	query := `
		SELECT DISTINCT ON (tr.incident_id)
			tr.incident_id::text,
			TRIM(COALESCE(u.first_name,'') || ' ' || COALESCE(u.last_name,'')) AS full_name,
			tr.transitioned_at::text
		FROM incident_transition_histories tr
		INNER JOIN workflow_states fws ON fws.id = tr.from_state_id
		INNER JOIN workflow_states tws ON tws.id = tr.to_state_id
		INNER JOIN users u ON u.id = tr.performed_by_id
		WHERE tws.name = ?
		  AND fws.name = ?
		  AND tr.incident_id::text IN (?)
		ORDER BY tr.incident_id, tr.transitioned_at DESC`

	rows, qerr := r.db.WithContext(ctx).Raw(query, toState, fromState, incidentIDs).Rows()
	if qerr != nil {
		err = fmt.Errorf("fetchStatusTransitionData(%s->%s): %w", fromState, toState, qerr)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var incidentID, fullName, transDate string
		if serr := rows.Scan(&incidentID, &fullName, &transDate); serr != nil {
			continue
		}
		names[incidentID] = fullName
		dates[incidentID] = transDate
	}
	return
}

// New
func (r *reportRepository) fetchMultiPairTransitionData(
	ctx context.Context,
	pairs []TransitionPair,
	incidentIDs []string,
) (names map[string]string, dates map[string]string, err error) {
	names = map[string]string{}
	dates = map[string]string{}

	for _, pair := range pairs {
		n, d, e := r.fetchStatusTransitionData(ctx, pair.From, pair.To, incidentIDs)
		if e != nil {
			err = e
			continue
		}
		for id, name := range n {
			if existing, ok := names[id]; ok {
				names[id] = existing + " | " + name
			} else {
				names[id] = name
			}
		}
		for id, date := range d {
			if existing, ok := dates[id]; ok {
				dates[id] = existing + " | " + date
			} else {
				dates[id] = date
			}
		}
	}
	return
}

// ── full path queries ───────────────────────────────────────────────────
func (r *reportRepository) fetchLocationPaths(
	ctx context.Context,
	locationIDs []string,
) (paths map[string]string, err error) {
	paths = map[string]string{}
	if len(locationIDs) == 0 {
		return
	}

	query := `
		WITH RECURSIVE location_hierarchy AS (
			SELECT
				id,
				name,
				parent_id,
				name::TEXT AS full_path
			FROM locations
			WHERE parent_id IS NULL

			UNION ALL

			SELECT
				l.id,
				l.name,
				l.parent_id,
				lh.full_path || ' > ' || l.name
			FROM locations l
			INNER JOIN location_hierarchy lh ON l.parent_id = lh.id
		)
		SELECT
			id::text,
			full_path
		FROM location_hierarchy
		WHERE id::text IN (?)
	`

	rows, qerr := r.db.WithContext(ctx).Raw(query, locationIDs).Rows()
	if qerr != nil {
		err = fmt.Errorf("fetchLocationPaths: %w", qerr)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var locationID, fullPath string
		if serr := rows.Scan(&locationID, &fullPath); serr != nil {
			continue
		}
		paths[locationID] = fullPath
	}
	return
}

func (r *reportRepository) fetchClassificationPaths(
	ctx context.Context,
	classificationIDs []string,
) (paths map[string]string, err error) {
	paths = map[string]string{}
	if len(classificationIDs) == 0 {
		return
	}

	query := `
		WITH RECURSIVE classification_hierarchy AS (
			SELECT
				id,
				name::TEXT AS full_path
			FROM classifications
			WHERE parent_id IS NULL

			UNION ALL

			SELECT
				c.id,
				ch.full_path || ' > ' || c.name
			FROM classifications c
			INNER JOIN classification_hierarchy ch ON c.parent_id = ch.id
		)
		SELECT
			id::text,
			full_path
		FROM classification_hierarchy
		WHERE id::text IN (?)
	`

	rows, qerr := r.db.WithContext(ctx).Raw(query, classificationIDs).Rows()
	if qerr != nil {
		err = fmt.Errorf("fetchClassificationPaths: %w", qerr)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var classificationID, fullPath string
		if serr := rows.Scan(&classificationID, &fullPath); serr != nil {
			continue
		}
		paths[classificationID] = fullPath
	}
	return
}

// ── Bulk enrichment queries ───────────────────────────────────────────────────

// fetchGeneralComments returns non-internal (is_internal = false) comments
// grouped by incident ID, ordered oldest-first.
func (r *reportRepository) fetchGeneralComments(ctx context.Context, incidentIDs []string) (map[string][]map[string]interface{}, error) {
	if len(incidentIDs) == 0 {
		return map[string][]map[string]interface{}{}, nil
	}

	query := `
		SELECT ic.incident_id::text,
			ic.content,
			TRIM(COALESCE(u.first_name,'') || ' ' || COALESCE(u.last_name,'')) AS author_name,
			COALESCE(ic.created_at::text, '') AS created_at
		FROM incident_comments ic
		LEFT JOIN users u ON ic.author_id = u.id
		WHERE ic.incident_id IN (?)
		  AND ic.deleted_at IS NULL
		  AND ic.is_internal = false
		ORDER BY ic.incident_id, ic.created_at ASC`

	rows, err := r.db.WithContext(ctx).Raw(query, incidentIDs).Rows()
	if err != nil {
		return nil, fmt.Errorf("fetchGeneralComments query failed: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]map[string]interface{})
	for rows.Next() {
		var incidentID, content, authorName, createdAt string
		if err := rows.Scan(&incidentID, &content, &authorName, &createdAt); err != nil {
			continue
		}
		result[incidentID] = append(result[incidentID], map[string]interface{}{
			"content":    content,
			"author":     authorName,
			"created_at": createdAt,
		})
	}
	return result, nil
}

func (r *reportRepository) fetchFeedbackData(
	ctx context.Context,
	incidentIDs []string,
) (map[string][]TransitionLinkedResult, error) {
	result := map[string][]TransitionLinkedResult{}
	if len(incidentIDs) == 0 {
		return result, nil
	}

	query := `
		SELECT
			f.incident_id::text,
			TRIM(COALESCE(u.first_name,'') || ' ' || COALESCE(u.last_name,'')) AS full_name,
			COALESCE(f.comment, '')                                             AS comment,
			f.created_at::text,
			COALESCE(tws.name, '')                                              AS to_state_name
		FROM incident_feedbacks f
		LEFT JOIN users u ON u.id = f.created_by_id
		LEFT JOIN incident_transition_histories tr ON tr.id = f.transition_history_id
		LEFT JOIN workflow_states tws ON tws.id = tr.to_state_id
		WHERE f.incident_id::text IN (?)
		ORDER BY f.incident_id, f.created_at ASC`

	rows, err := r.db.WithContext(ctx).Raw(query, incidentIDs).Rows()
	if err != nil {
		return nil, fmt.Errorf("fetchFeedbackData: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var fb TransitionLinkedResult
		if err := rows.Scan(
			&fb.IncidentID,
			&fb.CreatedBy,
			&fb.Comment,
			&fb.CreatedAt,
			&fb.ToStateName,
		); err != nil {
			continue
		}
		result[fb.IncidentID] = append(result[fb.IncidentID], fb)
	}
	return result, nil
}

func (r *reportRepository) fetchCommentData(
	ctx context.Context,
	incidentIDs []string,
) (map[string][]TransitionLinkedResult, error) {
	result := map[string][]TransitionLinkedResult{}
	if len(incidentIDs) == 0 {
		return result, nil
	}

	query := `
        SELECT
            f.incident_id::text,
            TRIM(COALESCE(u.first_name,'') || ' ' || COALESCE(u.last_name,'')) AS full_name,
            COALESCE(f.content, '')                                             AS comment,
            f.created_at::text,
            COALESCE(tws.name, '')                                              AS to_state_name
        FROM incident_comments f
        LEFT JOIN users u ON u.id = f.author_id
        LEFT JOIN incident_transition_histories tr ON tr.id = f.transition_history_id
        LEFT JOIN workflow_states tws ON tws.id = tr.to_state_id
        WHERE f.incident_id::text IN (?)
          AND f.is_internal = false
          AND f.deleted_at IS NULL
        ORDER BY f.incident_id, f.created_at ASC`

	rows, err := r.db.WithContext(ctx).Raw(query, incidentIDs).Rows()
	if err != nil {
		return nil, fmt.Errorf("fetchCommentData: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var fb TransitionLinkedResult
		if err := rows.Scan(
			&fb.IncidentID,
			&fb.CreatedBy,
			&fb.Comment,
			&fb.CreatedAt,
			&fb.ToStateName,
		); err != nil {
			continue
		}
		result[fb.IncidentID] = append(result[fb.IncidentID], fb)
	}
	return result, nil
}

// fetchContractorComments returns the latest contractor comment per incident.
// Contractor comments are those submitted during a state transition
// (transition_history_id IS NOT NULL), joined with the author's name.
func (r *reportRepository) fetchContractorComments(ctx context.Context, incidentIDs []string) (map[string]string, error) {
	if len(incidentIDs) == 0 {
		return map[string]string{}, nil
	}

	query := `
		SELECT DISTINCT ON (ic.incident_id)
			ic.incident_id::text,
			ic.content
			FROM incident_comments ic
			JOIN incident_transition_histories th ON th.id = ic.transition_history_id
			INNER JOIN workflow_states fws ON fws.id = th.from_state_id
			INNER JOIN workflow_states tws ON tws.id = th.to_state_id
			INNER JOIN users u ON u.id = th.performed_by_id
			WHERE ic.incident_id IN (?)
			  AND fws.name = 'Under Resolution'
			  AND tws.name = 'Ready To Close'
			  AND ic.deleted_at IS NULL
			  AND ic.is_internal = false`

	rows, err := r.db.WithContext(ctx).Raw(query, incidentIDs).Rows()
	if err != nil {
		return nil, fmt.Errorf("fetchContractorComments query failed: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var incidentID, content string
		if err := rows.Scan(&incidentID, &content); err != nil {
			continue
		}
		result[incidentID] = content
	}
	return result, nil
}

// fetchBeforeAttachments returns preview URLs for attachments uploaded at
// incident creation (transition_history_id IS NULL = no transition link),
// grouped by incident ID. These are typically creator/agent uploads.
func (r *reportRepository) fetchBeforeAttachments(ctx context.Context, incidentIDs []string, protocol, hostname, token string) (map[string][]string, error) {
	if len(incidentIDs) == 0 || hostname == "" {
		return map[string][]string{}, nil
	}

	query := `
		SELECT ia.incident_id::text, ia.id::text
		FROM incident_attachments ia
		WHERE ia.incident_id IN (?)
		  AND ia.deleted_at IS NULL
		  AND ia.transition_history_id IS NULL
		ORDER BY ia.incident_id, ia.created_at ASC`

	return r.scanAttachmentURLs(ctx, query, incidentIDs, protocol, hostname, token)
}

// fetchAfterAttachments returns preview URLs for attachments uploaded during
// a contractor transition (transition_history_id IS NOT NULL),
// grouped by incident ID.
func (r *reportRepository) fetchAfterAttachments(ctx context.Context, incidentIDs []string, protocol, hostname, token string) (map[string][]string, error) {
	if len(incidentIDs) == 0 || hostname == "" {
		return map[string][]string{}, nil
	}

	query := `
		SELECT ia.incident_id::text, ia.id::text
		FROM incident_attachments ia
		JOIN incident_transition_histories th ON th.id = ia.transition_history_id
		INNER JOIN workflow_states fws ON fws.id = th.from_state_id
		INNER JOIN workflow_states tws ON tws.id = th.to_state_id
		WHERE tws.name = 'Ready To Close'
			AND fws.name = 'Under Resolution'
		  	AND ia.incident_id IN (?)
		  	AND ia.deleted_at IS NULL
		  	AND ia.transition_history_id IS NOT NULL
		ORDER BY ia.incident_id, ia.created_at ASC`

	return r.scanAttachmentURLs(ctx, query, incidentIDs, protocol, hostname, token)
}

// fetchAllAttachments returns preview URLs for ALL non-deleted attachments,
// grouped by incident ID.
func (r *reportRepository) fetchAllAttachments(ctx context.Context, incidentIDs []string, protocol, hostname, token string) (map[string][]string, error) {
	if len(incidentIDs) == 0 || hostname == "" {
		return map[string][]string{}, nil
	}

	query := `
		SELECT ia.incident_id::text, ia.id::text
		FROM incident_attachments ia
		WHERE ia.incident_id IN (?)
		  AND ia.deleted_at IS NULL
		ORDER BY ia.incident_id, ia.created_at ASC`

	return r.scanAttachmentURLs(ctx, query, incidentIDs, protocol, hostname, token)
}

// scanAttachmentURLs is a shared scanner used by the three attachment fetchers.
func (r *reportRepository) scanAttachmentURLs(ctx context.Context, query string, incidentIDs []string, protocol, hostname, token string) (map[string][]string, error) {
	rows, err := r.db.WithContext(ctx).Raw(query, incidentIDs).Rows()
	if err != nil {
		return nil, fmt.Errorf("scanAttachmentURLs query failed: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]string)
	for rows.Next() {
		var incidentID, attachID string
		if err := rows.Scan(&incidentID, &attachID); err != nil {
			continue
		}
		url := protocol + "://" + hostname + "/api/v1/attachments/" + attachID + "/preview?token=" + token
		result[incidentID] = append(result[incidentID], url)
	}
	return result, nil
}

// fetchReopenData returns (names, dates) maps for the most-recent reopen per
// incident. A reopen is detected in incident_revisions when action_type =
// 'status_changed' and the old state is terminal while the new state is not.
// Uses the same JSONB changes pattern as GetTransitionUserNames.
func (r *reportRepository) fetchReopenDataOld(ctx context.Context, incidentIDs []string) (names map[string]string, dates map[string]string, err error) {
	names = map[string]string{}
	dates = map[string]string{}
	if len(incidentIDs) == 0 {
		return
	}

	// Join workflow_states on the old/new state names stored in the changes JSON
	// to detect terminal → non-terminal transitions (= reopen).
	query := `
		SELECT DISTINCT ON (ir.incident_id)
			ir.incident_id::text,
			TRIM(COALESCE(u.first_name,'') || ' ' || COALESCE(u.last_name,'')) AS full_name,
			ir.created_at::text AS reopen_date
		FROM incident_revisions ir
		CROSS JOIN jsonb_array_elements(ir.changes::jsonb) AS chg
		JOIN workflow_states ws_old
			ON ws_old.name = chg->>'old_value' AND ws_old.state_type = 'terminal'
		JOIN workflow_states ws_new
			ON ws_new.name = chg->>'new_value' AND ws_new.state_type != 'terminal'
		LEFT JOIN users u ON ir.performed_by_id = u.id
		WHERE ir.action_type = 'status_changed'
		  AND chg->>'field_name' = 'current_state_id'
		  AND ir.incident_id IN (?)
		ORDER BY ir.incident_id, ir.created_at DESC`

	rows, qerr := r.db.WithContext(ctx).Raw(query, incidentIDs).Rows()
	if qerr != nil {
		err = fmt.Errorf("fetchReopenData query failed: %w", qerr)
		return
	}
	defer rows.Close()

	for rows.Next() {
		var incidentID, fullName, reopenDate string
		if serr := rows.Scan(&incidentID, &fullName, &reopenDate); serr != nil {
			continue
		}
		names[incidentID] = fullName
		dates[incidentID] = reopenDate
	}
	return
}

// fetchEscalationDates returns the earliest escalation (notified_at) per incident
// from the escalation_slas table.
func (r *reportRepository) fetchEscalationDates(ctx context.Context, incidentIDs []string) (map[string]string, error) {
	if len(incidentIDs) == 0 {
		return map[string]string{}, nil
	}

	query := `
		SELECT DISTINCT ON (es.incident_id)
			es.incident_id::text,
			es.notified_at::text
		FROM escalation_slas es
		WHERE es.incident_id IN (?)
		  AND es.notified_at IS NOT NULL
		ORDER BY es.incident_id, es.notified_at ASC`

	rows, err := r.db.WithContext(ctx).Raw(query, incidentIDs).Rows()
	if err != nil {
		return nil, fmt.Errorf("fetchEscalationDates query failed: %w", err)
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var incidentID, date string
		if err := rows.Scan(&incidentID, &date); err != nil {
			continue
		}
		result[incidentID] = date
	}
	return result, nil
}

// fetchTransitionEnrichments returns per-(incident, transition_code) enrichment data
// covering: performer name & timestamp, first non-internal comment, first feedback,
// and all attachment preview URLs. Only called when at least one {code}_* column is
// requested by the report.
func (r *reportRepository) fetchTransitionEnrichments(
	ctx context.Context,
	incidentIDs []string,
	protocol, hostname, token string,
) (map[string]map[string]TransitionEnrichment, error) {
	result := map[string]map[string]TransitionEnrichment{}
	if len(incidentIDs) == 0 {
		return result, nil
	}

	ensure := func(iID, code string) {
		if result[iID] == nil {
			result[iID] = map[string]TransitionEnrichment{}
		}
	}

	// Query 1 — performers & timestamps (most-recent per incident+code)
	q1 := `
		SELECT DISTINCT ON (ith.incident_id, wt.code)
			ith.incident_id::text,
			wt.code,
			TRIM(COALESCE(u.first_name,'') || ' ' || COALESCE(u.last_name,'')) AS performer,
			ith.transitioned_at::text
		FROM incident_transition_histories ith
		INNER JOIN workflow_transitions wt ON wt.id = ith.transition_id AND wt.deleted_at IS NULL
		LEFT JOIN users u ON u.id = ith.performed_by_id
		WHERE ith.incident_id::text IN (?)
		ORDER BY ith.incident_id, wt.code, ith.transitioned_at DESC`
	rows1, err := r.db.WithContext(ctx).Raw(q1, incidentIDs).Rows()
	if err != nil {
		return nil, fmt.Errorf("fetchTransitionEnrichments performers: %w", err)
	}
	defer rows1.Close()
	for rows1.Next() {
		var iID, code, performer, transAt string
		if err := rows1.Scan(&iID, &code, &performer, &transAt); err != nil {
			continue
		}
		ensure(iID, code)
		e := result[iID][code]
		e.PerformedBy = performer
		e.TransitionedAt = transAt
		result[iID][code] = e
	}

	// Query 2 — all comments per incident+code, concatenated with " | "
	// is_internal is intentionally not filtered here: transition-linked comments
	// (e.g. reopen notes) are saved as internal but must still appear in reports.
	q2 := `
		SELECT
			c.incident_id::text,
			wt.code,
			STRING_AGG(COALESCE(c.content, ''), ' | ' ORDER BY c.created_at ASC) AS comment
		FROM incident_comments c
		INNER JOIN incident_transition_histories ith ON ith.id = c.transition_history_id
		INNER JOIN workflow_transitions wt ON wt.id = ith.transition_id AND wt.deleted_at IS NULL
		WHERE c.incident_id::text IN (?)
		  AND c.deleted_at IS NULL
		GROUP BY c.incident_id, wt.code`
	rows2, err := r.db.WithContext(ctx).Raw(q2, incidentIDs).Rows()
	if err != nil {
		return nil, fmt.Errorf("fetchTransitionEnrichments comments: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var iID, code, comment string
		if err := rows2.Scan(&iID, &code, &comment); err != nil {
			continue
		}
		ensure(iID, code)
		e := result[iID][code]
		e.Comment = comment
		result[iID][code] = e
	}

	// Query 3 — all feedbacks per incident+code, concatenated with " | "
	q3 := `
		SELECT
			f.incident_id::text,
			wt.code,
			STRING_AGG(COALESCE(f.comment, ''), ' | ' ORDER BY f.created_at ASC) AS feedback
		FROM incident_feedbacks f
		INNER JOIN incident_transition_histories ith ON ith.id = f.transition_history_id
		INNER JOIN workflow_transitions wt ON wt.id = ith.transition_id AND wt.deleted_at IS NULL
		WHERE f.incident_id::text IN (?)
		GROUP BY f.incident_id, wt.code`
	rows3, err := r.db.WithContext(ctx).Raw(q3, incidentIDs).Rows()
	if err != nil {
		return nil, fmt.Errorf("fetchTransitionEnrichments feedbacks: %w", err)
	}
	defer rows3.Close()
	for rows3.Next() {
		var iID, code, feedback string
		if err := rows3.Scan(&iID, &code, &feedback); err != nil {
			continue
		}
		ensure(iID, code)
		e := result[iID][code]
		e.Feedback = feedback
		result[iID][code] = e
	}

	// Query 4 — all attachments per incident+code (build preview URLs)
	if hostname != "" {
		q4 := `
			SELECT
				ia.incident_id::text,
				wt.code,
				ia.id::text AS attachment_id
			FROM incident_attachments ia
			INNER JOIN incident_transition_histories ith ON ith.id = ia.transition_history_id
			INNER JOIN workflow_transitions wt ON wt.id = ith.transition_id AND wt.deleted_at IS NULL
			WHERE ia.incident_id::text IN (?)
			  AND ia.deleted_at IS NULL
			ORDER BY ia.incident_id, wt.code, ia.created_at ASC`
		rows4, err := r.db.WithContext(ctx).Raw(q4, incidentIDs).Rows()
		if err != nil {
			return nil, fmt.Errorf("fetchTransitionEnrichments attachments: %w", err)
		}
		defer rows4.Close()
		for rows4.Next() {
			var iID, code, attachID string
			if err := rows4.Scan(&iID, &code, &attachID); err != nil {
				continue
			}
			ensure(iID, code)
			url := protocol + "://" + hostname + "/api/v1/attachments/" + attachID + "/preview?token=" + token
			e := result[iID][code]
			e.Attachments = append(e.Attachments, url)
			result[iID][code] = e
		}
	}

	return result, nil
}

// loadDynamicTransitionPairs queries workflow_transitions joined with workflow_states
// and transition_allowed_roles to build a map of to-state-name → []TransitionPair.
// Each TransitionPair carries the from-state name and the comma-separated roles
// that are allowed to execute that transition.
// Returns the map so callers can replace the hardcoded TransitionPairs variable.
func (r *reportRepository) loadDynamicTransitionPairs(ctx context.Context) (map[string][]TransitionPair, error) {
	query := `
		SELECT
			wt.name                                                                  AS transition_name,
			wt.code                                                                  AS transition_code,
			fws.name                                                                 AS from_state,
			COALESCE(fws.state_type, 'normal')                                       AS from_state_type,
			tws.name                                                                 AS to_state,
			COALESCE(STRING_AGG(DISTINCT r.name, ',' ORDER BY r.name), '')          AS allowed_roles
		FROM workflow_transitions wt
		INNER JOIN workflow_states fws ON fws.id = wt.from_state_id AND fws.deleted_at IS NULL
		INNER JOIN workflow_states tws ON tws.id = wt.to_state_id   AND tws.deleted_at IS NULL
		LEFT  JOIN transition_allowed_roles tar ON tar.workflow_transition_id = wt.id
		LEFT  JOIN roles r ON r.id = tar.role_id AND r.deleted_at IS NULL AND r.is_active = true
		WHERE wt.deleted_at IS NULL
		  AND wt.is_active  = true
		GROUP BY wt.name, wt.code, fws.name, fws.state_type, tws.name
		ORDER BY tws.name, fws.name`

	rows, err := r.db.WithContext(ctx).Raw(query).Rows()
	if err != nil {
		return nil, fmt.Errorf("loadDynamicTransitionPairs: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]TransitionPair)
	for rows.Next() {
		var transName, transCode, fromState, fromStateType, toState, rolesStr string
		if err := rows.Scan(&transName, &transCode, &fromState, &fromStateType, &toState, &rolesStr); err != nil {
			continue
		}
		var roles []string
		if rolesStr != "" {
			roles = strings.Split(rolesStr, ",")
		}
		result[toState] = append(result[toState], TransitionPair{
			Name:          transName,
			Code:          transCode,
			From:          fromState,
			To:            toState,
			FromStateType: fromStateType,
			Roles:         roles,
		})
	}
	return result, nil
}

// ── helpers ───────────────────────────────────────────────────────────────────

// extractCF parses a custom_fields JSON blob (string or map) and returns the
// value for key, or "" if absent/unparseable.
func extractCF(raw interface{}, key string) interface{} {
	if raw == nil {
		return ""
	}
	var data map[string]interface{}
	switch v := raw.(type) {
	case string:
		if err := json.Unmarshal([]byte(v), &data); err != nil {
			return ""
		}
	case map[string]interface{}:
		data = v
	default:
		return ""
	}
	if val, ok := data[key]; ok {
		return val
	}
	return ""
}

// IDStr extracts an incident UUID as a lowercase hyphenated string from
// whatever type the pgx/GORM driver returns (string, []byte, or [16]byte).
func IDStr(v interface{}) string {
	if v == nil {
		return ""
	}
	switch id := v.(type) {
	case string:
		return id
	case []byte:
		return string(id)
	case [16]byte:
		return uuid.UUID(id).String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

func strOrEmpty(v interface{}) string {
	if v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func filterEmpty(ss []string) []string {
	out := ss[:0]
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}

func (r *reportRepository) ExecuteUserQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error) {
	var total int64
	var results []map[string]interface{}

	reqColumns, _ := ctx.Value(constants.ContextKeys.REPORT_COLUMNS).([]models.ColumnField)

	query := r.db.WithContext(ctx).Model(&models.User{})
	query = r.applyFilters(ctx, query, filters)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	query = r.applySorting(query, sorting)
	if sorting == nil {
		query = query.Order("users.created_at DESC")
	}

	offset := (page - 1) * limit
	rows, err := query.
		Select("users.id, users.email, users.username, users.first_name, users.last_name, users.phone, users.avatar, users.is_active, users.is_super_admin, users.created_at, users.updated_at, users.last_login_at, " +
			"departments.name as department_name, locations.name as location_name").
		Joins("LEFT JOIN departments ON users.department_id = departments.id").
		Joins("LEFT JOIN locations ON users.location_id = locations.id").
		Offset(offset).
		Limit(limit).
		Rows()

	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	for rows.Next() {
		columns := make([]interface{}, len(cols))
		columnPointers := make([]interface{}, len(cols))
		for i := range columns {
			columnPointers[i] = &columns[i]
		}

		if err := rows.Scan(columnPointers...); err != nil {
			continue
		}

		rawRow := make(map[string]interface{})
		for i, colName := range cols {
			val := columns[i]
			if b, ok := val.([]byte); ok {
				rawRow[colName] = string(b)
			} else if t, ok := val.(time.Time); ok {
				rawRow[colName] = t.Format(time.RFC3339Nano) // convert to string here
			} else {
				rawRow[colName] = val
			}
		}

		row := make(map[string]interface{})
		if len(reqColumns) > 0 {
			for _, col := range reqColumns {
				row[col.Label] = rawRow[col.Field]
			}
		} else {
			for k, v := range rawRow {
				row[k] = v
			}
			if v, ok := rawRow["department_name"]; ok {
				row["Department Name"] = v
			}
			if v, ok := rawRow["location_name"]; ok {
				row["Location Name"] = v
			}
		}

		results = append(results, row)
	}

	return results, total, nil
}

// ExecuteUserPerformanceQuery builds the User Performance report.
//
// Each row represents a single transition from incident_transition_histories so
// callers can see exactly who performed each state change, when, with what comment,
// and what attachments were uploaded during that transition.
//
// Filterable fields (use as filter.Field in payload):
//
//	performed_by_id | transitioned_at | incident_id | incident_number |
//	classification_id | location_id | record_type |
//	classification_name | to_state_name | state_name | location_name |
//	user_email | user_first_name | user_last_name
//
// Output col.Field names:
//
//	incident_number | resource_id | status | user | timestamp | comment | location | classification | attachment | attachments
func (r *reportRepository) ExecuteUserPerformanceQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error) {
	hostname, _ := ctx.Value(constants.ContextKeys.HOSTNAME).(string)
	protocol, _ := ctx.Value(constants.ContextKeys.PROTOCOL).(string)
	token, _ := ctx.Value(constants.ContextKeys.Token).(string)
	reqColumns, _ := ctx.Value(constants.ContextKeys.REPORT_COLUMNS).([]models.ColumnField)

	buildBase := func() *gorm.DB {
		q := r.db.WithContext(ctx).
			Table("incident_transition_histories").
			Joins("INNER JOIN incidents ON incidents.id = incident_transition_histories.incident_id").
			Joins("LEFT JOIN workflow_states ON workflow_states.id = incident_transition_histories.to_state_id").
			Joins("LEFT JOIN users perf_users ON perf_users.id = incident_transition_histories.performed_by_id").
			Joins("LEFT JOIN locations ON locations.id = incidents.location_id").
			Joins("LEFT JOIN classifications ON classifications.id = incidents.classification_id")
		return r.applyFilters(ctx, q, filters)
	}

	var total int64
	if err := buildBase().Select("COUNT(*)").Scan(&total).Error; err != nil {
		return nil, 0, err
	}

	orderClause := "incident_transition_histories.transitioned_at DESC"
	if sorting != nil && sorting.Field != "" {
		dir := "ASC"
		if strings.EqualFold(sorting.Direction, "desc") {
			dir = "DESC"
		}
		if col, ok := userPerformanceFilterFields[sorting.Field]; ok {
			orderClause = col + " " + dir
		}
	}

	offset := (page - 1) * limit
	dataRows, err := buildBase().
		Select(
			"incident_transition_histories.id::text AS transition_id, " +
				"incident_transition_histories.transitioned_at, " +
				"incidents.incident_number, " +
				"COALESCE(workflow_states.name, '') AS to_state_name, " +
				"COALESCE(perf_users.first_name, '') AS user_first_name, " +
				"COALESCE(perf_users.last_name, '') AS user_last_name, " +
				"COALESCE(perf_users.email, '') AS user_email, " +
				"COALESCE(locations.name, '') AS location_name, " +
				"COALESCE(classifications.name, '') AS classification_name",
		).
		Order(orderClause).
		Offset(offset).
		Limit(limit).
		Rows()
	if err != nil {
		return nil, 0, err
	}
	defer dataRows.Close()

	type transEntry struct {
		transitionID       string
		transitionedAt     time.Time
		incidentNumber     string
		toStateName        string
		userFullName       string
		locationName       string
		classificationName string
	}

	var entries []transEntry
	transitionIDs := make([]string, 0)

	for dataRows.Next() {
		var (
			transID, incNumber, stateName, firstName, lastName, email string
			locationName, classificationName                          string
			transitionedAt                                            time.Time
		)
		if err := dataRows.Scan(&transID, &transitionedAt, &incNumber, &stateName, &firstName, &lastName, &email, &locationName, &classificationName); err != nil {
			continue
		}
		fullName := strings.TrimSpace(firstName + " " + lastName)
		if fullName == "" {
			fullName = email
		}
		entries = append(entries, transEntry{
			transitionID:       transID,
			transitionedAt:     transitionedAt,
			incidentNumber:     incNumber,
			toStateName:        stateName,
			userFullName:       fullName,
			locationName:       locationName,
			classificationName: classificationName,
		})
		transitionIDs = append(transitionIDs, transID)
	}

	if len(entries) == 0 {
		return []map[string]interface{}{}, total, nil
	}

	commentsMap, _ := r.fetchCommentsByTransition(ctx, transitionIDs)
	attachMap, _ := r.fetchAttachmentsByTransition(ctx, transitionIDs, protocol, hostname, token)

	results := make([]map[string]interface{}, 0, len(entries))
	for _, e := range entries {
		commentStr := commentsMap[e.transitionID]
		attachURLs := attachMap[e.transitionID]
		attachStr := strings.Join(attachURLs, " | ")
		log.Println(e.locationName)
		rawRow := map[string]interface{}{
			"incident_number": e.incidentNumber,
			"resource_id":     e.incidentNumber,
			"status":          e.toStateName,
			"user":            e.userFullName,
			"timestamp":       e.transitionedAt,
			"comment":         commentStr,
			"location":        e.locationName,
			"classification":  e.classificationName,
			"attachment":      attachStr,
			"attachments":     attachStr,
		}

		row := make(map[string]interface{})
		if len(reqColumns) > 0 {
			for _, col := range reqColumns {
				row[col.Label] = rawRow[col.Field]
			}
		} else {
			row["Incident Number"] = rawRow["incident_number"]
			row["Status"] = rawRow["status"]
			row["User"] = rawRow["user"]
			row["Date & Time"] = rawRow["timestamp"]
			row["Comment"] = rawRow["comment"]
			row["Location"] = rawRow["location"]
			row["Classification"] = rawRow["classification"]
			row["Attachments"] = rawRow["attachments"]
		}

		results = append(results, row)
	}

	return results, total, nil
}

// fetchCommentsByTransition returns the first (oldest) comment content per transition ID.
func (r *reportRepository) fetchCommentsByTransition(ctx context.Context, transitionIDs []string) (map[string]string, error) {
	result := make(map[string]string)
	if len(transitionIDs) == 0 {
		return result, nil
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT DISTINCT ON (ic.transition_history_id)
			ic.transition_history_id::text,
			ic.content
		FROM incident_comments ic
		WHERE ic.transition_history_id::text IN (?)
		  AND ic.deleted_at IS NULL
		ORDER BY ic.transition_history_id, ic.created_at ASC
	`, transitionIDs).Rows()
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var tid, content string
		if err := rows.Scan(&tid, &content); err != nil {
			continue
		}
		result[tid] = content
	}
	return result, nil
}

// fetchAttachmentsByTransition returns signed preview URLs for attachments grouped by transition ID.
func (r *reportRepository) fetchAttachmentsByTransition(ctx context.Context, transitionIDs []string, protocol, hostname, token string) (map[string][]string, error) {
	result := make(map[string][]string)
	if len(transitionIDs) == 0 || hostname == "" {
		return result, nil
	}
	rows, err := r.db.WithContext(ctx).Raw(`
		SELECT ia.transition_history_id::text, ia.id::text
		FROM incident_attachments ia
		WHERE ia.transition_history_id::text IN (?)
		  AND ia.deleted_at IS NULL
		ORDER BY ia.transition_history_id, ia.created_at ASC
	`, transitionIDs).Rows()
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var tid, attachID string
		if err := rows.Scan(&tid, &attachID); err != nil {
			continue
		}
		url := protocol + "://" + hostname + "/api/v1/attachments/" + attachID + "/preview?token=" + token
		result[tid] = append(result[tid], url)
	}
	return result, nil
}

func (r *reportRepository) ExecuteWorkflowQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error) {
	var total int64
	var results []map[string]interface{}

	reqColumns, _ := ctx.Value(constants.ContextKeys.REPORT_COLUMNS).([]models.ColumnField)

	query := r.db.WithContext(ctx).Model(&models.Workflow{})
	query = r.applyFilters(ctx, query, filters)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	query = r.applySorting(query, sorting)
	if sorting == nil {
		query = query.Order("workflows.created_at DESC")
	}

	offset := (page - 1) * limit
	rows, err := query.
		Select("workflows.*, creators.username as created_by_username").
		Joins("LEFT JOIN users as creators ON workflows.created_by_id = creators.id").
		Offset(offset).
		Limit(limit).
		Rows()

	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	for rows.Next() {
		columns := make([]interface{}, len(cols))
		columnPointers := make([]interface{}, len(cols))
		for i := range columns {
			columnPointers[i] = &columns[i]
		}

		if err := rows.Scan(columnPointers...); err != nil {
			continue
		}

		rawRow := make(map[string]interface{})
		for i, colName := range cols {
			val := columns[i]
			if b, ok := val.([]byte); ok {
				rawRow[colName] = string(b)
			} else {
				rawRow[colName] = val
			}
		}

		row := make(map[string]interface{})
		if len(reqColumns) > 0 {
			for _, col := range reqColumns {
				row[col.Label] = rawRow[col.Field]
			}
		} else {
			for k, v := range rawRow {
				row[k] = v
			}
			if v, ok := rawRow["created_by_username"]; ok {
				row["Created By Username"] = v
			}
		}

		results = append(results, row)
	}

	return results, total, nil
}

func (r *reportRepository) ExecuteDepartmentQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error) {
	var total int64
	var results []map[string]interface{}

	reqColumns, _ := ctx.Value(constants.ContextKeys.REPORT_COLUMNS).([]models.ColumnField)

	query := r.db.WithContext(ctx).Model(&models.Department{})
	query = r.applyFilters(ctx, query, filters)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	query = r.applySorting(query, sorting)
	if sorting == nil {
		query = query.Order("departments.name ASC")
	}

	offset := (page - 1) * limit
	rows, err := query.
		Select("departments.*, parents.name as parent_name, " +
			"managers.username as manager_username, managers.first_name as manager_first_name, managers.last_name as manager_last_name").
		Joins("LEFT JOIN departments as parents ON departments.parent_id = parents.id").
		Joins("LEFT JOIN users as managers ON departments.manager_id = managers.id").
		Offset(offset).
		Limit(limit).
		Rows()

	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	for rows.Next() {
		columns := make([]interface{}, len(cols))
		columnPointers := make([]interface{}, len(cols))
		for i := range columns {
			columnPointers[i] = &columns[i]
		}

		if err := rows.Scan(columnPointers...); err != nil {
			continue
		}

		rawRow := make(map[string]interface{})
		for i, colName := range cols {
			val := columns[i]
			if b, ok := val.([]byte); ok {
				rawRow[colName] = string(b)
			} else {
				rawRow[colName] = val
			}
		}

		row := make(map[string]interface{})
		if len(reqColumns) > 0 {
			for _, col := range reqColumns {
				row[col.Label] = rawRow[col.Field]
			}
		} else {
			for k, v := range rawRow {
				row[k] = v
			}
			if v, ok := rawRow["parent_name"]; ok {
				row["parent.name"] = v
			}
			if v, ok := rawRow["manager_username"]; ok {
				row["manager.username"] = v
			}
			mgrFirst, _ := rawRow["manager_first_name"].(string)
			mgrLast, _ := rawRow["manager_last_name"].(string)
			mgrFullName := ""
			if mgrFirst != "" || mgrLast != "" {
				mgrFullName = mgrFirst
				if mgrLast != "" {
					if mgrFullName != "" {
						mgrFullName += " "
					}
					mgrFullName += mgrLast
				}
			}
			row["manager.full_name"] = mgrFullName
		}

		results = append(results, row)
	}

	return results, total, nil
}

func (r *reportRepository) ExecuteLocationQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error) {
	var total int64
	var results []map[string]interface{}

	reqColumns, _ := ctx.Value(constants.ContextKeys.REPORT_COLUMNS).([]models.ColumnField)

	query := r.db.WithContext(ctx).Model(&models.Location{})
	query = r.applyFilters(ctx, query, filters)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	query = r.applySorting(query, sorting)
	if sorting == nil {
		query = query.Order("locations.name ASC")
	}

	offset := (page - 1) * limit
	rows, err := query.
		Select("locations.*, parents.name as parent_name").
		Joins("LEFT JOIN locations as parents ON locations.parent_id = parents.id").
		Offset(offset).
		Limit(limit).
		Rows()

	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	for rows.Next() {
		columns := make([]interface{}, len(cols))
		columnPointers := make([]interface{}, len(cols))
		for i := range columns {
			columnPointers[i] = &columns[i]
		}

		if err := rows.Scan(columnPointers...); err != nil {
			continue
		}

		rawRow := make(map[string]interface{})
		for i, colName := range cols {
			val := columns[i]
			if b, ok := val.([]byte); ok {
				rawRow[colName] = string(b)
			} else {
				rawRow[colName] = val
			}
		}

		row := make(map[string]interface{})
		if len(reqColumns) > 0 {
			for _, col := range reqColumns {
				row[col.Label] = rawRow[col.Field]
			}
		} else {
			for k, v := range rawRow {
				row[k] = v
			}
			if v, ok := rawRow["parent_name"]; ok {
				row["parent.name"] = v
			}
		}

		results = append(results, row)
	}

	return results, total, nil
}

func (r *reportRepository) ExecuteClassificationQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error) {
	var total int64
	var results []map[string]interface{}

	reqColumns, _ := ctx.Value(constants.ContextKeys.REPORT_COLUMNS).([]models.ColumnField)

	query := r.db.WithContext(ctx).Model(&models.Classification{})
	query = r.applyFilters(ctx, query, filters)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	query = r.applySorting(query, sorting)
	if sorting == nil {
		query = query.Order("classifications.name ASC")
	}

	offset := (page - 1) * limit
	rows, err := query.
		Select("classifications.*, parents.name as parent_name").
		Joins("LEFT JOIN classifications as parents ON classifications.parent_id = parents.id").
		Offset(offset).
		Limit(limit).
		Rows()

	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	for rows.Next() {
		columns := make([]interface{}, len(cols))
		columnPointers := make([]interface{}, len(cols))
		for i := range columns {
			columnPointers[i] = &columns[i]
		}

		if err := rows.Scan(columnPointers...); err != nil {
			continue
		}

		rawRow := make(map[string]interface{})
		for i, colName := range cols {
			val := columns[i]
			if b, ok := val.([]byte); ok {
				rawRow[colName] = string(b)
			} else {
				rawRow[colName] = val
			}
		}

		row := make(map[string]interface{})
		if len(reqColumns) > 0 {
			for _, col := range reqColumns {
				row[col.Label] = rawRow[col.Field]
			}
		} else {
			for k, v := range rawRow {
				row[k] = v
			}
			if v, ok := rawRow["parent_name"]; ok {
				row["parent.name"] = v
			}
		}

		results = append(results, row)
	}

	return results, total, nil
}

func (r *reportRepository) ExecuteActionLogQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error) {
	var total int64
	var results []map[string]interface{}

	reqColumns, _ := ctx.Value(constants.ContextKeys.REPORT_COLUMNS).([]models.ColumnField)

	query := r.db.WithContext(ctx).Model(&models.ActionLog{})
	query = r.applyFilters(ctx, query, filters)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	query = r.applySorting(query, sorting)
	if sorting == nil {
		query = query.Order("action_logs.created_at DESC")
	}

	offset := (page - 1) * limit
	rows, err := query.
		Select("action_logs.id, action_logs.action, action_logs.module, action_logs.resource_id, " +
			"action_logs.description, action_logs.ip_address, action_logs.user_agent, " +
			"action_logs.status, action_logs.error_msg, action_logs.duration, action_logs.created_at, " +
			"users.email as user_email, users.username as user_username, " +
			"users.first_name as user_first_name, users.last_name as user_last_name").
		Joins("LEFT JOIN users ON action_logs.user_id = users.id").
		Offset(offset).
		Limit(limit).
		Rows()
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	cols, _ := rows.Columns()
	for rows.Next() {
		columns := make([]interface{}, len(cols))
		columnPointers := make([]interface{}, len(cols))
		for i := range columns {
			columnPointers[i] = &columns[i]
		}
		if err := rows.Scan(columnPointers...); err != nil {
			continue
		}

		rawRow := make(map[string]interface{})
		for i, colName := range cols {
			val := columns[i]
			if b, ok := val.([]byte); ok {
				rawRow[colName] = string(b)
			} else {
				rawRow[colName] = val
			}
		}

		// Synthesise user_full_name from the joined first/last name columns.
		firstName, _ := rawRow["user_first_name"].(string)
		lastName, _ := rawRow["user_last_name"].(string)
		switch {
		case firstName != "" && lastName != "":
			rawRow["user_full_name"] = firstName + " " + lastName
		case firstName != "":
			rawRow["user_full_name"] = firstName
		default:
			rawRow["user_full_name"] = lastName
		}

		row := make(map[string]interface{})
		if len(reqColumns) > 0 {
			for _, col := range reqColumns {
				row[col.Label] = rawRow[col.Field]
			}
		} else {
			for k, v := range rawRow {
				row[k] = v
			}
		}

		results = append(results, row)
	}

	return results, total, nil
}

// ── Count-group helpers ───────────────────────────────────────────────────────

// buildCountRow maps a rawRow (field→value) to the output row using reqColumns
// (col.Field → col.Label). Falls back to the default display-name map when no
// columns are injected via context.
func buildCountRow(rawRow map[string]interface{}, reqColumns []models.ColumnField, defaults map[string]string) map[string]interface{} {
	row := make(map[string]interface{})
	if len(reqColumns) > 0 {
		for _, col := range reqColumns {
			row[col.Label] = rawRow[col.Field]
		}
	} else {
		for field, label := range defaults {
			row[label] = rawRow[field]
		}
	}
	return row
}

// ── Location count (without status) ──────────────────────────────────────────
// Output col.Field names: location_name | parent_location_name | incident_count
// The classifications JOIN is kept for filter support (classification_id / classification_name)
// but is not part of the grouping — one row per location.
func (r *reportRepository) ExecuteLocationCountQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error) {
	reqColumns, _ := ctx.Value(constants.ContextKeys.REPORT_COLUMNS).([]models.ColumnField)
	buildBase := func() *gorm.DB {
		q := r.db.WithContext(ctx).Debug().
			Table("locations").
			Joins("LEFT JOIN locations parent_loc ON parent_loc.id = locations.parent_id").
			Joins("LEFT JOIN incidents ON incidents.location_id = locations.id AND incidents.deleted_at IS NULL").
			Joins("LEFT JOIN classifications class ON class.id = incidents.classification_id")
		if needsStatusJoin(filters) {
			q = q.Joins("LEFT JOIN workflow_states ON workflow_states.id = incidents.current_state_id")
		}
		return r.applyFilters(ctx, q, filters)
	}
	var total int64
	if err := buildBase().Select("COUNT(DISTINCT locations.id)").Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	orderClause := "COUNT(incidents.id) DESC"
	if sorting != nil && sorting.Field != "" {
		if col, ok := locationCountFilterFields[sorting.Field]; ok {
			dir := "ASC"
			if strings.EqualFold(sorting.Direction, "desc") {
				dir = "DESC"
			}
			orderClause = col + " " + dir
		}
	}
	offset := (page - 1) * limit
	rows, err := buildBase().Debug().
		Select("locations.id::text AS location_id, locations.name AS location_name, COALESCE(parent_loc.name, '') AS parent_location_name, COUNT(incidents.id) AS incident_count").
		Group("locations.id, locations.name, parent_loc.name").
		Order(orderClause).Offset(offset).Limit(limit).Rows()
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	type locCountRow struct {
		id, name, parent string
		count            int64
	}
	var raw []locCountRow
	var allIDs []string
	for rows.Next() {
		var r locCountRow
		if err := rows.Scan(&r.id, &r.name, &r.parent, &r.count); err != nil {
			continue
		}
		raw = append(raw, r)
		allIDs = append(allIDs, r.id)
	}

	pathMap, _ := r.fetchLocationPaths(ctx, allIDs)
	defaults := map[string]string{"location_name": "Location", "parent_location_name": "Parent Location", "incident_count": "No. of Incidents"}
	var results []map[string]interface{}
	for _, loc := range raw {
		displayName := pathMap[loc.id]
		if displayName == "" {
			displayName = loc.name
		}
		results = append(results, buildCountRow(map[string]interface{}{"location_name": displayName, "parent_location_name": loc.parent, "incident_count": loc.count}, reqColumns, defaults))
	}
	return results, total, nil
}

// ── Location count by status ──────────────────────────────────────────────────
// Output col.Field names: location_name | parent_location_name | classification_name | status_name | incident_count
func (r *reportRepository) ExecuteLocationCountByStatusQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error) {
	reqColumns, _ := ctx.Value(constants.ContextKeys.REPORT_COLUMNS).([]models.ColumnField)
	// Step 0: Build full path map for all locations using recursive CTE
	type pathRow struct {
		ID       string
		FullPath string
	}
	var pathRows []pathRow
	err := r.db.WithContext(ctx).Raw(`
    WITH RECURSIVE location_tree AS (
        SELECT 
            id,
            parent_id,
            name::text AS full_path
        FROM locations
        WHERE deleted_at IS NULL AND parent_id IS NULL

        UNION ALL

        SELECT 
            l.id,
            l.parent_id,
            lt.full_path || ' > ' || l.name
        FROM locations l
        INNER JOIN location_tree lt ON lt.id = l.parent_id
        WHERE l.deleted_at IS NULL
    )
    SELECT id, full_path FROM location_tree
`).Scan(&pathRows).Error
	if err != nil {
		return nil, 0, err
	}

	// Build lookup map: location_id → full_path
	fullPathMap := make(map[string]string, len(pathRows))
	for _, p := range pathRows {
		fullPathMap[p.ID] = p.FullPath
	}

	buildBase := func() *gorm.DB {
		q := r.db.WithContext(ctx).Debug().
			Table("locations").
			Joins("LEFT JOIN locations parent_loc ON parent_loc.id = locations.parent_id").
			Joins("LEFT JOIN incidents ON incidents.location_id = locations.id AND incidents.deleted_at IS NULL").
			Joins("LEFT JOIN classifications class ON class.id = incidents.classification_id").
			Joins("INNER JOIN workflow_states ON workflow_states.id = incidents.current_state_id").
			Joins("INNER JOIN workflows ON workflows.id = workflow_states.workflow_id AND workflows.record_type = 'incident' AND workflows.deleted_at IS NULL")
		return r.applyFilters(ctx, q, filters)
	}

	buildBaseForPaging := func() *gorm.DB {
		q := r.db.WithContext(ctx).Debug().
			Table("locations").
			Joins("LEFT JOIN locations parent_loc ON parent_loc.id = locations.parent_id").
			Joins("LEFT JOIN incidents ON incidents.location_id = locations.id AND incidents.deleted_at IS NULL").
			Joins("LEFT JOIN classifications class ON class.id = incidents.classification_id").
			Joins("INNER JOIN workflow_states ON workflow_states.id = incidents.current_state_id").
			Joins("INNER JOIN workflows ON workflows.id = workflow_states.workflow_id AND workflows.record_type = 'incident' AND workflows.deleted_at IS NULL")
		return r.applyFilters(ctx, q, filters)
	}

	// Step 1: Get distinct location IDs with correct pagination
	// Build ordered list of location IDs for this page first
	orderClause := "locations.name ASC"
	if sorting != nil && sorting.Field != "" {
		if col, ok := locationCountByStatusFilterFields[sorting.Field]; ok {
			dir := "ASC"
			if strings.EqualFold(sorting.Direction, "desc") {
				dir = "DESC"
			}
			orderClause = col + " " + dir
		}
	}

	// Step 2: Get total distinct location count
	var total int64
	if err := buildBaseForPaging().
		Select("COUNT(DISTINCT locations.id)").
		Scan(&total).Error; err != nil {
		return nil, 0, err
	}

	// Step 3: Get paginated location IDs in correct order
	offset := (page - 1) * limit
	type locRow struct {
		LocationID   string
		LocationName string
		ParentName   string
	}
	var pagedLocs []locRow
	if err := buildBaseForPaging().
		Select("DISTINCT locations.id AS location_id, locations.name AS location_name, COALESCE(parent_loc.name, '') AS parent_name").
		Group("locations.id, locations.name, parent_loc.name").
		Order(orderClause).
		Offset(offset).
		Limit(limit).
		Scan(&pagedLocs).Error; err != nil {
		return nil, 0, err
	}

	if len(pagedLocs) == 0 {
		return []map[string]interface{}{}, total, nil
	}

	// Step 4: Extract location IDs for filtering status query
	// Step 4: Use ID as key, not name
	locIDs := make([]string, len(pagedLocs))
	locOrder := make([]string, len(pagedLocs)) // ordered list of IDs
	locMeta := map[string]locRow{}             // keyed by ID

	for i, loc := range pagedLocs {
		locIDs[i] = loc.LocationID
		locOrder[i] = loc.LocationID  // ✅ ID, not name
		locMeta[loc.LocationID] = loc // ✅ ID, not name
	}

	// Step 6: Fetch status breakdown only for this page's locations
	rows, err := buildBase().
		Select(`locations.id::text AS location_id, COALESCE(workflow_states.code, '') AS status_name, COUNT(incidents.id) AS incident_count`).
		Where("locations.id IN ?", locIDs).
		Group("locations.id, workflow_states.code").
		Rows()
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	// Step 7: Accumulate pivot data keyed by location ID
	stateCount := map[string]map[string]int64{}
	locationTotal := map[string]int64{}
	allStatuses := map[string]struct{}{}

	for rows.Next() {
		var locationID, statusName string
		var count int64
		if err := rows.Scan(&locationID, &statusName, &count); err != nil {
			continue
		}
		allStatuses[statusName] = struct{}{}
		locationTotal[locationID] += count

		if stateCount[locationID] == nil {
			stateCount[locationID] = map[string]int64{}
		}
		stateCount[locationID][statusName] += count
	}

	// Step 8: Sort statuses for consistent column ordering
	sortedStatuses := make([]string, 0, len(allStatuses))
	for s := range allStatuses {
		sortedStatuses = append(sortedStatuses, s)
	}
	sort.Strings(sortedStatuses)

	defaults := map[string]string{
		"location_name":        "Location",
		"parent_location_name": "Parent Location",
		"status_name":          "Status",
		"incident_count":       "No. of Incidents",
		"total":                "Total",
		"percentage":           "Percentage",
	}

	pathMap, _ := r.fetchLocationPaths(ctx, locIDs)

	// Step 9: Build results in correct paginated order
	var results []map[string]interface{}
	for _, locationID := range locOrder {
		meta := locMeta[locationID]
		locTotal := locationTotal[locationID]
		statuses := stateCount[locationID]

		displayName := pathMap[locationID]
		if displayName == "" {
			displayName = meta.LocationName
		}

		row := map[string]interface{}{
			"location_name":        displayName,
			"parent_location_name": meta.ParentName,
		}

		for _, statusName := range sortedStatuses {
			row[statusName] = statuses[statusName]
		}

		closedCount := statuses["closed"]
		var percentage float64
		if locTotal > 0 && closedCount > 0 {
			percentage = math.Round((float64(closedCount)/float64(locTotal))*10000) / 100
		}
		row["total"] = locTotal
		row["incident_count"] = locTotal
		row["percentage"] = fmt.Sprintf("%.2f%%", percentage)

		results = append(results, buildCountRow(row, reqColumns, defaults))
	}
	return results, total, nil
}

// ── Classification count (without status) ────────────────────────────────────
// Output col.Field names: classification_name | parent_classification_name | location_name | incident_count
func (r *reportRepository) ExecuteClassificationCountQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error) {
	reqColumns, _ := ctx.Value(constants.ContextKeys.REPORT_COLUMNS).([]models.ColumnField)
	buildBase := func() *gorm.DB {
		q := r.db.WithContext(ctx).
			Table("classifications").
			Joins("LEFT JOIN classifications parent_cls ON parent_cls.id = classifications.parent_id").
			Joins("LEFT JOIN incidents ON incidents.classification_id = classifications.id AND incidents.deleted_at IS NULL").
			Joins("LEFT JOIN locations loc ON loc.id = incidents.location_id")
		if needsStatusJoin(filters) {
			q = q.Joins("LEFT JOIN workflow_states ON workflow_states.id = incidents.current_state_id")
		}
		return r.applyFilters(ctx, q, filters)
	}
	var total int64
	if err := buildBase().Select("COUNT(DISTINCT classifications.id)").Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	orderClause := "COUNT(incidents.id) DESC"
	if sorting != nil && sorting.Field != "" {
		if col, ok := classificationCountFilterFields[sorting.Field]; ok {
			dir := "ASC"
			if strings.EqualFold(sorting.Direction, "desc") {
				dir = "DESC"
			}
			orderClause = col + " " + dir
		}
	}
	offset := (page - 1) * limit
	rows, err := buildBase().
		Select("classifications.id::text AS classification_id, classifications.name AS classification_name, COALESCE(parent_cls.name, '') AS parent_classification_name, COUNT(incidents.id) AS incident_count").
		Group("classifications.id, classifications.name, parent_cls.name").
		Order(orderClause).Offset(offset).Limit(limit).Rows()
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	type clsCountRow struct {
		id, name, parent string
		count            int64
	}
	var raw []clsCountRow
	var allIDs []string
	for rows.Next() {
		var c clsCountRow
		if err := rows.Scan(&c.id, &c.name, &c.parent, &c.count); err != nil {
			continue
		}
		raw = append(raw, c)
		allIDs = append(allIDs, c.id)
	}

	pathMap, _ := r.fetchClassificationPaths(ctx, allIDs)
	defaults := map[string]string{"classification_name": "Classification", "parent_classification_name": "Parent Classification", "incident_count": "No. of Incidents"}
	var results []map[string]interface{}
	for _, cls := range raw {
		displayName := pathMap[cls.id]
		if displayName == "" {
			displayName = cls.name
		}
		results = append(results, buildCountRow(map[string]interface{}{"classification_name": displayName, "parent_classification_name": cls.parent, "incident_count": cls.count}, reqColumns, defaults))
	}
	return results, total, nil
}

// ── Classification count by status ───────────────────────────────────────────
// Output col.Field names: classification_name | parent_classification_name | location_name | status_name | incident_count
func (r *reportRepository) ExecuteClassificationCountByStatusQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error) {
	reqColumns, _ := ctx.Value(constants.ContextKeys.REPORT_COLUMNS).([]models.ColumnField)

	// buildBase includes workflow_states for the status breakdown query
	buildBase := func() *gorm.DB {
		q := r.db.WithContext(ctx).Debug().
			Table("classifications").
			Joins("LEFT JOIN classifications parent_cls ON parent_cls.id = classifications.parent_id").
			Joins("LEFT JOIN incidents ON incidents.classification_id = classifications.id AND incidents.deleted_at IS NULL").
			Joins("LEFT JOIN locations loc ON loc.id = incidents.location_id").
			Joins("INNER JOIN workflow_states ON workflow_states.id = incidents.current_state_id").
			Joins("INNER JOIN workflows ON workflows.id = workflow_states.workflow_id AND workflows.record_type = 'incident' AND workflows.deleted_at IS NULL")
		return r.applyFilters(ctx, q, filters)
	}
	buildBaseForPaging := func() *gorm.DB {
		q := r.db.WithContext(ctx).Debug().
			Table("classifications").
			Joins("LEFT JOIN classifications parent_cls ON parent_cls.id = classifications.parent_id").
			Joins("LEFT JOIN incidents ON incidents.classification_id = classifications.id AND incidents.deleted_at IS NULL").
			Joins("LEFT JOIN locations loc ON loc.id = incidents.location_id").
			Joins("INNER JOIN workflow_states ON workflow_states.id = incidents.current_state_id").
			Joins("INNER JOIN workflows ON workflows.id = workflow_states.workflow_id AND workflows.record_type = 'incident' AND workflows.deleted_at IS NULL")
		return r.applyFilters(ctx, q, filters)
	}

	var total int64
	if err := buildBaseForPaging().Select("COUNT(DISTINCT classifications.id)").Scan(&total).Error; err != nil {
		return nil, 0, err
	}

	orderClause := "classifications.name ASC"
	if sorting != nil && sorting.Field != "" {
		if col, ok := classificationCountByStatusFilterFields[sorting.Field]; ok {
			dir := "ASC"
			if strings.EqualFold(sorting.Direction, "desc") {
				dir = "DESC"
			}
			orderClause = col + " " + dir
		}
	}

	// Phase C: paginate classification IDs
	offset := (page - 1) * limit
	type clsRow struct {
		ClassificationID   string
		ClassificationName string
		ParentName         string
	}
	var pagedCls []clsRow
	if err := buildBaseForPaging().
		Select("classifications.id::text AS classification_id, classifications.name AS classification_name, COALESCE(parent_cls.name, '') AS parent_name").
		Group("classifications.id, classifications.name, parent_cls.name").
		Order(orderClause).Offset(offset).Limit(limit).
		Scan(&pagedCls).Error; err != nil {
		return nil, 0, err
	}
	if len(pagedCls) == 0 {
		return []map[string]interface{}{}, total, nil
	}

	clsIDs := make([]string, len(pagedCls))
	clsOrder := make([]string, len(pagedCls))
	clsMeta := map[string]clsRow{}
	for i, c := range pagedCls {
		clsIDs[i] = c.ClassificationID
		clsOrder[i] = c.ClassificationID
		clsMeta[c.ClassificationID] = c
	}

	// Phase E: status breakdown for this page's classification IDs only
	rows, err := buildBase().
		Select(`classifications.id::text AS classification_id, COALESCE(workflow_states.code, '') AS status_name, COUNT(incidents.id) AS incident_count`).
		Where("classifications.id::text IN ?", clsIDs).
		Group("classifications.id, workflow_states.code").
		Rows()
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	stateCount := map[string]map[string]int64{}
	classificationTotal := map[string]int64{}
	allStatuses := map[string]struct{}{}

	for rows.Next() {
		var classificationID, statusName string
		var count int64
		if err := rows.Scan(&classificationID, &statusName, &count); err != nil {
			continue
		}

		if statusName == "" {
			statusName = "X"
			count = 1
		}
		allStatuses[statusName] = struct{}{}
		classificationTotal[classificationID] += count
		if stateCount[classificationID] == nil {
			stateCount[classificationID] = map[string]int64{}
		}
		stateCount[classificationID][statusName] += count
	}

	sortedStatuses := make([]string, 0, len(allStatuses))
	for s := range allStatuses {
		sortedStatuses = append(sortedStatuses, s)
	}
	sort.Strings(sortedStatuses)

	// Phase F: build results using full classification path
	pathMap, _ := r.fetchClassificationPaths(ctx, clsIDs)
	defaults := map[string]string{
		"classification_name":        "Classification",
		"parent_classification_name": "Parent Classification",
		"status_name":                "Status",
		"incident_count":             "No. of Incidents",
		"total":                      "Total",
		"percentage":                 "Percentage",
	}
	var results []map[string]interface{}
	for _, clsID := range clsOrder {
		meta := clsMeta[clsID]
		clsTotal := classificationTotal[clsID]
		statuses := stateCount[clsID]

		displayName := pathMap[clsID]
		if displayName == "" {
			displayName = meta.ClassificationName
		}

		row := map[string]interface{}{
			"classification_name":        displayName,
			"parent_classification_name": meta.ParentName,
		}
		for _, statusName := range sortedStatuses {
			row[statusName] = statuses[statusName]
		}

		closedCount := statuses["closed"]
		var percentage float64
		if clsTotal > 0 && closedCount > 0 {
			percentage = math.Round((float64(closedCount)/float64(clsTotal))*10000) / 100
		}
		row["incident_count"] = clsTotal
		row["total"] = clsTotal
		row["percentage"] = fmt.Sprintf("%.2f%%", percentage)
		results = append(results, buildCountRow(row, reqColumns, defaults))
	}

	return results, total, nil
}

// ── Department count (without status) ────────────────────────────────────────
// Output col.Field names: department_name | parent_department_name | incident_count
func (r *reportRepository) ExecuteDepartmentCountQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error) {
	reqColumns, _ := ctx.Value(constants.ContextKeys.REPORT_COLUMNS).([]models.ColumnField)
	buildBase := func() *gorm.DB {
		q := r.db.WithContext(ctx).
			Table("departments").
			Joins("LEFT JOIN departments parent_dept ON parent_dept.id = departments.parent_id").
			Joins("LEFT JOIN incidents ON incidents.department_id = departments.id AND incidents.deleted_at IS NULL")
		if needsStatusJoin(filters) {
			q = q.Joins("LEFT JOIN workflow_states ON workflow_states.id = incidents.current_state_id")
		}
		return r.applyFilters(ctx, q, filters)
	}
	var total int64
	if err := buildBase().Select("COUNT(DISTINCT departments.id)").Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	orderClause := "COUNT(incidents.id) DESC"
	if sorting != nil && sorting.Field != "" {
		if col, ok := departmentCountFilterFields[sorting.Field]; ok {
			dir := "ASC"
			if strings.EqualFold(sorting.Direction, "desc") {
				dir = "DESC"
			}
			orderClause = col + " " + dir
		}
	}
	offset := (page - 1) * limit
	rows, err := buildBase().
		Select("departments.name AS department_name, COALESCE(parent_dept.name, '') AS parent_department_name, COUNT(incidents.id) AS incident_count").
		Group("departments.id, departments.name, parent_dept.name").
		Order(orderClause).Offset(offset).Limit(limit).Rows()
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	defaults := map[string]string{"department_name": "Department", "parent_department_name": "Parent Department", "incident_count": "No. of Incidents"}
	var results []map[string]interface{}
	for rows.Next() {
		var departmentName, parentName string
		var count int64
		if err := rows.Scan(&departmentName, &parentName, &count); err != nil {
			continue
		}
		results = append(results, buildCountRow(map[string]interface{}{"department_name": departmentName, "parent_department_name": parentName, "incident_count": count}, reqColumns, defaults))
	}
	return results, total, nil
}

// ── Department count by status ────────────────────────────────────────────────
// Output col.Field names: department_name | parent_department_name | status_name | incident_count
func (r *reportRepository) ExecuteDepartmentCountByStatusQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error) {
	reqColumns, _ := ctx.Value(constants.ContextKeys.REPORT_COLUMNS).([]models.ColumnField)
	buildBase := func() *gorm.DB {
		q := r.db.WithContext(ctx).
			Table("departments").
			Joins("LEFT JOIN departments parent_dept ON parent_dept.id = departments.parent_id").
			Joins("LEFT JOIN incidents ON incidents.department_id = departments.id AND incidents.deleted_at IS NULL").
			Joins("LEFT JOIN workflow_states ON workflow_states.id = incidents.current_state_id")
		return r.applyFilters(ctx, q, filters)
	}
	var total int64
	if err := buildBase().Select("COUNT(DISTINCT departments.id::text || '|' || COALESCE(workflow_states.name, ''))").Scan(&total).Error; err != nil {
		return nil, 0, err
	}
	orderClause := "departments.name ASC, COUNT(incidents.id) DESC"
	if sorting != nil && sorting.Field != "" {
		if col, ok := departmentCountByStatusFilterFields[sorting.Field]; ok {
			dir := "ASC"
			if strings.EqualFold(sorting.Direction, "desc") {
				dir = "DESC"
			}
			orderClause = col + " " + dir
		}
	}
	offset := (page - 1) * limit
	rows, err := buildBase().
		Select("departments.name AS department_name, COALESCE(parent_dept.name, '') AS parent_department_name, COALESCE(workflow_states.name, '') AS status_name, COUNT(incidents.id) AS incident_count").
		Group("departments.id, departments.name, parent_dept.name, workflow_states.name").
		Order(orderClause).Offset(offset).Limit(limit).Rows()
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	defaults := map[string]string{"department_name": "Department", "parent_department_name": "Parent Department", "status_name": "Status", "incident_count": "No. of Incidents"}
	var results []map[string]interface{}
	for rows.Next() {
		var departmentName, parentName, statusName string
		var count int64
		if err := rows.Scan(&departmentName, &parentName, &statusName, &count); err != nil {
			continue
		}
		results = append(results, buildCountRow(map[string]interface{}{"department_name": departmentName, "parent_department_name": parentName, "status_name": statusName, "incident_count": count}, reqColumns, defaults))
	}
	return results, total, nil
}
