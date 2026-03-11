package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/pkg/constants"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

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
	GetTransitionUserNames(ctx context.Context, newStateName string, incidentIDs []string) (map[string]string, error)
	ExecuteUserQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error)
	ExecuteWorkflowQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error)
	ExecuteDepartmentQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error)
	ExecuteLocationQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error)
	ExecuteClassificationQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error)
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

func (r *reportRepository) applyFilters(query *gorm.DB, filters []models.ReportFilterConfig) *gorm.DB {
	for _, f := range filters {
		switch f.Operator {
		case "equals":
			query = query.Where(f.Field+" = ?", f.Value)
		case "not_equals":
			query = query.Where(f.Field+" != ?", f.Value)
		case "contains":
			query = query.Where(f.Field+" ILIKE ?", "%"+f.Value.(string)+"%")
		case "starts_with":
			query = query.Where(f.Field+" ILIKE ?", f.Value.(string)+"%")
		case "ends_with":
			query = query.Where(f.Field+" ILIKE ?", "%"+f.Value.(string))
		case "gt":
			query = query.Where(f.Field+" > ?", f.Value)
		case "lt":
			query = query.Where(f.Field+" < ?", f.Value)
		case "gte":
			query = query.Where(f.Field+" >= ?", f.Value)
		case "lte":
			query = query.Where(f.Field+" <= ?", f.Value)
		case "in":
			query = query.Where(f.Field+" IN ?", f.Value)
		case "is_null":
			query = query.Where(f.Field + " IS NULL")
		case "is_not_null":
			query = query.Where(f.Field + " IS NOT NULL")
		case "between":
			if arr, ok := f.Value.([]interface{}); ok && len(arr) == 2 {
				query = query.Where(f.Field+" BETWEEN ? AND ?", arr[0], arr[1])
			}
		}
	}
	return query
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
	query = r.applyFilters(query, filters)

	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	query = r.applySorting(query, sorting)
	if sorting == nil {
		query = query.Order("incidents.created_at DESC")
	}

	offset := (page - 1) * limit
	rows, err := query.
		Select(
			"incidents.incident_number, " +
				"incidents.created_by_mobile, " +
				"reporters.first_name as reporter_first_name, reporters.last_name as reporter_last_name, " +
				"classifications.name as classification_name, " +
				"locations.name as location_name, " +
				"incidents.title, " +
				"incidents.created_at").
		Joins("LEFT JOIN users as reporters ON incidents.reporter_id = reporters.id").
		Joins("LEFT JOIN classifications ON incidents.classification_id = classifications.id").
		Joins("LEFT JOIN locations ON incidents.location_id = locations.id").
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
	query := r.db.WithContext(ctx).Model(&models.Incident{})
	query = r.applyFilters(query, filters)
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	query = r.applySorting(query, sorting)
	if sorting == nil {
		query = query.Order("incidents.created_at DESC")
	}
	offset := (page - 1) * limit
	rows, err := query.
		Select("incidents.*, " +
			"reporters.email as reporter_email, reporters.first_name as reporter_first_name, reporters.last_name as reporter_last_name, " +
			"reporters.username as reporter_username, " +
			"assignees.email as assignee_email, assignees.first_name as assignee_first_name, assignees.last_name as assignee_last_name, " +
			"assignees.username as assignee_username, " +
			"workflow_states.name as current_state_name, workflow_states.state_type as current_state_state_type, " +
			"classifications.name as classification_name, " +
			"departments.name as department_name, " +
			"locations.name as location_name, " +
			"incident_attachments.id as attachment_id, " +
			"workflows.name as workflow_name").
		Joins("LEFT JOIN users as reporters ON incidents.reporter_id = reporters.id").
		Joins("LEFT JOIN users as assignees ON incidents.assignee_id = assignees.id").
		Joins("LEFT JOIN workflow_states ON incidents.current_state_id = workflow_states.id").
		Joins("LEFT JOIN classifications ON incidents.classification_id = classifications.id").
		Joins("LEFT JOIN departments ON incidents.department_id = departments.id").
		Joins("LEFT JOIN locations ON incidents.location_id = locations.id").
		Joins("LEFT JOIN workflows ON incidents.workflow_id = workflows.id").
		Joins("LEFT JOIN incident_attachments ON incidents.id = incident_attachments.incident_id").
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
		for _, row := range results {
			if id := incidentIDStr(row["id"]); id != "" {
				incidentIDs = append(incidentIDs, id)
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

		// 1. Per-status transition data – only fetched when the matching
		//    By/Date column is actually requested.
		var rejectedNames, rejectedDates map[string]string
		if hasCol("rejected_by", "rejected_date") {
			rejectedNames, rejectedDates, _ = r.fetchRejectedData(ctx, incidentIDs)
		}

		var underResNames, underResDates map[string]string
		if hasCol("under_resolution_by", "under_resolution_date") {
			underResNames, underResDates, _ = r.fetchUnderResolutionData(ctx, incidentIDs)
		}

		var inProgressNames, inProgressDates map[string]string
		if hasCol("in_progress_by", "in_progress_date") {
			inProgressNames, inProgressDates, _ = r.fetchInProgressData(ctx, incidentIDs)
		}

		var readyToCloseNames, readyToCloseDates map[string]string
		if hasCol("ready_to_close_by", "ready_to_close_date") {
			readyToCloseNames, readyToCloseDates, _ = r.fetchReadyToCloseData(ctx, incidentIDs)
		}

		var closedNames, closedDates map[string]string
		if hasCol("closed_by", "closed_date", "contractor", "contractor_user", "solved_by") {
			closedNames, closedDates, _ = r.fetchClosedData(ctx, incidentIDs)
		}

		// 2. Reopened By + Reopen Date
		var reopenedByNames, reopenDates map[string]string
		if hasCol("reopened_by", "reopen_date") {
			reopenedByNames, reopenDates, _ = r.fetchReopenData(ctx, incidentIDs)
		}

		// 3. Escalation Date
		var escalationDates map[string]string
		if hasCol("escalation_date") {
			escalationDates, _ = r.fetchEscalationDates(ctx, incidentIDs)
		}

		// 4. General comments
		var commentsMap map[string][]map[string]interface{}
		if hasCol("comments", "comments_array") {
			commentsMap, _ = r.fetchGeneralComments(ctx, incidentIDs)
		}

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

		for i, row := range results {
			incidentID := incidentIDStr(row["id"])
			if incidentID == "" {
				continue
			}

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

			// Rejected
			if name := rejectedNames[incidentID]; name != "" {
				setField(results[i], "rejected_by", name)
			}
			if date := rejectedDates[incidentID]; date != "" {
				setField(results[i], "rejected_date", date)
			}

			// Under Resolution
			if name := underResNames[incidentID]; name != "" {
				setField(results[i], "under_resolution_by", name)
				setField(results[i], "approved_by", name)
			}
			if date := underResDates[incidentID]; date != "" {
				setField(results[i], "under_resolution_date", date)
				setField(results[i], "approved_time", date)
				setField(results[i], "approved_at", date)
			}

			// In Progress
			if name := inProgressNames[incidentID]; name != "" {
				setField(results[i], "in_progress_by", name)
			}
			if date := inProgressDates[incidentID]; date != "" {
				setField(results[i], "in_progress_date", date)
			}

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
			if comments, ok := commentsMap[incidentID]; ok {
				parts := make([]string, 0, len(comments))
				for _, c := range comments {
					parts = append(parts, fmt.Sprintf("%s", c["content"]))
				}
				setField(results[i], "comments", strings.Join(parts, " | "))
				setField(results[i], "comments_array", comments)
			}

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

			_ = row
		}
	}

	return results, total, nil
}

// GetTransitionUserNames returns incident_id → performer full name for the
// most-recent status_changed revision where new_value = newStateName.
// Delegates to fetchStatusTransitionData (names only).
func (r *reportRepository) GetTransitionUserNames(ctx context.Context, newStateName string, incidentIDs []string) (map[string]string, error) {
	names, _, err := r.fetchStatusTransitionData(ctx, models.IncidentRevisionStatus(newStateName), incidentIDs)
	return names, err
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
	status models.IncidentRevisionStatus,
	incidentIDs []string,
) (names map[string]string, dates map[string]string, err error) {
	names = map[string]string{}
	dates = map[string]string{}
	if len(incidentIDs) == 0 {
		return
	}

	query := `
		SELECT DISTINCT ON (ir.incident_id)
			ir.incident_id::text,
			TRIM(COALESCE(u.first_name,'') || ' ' || COALESCE(u.last_name,'')) AS full_name,
			ir.created_at::text AS transition_date
		FROM incident_revisions ir
		CROSS JOIN jsonb_array_elements(ir.changes::jsonb) AS chg
		LEFT JOIN users u ON ir.performed_by_id = u.id
		WHERE ir.action_type = 'status_changed'
		  AND chg->>'field_name' = 'current_state_id'
		  AND chg->>'new_value' = ?
		  AND ir.incident_id IN (?)
		ORDER BY ir.incident_id, ir.created_at DESC`

	rows, qerr := r.db.WithContext(ctx).Raw(query, string(status), incidentIDs).Rows()
	if qerr != nil {
		err = fmt.Errorf("fetchStatusTransitionData(%s) query failed: %w", status, qerr)
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

// ── Per-status wrappers (use IncidentRevisionStatus constants) ────────────────

func (r *reportRepository) fetchRejectedData(ctx context.Context, incidentIDs []string) (map[string]string, map[string]string, error) {
	return r.fetchStatusTransitionData(ctx, models.RevisionStatusRejected, incidentIDs)
}

func (r *reportRepository) fetchUnderResolutionData(ctx context.Context, incidentIDs []string) (map[string]string, map[string]string, error) {
	return r.fetchStatusTransitionData(ctx, models.RevisionStatusUnderResolution, incidentIDs)
}

func (r *reportRepository) fetchInProgressData(ctx context.Context, incidentIDs []string) (map[string]string, map[string]string, error) {
	return r.fetchStatusTransitionData(ctx, models.RevisionStatusInProgress, incidentIDs)
}

func (r *reportRepository) fetchReadyToCloseData(ctx context.Context, incidentIDs []string) (map[string]string, map[string]string, error) {
	return r.fetchStatusTransitionData(ctx, models.RevisionStatusReadyToClose, incidentIDs)
}

func (r *reportRepository) fetchClosedData(ctx context.Context, incidentIDs []string) (map[string]string, map[string]string, error) {
	return r.fetchStatusTransitionData(ctx, models.RevisionStatusClosed, incidentIDs)
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
		WHERE ic.incident_id IN (?)
		  AND ic.deleted_at IS NULL
		  AND ic.transition_history_id IS NOT NULL
		ORDER BY ic.incident_id, ic.created_at DESC`

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
		WHERE ia.incident_id IN (?)
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
func (r *reportRepository) fetchReopenData(ctx context.Context, incidentIDs []string) (names map[string]string, dates map[string]string, err error) {
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

// incidentIDStr extracts an incident UUID as a lowercase hyphenated string from
// whatever type the pgx/GORM driver returns (string, []byte, or [16]byte).
func incidentIDStr(v interface{}) string {
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
	query = r.applyFilters(query, filters)

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

func (r *reportRepository) ExecuteWorkflowQuery(ctx context.Context, filters []models.ReportFilterConfig, sorting *models.ReportSortConfig, page, limit int) ([]map[string]interface{}, int64, error) {
	var total int64
	var results []map[string]interface{}

	reqColumns, _ := ctx.Value(constants.ContextKeys.REPORT_COLUMNS).([]models.ColumnField)

	query := r.db.WithContext(ctx).Model(&models.Workflow{})
	query = r.applyFilters(query, filters)

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
	query = r.applyFilters(query, filters)

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
	query = r.applyFilters(query, filters)

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
	query = r.applyFilters(query, filters)

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
