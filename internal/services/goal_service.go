package services

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/services/metricformula"
	"github.com/automax/backend/internal/storage"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
	"gorm.io/gorm"
)

// ════════════════════════════════════════════════════
// GoalService Interface
// ════════════════════════════════════════════════════

type GoalService interface {
	// Goal CRUD
	CreateGoal(ctx context.Context, req *models.GoalCreateRequest, userID uuid.UUID) (*models.GoalResponse, error)
	GetGoal(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*models.GoalResponse, error)
	ListGoals(ctx context.Context, filter *models.GoalFilter) ([]models.GoalResponse, int64, error)
	UpdateGoal(ctx context.Context, id uuid.UUID, req *models.GoalUpdateRequest, userID uuid.UUID) (*models.GoalResponse, error)
	DeleteGoal(ctx context.Context, id uuid.UUID, userID uuid.UUID) error

	// Status Transitions
	TransitionGoalStatus(ctx context.Context, id uuid.UUID, newStatus string, userID uuid.UUID) (*models.GoalResponse, error)

	// Collaborators
	AddCollaborator(ctx context.Context, goalID uuid.UUID, req *models.CollaboratorAddRequest, userID uuid.UUID) error
	RemoveCollaborator(ctx context.Context, goalID uuid.UUID, collaboratorUserID uuid.UUID, userID uuid.UUID) error

	// Metrics
	CreateMetric(ctx context.Context, goalID uuid.UUID, req *models.GoalMetricCreateRequest, userID uuid.UUID) (*models.GoalMetricResponse, error)
	UpdateMetric(ctx context.Context, id uuid.UUID, req *models.GoalMetricUpdateRequest, userID uuid.UUID) (*models.GoalMetricResponse, error)
	DeleteMetric(ctx context.Context, id uuid.UUID, userID uuid.UUID) error
	UpdateMetricValue(ctx context.Context, id uuid.UUID, req *models.MetricValueUpdateRequest, userID uuid.UUID) (*models.GoalMetricValueChangeResponse, error)
	GetMetricHistory(ctx context.Context, metricID uuid.UUID, page int, limit int, userID uuid.UUID) ([]models.MetricHistoryResponse, int64, error)
	ListMetricValueChanges(ctx context.Context, metricID uuid.UUID, userID uuid.UUID) ([]models.GoalMetricValueChangeResponse, error)

	// Metric Workflow Transitions
	GetAvailableMetricTransitions(ctx context.Context, metricID uuid.UUID, userID uuid.UUID) ([]models.AvailableTransitionResponse, error)
	TransitionMetric(ctx context.Context, metricID uuid.UUID, req *models.MetricTransitionRequest, userID uuid.UUID) (*models.GoalMetricResponse, error)
	GetAvailableMetricValueChangeTransitions(ctx context.Context, changeID uuid.UUID, userID uuid.UUID) ([]models.AvailableTransitionResponse, error)
	TransitionMetricValueChange(ctx context.Context, changeID uuid.UUID, req *models.MetricTransitionRequest, userID uuid.UUID) (*models.GoalMetricValueChangeResponse, error)
	ListPendingMetricApprovals(ctx context.Context, userID uuid.UUID, page int, limit int) ([]models.MetricApprovalListResponse, int64, error)
	ListPendingMetricValueChangeApprovals(ctx context.Context, userID uuid.UUID, page int, limit int) ([]models.MetricApprovalListResponse, int64, error)

	// Evidence
	CreateEvidence(ctx context.Context, goalID uuid.UUID, title string, evidenceType string, comment string, metricID *uuid.UUID, fileName string, fileSize int64, mimeType string, fileData []byte, userID uuid.UUID) (*models.EvidenceResponse, error)
	DeleteEvidence(ctx context.Context, id uuid.UUID, userID uuid.UUID) error
	ListEvidences(ctx context.Context, goalID uuid.UUID, filter *models.EvidenceFilter, userID uuid.UUID) ([]models.EvidenceResponse, int64, error)
	GetEvidence(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*models.EvidenceResponse, error)
	GetEvidencePreview(ctx context.Context, evidenceID uuid.UUID, userID uuid.UUID) (string, error)
	GetEvidenceDownloadURL(ctx context.Context, evidenceID uuid.UUID, userID uuid.UUID) (string, error)
	DownloadEvidenceFile(ctx context.Context, evidenceID uuid.UUID, userID uuid.UUID) (io.ReadCloser, *storage.DmsFile, string, string, error)
	ReplaceEvidenceFile(ctx context.Context, evidenceID uuid.UUID, fileName string, fileSize int64, mimeType string, fileData []byte, userID uuid.UUID) (*models.EvidenceResponse, error)

	// Evidence Workflow Transitions
	GetAvailableEvidenceTransitions(ctx context.Context, evidenceID uuid.UUID, userID uuid.UUID) ([]models.AvailableTransitionResponse, error)
	ExecuteEvidenceTransition(ctx context.Context, evidenceID uuid.UUID, req *models.EvidenceTransitionRequest, userID uuid.UUID) (*models.EvidenceResponse, error)
	GetEvidenceTransitionHistory(ctx context.Context, evidenceID uuid.UUID, userID uuid.UUID) ([]models.EvidenceTransitionHistoryResponse, error)
	ListPendingApprovals(ctx context.Context, userID uuid.UUID, page int, limit int) ([]models.ApprovalListResponse, int64, error)
	ListCompletedApprovals(ctx context.Context, userID uuid.UUID, page int, limit int) ([]models.ApprovalListResponse, int64, error)

	// Export
	ExportGoalsCSV(ctx context.Context, filter *models.GoalFilter) ([]byte, error)
	ExportGoalsJSON(ctx context.Context, filter *models.GoalFilter) ([]models.GoalResponse, error)

	// Clone
	CloneGoal(ctx context.Context, id uuid.UUID, req *models.GoalCloneRequest, userID uuid.UUID) (*models.GoalResponse, error)

	// Import
	ImportGoals(ctx context.Context, fileData []byte, fileName string, dryRun bool, userID uuid.UUID) (*models.GoalImportResponse, error)

	// Bulk Operations
	BulkAction(ctx context.Context, req *models.BulkActionRequest, userID uuid.UUID) (*models.BulkActionResponse, error)

	// Comments
	AddComment(ctx context.Context, goalID uuid.UUID, content string, userID uuid.UUID) (*models.GoalCommentResponse, error)
	ListComments(ctx context.Context, goalID uuid.UUID, page, limit int, userID uuid.UUID) ([]models.GoalCommentResponse, int64, error)
	DeleteComment(ctx context.Context, commentID uuid.UUID, userID uuid.UUID) error

	// Hierarchy
	GetGoalTree(ctx context.Context, rootID uuid.UUID, userID uuid.UUID) (*models.GoalResponse, error)
	ListChildGoals(ctx context.Context, parentID uuid.UUID, userID uuid.UUID) ([]models.GoalResponse, error)

	// Check-ins
	CreateCheckIn(ctx context.Context, goalID uuid.UUID, req *models.CheckInCreateRequest, userID uuid.UUID) (*models.CheckInResponse, error)
	ListCheckIns(ctx context.Context, goalID uuid.UUID, page int, limit int, userID uuid.UUID) ([]models.CheckInResponse, int64, error)
	DeleteCheckIn(ctx context.Context, id uuid.UUID, userID uuid.UUID) error

	// Progress
	RecalculateProgress(ctx context.Context, goalID uuid.UUID) error

	// Access
	CheckGoalAccess(ctx context.Context, goalID uuid.UUID, userID uuid.UUID) error

	// Metric Import/Export
	ExportMetricsTemplate(ctx context.Context, filter *models.GoalFilter, format string) ([]byte, error)
	ImportMetricsDryRun(ctx context.Context, fileData []byte, fileName string, userID uuid.UUID) (*models.MetricImportDryRunResponse, error)
	ImportMetricsCommit(ctx context.Context, fileData []byte, fileName string, title string, comment string, primaryGoalID uuid.UUID, userID uuid.UUID) (*models.MetricImportBatchResponse, error)
	GetMetricImportBatch(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*models.MetricImportBatchResponse, error)
	ListMetricImportBatches(ctx context.Context, filter *models.MetricImportBatchFilter, userID uuid.UUID) ([]models.MetricImportBatchResponse, int64, error)
	DeleteMetricImportBatch(ctx context.Context, id uuid.UUID, userID uuid.UUID) error
	GetAvailableMetricBatchTransitions(ctx context.Context, batchID uuid.UUID, userID uuid.UUID) ([]models.AvailableTransitionResponse, error)
	ExecuteMetricBatchTransition(ctx context.Context, batchID uuid.UUID, req *models.MetricImportBatchTransitionRequest, userID uuid.UUID) (*models.MetricImportBatchResponse, error)
	GetMetricBatchTransitionHistory(ctx context.Context, batchID uuid.UUID, userID uuid.UUID) ([]models.MetricImportBatchTransitionHistoryResponse, error)
}

// Sentinel errors used by handlers to pick an HTTP status without relying on
// fragile message-substring matching. Handler-facing text is looked up via
// i18n using the same key (see CreateGoal's usage in goal_handler.go).
var (
	ErrGoalAlreadyExists          = errors.New("goal_already_exists")
	ErrParentGoalNotFound         = errors.New("parent_goal_not_found")
	ErrGoalHierarchyDepthExceeded = errors.New("max_goal_hierarchy_depth_exceeded")
)

// ──────────────────────────────────────────────────
// Typed errors (HTTP status carried to the handler)
// ──────────────────────────────────────────────────

// GoalError is a client-facing error carrying the HTTP status the handler
// should return and an already-translated message. Anything that is not a
// GoalError is treated as an internal (500) error by the goal handlers.
type GoalError struct {
	Status int
	Key    string
	Msg    string
	Err    error
}

func (e *GoalError) Error() string { return e.Msg }
func (e *GoalError) Unwrap() error { return e.Err }

// goalLocalMessages holds translations for goal-module keys that are not (yet)
// present in pkg/i18n/messages.go. gT prefers the shared catalogue and falls
// back to this table so users never see a raw key.
var goalLocalMessages = map[string][2]string{
	"goal_closed_read_only":            {"Closed goals cannot be modified", "لا يمكن تعديل الأهداف المغلقة"},
	"goal_title_required":              {"Title is required", "العنوان مطلوب"},
	"goal_description_required":        {"Description is required", "الوصف مطلوب"},
	"goal_title_too_long":              {"Title must be at most 255 characters", "يجب ألا يتجاوز العنوان 255 حرفًا"},
	"goal_invalid_priority":            {"Invalid priority (must be Critical, High, Medium or Low)", "أولوية غير صالحة (حرجة، عالية، متوسطة، منخفضة)"},
	"goal_start_after_target":          {"Start date must be on or before the target date", "يجب أن يكون تاريخ البدء في أو قبل التاريخ المستهدف"},
	"goal_invalid_metadata":            {"Metadata must be a valid JSON object", "يجب أن تكون البيانات الوصفية كائن JSON صالحًا"},
	"goal_owner_not_found":             {"Owner user not found", "المستخدم المالك غير موجود"},
	"goal_department_not_found":        {"Department not found", "القسم غير موجود"},
	"goal_parent_change_unsupported":   {"Changing the parent goal is not supported; clone the goal under the new parent instead", "تغيير الهدف الأب غير مدعوم؛ انسخ الهدف تحت الهدف الأب الجديد بدلاً من ذلك"},
	"goal_title_exists":                {"A goal with this title already exists for this owner", "يوجد هدف بنفس العنوان لهذا المالك"},
	"goal_delete_blocked":              {"Goal cannot be deleted while it is referenced by: %s", "لا يمكن حذف الهدف لأنه مرتبط بـ: %s"},
	"goal_documenta_folder_failed":     {"Could not create the document folder for this goal. Please try again or contact support.", "تعذر إنشاء مجلد المستندات لهذا الهدف. حاول مرة أخرى أو تواصل مع الدعم."},
	"goal_invalid_sort":                {"Invalid sort_by or sort_order", "قيمة الترتيب غير صالحة"},
	"metric_weight_positive":           {"Weight must be greater than 0", "يجب أن يكون الوزن أكبر من 0"},
	"metric_name_required":             {"Metric name is required", "اسم المقياس مطلوب"},
	"metric_name_exists":               {"A metric with this name already exists on this goal", "يوجد مقياس بنفس الاسم في هذا الهدف"},
	"metric_invalid_formula":           {"Invalid formula: %s", "صيغة غير صالحة: %s"},
	"evidence_invalid_type":            {"Invalid evidence type (must be Report, Photo, Certificate, Invoice or Other)", "نوع الدليل غير صالح"},
	"evidence_title_required":          {"Evidence title is required", "عنوان الدليل مطلوب"},
	"evidence_metric_not_in_goal":      {"The selected metric does not belong to this goal", "المقياس المحدد لا ينتمي إلى هذا الهدف"},
	"evidence_invalid_metric_id":       {"Invalid metric_id", "معرّف المقياس غير صالح"},
	"evidence_empty_file":              {"The uploaded file is empty", "الملف المرفوع فارغ"},
	"evidence_file_type_not_allowed":   {"This file type is not allowed", "نوع الملف غير مسموح"},
	"collaborator_already_exists":      {"User is already a collaborator on this goal", "المستخدم متعاون بالفعل في هذا الهدف"},
	"check_in_metric_not_found":        {"Metric %s not found", "المقياس %s غير موجود"},
	"check_in_metric_not_in_goal":      {"Metric %s does not belong to this goal", "المقياس %s لا ينتمي إلى هذا الهدف"},
	"metric_batch_access_denied_goals": {"You do not have edit access to goal(s): %s", "ليست لديك صلاحية تعديل الأهداف: %s"},
	"failed_to_create":                 {"Failed to create", "فشل الإنشاء"},
	"failed_to_update":                 {"Failed to update", "فشل التحديث"},
	"failed_to_load_data":              {"Failed to load data", "فشل تحميل البيانات"},
	"value_required":                   {"Value is required", "القيمة مطلوبة"},
	"failed_to_parse_file_detail":      {"Failed to parse file: %s", "فشل تحليل الملف: %s"},
	"target_value_required":            {"Target value is required", "القيمة المستهدفة مطلوبة"},
}

// gT translates key using the shared catalogue, falling back to goalLocalMessages.
func gT(ctx context.Context, key string) string {
	if msg := i18n.T(ctx, key); msg != key {
		return msg
	}
	if pair, ok := goalLocalMessages[key]; ok {
		lang, _ := ctx.Value(constants.ContextKeys.ACCEPT_LANGUAGE).(string)
		if lang == "ar" && pair[1] != "" {
			return pair[1]
		}
		return pair[0]
	}
	return key
}

func gTf(ctx context.Context, key string, args ...interface{}) string {
	return fmt.Sprintf(gT(ctx, key), args...)
}

func newGoalErr(ctx context.Context, status int, key string, args ...interface{}) *GoalError {
	msg := gT(ctx, key)
	if len(args) > 0 {
		msg = fmt.Sprintf(msg, args...)
	}
	return &GoalError{Status: status, Key: key, Msg: msg}
}

func errBadRequest(ctx context.Context, key string, args ...interface{}) error {
	return newGoalErr(ctx, http.StatusBadRequest, key, args...)
}

func errForbidden(ctx context.Context) error {
	return newGoalErr(ctx, http.StatusForbidden, "access_denied")
}

func errNotFound(ctx context.Context, key string) error {
	return newGoalErr(ctx, http.StatusNotFound, key)
}

func errConflict(ctx context.Context, key string, args ...interface{}) error {
	return newGoalErr(ctx, http.StatusConflict, key, args...)
}

// notFoundOr maps gorm.ErrRecordNotFound to a 404 GoalError (key), and wraps
// anything else as an internal error.
func notFoundOr(ctx context.Context, err error, key string) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		ge := newGoalErr(ctx, http.StatusNotFound, key)
		ge.Err = err
		return ge
	}
	return fmt.Errorf("%s: %w", i18n.T(ctx, key), err)
}

// ════════════════════════════════════════════════════
// Private Implementation
// ════════════════════════════════════════════════════

type goalService struct {
	goalRepo            repository.GoalRepository
	workflowRepo        repository.WorkflowRepository
	userRepo            repository.UserRepository
	departmentRepo      repository.DepartmentRepository
	documentaClient     storage.DocumentaClient
	notificationService *NotificationService
	actionLogService    ActionLogService
	cfg                 *config.Config
	wsHub               *WSHub
}

func NewGoalService(
	goalRepo repository.GoalRepository,
	workflowRepo repository.WorkflowRepository,
	userRepo repository.UserRepository,
	departmentRepo repository.DepartmentRepository,
	documentaClient storage.DocumentaClient,
	notificationService *NotificationService,
	actionLogService ActionLogService,
	cfg *config.Config,
	wsHub *WSHub,
) GoalService {
	return &goalService{
		goalRepo:            goalRepo,
		workflowRepo:        workflowRepo,
		userRepo:            userRepo,
		departmentRepo:      departmentRepo,
		documentaClient:     documentaClient,
		notificationService: notificationService,
		actionLogService:    actionLogService,
		cfg:                 cfg,
		wsHub:               wsHub,
	}
}

// ──────────────────────────────────────────────────
// Access Control Helpers
// ──────────────────────────────────────────────────

// canAccessGoal checks if the user can access (view) the given goal.
// Access is granted if the user is:
//   - Super admin
//   - Goal owner
//   - A collaborator on the goal
//   - In the same department as the goal
//
// This is the same rule the goal list applies (repository.GoalVisibilitySQL).
func (s *goalService) canAccessGoal(ctx context.Context, goalID uuid.UUID, userID uuid.UUID) (bool, error) {
	goal, err := s.goalRepo.FindByID(ctx, goalID)
	if err != nil {
		return false, err
	}
	return s.canAccessGoalObj(ctx, goal, userID), nil
}

func (s *goalService) canAccessGoalObj(ctx context.Context, goal *models.Goal, userID uuid.UUID) bool {
	if s.isSuperAdmin(ctx) {
		return true
	}
	if goal.OwnerID == userID {
		return true
	}
	collaborators, _ := s.goalRepo.ListCollaborators(ctx, goal.ID)
	for _, c := range collaborators {
		if c.UserID == userID {
			return true
		}
	}
	if goal.DepartmentID != nil {
		user, err := s.userRepo.FindByID(ctx, userID)
		if err == nil && user != nil && user.DepartmentID != nil && *user.DepartmentID == *goal.DepartmentID {
			return true
		}
	}
	return false
}

// requireView loads the goal (404 if missing) and checks view access (403).
func (s *goalService) requireView(ctx context.Context, goalID uuid.UUID, userID uuid.UUID) (*models.Goal, error) {
	goal, err := s.goalRepo.FindByID(ctx, goalID)
	if err != nil {
		return nil, notFoundOr(ctx, err, "goal_not_found")
	}
	if !s.canAccessGoalObj(ctx, goal, userID) {
		return nil, errForbidden(ctx)
	}
	return goal, nil
}

// requireModify loads the goal (404 if missing) and checks modify access (403):
// super admin, owner, or reviewer collaborator.
func (s *goalService) requireModify(ctx context.Context, goalID uuid.UUID, userID uuid.UUID) (*models.Goal, error) {
	goal, err := s.goalRepo.FindByID(ctx, goalID)
	if err != nil {
		return nil, notFoundOr(ctx, err, "goal_not_found")
	}
	if !s.canModifyGoalObj(ctx, goal, userID) {
		return nil, errForbidden(ctx)
	}
	return goal, nil
}

// ensureGoalOpen rejects modifications to Closed goals.
func ensureGoalOpen(ctx context.Context, goal *models.Goal) error {
	if goal.Status == models.GoalStatusClosed {
		return errConflict(ctx, "goal_closed_read_only")
	}
	return nil
}

// CheckGoalAccess is exported for handlers that read goal-scoped data through
// other services (e.g. the activity log).
func (s *goalService) CheckGoalAccess(ctx context.Context, goalID uuid.UUID, userID uuid.UUID) error {
	_, err := s.requireView(ctx, goalID, userID)
	return err
}

// canSeeUnapprovedItems returns true if the caller should see metrics/evidence
// regardless of workflow state. This includes super admins, the goal owner, any
// goal collaborator, and users that hold a goals:approve permission. View-only
// callers (goals:view but no other privileges) get the filtered listing.
//
// We deliberately *do not* peek at the user's permissions through the role
// store here — instead we look at whether the caller already has approve
// authority via the *models.User snapshot in context. That's sufficient for
// the cases the platform exercises (approve permission is granted by role
// assignment which IsSuperAdmin or has goals:approve).
func (s *goalService) canSeeUnapprovedItems(ctx context.Context, goalID uuid.UUID, userID uuid.UUID) bool {
	if s.isSuperAdmin(ctx) {
		return true
	}
	if s.userHasPermission(ctx, "goals:approve") || s.userHasPermission(ctx, "goals:delete") {
		return true
	}
	goal, err := s.goalRepo.FindByID(ctx, goalID)
	if err != nil {
		return false
	}
	if goal.OwnerID == userID {
		return true
	}
	collaborators, _ := s.goalRepo.ListCollaborators(ctx, goalID)
	for _, c := range collaborators {
		if c.UserID == userID {
			return true
		}
	}
	return false
}

// userHasPermission checks the *models.User in context for a permission code.
func (s *goalService) userHasPermission(ctx context.Context, code string) bool {
	user, ok := ctx.Value(constants.ContextKeys.User).(*models.User)
	if !ok || user == nil {
		return false
	}
	return user.HasPermission(code)
}

// canModifyGoal checks if the user can modify the goal (owner, reviewer, or super admin)
func (s *goalService) canModifyGoal(ctx context.Context, goalID uuid.UUID, userID uuid.UUID) (bool, error) {
	goal, err := s.goalRepo.FindByID(ctx, goalID)
	if err != nil {
		return false, err
	}
	return s.canModifyGoalObj(ctx, goal, userID), nil
}

func (s *goalService) canModifyGoalObj(ctx context.Context, goal *models.Goal, userID uuid.UUID) bool {
	if s.isSuperAdmin(ctx) {
		return true
	}
	// Owner can always modify
	if goal.OwnerID == userID {
		return true
	}
	// Collaborators with reviewer_l1 or reviewer_l2 role can modify
	collaborators, _ := s.goalRepo.ListCollaborators(ctx, goal.ID)
	for _, c := range collaborators {
		if c.UserID == userID && (c.Role == models.CollaboratorRoleReviewerL1 || c.Role == models.CollaboratorRoleReviewerL2) {
			return true
		}
	}
	return false
}

// metricSiblingsMap builds the name -> current value map consumed by the formula
// evaluator. Pass the result of ListMetricsByGoalID; the caller can exclude
// a specific metric (e.g. the one being recomputed) by nil'ing it first.
func metricSiblingsMap(metrics []models.GoalMetric) map[string]float64 {
	out := make(map[string]float64, len(metrics))
	for _, m := range metrics {
		out[m.Name] = m.CurrentValue
	}
	return out
}

// recomputeDependentFormulas re-evaluates every formula-bearing metric on the
// goal that references the changed metric by name. Errors are logged, not
// surfaced — one bad formula should not block a value update. changedMetricID
// is excluded so the just-updated metric doesn't re-run its own formula.
func (s *goalService) recomputeDependentFormulas(ctx context.Context, goalID, changedMetricID uuid.UUID) {
	all, err := s.goalRepo.ListMetricsByGoalID(ctx, goalID)
	if err != nil {
		return
	}
	// Find which metric name was changed so we can scan formulas for references.
	var changedName string
	for _, m := range all {
		if m.ID == changedMetricID {
			changedName = m.Name
			break
		}
	}
	for _, m := range all {
		if m.ID == changedMetricID || strings.TrimSpace(m.Formula) == "" {
			continue
		}
		if changedName != "" && !strings.Contains(m.Formula, changedName) {
			continue // formula doesn't reference the changed metric; skip
		}
		siblings := make([]models.GoalMetric, 0, len(all))
		for _, sib := range all {
			if sib.ID != m.ID {
				siblings = append(siblings, sib)
			}
		}
		computed, ferr := metricformula.Evaluate(m.Formula, metricSiblingsMap(siblings))
		if ferr != nil {
			log.Printf("[goal_service] recomputeDependentFormulas: metric %s formula %q failed: %v", m.ID, m.Formula, ferr)
			continue
		}
		if computed == m.CurrentValue {
			continue
		}
		history := &models.MetricHistory{
			MetricID: m.ID,
			OldValue: m.CurrentValue,
			NewValue: computed,
			Comment:  fmt.Sprintf("Auto-recomputed after %s changed", changedName),
		}
		_ = s.goalRepo.CreateMetricHistory(ctx, history)
		metricCopy := m
		metricCopy.CurrentValue = computed
		if err := s.goalRepo.UpdateMetric(ctx, &metricCopy); err != nil {
			log.Printf("[goal_service] recomputeDependentFormulas: update metric %s failed: %v", m.ID, err)
		}
	}
}

// isSuperAdmin checks if the current request is from a super admin
func (s *goalService) isSuperAdmin(ctx context.Context) bool {
	if user, ok := ctx.Value(constants.ContextKeys.User).(*models.User); ok {
		return user.IsSuperAdmin
	}
	return false
}

// getUserDepartmentIDs returns the department IDs the user belongs to
func (s *goalService) getUserDepartmentIDs(ctx context.Context, userID uuid.UUID) []uuid.UUID {
	user, err := s.userRepo.FindByID(ctx, userID)
	if err != nil || user == nil {
		return nil
	}
	var deptIDs []uuid.UUID
	if user.DepartmentID != nil {
		deptIDs = append(deptIDs, *user.DepartmentID)
	}
	return deptIDs
}

// ──────────────────────────────────────────────────
// 1. CreateGoal
// ──────────────────────────────────────────────────

func (s *goalService) CreateGoal(ctx context.Context, req *models.GoalCreateRequest, userID uuid.UUID) (*models.GoalResponse, error) {
	// Validate everything BEFORE any side effect (Documenta folder creation).
	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		return nil, errBadRequest(ctx, "goal_title_required")
	}
	if strings.TrimSpace(req.Description) == "" {
		return nil, errBadRequest(ctx, "goal_description_required")
	}
	if !models.IsValidGoalPriority(req.Priority) {
		return nil, errBadRequest(ctx, "goal_invalid_priority")
	}
	if err := req.Validate(); err != nil {
		return nil, errBadRequest(ctx, "goal_start_after_target")
	}
	if !validMetadataJSON(req.Metadata) {
		return nil, errBadRequest(ctx, "goal_invalid_metadata")
	}
	if u, uErr := s.userRepo.FindByID(ctx, req.OwnerID); uErr != nil || u == nil {
		return nil, errBadRequest(ctx, "goal_owner_not_found")
	}
	if req.DepartmentID != nil {
		if d, dErr := s.departmentRepo.FindByID(ctx, *req.DepartmentID); dErr != nil || d == nil {
			return nil, errBadRequest(ctx, "goal_department_not_found")
		}
	}

	// Check duplicate goal (title &  owner)
	existingGoal, err := s.goalRepo.FindGoalByTitleAndOwner(ctx, req.Title, req.OwnerID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(ctx, "failed_to_check_existing_goal"), err)
	}
	if existingGoal != nil {
		return nil, ErrGoalAlreadyExists
	}

	// Validate parent goal hierarchy
	var parentPath string
	var parentLevel int
	if req.ParentGoalID != nil {
		parent, err := s.goalRepo.FindByID(ctx, *req.ParentGoalID)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrParentGoalNotFound, err)
		}
		if parent.Level >= 2 {
			return nil, fmt.Errorf("%w: level %d", ErrGoalHierarchyDepthExceeded, parent.Level)
		}
		parentPath = parent.Path
		if parentPath == "" {
			parentPath = parent.ID.String()
		}
		parentLevel = parent.Level
	}

	// Create this goal's Documenta folder (unique name: title + short id).
	goalID := uuid.New()
	folderID, err := s.createGoalFolder(ctx, req.Title, goalID)
	if err != nil {
		return nil, err
	}

	// Default metadata to empty JSON object
	metadata := req.Metadata
	if strings.TrimSpace(metadata) == "" {
		metadata = "{}"
	}

	goalLevel := 0
	if req.ParentGoalID != nil {
		goalLevel = parentLevel + 1
	}

	goal := &models.Goal{
		ID:                goalID,
		Title:             req.Title,
		Description:       req.Description,
		Category:          req.Category,
		CategoryID:        req.CategoryID,
		Priority:          req.Priority,
		Status:            models.GoalStatusDraft,
		OwnerID:           req.OwnerID,
		DepartmentID:      req.DepartmentID,
		ParentGoalID:      req.ParentGoalID,
		Level:             goalLevel,
		StartDate:         req.StartDate,
		TargetDate:        req.TargetDate,
		ReviewDate:        req.ReviewDate,
		DocumentaFolderID: folderID,
		Metadata:          metadata,
		CreatedByID:       userID,
	}

	if err := s.goalRepo.Create(ctx, goal); err != nil {
		log.Printf("[goal_service] CreateGoal: insert failed: %v", err)
		return nil, fmt.Errorf("failed to create goal: %w", err)
	}

	// Set materialized path after creation (need the generated ID)
	if parentPath != "" {
		goal.Path = parentPath + "." + goal.ID.String()
	} else {
		goal.Path = goal.ID.String()
	}
	if err := s.goalRepo.Update(ctx, goal); err != nil {
		return nil, fmt.Errorf("failed to set goal path: %w", err)
	}
	if goal.ParentGoalID != nil {
		_ = s.RecalculateProgress(ctx, *goal.ParentGoalID)
	}

	// Add the creator as a collaborator when they are not the owner. If
	// an admin/manager creates a goal on someone else's behalf they still
	// need access to the goal they authored; adding them as a collaborator
	// (not duplicating them when they are already the owner) preserves the
	// owner-vs-collaborator distinction without losing audit/access.
	if userID != req.OwnerID {
		collab := &models.GoalCollaborator{
			GoalID: goal.ID,
			UserID: userID,
			Role:   "collaborator",
		}
		if err := s.goalRepo.AddCollaborator(ctx, collab); err != nil {
			log.Printf("[goal_service] CreateGoal: failed to add creator %s as collaborator on goal %s: %v", userID, goal.ID, err)
		}
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "create",
		Module:      "goals",
		ResourceID:  goal.ID.String(),
		Description: "Created goal: " + goal.Title,
		Status:      "success",
	})

	// Fetch back with relations
	goalWithRelations, err := s.goalRepo.FindByIDWithRelations(ctx, goal.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch created goal: %w", err)
	}

	resp := goalWithRelations.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// 2. GetGoal
// ──────────────────────────────────────────────────

func (s *goalService) GetGoal(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*models.GoalResponse, error) {
	// Access check
	if _, err := s.requireView(ctx, id, userID); err != nil {
		return nil, err
	}

	goal, err := s.goalRepo.FindByIDWithRelations(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(ctx, "goal_not_found"), err)
	}

	// Visibility gate: view-only callers see only terminal-approved metrics
	// and evidences. Owners, collaborators, super admins, and approve-permission
	// holders see everything (so reviewers can see drafts).
	if !s.canSeeUnapprovedItems(ctx, id, userID) {
		filteredMetrics := make([]models.GoalMetric, 0, len(goal.Metrics))
		for _, m := range goal.Metrics {
			if m.CurrentStateID == nil {
				// Legacy metric (pre-gate) — visible.
				filteredMetrics = append(filteredMetrics, m)
				continue
			}
			if m.CurrentState != nil && m.CurrentState.StateType == "terminal" {
				filteredMetrics = append(filteredMetrics, m)
			}
		}
		goal.Metrics = filteredMetrics

		filteredEvidences := make([]models.Evidence, 0, len(goal.Evidences))
		for _, e := range goal.Evidences {
			if e.CurrentStateID == nil {
				filteredEvidences = append(filteredEvidences, e)
				continue
			}
			if e.CurrentState != nil && e.CurrentState.StateType == "terminal" {
				filteredEvidences = append(filteredEvidences, e)
			}
		}
		goal.Evidences = filteredEvidences
	}

	resp := goal.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// 3. ListGoals
// ──────────────────────────────────────────────────

func (s *goalService) ListGoals(ctx context.Context, filter *models.GoalFilter) ([]models.GoalResponse, int64, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit < 1 {
		filter.Limit = 10
	}
	if _, err := repository.ResolveGoalSort(filter.SortBy, filter.SortOrder); err != nil {
		return nil, 0, errBadRequest(ctx, "goal_invalid_sort")
	}

	goals, total, err := s.goalRepo.List(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list goals: %w", err)
	}

	responses := make([]models.GoalResponse, len(goals))
	for i, g := range goals {
		responses[i] = g.ToResponse()
	}

	return responses, total, nil
}

// ──────────────────────────────────────────────────
// 4. UpdateGoal
// ──────────────────────────────────────────────────

func (s *goalService) UpdateGoal(ctx context.Context, id uuid.UUID, req *models.GoalUpdateRequest, userID uuid.UUID) (*models.GoalResponse, error) {
	// Existence (404) + access (403)
	goal, err := s.requireModify(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if err := ensureGoalOpen(ctx, goal); err != nil {
		return nil, err
	}

	// Validate + apply non-nil fields
	if req.Title != nil {
		t := strings.TrimSpace(*req.Title)
		if t == "" {
			return nil, errBadRequest(ctx, "goal_title_required")
		}
		if len([]rune(t)) > 255 {
			return nil, errBadRequest(ctx, "goal_title_too_long")
		}
		goal.Title = t
	}
	if req.Description != nil {
		if strings.TrimSpace(*req.Description) == "" {
			return nil, errBadRequest(ctx, "goal_description_required")
		}
		goal.Description = *req.Description
	}
	if req.Category != nil {
		goal.Category = *req.Category
	}
	if req.CategoryID != nil {
		goal.CategoryID = req.CategoryID
	}
	if req.Priority != nil {
		if !models.IsValidGoalPriority(*req.Priority) {
			return nil, errBadRequest(ctx, "goal_invalid_priority")
		}
		goal.Priority = *req.Priority
	}
	if req.DepartmentID != nil {
		if d, dErr := s.departmentRepo.FindByID(ctx, *req.DepartmentID); dErr != nil || d == nil {
			return nil, errBadRequest(ctx, "goal_department_not_found")
		}
		goal.DepartmentID = req.DepartmentID
	}
	if req.OwnerID != nil {
		if u, uErr := s.userRepo.FindByID(ctx, *req.OwnerID); uErr != nil || u == nil {
			return nil, errBadRequest(ctx, "goal_owner_not_found")
		}
		goal.OwnerID = *req.OwnerID
	}
	// Re-parenting would require re-computing level/path for the whole
	// subtree plus cycle checks; it is not supported. Sending the current
	// parent (as edit forms do) is accepted.
	if req.ParentGoalID != nil {
		if goal.ParentGoalID == nil || *goal.ParentGoalID != *req.ParentGoalID {
			return nil, errBadRequest(ctx, "goal_parent_change_unsupported")
		}
	}
	if req.StartDate != nil {
		goal.StartDate = req.StartDate
	}
	if req.TargetDate != nil {
		goal.TargetDate = req.TargetDate
	}
	if req.ReviewDate != nil {
		goal.ReviewDate = req.ReviewDate
	}
	if goal.StartDate != nil && goal.TargetDate != nil && goal.StartDate.After(*goal.TargetDate) {
		return nil, errBadRequest(ctx, "goal_start_after_target")
	}
	if req.Metadata != nil {
		if !validMetadataJSON(*req.Metadata) {
			return nil, errBadRequest(ctx, "goal_invalid_metadata")
		}
		goal.Metadata = *req.Metadata
		if strings.TrimSpace(goal.Metadata) == "" {
			goal.Metadata = "{}"
		}
	}
	if req.Title != nil || req.OwnerID != nil {
		if existing, exErr := s.goalRepo.FindGoalByTitleAndOwner(ctx, goal.Title, goal.OwnerID); exErr == nil && existing != nil && existing.ID != goal.ID {
			return nil, errConflict(ctx, "goal_title_exists")
		}
	}
	// Don't let GORM's Save re-associate preloaded relations.
	goal.Owner, goal.Department, goal.CategoryRef, goal.ParentGoal, goal.CreatedBy = nil, nil, nil, nil, nil

	if err := s.goalRepo.Update(ctx, goal); err != nil {
		return nil, fmt.Errorf("failed to update goal: %w", err)
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "update",
		Module:      "goals",
		ResourceID:  goal.ID.String(),
		Description: "Updated goal: " + goal.Title,
		Status:      "success",
	})

	// WebSocket broadcast
	if s.wsHub != nil {
		s.wsHub.BroadcastToGoal(goal.ID, "goal_updated", map[string]interface{}{
			"goal_id":    goal.ID,
			"updated_by": userID,
		}, userID)
		s.wsHub.BroadcastGoalToAll("goal_updated", map[string]interface{}{
			"goal_id":    goal.ID,
			"updated_by": userID,
		})
	}

	// Fetch back with relations
	goalWithRelations, err := s.goalRepo.FindByIDWithRelations(ctx, goal.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch updated goal: %w", err)
	}

	resp := goalWithRelations.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// 5. DeleteGoal
// ──────────────────────────────────────────────────

func (s *goalService) DeleteGoal(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {

	goal, err := s.goalRepo.FindByID(ctx, id)
	if err != nil {
		return notFoundOr(ctx, err, "goal_not_found")
	}
	isSuperAdmin := s.isSuperAdmin(ctx)

	if goal.Status == models.GoalStatusDraft {
		// Draft goals: only the owner or a super admin may delete
		if goal.OwnerID != userID && !isSuperAdmin {
			return errForbidden(ctx)
		}
	} else {
		// Non-draft goals: only a super admin can  delete
		if !isSuperAdmin {
			return errForbidden(ctx)
		}
	}

	// Block deletion while child goals or KPI-module records still point at
	// this goal (they would be left dangling). The goal's own metrics,
	// evidences, check-ins and comments are soft-deleted with it.
	refs, err := s.goalRepo.FindGoalReferences(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to check goal references: %w", err)
	}
	if len(refs) > 0 {
		parts := make([]string, 0, len(refs))
		for _, r := range refs {
			parts = append(parts, fmt.Sprintf("%d %s", r.Count, r.Kind))
		}
		return errConflict(ctx, "goal_delete_blocked", strings.Join(parts, ", "))
	}

	if err := s.goalRepo.DeleteGoalWithOwnedRecords(ctx, id); err != nil {
		return fmt.Errorf("failed to delete goal: %w", err)
	}
	if goal.ParentGoalID != nil {
		_ = s.RecalculateProgress(ctx, *goal.ParentGoalID)
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "delete",
		Module:      "goals",
		ResourceID:  goal.ID.String(),
		Description: "Deleted goal: " + goal.Title,
		Status:      "success",
	})

	return nil
}

// ──────────────────────────────────────────────────
// 6. TransitionGoalStatus
// ──────────────────────────────────────────────────

func (s *goalService) TransitionGoalStatus(ctx context.Context, id uuid.UUID, newStatus string, userID uuid.UUID) (*models.GoalResponse, error) {
	// Same rule as editing the goal: owner, reviewer collaborator or super admin.
	goal, err := s.requireModify(ctx, id, userID)
	if err != nil {
		return nil, err
	}

	if !models.IsValidGoalTransition(goal.Status, newStatus) {
		return nil, errBadRequest(ctx, "invalid_goal_status_transition", goal.Status, newStatus)
	}

	oldStatus := goal.Status
	goal.Status = newStatus

	if err := s.goalRepo.Update(ctx, goal); err != nil {
		return nil, fmt.Errorf("failed to update goal status: %w", err)
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "transition",
		Module:      "goals",
		ResourceID:  goal.ID.String(),
		Description: fmt.Sprintf("Transitioned goal '%s' from %s to %s", goal.Title, oldStatus, newStatus),
		Status:      "success",
	})

	// Fetch back with relations
	goalWithRelations, err := s.goalRepo.FindByIDWithRelations(ctx, goal.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch updated goal: %w", err)
	}

	resp := goalWithRelations.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// 7. AddCollaborator
// ──────────────────────────────────────────────────

func (s *goalService) AddCollaborator(ctx context.Context, goalID uuid.UUID, req *models.CollaboratorAddRequest, userID uuid.UUID) error {
	// Verify goal exists
	goal, err := s.goalRepo.FindByID(ctx, goalID)
	if err != nil {
		return notFoundOr(ctx, err, "goal_not_found")
	}

	// Only goal owner or super admin can manage collaborators
	if goal.OwnerID != userID && !s.isSuperAdmin(ctx) {
		return errForbidden(ctx)
	}

	// Verify user exists
	user, err := s.userRepo.FindByID(ctx, req.UserID)
	if err != nil || user == nil {
		return errBadRequest(ctx, "user_not_found")
	}

	// Reject duplicates (one collaborator row per user per goal)
	existing, _ := s.goalRepo.ListCollaborators(ctx, goalID)
	for _, c := range existing {
		if c.UserID == req.UserID {
			return errConflict(ctx, "collaborator_already_exists")
		}
	}

	collaborator := &models.GoalCollaborator{
		GoalID: goalID,
		UserID: req.UserID,
		Role:   req.Role,
	}

	if err := s.goalRepo.AddCollaborator(ctx, collaborator); err != nil {
		return fmt.Errorf("failed to add collaborator: %w", err)
	}

	// Send in-app notification
	s.notificationService.SendNotification(
		ctx,
		"notification",
		nil,
		"en",
		[]string{user.Email},
		nil,
		nil,
		"Goal Assignment",
		"You have been assigned as "+req.Role+" to goal: "+goal.Title,
		nil,
		nil,
		&userID,
		nil,
	)

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "create",
		Module:      "goals",
		ResourceID:  goalID.String(),
		Description: fmt.Sprintf("Added collaborator %s (%s) to goal: %s", user.Email, req.Role, goal.Title),
		Status:      "success",
	})

	// WebSocket broadcast
	if s.wsHub != nil {
		s.wsHub.BroadcastToGoal(goalID, "collaborator_changed", map[string]interface{}{
			"goal_id": goalID,
		}, userID)
	}

	return nil
}

// ──────────────────────────────────────────────────
// 8. RemoveCollaborator
// ──────────────────────────────────────────────────

func (s *goalService) RemoveCollaborator(ctx context.Context, goalID uuid.UUID, collaboratorUserID uuid.UUID, userID uuid.UUID) error {
	// Only goal owner or super admin can manage collaborators
	goal, err := s.goalRepo.FindByID(ctx, goalID)
	if err != nil {
		return notFoundOr(ctx, err, "goal_not_found")
	}
	if goal.OwnerID != userID && !s.isSuperAdmin(ctx) {
		return errForbidden(ctx)
	}

	if err := s.goalRepo.RemoveCollaborator(ctx, goalID, collaboratorUserID); err != nil {
		return fmt.Errorf("failed to remove collaborator: %w", err)
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "delete",
		Module:      "goals",
		ResourceID:  goalID.String(),
		Description: fmt.Sprintf("Removed collaborator %s from goal %s", collaboratorUserID.String(), goalID.String()),
		Status:      "success",
	})

	// WebSocket broadcast
	if s.wsHub != nil {
		s.wsHub.BroadcastToGoal(goalID, "collaborator_changed", map[string]interface{}{
			"goal_id": goalID,
		}, userID)
	}

	return nil
}

// ──────────────────────────────────────────────────
// 9. CreateMetric
// ──────────────────────────────────────────────────

func (s *goalService) CreateMetric(ctx context.Context, goalID uuid.UUID, req *models.GoalMetricCreateRequest, userID uuid.UUID) (*models.GoalMetricResponse, error) {
	// Existence (404) + access (403)
	goal, err := s.requireModify(ctx, goalID, userID)
	if err != nil {
		return nil, err
	}
	if err := ensureGoalOpen(ctx, goal); err != nil {
		return nil, err
	}

	// Validate
	name := strings.TrimSpace(req.Name)
	if name == "" {
		return nil, errBadRequest(ctx, "metric_name_required")
	}
	if !models.IsValidMetricType(req.MetricType) {
		return nil, errBadRequest(ctx, "invalid_metric_type", req.MetricType)
	}
	if req.TargetValue == nil {
		return nil, errBadRequest(ctx, "target_value_required")
	}
	weight := 1.0
	if req.Weight != nil {
		if *req.Weight <= 0 {
			return nil, errBadRequest(ctx, "metric_weight_positive")
		}
		weight = *req.Weight
	}
	siblings, _ := s.goalRepo.ListMetricsByGoalID(ctx, goalID)
	for _, sib := range siblings {
		if strings.EqualFold(strings.TrimSpace(sib.Name), name) {
			return nil, errConflict(ctx, "metric_name_exists")
		}
	}

	metric := &models.GoalMetric{
		GoalID:        goalID,
		Name:          name,
		MetricType:    req.MetricType,
		Unit:          req.Unit,
		BaselineValue: req.BaselineValue,
		CurrentValue:  req.CurrentValue,
		TargetValue:   *req.TargetValue,
		Weight:        weight,
		Formula:       strings.TrimSpace(req.Formula),
		Version:       1,
	}

	// If a formula was provided, validate it compiles and pre-compute CurrentValue
	// from siblings so the metric isn't stuck at 0 until the next value update.
	if metric.Formula != "" {
		computed, ferr := metricformula.Evaluate(metric.Formula, metricSiblingsMap(siblings))
		if ferr != nil {
			return nil, errBadRequest(ctx, "metric_invalid_formula", ferr.Error())
		}
		metric.CurrentValue = computed
	}

	// Assign the metric workflow's initial state. Owners can see drafts but
	// view-only callers will be filtered out by ListMetrics until a transition
	// reaches the terminal-approved state.
	if wf, initial, wfErr := s.resolveGoalWorkflow(ctx, goalWfMetric); wfErr == nil && wf != nil && initial != nil {
		metric.WorkflowID = &wf.ID
		metric.CurrentStateID = &initial.ID
	}

	if err := s.goalRepo.CreateMetric(ctx, metric); err != nil {
		return nil, fmt.Errorf("failed to create metric: %w", err)
	}

	// Recalculate goal progress
	if err := s.RecalculateProgress(ctx, goalID); err != nil {
		return nil, fmt.Errorf("failed to recalculate progress: %w", err)
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "create",
		Module:      "goals",
		ResourceID:  metric.ID.String(),
		Description: fmt.Sprintf("Created metric '%s' for goal: %s", metric.Name, goal.Title),
		Status:      "success",
	})

	resp := metric.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// 10. UpdateMetric
// ──────────────────────────────────────────────────

func (s *goalService) UpdateMetric(ctx context.Context, id uuid.UUID, req *models.GoalMetricUpdateRequest, userID uuid.UUID) (*models.GoalMetricResponse, error) {
	metric, err := s.goalRepo.FindMetricByID(ctx, id)
	if err != nil {
		return nil, notFoundOr(ctx, err, "metric_not_found")
	}

	// Access check via parent goal
	goal, err := s.requireModify(ctx, metric.GoalID, userID)
	if err != nil {
		return nil, err
	}
	if err := ensureGoalOpen(ctx, goal); err != nil {
		return nil, err
	}

	// Validate + apply non-nil fields
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, errBadRequest(ctx, "metric_name_required")
		}
		if !strings.EqualFold(name, metric.Name) {
			siblings, _ := s.goalRepo.ListMetricsByGoalID(ctx, metric.GoalID)
			for _, sib := range siblings {
				if sib.ID != metric.ID && strings.EqualFold(strings.TrimSpace(sib.Name), name) {
					return nil, errConflict(ctx, "metric_name_exists")
				}
			}
		}
		metric.Name = name
	}
	if req.MetricType != nil {
		if !models.IsValidMetricType(*req.MetricType) {
			return nil, errBadRequest(ctx, "invalid_metric_type", *req.MetricType)
		}
		metric.MetricType = *req.MetricType
	}
	if req.Unit != nil {
		metric.Unit = *req.Unit
	}
	if req.BaselineValue != nil {
		metric.BaselineValue = *req.BaselineValue
	}
	if req.TargetValue != nil {
		metric.TargetValue = *req.TargetValue
	}
	if req.Weight != nil {
		if *req.Weight <= 0 {
			return nil, errBadRequest(ctx, "metric_weight_positive")
		}
		metric.Weight = *req.Weight
	}
	if req.Formula != nil {
		metric.Formula = strings.TrimSpace(*req.Formula)
	}

	// If the (possibly new) formula is non-empty, validate and recompute now.
	if metric.Formula != "" {
		siblings, _ := s.goalRepo.ListMetricsByGoalID(ctx, metric.GoalID)
		// Exclude self so a formula can't read its own stale value.
		filtered := make([]models.GoalMetric, 0, len(siblings))
		for _, sib := range siblings {
			if sib.ID != metric.ID {
				filtered = append(filtered, sib)
			}
		}
		computed, ferr := metricformula.Evaluate(metric.Formula, metricSiblingsMap(filtered))
		if ferr != nil {
			return nil, errBadRequest(ctx, "metric_invalid_formula", ferr.Error())
		}
		metric.CurrentValue = computed
	}

	if err := s.goalRepo.UpdateMetric(ctx, metric); err != nil {
		return nil, fmt.Errorf("failed to update metric: %w", err)
	}

	// Recalculate goal progress
	if err := s.RecalculateProgress(ctx, metric.GoalID); err != nil {
		return nil, fmt.Errorf("failed to recalculate progress: %w", err)
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "update",
		Module:      "goals",
		ResourceID:  metric.ID.String(),
		Description: fmt.Sprintf("Updated metric: %s", metric.Name),
		Status:      "success",
	})

	resp := metric.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// 11. DeleteMetric
// ──────────────────────────────────────────────────

func (s *goalService) DeleteMetric(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	// Find metric to get goal ID
	metric, err := s.goalRepo.FindMetricByID(ctx, id)
	if err != nil {
		return notFoundOr(ctx, err, "metric_not_found")
	}

	// Access check via parent goal
	goal, err := s.requireModify(ctx, metric.GoalID, userID)
	if err != nil {
		return err
	}
	if err := ensureGoalOpen(ctx, goal); err != nil {
		return err
	}

	goalID := metric.GoalID

	if err := s.goalRepo.DeleteMetric(ctx, id); err != nil {
		return fmt.Errorf("failed to delete metric: %w", err)
	}

	// Recalculate goal progress
	if err := s.RecalculateProgress(ctx, goalID); err != nil {
		return fmt.Errorf("failed to recalculate progress: %w", err)
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "delete",
		Module:      "goals",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("Deleted metric: %s", metric.Name),
		Status:      "success",
	})

	return nil
}

// ──────────────────────────────────────────────────
// 12. UpdateMetricValue
// ──────────────────────────────────────────────────
//
// Per item #21, a value change is no longer mutated directly. Instead a
// GoalMetricValueChange is created and put through the metric_value_change
// approval workflow. The metric's CurrentValue is only updated when the
// value change reaches a terminal-approved state via TransitionMetricValueChange.
func (s *goalService) UpdateMetricValue(ctx context.Context, id uuid.UUID, req *models.MetricValueUpdateRequest, userID uuid.UUID) (*models.GoalMetricValueChangeResponse, error) {
	if req.Value == nil {
		return nil, errBadRequest(ctx, "value_required")
	}
	metric, err := s.goalRepo.FindMetricByID(ctx, id)
	if err != nil {
		return nil, notFoundOr(ctx, err, "metric_not_found")
	}

	// Access check via parent goal
	goal, err := s.requireModify(ctx, metric.GoalID, userID)
	if err != nil {
		return nil, err
	}
	if err := ensureGoalOpen(ctx, goal); err != nil {
		return nil, err
	}

	change, err := s.buildMetricValueChange(ctx, metric, *req.Value, req.Comment, userID)
	if err != nil {
		return nil, err
	}

	if err := s.goalRepo.CreateMetricValueChange(ctx, change); err != nil {
		return nil, fmt.Errorf("failed to create metric value change: %w", err)
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "create",
		Module:      "goals",
		ResourceID:  change.ID.String(),
		Description: fmt.Sprintf("Proposed metric value change for '%s': %.2f -> %.2f (pending approval)", metric.Name, metric.CurrentValue, change.ProposedValue),
		Status:      "success",
	})

	// Reload with relations
	reloaded, rerr := s.goalRepo.FindMetricValueChangeByIDWithRelations(ctx, change.ID)
	if rerr == nil {
		resp := reloaded.ToResponse()
		return &resp, nil
	}
	resp := change.ToResponse()
	return &resp, nil
}

// buildMetricValueChange computes the proposed value (formula metrics ignore
// the submitted value) and attaches the metric_value_change workflow's initial
// state. It does not persist anything.
func (s *goalService) buildMetricValueChange(ctx context.Context, metric *models.GoalMetric, value float64, comment string, userID uuid.UUID) (*models.GoalMetricValueChange, error) {
	newValue := value
	if strings.TrimSpace(metric.Formula) != "" {
		siblings, _ := s.goalRepo.ListMetricsByGoalID(ctx, metric.GoalID)
		filtered := make([]models.GoalMetric, 0, len(siblings))
		for _, sib := range siblings {
			if sib.ID != metric.ID {
				filtered = append(filtered, sib)
			}
		}
		if computed, ferr := metricformula.Evaluate(metric.Formula, metricSiblingsMap(filtered)); ferr == nil {
			newValue = computed
		} else {
			log.Printf("[goal_service] UpdateMetricValue: formula %q on metric %s failed to evaluate (%v); falling back to submitted value",
				metric.Formula, metric.ID, ferr)
		}
	}

	wf, initial, wfErr := s.resolveGoalWorkflow(ctx, goalWfMetricValueChange)
	if wfErr != nil && wf != nil {
		return nil, fmt.Errorf("%s", i18n.T(ctx, "metric_value_change_workflow_no_initial_state"))
	}
	if wfErr != nil || wf == nil || initial == nil {
		return nil, fmt.Errorf("%s", i18n.T(ctx, "no_active_metric_value_change_workflow"))
	}

	return &models.GoalMetricValueChange{
		MetricID:       metric.ID,
		ProposedValue:  newValue,
		PreviousValue:  metric.CurrentValue,
		Comment:        comment,
		SubmittedByID:  userID,
		WorkflowID:     wf.ID,
		CurrentStateID: initial.ID,
		Version:        1,
	}, nil
}

// ──────────────────────────────────────────────────
// 13. GetMetricHistory
// ──────────────────────────────────────────────────

func (s *goalService) GetMetricHistory(ctx context.Context, metricID uuid.UUID, page int, limit int, userID uuid.UUID) ([]models.MetricHistoryResponse, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 20
	}

	metric, err := s.goalRepo.FindMetricByID(ctx, metricID)
	if err != nil {
		return nil, 0, notFoundOr(ctx, err, "metric_not_found")
	}
	if _, err := s.requireView(ctx, metric.GoalID, userID); err != nil {
		return nil, 0, err
	}

	histories, total, err := s.goalRepo.ListMetricHistory(ctx, metricID, page, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list metric history: %w", err)
	}

	responses := make([]models.MetricHistoryResponse, len(histories))
	for i, h := range histories {
		responses[i] = h.ToResponse()
	}

	return responses, total, nil
}

// ──────────────────────────────────────────────────
// 14. CreateEvidence
// ──────────────────────────────────────────────────

func (s *goalService) CreateEvidence(ctx context.Context, goalID uuid.UUID, title string, evidenceType string, comment string, metricID *uuid.UUID, fileName string, fileSize int64, mimeType string, fileData []byte, userID uuid.UUID) (*models.EvidenceResponse, error) {
	// Existence (404) + access (403)
	goal, err := s.requireView(ctx, goalID, userID)
	if err != nil {
		return nil, err
	}
	if err := ensureGoalOpen(ctx, goal); err != nil {
		return nil, err
	}

	// Validate input before touching Documenta
	title = strings.TrimSpace(title)
	if title == "" {
		return nil, errBadRequest(ctx, "evidence_title_required")
	}
	if len([]rune(title)) > 255 {
		return nil, errBadRequest(ctx, "goal_title_too_long")
	}
	if !isValidEvidenceType(evidenceType) {
		return nil, errBadRequest(ctx, "evidence_invalid_type")
	}
	if strings.TrimSpace(comment) == "" {
		return nil, errBadRequest(ctx, "comment_is_mandatory")
	}
	if err := validateEvidenceFile(ctx, fileName, fileData); err != nil {
		return nil, err
	}
	var evidenceMetric *models.GoalMetric
	if metricID != nil {
		m, mErr := s.goalRepo.FindMetricByID(ctx, *metricID)
		if mErr != nil || m == nil || m.GoalID != goalID {
			return nil, errBadRequest(ctx, "evidence_metric_not_in_goal")
		}
		evidenceMetric = m
	}
	wf, initialState, wfErr := s.resolveGoalWorkflow(ctx, goalWfEvidence)
	if wfErr != nil || wf == nil || initialState == nil {
		return nil, fmt.Errorf("%s", i18n.T(ctx, "no_active_evidence_approval_workflow"))
	}

	// Determine upload folder (metric subfolder if metricID provided). If the goal's folder ID is missing (e.g. created before Documenta was enabled), lazily provision the hierarchy now so the upload can proceed.
	uploadFolderID := goal.DocumentaFolderID
	if uploadFolderID == "" {
		goalMgmtID, ensureErr := s.documentaClient.EnsureFolder(ctx, s.cfg.Documenta.WorkspaceName, "", "Goal Management")
		if ensureErr != nil {
			return nil, fmt.Errorf("failed to ensure Goal Management folder: %w", ensureErr)
		}
		newFolderID, createErr := s.documentaClient.CreateFolder(ctx, s.cfg.Documenta.WorkspaceName, goalMgmtID, goalFolderName(goal.Title, goal.ID))
		if createErr != nil {
			return nil, fmt.Errorf("failed to create goal folder for evidence upload: %w", createErr)
		}
		uploadFolderID = newFolderID
		// Persist the new folder ID so future uploads don't need to recreate it.
		if updateErr := s.goalRepo.UpdateDocumentaFolderID(ctx, goalID, newFolderID); updateErr != nil {
			log.Printf("[goal_service] CreateEvidence: failed to persist DocumentaFolderID for goal %s: %v", goalID, updateErr)
		}
	}
	var metricName string
	if evidenceMetric != nil {
		metricName = evidenceMetric.Name
		// Lazy-create metric subfolder under goal folder
		metricFolderID, ensureErr := s.documentaClient.EnsureFolder(
			ctx, s.cfg.Documenta.WorkspaceName, uploadFolderID, evidenceMetric.Name,
		)
		if ensureErr == nil {
			uploadFolderID = metricFolderID
		}
	}

	// Resolve uploader info for metadata
	uploader, _ := s.userRepo.FindByID(ctx, userID)
	uploaderEmail := "unknown"
	if uploader != nil {
		uploaderEmail = uploader.Email
	}

	// Resolve department name
	departmentName := ""
	if goal.DepartmentID != nil {
		dept, _ := s.departmentRepo.FindByID(ctx, *goal.DepartmentID)
		if dept != nil {
			departmentName = dept.Name
		}
	}

	// Build full 11-tag metadata
	metadata := map[string]string{
		"goal_id":       goal.ID.String(),
		"goal_title":    goal.Title,
		"goal_priority": goal.Priority,
		"goal_status":   goal.Status,
		"evidence_type": evidenceType,
		"uploaded_by":   uploaderEmail,
		"uploaded_at":   time.Now().UTC().Format(time.RFC3339),
		"source_system": "automax",
		"metric_id":     "",
		"metric_name":   "",
		"department":    departmentName,
	}
	if metricID != nil {
		metadata["metric_id"] = metricID.String()
		metadata["metric_name"] = metricName
	}

	fileReader := bytes.NewReader(fileData)
	documentaFileID, err := s.documentaClient.UploadFile(ctx, uploadFolderID, fileName, fileReader, fileSize, metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to upload file to documenta: %w", err)
	}

	// Persist metadata as searchable tags (upload-time metadata may not index)
	if tagErr := s.documentaClient.SetTags(ctx, documentaFileID, metadata); tagErr != nil {
		log.Printf("[goal_service] CreateEvidence: SetTags failed for file %s: %v", documentaFileID, tagErr)
	}

	evidence := &models.Evidence{
		GoalID:          goalID,
		MetricID:        metricID,
		Title:           title,
		EvidenceType:    evidenceType,
		Comment:         comment,
		Status:          models.EvidenceStatusDraft,
		DocumentaFileID: documentaFileID,
		FileName:        fileName,
		FileSize:        fileSize,
		MimeType:        mimeType,
		UploadedByID:    userID,
		WorkflowID:      &wf.ID,
		CurrentStateID:  &initialState.ID,
		Version:         1,
	}

	if err := s.goalRepo.CreateEvidence(ctx, evidence); err != nil {
		return nil, fmt.Errorf("failed to create evidence: %w", err)
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "create",
		Module:      "goals",
		ResourceID:  evidence.ID.String(),
		Description: fmt.Sprintf("Created evidence '%s' for goal: %s", evidence.Title, goal.Title),
		Status:      "success",
	})

	// WebSocket broadcast
	if s.wsHub != nil {
		s.wsHub.BroadcastToGoal(goalID, "evidence_created", map[string]interface{}{
			"evidence_id": evidence.ID,
			"goal_id":     goalID,
			"title":       evidence.Title,
		}, userID)
	}

	// Reload with relations for response
	evidenceWithRelations, reloadErr := s.goalRepo.FindEvidenceByIDWithRelations(ctx, evidence.ID)
	if reloadErr != nil {
		resp := evidence.ToResponse()
		return &resp, nil
	}
	resp := evidenceWithRelations.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// 15. ListEvidences
// ──────────────────────────────────────────────────

func (s *goalService) ListEvidences(ctx context.Context, goalID uuid.UUID, filter *models.EvidenceFilter, userID uuid.UUID) ([]models.EvidenceResponse, int64, error) {
	// Access check
	canAccess, _ := s.canAccessGoal(ctx, goalID, userID)
	if !canAccess {
		return nil, 0, fmt.Errorf("%s", i18n.T(ctx, "access_denied"))
	}

	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit < 1 {
		filter.Limit = 10
	}

	// Visibility gate: view-only callers see only terminal-approved evidence.
	if !s.canSeeUnapprovedItems(ctx, goalID, userID) {
		filter.ApprovedOnly = true
	}

	evidences, total, err := s.goalRepo.ListEvidencesByGoalID(ctx, goalID, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list evidences: %w", err)
	}

	responses := make([]models.EvidenceResponse, len(evidences))
	for i, e := range evidences {
		responses[i] = e.ToResponse()
	}

	return responses, total, nil
}

// ──────────────────────────────────────────────────
// 16. GetEvidence
// ──────────────────────────────────────────────────

func (s *goalService) GetEvidence(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*models.EvidenceResponse, error) {
	evidence, err := s.loadEvidenceForView(ctx, id, userID)
	if err != nil {
		return nil, err
	}

	resp := evidence.ToResponse()
	return &resp, nil
}

// loadEvidenceForView loads an evidence (404) and checks view access to its
// goal (403). View-only callers cannot see unapproved evidence (404, same as
// the list endpoint hiding it).
func (s *goalService) loadEvidenceForView(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*models.Evidence, error) {
	evidence, err := s.goalRepo.FindEvidenceByIDWithRelations(ctx, id)
	if err != nil {
		return nil, notFoundOr(ctx, err, "evidence_not_found")
	}
	if _, err := s.requireView(ctx, evidence.GoalID, userID); err != nil {
		return nil, err
	}
	if !s.canSeeUnapprovedItems(ctx, evidence.GoalID, userID) {
		if evidence.CurrentStateID != nil && (evidence.CurrentState == nil || evidence.CurrentState.StateType != "terminal") {
			return nil, errNotFound(ctx, "evidence_not_found")
		}
	}
	return evidence, nil
}

// ──────────────────────────────────────────────────
// 17. DeleteEvidence
// ──────────────────────────────────────────────────

func (s *goalService) DeleteEvidence(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	evidence, err := s.goalRepo.FindEvidenceByIDWithRelations(ctx, id)
	if err != nil {
		return notFoundOr(ctx, err, "evidence_not_found")
	}

	// Only the uploader (who can still see the goal) or someone who can
	// modify the goal may delete evidence.
	if err := s.requireEvidenceEdit(ctx, evidence, userID); err != nil {
		return err
	}

	// Block deletion only for Approved evidence
	if evidence.Status == models.EvidenceStatusApproved {
		return errConflict(ctx, "approved_evidence_cannot_be_deleted", evidence.Status)
	}

	// Delete file from Documenta
	if evidence.DocumentaFileID != "" {
		if delErr := s.documentaClient.DeleteFile(ctx, evidence.DocumentaFileID); delErr != nil {
			log.Printf("Warning: failed to delete file from Documenta: %v", delErr)
		}
	}

	if err := s.goalRepo.DeleteEvidence(ctx, id); err != nil {
		return fmt.Errorf("failed to delete evidence: %w", err)
	}

	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "delete",
		Module:      "goals",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("Deleted evidence '%s'", evidence.Title),
		Status:      "success",
	})

	return nil
}

// requireEvidenceEdit: uploader with view access to the goal, or anyone who
// can modify the goal (owner / reviewer collaborator / super admin).
func (s *goalService) requireEvidenceEdit(ctx context.Context, evidence *models.Evidence, userID uuid.UUID) error {
	goal, err := s.goalRepo.FindByID(ctx, evidence.GoalID)
	if err != nil {
		return notFoundOr(ctx, err, "goal_not_found")
	}
	if s.canModifyGoalObj(ctx, goal, userID) {
		return nil
	}
	if evidence.UploadedByID == userID && s.canAccessGoalObj(ctx, goal, userID) {
		return nil
	}
	return errForbidden(ctx)
}

// ──────────────────────────────────────────────────
// 17b. ReplaceEvidenceFile
// ──────────────────────────────────────────────────

func (s *goalService) ReplaceEvidenceFile(ctx context.Context, evidenceID uuid.UUID, fileName string, fileSize int64, mimeType string, fileData []byte, userID uuid.UUID) (*models.EvidenceResponse, error) {
	evidence, err := s.goalRepo.FindEvidenceByIDWithRelations(ctx, evidenceID)
	if err != nil {
		return nil, notFoundOr(ctx, err, "evidence_not_found")
	}
	if err := s.requireEvidenceEdit(ctx, evidence, userID); err != nil {
		return nil, err
	}

	// Only allow replacement in the workflow's initial (draft) state or
	// changes_requested.
	if evidence.CurrentState == nil || !(evidence.CurrentState.StateType == "initial" || evidence.CurrentState.Code == "draft" || evidence.CurrentState.Code == "changes_requested") {
		return nil, errConflict(ctx, "evidence_file_replace_invalid_state")
	}
	if err := validateEvidenceFile(ctx, fileName, fileData); err != nil {
		return nil, err
	}

	// Get goal for folder ID
	goal, err := s.goalRepo.FindByID(ctx, evidence.GoalID)
	if err != nil {
		return nil, notFoundOr(ctx, err, "goal_not_found")
	}
	oldFileID := evidence.DocumentaFileID

	// Determine upload folder (metric subfolder if evidence has metricID)
	uploadFolderID := goal.DocumentaFolderID
	var metricName string
	if evidence.MetricID != nil {
		metric, metricErr := s.goalRepo.FindMetricByID(ctx, *evidence.MetricID)
		if metricErr == nil && metric != nil {
			metricName = metric.Name
			metricFolderID, ensureErr := s.documentaClient.EnsureFolder(
				ctx, s.cfg.Documenta.WorkspaceName, goal.DocumentaFolderID, metric.Name,
			)
			if ensureErr == nil {
				uploadFolderID = metricFolderID
			}
		}
	}

	// Resolve uploader info
	uploader, _ := s.userRepo.FindByID(ctx, userID)
	uploaderEmail := "unknown"
	if uploader != nil {
		uploaderEmail = uploader.Email
	}

	// Resolve department name
	departmentName := ""
	if goal.DepartmentID != nil {
		dept, _ := s.departmentRepo.FindByID(ctx, *goal.DepartmentID)
		if dept != nil {
			departmentName = dept.Name
		}
	}

	// Build full 11-tag metadata
	metadata := map[string]string{
		"goal_id":       goal.ID.String(),
		"goal_title":    goal.Title,
		"goal_priority": goal.Priority,
		"goal_status":   goal.Status,
		"evidence_type": evidence.EvidenceType,
		"uploaded_by":   uploaderEmail,
		"uploaded_at":   time.Now().UTC().Format(time.RFC3339),
		"source_system": "automax",
		"metric_id":     "",
		"metric_name":   "",
		"department":    departmentName,
	}
	if evidence.MetricID != nil {
		metadata["metric_id"] = evidence.MetricID.String()
		metadata["metric_name"] = metricName
	}

	fileReader := bytes.NewReader(fileData)
	newFileID, err := s.documentaClient.UploadFile(ctx, uploadFolderID, fileName, fileReader, fileSize, metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to upload replacement file: %w", err)
	}

	// Persist metadata as searchable tags (upload-time metadata may not index)
	if tagErr := s.documentaClient.SetTags(ctx, newFileID, metadata); tagErr != nil {
		log.Printf("[goal_service] ReplaceEvidenceFile: SetTags failed for file %s: %v", newFileID, tagErr)
	}

	// Update evidence record
	evidence.DocumentaFileID = newFileID
	evidence.FileName = fileName
	evidence.FileSize = fileSize
	evidence.MimeType = mimeType
	evidence.Version++
	// Avoid GORM re-saving preloaded associations.
	evidence.Goal, evidence.Metric, evidence.UploadedBy, evidence.CurrentState, evidence.AssignedTo = nil, nil, nil, nil, nil

	if err := s.goalRepo.UpdateEvidence(ctx, evidence); err != nil {
		// Keep the old file; remove the orphaned new upload.
		if delErr := s.documentaClient.DeleteFile(ctx, newFileID); delErr != nil {
			log.Printf("Warning: failed to delete orphaned replacement file %s: %v", newFileID, delErr)
		}
		return nil, fmt.Errorf("failed to update evidence: %w", err)
	}

	// Only now that the new file is stored and referenced, delete the old one.
	if oldFileID != "" && oldFileID != newFileID {
		if delErr := s.documentaClient.DeleteFile(ctx, oldFileID); delErr != nil {
			log.Printf("Warning: failed to delete old file from Documenta: %v", delErr)
		}
	}

	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "update",
		Module:      "goals",
		ResourceID:  evidenceID.String(),
		Description: fmt.Sprintf("Replaced evidence file '%s' with '%s'", evidence.Title, fileName),
		Status:      "success",
	})

	// Reload with relations
	updated, reloadErr := s.goalRepo.FindEvidenceByIDWithRelations(ctx, evidenceID)
	if reloadErr != nil {
		resp := evidence.ToResponse()
		return &resp, nil
	}
	resp := updated.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// 18. GetAvailableEvidenceTransitions
// ──────────────────────────────────────────────────

func (s *goalService) GetAvailableEvidenceTransitions(ctx context.Context, evidenceID uuid.UUID, userID uuid.UUID) ([]models.AvailableTransitionResponse, error) {
	evidence, err := s.loadEvidenceForView(ctx, evidenceID, userID)
	if err != nil {
		return nil, err
	}

	if evidence.CurrentStateID == nil || evidence.WorkflowID == nil {
		return nil, errBadRequest(ctx, "evidence_no_workflow_assigned")
	}

	// Get all transitions from current state
	transitions, err := s.workflowRepo.ListTransitionsFromState(ctx, *evidence.CurrentStateID)
	if err != nil {
		return nil, fmt.Errorf("failed to get transitions: %w", err)
	}

	// Get collaborators to determine L2 reviewer existence
	collaborators, err := s.goalRepo.ListCollaborators(ctx, evidence.GoalID)
	if err != nil {
		return nil, fmt.Errorf("failed to list collaborators: %w", err)
	}

	var hasL2Reviewer bool
	for _, c := range collaborators {
		if c.Role == models.CollaboratorRoleReviewerL2 {
			hasL2Reviewer = true
			break
		}
	}

	var results []models.AvailableTransitionResponse
	for _, t := range transitions {
		canExecute := true
		reason := ""

		// Load requirements
		requirements, _ := s.workflowRepo.GetTransitionRequirements(ctx, t.ID)

		switch t.Code {
		case "submit", "resubmit":
			// Only the evidence uploader or goal owner can submit/resubmit
			goal, _ := s.goalRepo.FindByID(ctx, evidence.GoalID)
			if evidence.UploadedByID != userID && (goal == nil || goal.OwnerID != userID) {
				canExecute = false
				reason = "Only the uploader or goal owner can submit"
			}
		case "approve_l1":
			// Only show if L2 reviewer exists; only assigned reviewer can execute
			if !hasL2Reviewer {
				continue // skip this transition, show approve_l1_final instead
			}
			if evidence.AssignedToID == nil || *evidence.AssignedToID != userID {
				canExecute = false
				reason = "Only the assigned reviewer can approve"
			}
		case "approve_l1_final":
			// Only show if NO L2 reviewer; only assigned reviewer can execute
			if hasL2Reviewer {
				continue // skip this transition, show approve_l1 instead
			}
			if evidence.AssignedToID == nil || *evidence.AssignedToID != userID {
				canExecute = false
				reason = "Only the assigned reviewer can approve"
			}
		case "request_changes_l1", "reject_l1", "request_changes_l2", "reject_l2":
			// Only assigned reviewer can reject/request changes
			if evidence.AssignedToID == nil || *evidence.AssignedToID != userID {
				canExecute = false
				reason = "Only the assigned reviewer can perform this action"
			}
		case "approve_l2":
			if evidence.AssignedToID == nil || *evidence.AssignedToID != userID {
				canExecute = false
				reason = "Only the assigned L2 reviewer can approve"
			}
		case "assign_l1":
			// System transition — not available to users
			continue
		default:
			// Unknown transition codes (non-goal workflow): goal editors only.
			if !s.canModifyGoalObj(ctx, evidence.Goal, userID) {
				canExecute = false
				reason = "Only the goal owner or a reviewer can perform this action"
			}
		}

		// Build requirements response
		reqResponses := make([]models.TransitionRequirementResponse, len(requirements))
		for i, r := range requirements {
			reqResponses[i] = models.ToTransitionRequirementResponse(&r)
		}

		results = append(results, models.AvailableTransitionResponse{
			Transition:   models.ToWorkflowTransitionResponse(&t),
			CanExecute:   canExecute,
			Requirements: reqResponses,
			Reason:       reason,
		})
	}

	return results, nil
}

// ──────────────────────────────────────────────────
// 19. ExecuteEvidenceTransition
// ──────────────────────────────────────────────────

func (s *goalService) ExecuteEvidenceTransition(ctx context.Context, evidenceID uuid.UUID, req *models.EvidenceTransitionRequest, userID uuid.UUID) (*models.EvidenceResponse, error) {
	transitionID, err := uuid.Parse(req.TransitionID)
	if err != nil {
		return nil, errBadRequest(ctx, "invalid_transition_id_svc")
	}

	// Existence (404) + view access to the goal (403) before anything else.
	pre, err := s.goalRepo.FindEvidenceByID(ctx, evidenceID)
	if err != nil {
		return nil, notFoundOr(ctx, err, "evidence_not_found")
	}
	evGoal, err := s.requireView(ctx, pre.GoalID, userID)
	if err != nil {
		return nil, err
	}

	// Begin transaction
	tx := s.goalRepo.BeginTx(ctx)
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Pessimistic lock on evidence
	evidence, err := s.goalRepo.FindEvidenceByIDForUpdate(ctx, tx, evidenceID)
	if err != nil {
		tx.Rollback()
		return nil, notFoundOr(ctx, err, "evidence_not_found")
	}

	// Optimistic concurrency check
	if evidence.Version != req.Version {
		tx.Rollback()
		return nil, fmt.Errorf("%s", i18n.Tf(ctx, "record_modified_by_another_user", evidence.Version, req.Version))
	}

	if evidence.CurrentStateID == nil || evidence.WorkflowID == nil {
		tx.Rollback()
		return nil, fmt.Errorf("%s", i18n.T(ctx, "evidence_no_workflow_assigned"))
	}

	// Fetch transition with relations
	transition, err := s.workflowRepo.FindTransitionByIDWithRelations(ctx, transitionID)
	if err != nil {
		tx.Rollback()
		return nil, errBadRequest(ctx, "transition_not_found")
	}

	// Validate transition belongs to evidence's workflow
	if transition.WorkflowID != *evidence.WorkflowID {
		tx.Rollback()
		return nil, fmt.Errorf("%s", i18n.T(ctx, "transition_not_in_workflow"))
	}

	// Validate transition's from_state matches evidence's current state
	if transition.FromStateID != *evidence.CurrentStateID {
		tx.Rollback()
		return nil, fmt.Errorf("%s", i18n.T(ctx, "transition_invalid_from_state"))
	}

	// Authorization check
	switch transition.Code {
	case "submit", "resubmit":
		goal, _ := s.goalRepo.FindByID(ctx, evidence.GoalID)
		if evidence.UploadedByID != userID && (goal == nil || goal.OwnerID != userID) {
			tx.Rollback()
			return nil, newGoalErr(ctx, http.StatusForbidden, "only_uploader_or_owner_submit")
		}
	case "approve_l1", "approve_l1_final", "request_changes_l1", "reject_l1", "approve_l2", "request_changes_l2", "reject_l2":
		if evidence.AssignedToID == nil || *evidence.AssignedToID != userID {
			tx.Rollback()
			return nil, newGoalErr(ctx, http.StatusForbidden, "only_reviewer_action")
		}
	default:
		// Unknown transition codes (e.g. a non-goal workflow): goal editors only.
		if !s.canModifyGoalObj(ctx, evGoal, userID) {
			tx.Rollback()
			return nil, errForbidden(ctx)
		}
	}

	// Validate requirements (comment required for reject/request_changes)
	requirements, _ := s.workflowRepo.GetTransitionRequirements(ctx, transitionID)
	for _, r := range requirements {
		if r.RequirementType == "comment" && r.IsMandatory != nil && *r.IsMandatory {
			if strings.TrimSpace(req.Comment) == "" {
				tx.Rollback()
				errMsg := r.ErrorMessage
				if errMsg == "" {
					errMsg = i18n.T(ctx, "comment_required_for_transition")
				}
				return nil, fmt.Errorf("%s", errMsg)
			}
		}
	}

	fromStateID := *evidence.CurrentStateID

	// Create transition history record
	now := time.Now()
	history := &models.EvidenceTransitionHistory{
		EvidenceID:     evidenceID,
		TransitionID:   &transitionID,
		FromStateID:    fromStateID,
		ToStateID:      transition.ToStateID,
		PerformedByID:  userID,
		Comment:        req.Comment,
		IsSystemAction: false,
		TransitionedAt: now,
	}
	if err := s.goalRepo.CreateTransitionHistory(ctx, tx, history); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create transition history: %w", err)
	}

	// Update evidence state
	evidence.CurrentStateID = &transition.ToStateID
	evidence.Version++

	// Get the target state to update the Status string for backward compat
	targetState, _ := s.workflowRepo.FindStateByID(ctx, transition.ToStateID)
	if targetState != nil {
		switch targetState.Code {
		case "draft":
			evidence.Status = models.EvidenceStatusDraft
		case "submitted":
			evidence.Status = models.EvidenceStatusSubmitted
		case "l1_review", "l2_review":
			evidence.Status = models.EvidenceStatusInReview
		case "approved":
			evidence.Status = models.EvidenceStatusApproved
		case "rejected":
			evidence.Status = models.EvidenceStatusRejected
		case "changes_requested":
			evidence.Status = models.EvidenceStatusChangesRequested
		default:
			if targetState.StateType == "terminal" {
				if isGoalApprovalTransition(transition) {
					evidence.Status = models.EvidenceStatusApproved
				} else {
					evidence.Status = models.EvidenceStatusRejected
				}
			}
		}
	}

	// Assignment logic based on target state
	collaborators, _ := s.goalRepo.ListCollaborators(ctx, evidence.GoalID)

	switch transition.Code {
	case "submit", "resubmit":
		// Auto-transition to l1_review: find L1 reviewer and assign
		var reviewerL1 *models.GoalCollaborator
		for i := range collaborators {
			if collaborators[i].Role == models.CollaboratorRoleReviewerL1 {
				reviewerL1 = &collaborators[i]
				break
			}
		}
		if reviewerL1 == nil {
			tx.Rollback()
			return nil, fmt.Errorf("%s", i18n.T(ctx, "no_l1_reviewer_assigned"))
		}

		// Find the l1_review state to auto-advance
		l1State, _ := s.workflowRepo.FindStateByCode(ctx, *evidence.WorkflowID, "l1_review")
		if l1State != nil {
			// Create system transition history for the auto-advance
			autoHistory := &models.EvidenceTransitionHistory{
				EvidenceID:     evidenceID,
				FromStateID:    transition.ToStateID,
				ToStateID:      l1State.ID,
				PerformedByID:  userID,
				Comment:        "Auto-assigned to L1 reviewer",
				IsSystemAction: true,
				TransitionedAt: now,
			}
			s.goalRepo.CreateTransitionHistory(ctx, tx, autoHistory)

			evidence.CurrentStateID = &l1State.ID
			evidence.Status = models.EvidenceStatusInReview
		}

		evidence.AssignedToID = &reviewerL1.UserID

	case "approve_l1":
		// L1 approved → move to L2 review
		var reviewerL2 *models.GoalCollaborator
		for i := range collaborators {
			if collaborators[i].Role == models.CollaboratorRoleReviewerL2 {
				reviewerL2 = &collaborators[i]
				break
			}
		}
		if reviewerL2 != nil {
			evidence.AssignedToID = &reviewerL2.UserID
		}

	case "approve_l1_final", "approve_l2":
		// Final approval → clear assignment, recalculate progress
		evidence.AssignedToID = nil

	case "request_changes_l1", "request_changes_l2", "reject_l1", "reject_l2":
		// Clear assignment
		evidence.AssignedToID = nil
	}

	// Save evidence
	if err := s.goalRepo.UpdateEvidenceInTx(ctx, tx, evidence); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to update evidence: %w", err)
	}

	// Commit
	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Post-commit actions (notifications, progress recalc)
	goal, _ := s.goalRepo.FindByID(ctx, evidence.GoalID)
	goalTitle := "a goal"
	if goal != nil {
		goalTitle = goal.Title
	}

	switch transition.Code {
	case "submit", "resubmit":
		// Notify reviewer
		if evidence.AssignedToID != nil {
			reviewer, rErr := s.userRepo.FindByID(ctx, *evidence.AssignedToID)
			if rErr == nil && reviewer != nil {
				s.notificationService.SendNotification(ctx, "notification", nil, "en",
					[]string{reviewer.Email}, nil, nil,
					"Evidence Submitted for Review",
					fmt.Sprintf("Evidence '%s' has been submitted for your review on goal: %s", evidence.Title, goalTitle),
					nil, nil, &userID, nil)
			}
		}

	case "approve_l1":
		// Notify L2 reviewer
		if evidence.AssignedToID != nil {
			reviewer, rErr := s.userRepo.FindByID(ctx, *evidence.AssignedToID)
			if rErr == nil && reviewer != nil {
				s.notificationService.SendNotification(ctx, "notification", nil, "en",
					[]string{reviewer.Email}, nil, nil,
					"Evidence Pending L2 Review",
					fmt.Sprintf("Evidence '%s' for goal '%s' requires your L2 review", evidence.Title, goalTitle),
					nil, nil, &userID, nil)
			}
		}

	case "approve_l1_final", "approve_l2":
		// Recalculate goal progress
		_ = s.RecalculateProgress(ctx, evidence.GoalID)
		// Notify submitter
		submitter, sErr := s.userRepo.FindByID(ctx, evidence.UploadedByID)
		if sErr == nil && submitter != nil {
			s.notificationService.SendNotification(ctx, "notification", nil, "en",
				[]string{submitter.Email}, nil, nil,
				"Evidence Approved",
				fmt.Sprintf("Your evidence '%s' for goal '%s' has been approved", evidence.Title, goalTitle),
				nil, nil, &userID, nil)
		}

	case "reject_l1", "reject_l2":
		// Notify submitter
		submitter, sErr := s.userRepo.FindByID(ctx, evidence.UploadedByID)
		if sErr == nil && submitter != nil {
			s.notificationService.SendNotification(ctx, "notification", nil, "en",
				[]string{submitter.Email}, nil, nil,
				"Evidence Rejected",
				fmt.Sprintf("Your evidence '%s' for goal '%s' has been rejected. Comment: %s", evidence.Title, goalTitle, req.Comment),
				nil, nil, &userID, nil)
		}

	case "request_changes_l1", "request_changes_l2":
		// Notify submitter
		submitter, sErr := s.userRepo.FindByID(ctx, evidence.UploadedByID)
		if sErr == nil && submitter != nil {
			s.notificationService.SendNotification(ctx, "notification", nil, "en",
				[]string{submitter.Email}, nil, nil,
				"Evidence Returned for Changes",
				fmt.Sprintf("Your evidence '%s' for goal '%s' has been returned for changes. Comment: %s", evidence.Title, goalTitle, req.Comment),
				nil, nil, &userID, nil)
		}
	}

	// Audit log
	transitionName := transition.Name
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "transition",
		Module:      "goals",
		ResourceID:  evidenceID.String(),
		Description: fmt.Sprintf("Executed '%s' on evidence '%s' for goal: %s", transitionName, evidence.Title, goalTitle),
		Status:      "success",
	})

	// WebSocket broadcast
	if s.wsHub != nil {
		wsData := map[string]interface{}{
			"evidence_id": evidenceID,
			"goal_id":     evidence.GoalID,
			"status":      evidence.Status,
			"transition":  transitionName,
		}
		s.wsHub.BroadcastToGoal(evidence.GoalID, "evidence_transitioned", wsData, userID)
		s.wsHub.BroadcastGoalToAll("evidence_transitioned", wsData)
	}

	// Reload with relations
	evidenceWithRelations, reloadErr := s.goalRepo.FindEvidenceByIDWithRelations(ctx, evidenceID)
	if reloadErr != nil {
		resp := evidence.ToResponse()
		return &resp, nil
	}
	resp := evidenceWithRelations.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// 20. GetEvidenceTransitionHistory
// ──────────────────────────────────────────────────

func (s *goalService) GetEvidenceTransitionHistory(ctx context.Context, evidenceID uuid.UUID, userID uuid.UUID) ([]models.EvidenceTransitionHistoryResponse, error) {
	if _, err := s.loadEvidenceForView(ctx, evidenceID, userID); err != nil {
		return nil, err
	}
	histories, err := s.goalRepo.ListTransitionHistory(ctx, evidenceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get transition history: %w", err)
	}

	responses := make([]models.EvidenceTransitionHistoryResponse, len(histories))
	for i, h := range histories {
		responses[i] = h.ToResponse()
	}

	return responses, nil
}

// ──────────────────────────────────────────────────
// 21. ListPendingApprovals
// ──────────────────────────────────────────────────

func (s *goalService) ListPendingApprovals(ctx context.Context, userID uuid.UUID, page int, limit int) ([]models.ApprovalListResponse, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}

	evidences, total, err := s.goalRepo.ListPendingApprovals(ctx, userID, page, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list pending approvals: %w", err)
	}

	responses := make([]models.ApprovalListResponse, len(evidences))
	for i, ev := range evidences {
		resp := models.ApprovalListResponse{
			ID:              ev.ID,
			EvidenceID:      ev.ID,
			EvidenceTitle:   ev.Title,
			EvidenceVersion: ev.Version,
			Status:          ev.Status,
			AssignedTo:      models.ToUserBriefResponse(ev.AssignedTo),
			SubmittedBy:     models.ToUserBriefResponse(ev.UploadedBy),
			CreatedAt:       ev.CreatedAt,
			UpdatedAt:       ev.UpdatedAt,
		}

		if ev.CurrentState != nil {
			resp.StateName = ev.CurrentState.Name
			resp.StateColor = ev.CurrentState.Color
		}

		if ev.Goal != nil {
			resp.GoalID = ev.Goal.ID
			resp.GoalTitle = ev.Goal.Title
			resp.GoalPriority = ev.Goal.Priority
		}

		responses[i] = resp
	}

	return responses, total, nil
}

// ──────────────────────────────────────────────────
// 22. ListCompletedApprovals
// ──────────────────────────────────────────────────

func (s *goalService) ListCompletedApprovals(ctx context.Context, userID uuid.UUID, page int, limit int) ([]models.ApprovalListResponse, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}

	histories, total, err := s.goalRepo.ListCompletedApprovals(ctx, userID, page, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list completed approvals: %w", err)
	}

	responses := make([]models.ApprovalListResponse, len(histories))
	for i, h := range histories {
		resp := models.ApprovalListResponse{
			ID:        h.ID,
			CreatedAt: h.CreatedAt,
			UpdatedAt: h.TransitionedAt,
		}

		if h.PerformedBy != nil {
			resp.SubmittedBy = models.ToUserBriefResponse(h.PerformedBy)
		}

		if h.ToState != nil {
			resp.StateName = h.ToState.Name
			resp.StateColor = h.ToState.Color
			resp.Status = h.ToState.Name
		}

		if h.Evidence != nil {
			resp.EvidenceID = h.Evidence.ID
			resp.EvidenceTitle = h.Evidence.Title
			resp.AssignedTo = models.ToUserBriefResponse(h.Evidence.AssignedTo)

			if h.Evidence.Goal != nil {
				resp.GoalID = h.Evidence.Goal.ID
				resp.GoalTitle = h.Evidence.Goal.Title
				resp.GoalPriority = h.Evidence.Goal.Priority
			}
		}

		responses[i] = resp
	}

	return responses, total, nil
}

// ════════════════════════════════════════════════════
// METRIC APPROVAL WORKFLOW
// ════════════════════════════════════════════════════

// GetAvailableMetricTransitions mirrors GetAvailableEvidenceTransitions for metrics.
func (s *goalService) GetAvailableMetricTransitions(ctx context.Context, metricID uuid.UUID, userID uuid.UUID) ([]models.AvailableTransitionResponse, error) {
	metric, err := s.goalRepo.FindMetricByIDWithRelations(ctx, metricID)
	if err != nil {
		return nil, notFoundOr(ctx, err, "metric_not_found")
	}
	if _, err := s.requireView(ctx, metric.GoalID, userID); err != nil {
		return nil, err
	}

	if metric.CurrentStateID == nil || metric.WorkflowID == nil {
		return nil, errBadRequest(ctx, "metric_no_workflow_assigned")
	}

	transitions, err := s.workflowRepo.ListTransitionsFromState(ctx, *metric.CurrentStateID)
	if err != nil {
		return nil, fmt.Errorf("failed to get transitions: %w", err)
	}

	collaborators, err := s.goalRepo.ListCollaborators(ctx, metric.GoalID)
	if err != nil {
		return nil, fmt.Errorf("failed to list collaborators: %w", err)
	}

	var hasL2Reviewer bool
	for _, c := range collaborators {
		if c.Role == models.CollaboratorRoleReviewerL2 {
			hasL2Reviewer = true
			break
		}
	}

	results := s.buildAvailableTransitions(ctx, transitions, metric.GoalID, metric.AssignedToID, userID, hasL2Reviewer)
	return results, nil
}

// GetAvailableMetricValueChangeTransitions mirrors GetAvailableEvidenceTransitions for value changes.
func (s *goalService) GetAvailableMetricValueChangeTransitions(ctx context.Context, changeID uuid.UUID, userID uuid.UUID) ([]models.AvailableTransitionResponse, error) {
	change, err := s.goalRepo.FindMetricValueChangeByIDWithRelations(ctx, changeID)
	if err != nil {
		return nil, notFoundOr(ctx, err, "metric_value_change_not_found")
	}
	if change.Metric == nil {
		return nil, errNotFound(ctx, "orphaned_metric_value_change")
	}
	if _, err := s.requireView(ctx, change.Metric.GoalID, userID); err != nil {
		return nil, err
	}

	transitions, err := s.workflowRepo.ListTransitionsFromState(ctx, change.CurrentStateID)
	if err != nil {
		return nil, fmt.Errorf("failed to get transitions: %w", err)
	}

	collaborators, err := s.goalRepo.ListCollaborators(ctx, change.Metric.GoalID)
	if err != nil {
		return nil, fmt.Errorf("failed to list collaborators: %w", err)
	}

	var hasL2Reviewer bool
	for _, c := range collaborators {
		if c.Role == models.CollaboratorRoleReviewerL2 {
			hasL2Reviewer = true
			break
		}
	}

	results := s.buildAvailableTransitions(ctx, transitions, change.Metric.GoalID, change.AssignedToID, userID, hasL2Reviewer)
	return results, nil
}

// buildAvailableTransitions implements the same gating logic used for evidence
// (submit/approve_l1/approve_l2/etc.) for any record participating in the
// shared 7-state workflow.
func (s *goalService) buildAvailableTransitions(ctx context.Context, transitions []models.WorkflowTransition, goalID uuid.UUID, assignedToID *uuid.UUID, userID uuid.UUID, hasL2Reviewer bool) []models.AvailableTransitionResponse {
	var results []models.AvailableTransitionResponse
	for _, t := range transitions {
		canExecute := true
		reason := ""

		requirements, _ := s.workflowRepo.GetTransitionRequirements(ctx, t.ID)

		switch t.Code {
		case "submit", "resubmit":
			goal, _ := s.goalRepo.FindByID(ctx, goalID)
			if goal == nil || (goal.OwnerID != userID && !s.isSuperAdmin(ctx)) {
				canExecute = false
				reason = "Only the goal owner can submit"
			}
		case "approve_l1":
			if !hasL2Reviewer {
				continue
			}
			if assignedToID == nil || *assignedToID != userID {
				canExecute = false
				reason = "Only the assigned reviewer can approve"
			}
		case "approve_l1_final":
			if hasL2Reviewer {
				continue
			}
			if assignedToID == nil || *assignedToID != userID {
				canExecute = false
				reason = "Only the assigned reviewer can approve"
			}
		case "request_changes_l1", "reject_l1", "request_changes_l2", "reject_l2":
			if assignedToID == nil || *assignedToID != userID {
				canExecute = false
				reason = "Only the assigned reviewer can perform this action"
			}
		case "approve_l2":
			if assignedToID == nil || *assignedToID != userID {
				canExecute = false
				reason = "Only the assigned L2 reviewer can approve"
			}
		case "assign_l1":
			continue
		default:
			if ok, _ := s.canModifyGoal(ctx, goalID, userID); !ok {
				canExecute = false
				reason = "Only the goal owner or a reviewer can perform this action"
			}
		}

		reqResponses := make([]models.TransitionRequirementResponse, len(requirements))
		for i, r := range requirements {
			reqResponses[i] = models.ToTransitionRequirementResponse(&r)
		}

		results = append(results, models.AvailableTransitionResponse{
			Transition:   models.ToWorkflowTransitionResponse(&t),
			CanExecute:   canExecute,
			Requirements: reqResponses,
			Reason:       reason,
		})
	}
	return results
}

// TransitionMetric runs a workflow transition on a metric definition.
func (s *goalService) TransitionMetric(ctx context.Context, metricID uuid.UUID, req *models.MetricTransitionRequest, userID uuid.UUID) (*models.GoalMetricResponse, error) {
	transitionID, err := uuid.Parse(req.TransitionID)
	if err != nil {
		return nil, errBadRequest(ctx, "invalid_transition_id_svc")
	}

	pre, err := s.goalRepo.FindMetricByID(ctx, metricID)
	if err != nil {
		return nil, notFoundOr(ctx, err, "metric_not_found")
	}
	mGoal, err := s.requireView(ctx, pre.GoalID, userID)
	if err != nil {
		return nil, err
	}

	tx := s.goalRepo.BeginTx(ctx)
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	metric, err := s.goalRepo.FindMetricByIDForUpdate(ctx, tx, metricID)
	if err != nil {
		tx.Rollback()
		return nil, notFoundOr(ctx, err, "metric_not_found")
	}

	if req.Version > 0 && metric.Version != req.Version {
		tx.Rollback()
		return nil, fmt.Errorf("%s", i18n.Tf(ctx, "record_modified_by_another_user", metric.Version, req.Version))
	}

	if metric.CurrentStateID == nil || metric.WorkflowID == nil {
		tx.Rollback()
		return nil, fmt.Errorf("%s", i18n.T(ctx, "metric_no_workflow_assigned"))
	}

	transition, err := s.workflowRepo.FindTransitionByIDWithRelations(ctx, transitionID)
	if err != nil {
		tx.Rollback()
		return nil, errBadRequest(ctx, "transition_not_found")
	}

	if transition.WorkflowID != *metric.WorkflowID {
		tx.Rollback()
		return nil, fmt.Errorf("%s", i18n.T(ctx, "transition_not_in_workflow"))
	}
	if transition.FromStateID != *metric.CurrentStateID {
		tx.Rollback()
		return nil, fmt.Errorf("%s", i18n.T(ctx, "transition_invalid_from_state"))
	}

	// Authorization
	switch transition.Code {
	case "submit", "resubmit":
		goal, _ := s.goalRepo.FindByID(ctx, metric.GoalID)
		if !s.isSuperAdmin(ctx) && (goal == nil || goal.OwnerID != userID) {
			tx.Rollback()
			return nil, newGoalErr(ctx, http.StatusForbidden, "only_owner_can_submit")
		}
	case "approve_l1", "approve_l1_final", "request_changes_l1", "reject_l1", "approve_l2", "request_changes_l2", "reject_l2":
		if metric.AssignedToID == nil || *metric.AssignedToID != userID {
			tx.Rollback()
			return nil, newGoalErr(ctx, http.StatusForbidden, "only_reviewer_action")
		}
	default:
		if !s.canModifyGoalObj(ctx, mGoal, userID) {
			tx.Rollback()
			return nil, errForbidden(ctx)
		}
	}

	// Validate requirements (mandatory comment for reject/request_changes)
	requirements, _ := s.workflowRepo.GetTransitionRequirements(ctx, transitionID)
	for _, r := range requirements {
		if r.RequirementType == "comment" && r.IsMandatory != nil && *r.IsMandatory {
			if strings.TrimSpace(req.Comment) == "" {
				tx.Rollback()
				errMsg := r.ErrorMessage
				if errMsg == "" {
					errMsg = i18n.T(ctx, "comment_required_for_transition")
				}
				return nil, fmt.Errorf("%s", errMsg)
			}
		}
	}

	fromStateID := *metric.CurrentStateID
	now := time.Now()

	history := &models.MetricTransitionHistory{
		MetricID:       metricID,
		TransitionID:   &transitionID,
		FromStateID:    fromStateID,
		ToStateID:      transition.ToStateID,
		PerformedByID:  userID,
		Comment:        req.Comment,
		IsSystemAction: false,
		TransitionedAt: now,
	}
	if err := s.goalRepo.CreateMetricTransitionHistory(ctx, tx, history); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create transition history: %w", err)
	}

	metric.CurrentStateID = &transition.ToStateID
	metric.Version++

	collaborators, _ := s.goalRepo.ListCollaborators(ctx, metric.GoalID)
	switch transition.Code {
	case "submit", "resubmit":
		var reviewerL1 *models.GoalCollaborator
		for i := range collaborators {
			if collaborators[i].Role == models.CollaboratorRoleReviewerL1 {
				reviewerL1 = &collaborators[i]
				break
			}
		}
		if reviewerL1 == nil {
			tx.Rollback()
			return nil, fmt.Errorf("%s", i18n.T(ctx, "no_l1_reviewer_assigned"))
		}
		l1State, _ := s.workflowRepo.FindStateByCode(ctx, *metric.WorkflowID, "l1_review")
		if l1State != nil {
			autoHistory := &models.MetricTransitionHistory{
				MetricID:       metricID,
				FromStateID:    transition.ToStateID,
				ToStateID:      l1State.ID,
				PerformedByID:  userID,
				Comment:        "Auto-assigned to L1 reviewer",
				IsSystemAction: true,
				TransitionedAt: now,
			}
			s.goalRepo.CreateMetricTransitionHistory(ctx, tx, autoHistory)
			metric.CurrentStateID = &l1State.ID
		}
		metric.AssignedToID = &reviewerL1.UserID
	case "approve_l1":
		var reviewerL2 *models.GoalCollaborator
		for i := range collaborators {
			if collaborators[i].Role == models.CollaboratorRoleReviewerL2 {
				reviewerL2 = &collaborators[i]
				break
			}
		}
		if reviewerL2 != nil {
			metric.AssignedToID = &reviewerL2.UserID
		}
	case "approve_l1_final", "approve_l2":
		metric.AssignedToID = nil
	case "request_changes_l1", "request_changes_l2", "reject_l1", "reject_l2":
		metric.AssignedToID = nil
	}

	if err := s.goalRepo.UpdateMetricInTx(ctx, tx, metric); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to update metric: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Approved/rejected metric definitions change what counts towards progress.
	if transition.ToState != nil && transition.ToState.StateType == "terminal" {
		if err := s.RecalculateProgress(ctx, metric.GoalID); err != nil {
			log.Printf("[goal_service] TransitionMetric: progress recalc failed: %v", err)
		}
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "transition",
		Module:      "goals",
		ResourceID:  metricID.String(),
		Description: fmt.Sprintf("Executed '%s' on metric '%s'", transition.Name, metric.Name),
		Status:      "success",
	})

	if s.wsHub != nil {
		s.wsHub.BroadcastToGoal(metric.GoalID, "metric_transitioned", map[string]interface{}{
			"metric_id":  metricID,
			"goal_id":    metric.GoalID,
			"transition": transition.Name,
		}, userID)
	}

	reloaded, rerr := s.goalRepo.FindMetricByIDWithRelations(ctx, metricID)
	if rerr == nil {
		resp := reloaded.ToResponse()
		return &resp, nil
	}
	resp := metric.ToResponse()
	return &resp, nil
}

// TransitionMetricValueChange runs a workflow transition on a value change.
// On a transition into a terminal-approved state the parent metric's
// CurrentValue is updated and AppliedAt is set.
func (s *goalService) TransitionMetricValueChange(ctx context.Context, changeID uuid.UUID, req *models.MetricTransitionRequest, userID uuid.UUID) (*models.GoalMetricValueChangeResponse, error) {
	transitionID, err := uuid.Parse(req.TransitionID)
	if err != nil {
		return nil, errBadRequest(ctx, "invalid_transition_id_svc")
	}

	pre, err := s.goalRepo.FindMetricValueChangeByIDWithRelations(ctx, changeID)
	if err != nil {
		return nil, notFoundOr(ctx, err, "metric_value_change_not_found")
	}
	if pre.Metric == nil {
		return nil, errNotFound(ctx, "orphaned_metric_value_change")
	}
	vcGoal, err := s.requireView(ctx, pre.Metric.GoalID, userID)
	if err != nil {
		return nil, err
	}

	tx := s.goalRepo.BeginTx(ctx)
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	change, err := s.goalRepo.FindMetricValueChangeByIDForUpdate(ctx, tx, changeID)
	if err != nil {
		tx.Rollback()
		return nil, notFoundOr(ctx, err, "metric_value_change_not_found")
	}

	if req.Version > 0 && change.Version != req.Version {
		tx.Rollback()
		return nil, fmt.Errorf("%s", i18n.Tf(ctx, "record_modified_by_another_user", change.Version, req.Version))
	}

	transition, err := s.workflowRepo.FindTransitionByIDWithRelations(ctx, transitionID)
	if err != nil {
		tx.Rollback()
		return nil, errBadRequest(ctx, "transition_not_found")
	}
	if transition.WorkflowID != change.WorkflowID {
		tx.Rollback()
		return nil, fmt.Errorf("%s", i18n.T(ctx, "transition_not_in_workflow"))
	}
	if transition.FromStateID != change.CurrentStateID {
		tx.Rollback()
		return nil, fmt.Errorf("%s", i18n.T(ctx, "transition_invalid_from_state"))
	}

	// Need parent metric for goal/owner lookups + applying value
	metric, mErr := s.goalRepo.FindMetricByID(ctx, change.MetricID)
	if mErr != nil {
		tx.Rollback()
		return nil, fmt.Errorf("%s: %w", i18n.T(ctx, "parent_metric_not_found"), mErr)
	}

	// Authorization
	switch transition.Code {
	case "submit", "resubmit":
		goal, _ := s.goalRepo.FindByID(ctx, metric.GoalID)
		if change.SubmittedByID != userID && !s.isSuperAdmin(ctx) && (goal == nil || goal.OwnerID != userID) {
			tx.Rollback()
			return nil, newGoalErr(ctx, http.StatusForbidden, "only_submitter_or_owner_submit")
		}
	case "approve_l1", "approve_l1_final", "request_changes_l1", "reject_l1", "approve_l2", "request_changes_l2", "reject_l2":
		if change.AssignedToID == nil || *change.AssignedToID != userID {
			tx.Rollback()
			return nil, newGoalErr(ctx, http.StatusForbidden, "only_reviewer_action")
		}
	default:
		if !s.canModifyGoalObj(ctx, vcGoal, userID) {
			tx.Rollback()
			return nil, errForbidden(ctx)
		}
	}

	requirements, _ := s.workflowRepo.GetTransitionRequirements(ctx, transitionID)
	for _, r := range requirements {
		if r.RequirementType == "comment" && r.IsMandatory != nil && *r.IsMandatory {
			if strings.TrimSpace(req.Comment) == "" {
				tx.Rollback()
				errMsg := r.ErrorMessage
				if errMsg == "" {
					errMsg = i18n.T(ctx, "comment_required_for_transition")
				}
				return nil, fmt.Errorf("%s", errMsg)
			}
		}
	}

	fromStateID := change.CurrentStateID
	now := time.Now()

	history := &models.MetricValueChangeTransitionHistory{
		MetricValueChangeID: changeID,
		TransitionID:        &transitionID,
		FromStateID:         fromStateID,
		ToStateID:           transition.ToStateID,
		PerformedByID:       userID,
		Comment:             req.Comment,
		IsSystemAction:      false,
		TransitionedAt:      now,
	}
	if err := s.goalRepo.CreateMetricValueChangeTransitionHistory(ctx, tx, history); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create transition history: %w", err)
	}

	change.CurrentStateID = transition.ToStateID
	change.Version++

	collaborators, _ := s.goalRepo.ListCollaborators(ctx, metric.GoalID)
	applied := false

	switch transition.Code {
	case "submit", "resubmit":
		var reviewerL1 *models.GoalCollaborator
		for i := range collaborators {
			if collaborators[i].Role == models.CollaboratorRoleReviewerL1 {
				reviewerL1 = &collaborators[i]
				break
			}
		}
		if reviewerL1 == nil {
			tx.Rollback()
			return nil, fmt.Errorf("%s", i18n.T(ctx, "no_l1_reviewer_assigned"))
		}
		l1State, _ := s.workflowRepo.FindStateByCode(ctx, change.WorkflowID, "l1_review")
		if l1State != nil {
			autoHistory := &models.MetricValueChangeTransitionHistory{
				MetricValueChangeID: changeID,
				FromStateID:         transition.ToStateID,
				ToStateID:           l1State.ID,
				PerformedByID:       userID,
				Comment:             "Auto-assigned to L1 reviewer",
				IsSystemAction:      true,
				TransitionedAt:      now,
			}
			s.goalRepo.CreateMetricValueChangeTransitionHistory(ctx, tx, autoHistory)
			change.CurrentStateID = l1State.ID
		}
		change.AssignedToID = &reviewerL1.UserID
	case "approve_l1":
		var reviewerL2 *models.GoalCollaborator
		for i := range collaborators {
			if collaborators[i].Role == models.CollaboratorRoleReviewerL2 {
				reviewerL2 = &collaborators[i]
				break
			}
		}
		if reviewerL2 != nil {
			change.AssignedToID = &reviewerL2.UserID
		}
	case "request_changes_l1", "request_changes_l2", "reject_l1", "reject_l2":
		change.AssignedToID = nil
	}

	// Any approval into a terminal state applies the proposed value (once):
	// the dedicated approve_l1_final/approve_l2 codes, or a non-rejection
	// transition into a terminal state of another goal workflow.
	if isGoalApprovalTransition(transition) && change.AppliedAt == nil {
		// Apply the proposed value to the parent metric.
		change.AssignedToID = nil
		change.AppliedAt = &now

		oldValue := metric.CurrentValue
		metric.CurrentValue = change.ProposedValue
		if err := tx.Model(&models.GoalMetric{}).Where("id = ?", metric.ID).
			Update("current_value", change.ProposedValue).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to apply value to metric: %w", err)
		}

		// Append a MetricHistory record so the change shows in the per-metric
		// history endpoint alongside legacy entries.
		histEntry := &models.MetricHistory{
			MetricID:    metric.ID,
			OldValue:    oldValue,
			NewValue:    change.ProposedValue,
			ChangedByID: userID,
			Comment:     fmt.Sprintf("Approved value change %s", changeID.String()[:8]),
		}
		if err := tx.Create(histEntry).Error; err != nil {
			log.Printf("[goal_service] TransitionMetricValueChange: history insert failed: %v", err)
		}
		applied = true
	}

	if err := s.goalRepo.UpdateMetricValueChangeInTx(ctx, tx, change); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to update value change: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Post-commit: cascade formula recompute & progress
	if applied {
		s.recomputeDependentFormulas(ctx, metric.GoalID, metric.ID)
		if err := s.RecalculateProgress(ctx, metric.GoalID); err != nil {
			log.Printf("[goal_service] TransitionMetricValueChange: progress recalc failed: %v", err)
		}
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "transition",
		Module:      "goals",
		ResourceID:  changeID.String(),
		Description: fmt.Sprintf("Executed '%s' on metric value change for '%s'", transition.Name, metric.Name),
		Status:      "success",
	})

	if s.wsHub != nil {
		s.wsHub.BroadcastToGoal(metric.GoalID, "metric_value_change_transitioned", map[string]interface{}{
			"change_id":  changeID,
			"metric_id":  metric.ID,
			"goal_id":    metric.GoalID,
			"transition": transition.Name,
			"applied":    applied,
		}, userID)
	}

	reloaded, rerr := s.goalRepo.FindMetricValueChangeByIDWithRelations(ctx, changeID)
	if rerr == nil {
		resp := reloaded.ToResponse()
		return &resp, nil
	}
	resp := change.ToResponse()
	return &resp, nil
}

// ListMetricValueChanges returns all value changes for a given metric.
func (s *goalService) ListMetricValueChanges(ctx context.Context, metricID uuid.UUID, userID uuid.UUID) ([]models.GoalMetricValueChangeResponse, error) {
	metric, err := s.goalRepo.FindMetricByID(ctx, metricID)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(ctx, "metric_not_found"), err)
	}
	canAccess, _ := s.canAccessGoal(ctx, metric.GoalID, userID)
	if !canAccess {
		return nil, fmt.Errorf("%s", i18n.T(ctx, "access_denied"))
	}

	changes, err := s.goalRepo.ListMetricValueChangesByMetricID(ctx, metricID)
	if err != nil {
		return nil, fmt.Errorf("failed to list value changes: %w", err)
	}

	responses := make([]models.GoalMetricValueChangeResponse, len(changes))
	for i, c := range changes {
		responses[i] = c.ToResponse()
	}
	return responses, nil
}

// ListPendingMetricApprovals lists metric definitions awaiting the caller's approval.
func (s *goalService) ListPendingMetricApprovals(ctx context.Context, userID uuid.UUID, page int, limit int) ([]models.MetricApprovalListResponse, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}

	metrics, total, err := s.goalRepo.ListPendingMetricApprovals(ctx, userID, page, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list pending metric approvals: %w", err)
	}

	responses := make([]models.MetricApprovalListResponse, len(metrics))
	for i, m := range metrics {
		resp := models.MetricApprovalListResponse{
			ID:         m.ID,
			MetricID:   m.ID,
			MetricName: m.Name,
			Version:    m.Version,
			AssignedTo: models.ToUserBriefResponse(m.AssignedTo),
			CreatedAt:  m.CreatedAt,
			UpdatedAt:  m.UpdatedAt,
		}
		if m.CurrentState != nil {
			resp.StateName = m.CurrentState.Name
			resp.StateColor = m.CurrentState.Color
		}
		if m.Goal != nil {
			resp.GoalID = m.Goal.ID
			resp.GoalTitle = m.Goal.Title
			resp.GoalPriority = m.Goal.Priority
		}
		responses[i] = resp
	}

	return responses, total, nil
}

// ListPendingMetricValueChangeApprovals lists value changes awaiting the caller's approval.
func (s *goalService) ListPendingMetricValueChangeApprovals(ctx context.Context, userID uuid.UUID, page int, limit int) ([]models.MetricApprovalListResponse, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}

	changes, total, err := s.goalRepo.ListPendingMetricValueChangeApprovals(ctx, userID, page, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list pending value change approvals: %w", err)
	}

	responses := make([]models.MetricApprovalListResponse, len(changes))
	for i, c := range changes {
		changeID := c.ID
		proposed := c.ProposedValue
		previous := c.PreviousValue
		resp := models.MetricApprovalListResponse{
			ID:            c.ID,
			ChangeID:      &changeID,
			MetricID:      c.MetricID,
			ProposedValue: &proposed,
			PreviousValue: &previous,
			Version:       c.Version,
			SubmittedBy:   models.ToUserBriefResponse(c.SubmittedBy),
			AssignedTo:    models.ToUserBriefResponse(c.AssignedTo),
			CreatedAt:     c.CreatedAt,
			UpdatedAt:     c.UpdatedAt,
		}
		if c.CurrentState != nil {
			resp.StateName = c.CurrentState.Name
			resp.StateColor = c.CurrentState.Color
		}
		if c.Metric != nil {
			resp.MetricName = c.Metric.Name
			if c.Metric.Goal != nil {
				resp.GoalID = c.Metric.Goal.ID
				resp.GoalTitle = c.Metric.Goal.Title
				resp.GoalPriority = c.Metric.Goal.Priority
			}
		}
		responses[i] = resp
	}

	return responses, total, nil
}

// ──────────────────────────────────────────────────
// 21. RecalculateProgress
// ──────────────────────────────────────────────────

func (s *goalService) RecalculateProgress(ctx context.Context, goalID uuid.UUID) error {
	return s.recalculateProgress(ctx, goalID, 0)
}

// recalculateProgress recomputes a goal's progress from its approved metrics
// and its child goals (see computeGoalProgress), then walks up to the parent.
func (s *goalService) recalculateProgress(ctx context.Context, goalID uuid.UUID, depth int) error {
	if depth > 10 {
		return nil // corrupt hierarchy safety net
	}
	metrics, err := s.goalRepo.ListMetricsByGoalID(ctx, goalID)
	if err != nil {
		return fmt.Errorf("failed to list metrics: %w", err)
	}

	goal, err := s.goalRepo.FindByID(ctx, goalID)
	if err != nil {
		return notFoundOr(ctx, err, "goal_not_found")
	}

	children, err := s.goalRepo.FindChildren(ctx, goalID)
	if err != nil {
		return fmt.Errorf("failed to find children for progress: %w", err)
	}
	childProgress := make([]float64, len(children))
	for i, c := range children {
		childProgress[i] = c.Progress
	}

	progress := computeGoalProgress(metrics, childProgress)
	if err := s.goalRepo.UpdateProgress(ctx, goalID, progress); err != nil {
		return err
	}

	// Cascade to the parent goal
	if goal.ParentGoalID != nil {
		if _, pErr := s.goalRepo.FindByID(ctx, *goal.ParentGoalID); pErr == nil {
			return s.recalculateProgress(ctx, *goal.ParentGoalID, depth+1)
		}
	}
	return nil
}

// ──────────────────────────────────────────────────
// 22. GetEvidencePreview
// ──────────────────────────────────────────────────

func (s *goalService) GetEvidencePreview(ctx context.Context, evidenceID uuid.UUID, userID uuid.UUID) (string, error) {
	evidence, err := s.loadEvidenceForView(ctx, evidenceID, userID)
	if err != nil {
		return "", err
	}

	previewURL, err := s.documentaClient.GetPreviewURL(ctx, evidence.DocumentaFileID)
	if err != nil {
		return "", fmt.Errorf("failed to get preview URL: %w", err)
	}

	return previewURL, nil
}

// ──────────────────────────────────────────────────
// 23. GetEvidenceDownloadURL
// ──────────────────────────────────────────────────

func (s *goalService) GetEvidenceDownloadURL(ctx context.Context, evidenceID uuid.UUID, userID uuid.UUID) (string, error) {
	evidence, err := s.loadEvidenceForView(ctx, evidenceID, userID)
	if err != nil {
		return "", err
	}

	downloadURL, err := s.documentaClient.GetDownloadURL(ctx, evidence.DocumentaFileID)
	if err != nil {
		return "", fmt.Errorf("failed to get download URL: %w", err)
	}

	return downloadURL, nil
}

// DownloadEvidenceFile proxies the evidence bytes from Documenta through the
// Automax backend. Returns the body reader plus the DmsFile metadata, a best-
// guess content type, and a filename for Content-Disposition. The caller owns
// the reader and must Close it.
//
// Replaces the redirect-based flow in GetEvidenceDownloadURL so the HTTP
// handler can stream bytes back to the client directly — this keeps downloads
// working under deployments that use a path prefix (VITE_BASE_PATH) where a
// root-relative 307 Location header would resolve to the wrong URL.
func (s *goalService) DownloadEvidenceFile(ctx context.Context, evidenceID uuid.UUID, userID uuid.UUID) (io.ReadCloser, *storage.DmsFile, string, string, error) {
	evidence, err := s.loadEvidenceForView(ctx, evidenceID, userID)
	if err != nil {
		return nil, nil, "", "", err
	}

	reader, info, err := s.documentaClient.DownloadFile(ctx, evidence.DocumentaFileID)
	if err != nil {
		return nil, nil, "", "", fmt.Errorf("failed to download evidence: %w", err)
	}

	contentType := ""
	if info != nil && info.MimeType != "" {
		contentType = info.MimeType
	} else if evidence.MimeType != "" {
		contentType = evidence.MimeType
	}

	filename := evidence.FileName
	if filename == "" && info != nil {
		filename = info.Name
	}

	return reader, info, contentType, filename, nil
}

// ──────────────────────────────────────────────────
// Export Goals
// ──────────────────────────────────────────────────

func (s *goalService) ExportGoalsCSV(ctx context.Context, filter *models.GoalFilter) ([]byte, error) {
	goals, err := s.goalRepo.ListForExport(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list goals for export: %w", err)
	}

	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	// Header row
	header := []string{
		"ID", "Title", "Description", "Category", "Priority", "Status",
		"Owner", "Department", "Start Date", "Target Date", "Review Date",
		"Progress", "Metric Name", "Metric Type", "Metric Unit",
		"Baseline Value", "Current Value", "Target Value", "Weight",
	}
	if err := writer.Write(header); err != nil {
		return nil, fmt.Errorf("failed to write CSV header: %w", err)
	}

	for _, g := range goals {
		if len(g.Metrics) == 0 {
			row := goalToCSVRow(&g, nil)
			if err := writer.Write(row); err != nil {
				return nil, fmt.Errorf("failed to write CSV row: %w", err)
			}
		} else {
			for _, m := range g.Metrics {
				row := goalToCSVRow(&g, &m)
				if err := writer.Write(row); err != nil {
					return nil, fmt.Errorf("failed to write CSV row: %w", err)
				}
			}
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("CSV writer error: %w", err)
	}

	return buf.Bytes(), nil
}

func goalToCSVRow(g *models.Goal, m *models.GoalMetric) []string {
	ownerName := ""
	if g.Owner != nil {
		ownerName = g.Owner.Email
	}
	deptName := ""
	if g.Department != nil {
		deptName = g.Department.Name
	}
	startDate := ""
	if g.StartDate != nil {
		startDate = g.StartDate.Format("2006-01-02")
	}
	targetDate := ""
	if g.TargetDate != nil {
		targetDate = g.TargetDate.Format("2006-01-02")
	}
	reviewDate := ""
	if g.ReviewDate != nil {
		reviewDate = g.ReviewDate.Format("2006-01-02")
	}

	row := []string{
		g.ID.String(), g.Title, g.Description, g.Category, g.Priority, g.Status,
		ownerName, deptName, startDate, targetDate, reviewDate,
		fmt.Sprintf("%.1f", g.Progress),
	}

	if m != nil {
		row = append(row,
			m.Name, m.MetricType, m.Unit,
			fmt.Sprintf("%.2f", m.BaselineValue),
			fmt.Sprintf("%.2f", m.CurrentValue),
			fmt.Sprintf("%.2f", m.TargetValue),
			fmt.Sprintf("%.2f", m.Weight),
		)
	} else {
		row = append(row, "", "", "", "", "", "", "")
	}

	return row
}

func (s *goalService) ExportGoalsJSON(ctx context.Context, filter *models.GoalFilter) ([]models.GoalResponse, error) {
	goals, err := s.goalRepo.ListForExport(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list goals for export: %w", err)
	}

	responses := make([]models.GoalResponse, len(goals))
	for i, g := range goals {
		responses[i] = g.ToResponse()
	}

	return responses, nil
}

// ──────────────────────────────────────────────────
// Clone Goal
// ──────────────────────────────────────────────────

func (s *goalService) CloneGoal(ctx context.Context, id uuid.UUID, req *models.GoalCloneRequest, userID uuid.UUID) (*models.GoalResponse, error) {
	// Must be able to view the source goal.
	if _, err := s.requireView(ctx, id, userID); err != nil {
		var ge *GoalError
		if errors.As(err, &ge) && ge.Status == http.StatusNotFound {
			return nil, errNotFound(ctx, "source_goal_not_found")
		}
		return nil, err
	}
	source, err := s.goalRepo.FindByIDWithRelations(ctx, id)
	if err != nil {
		return nil, notFoundOr(ctx, err, "source_goal_not_found")
	}

	// Determine title
	title := "Copy of " + source.Title
	if strings.TrimSpace(req.Title) != "" {
		title = strings.TrimSpace(req.Title)
	}
	if len([]rune(title)) > 255 {
		return nil, errBadRequest(ctx, "goal_title_too_long")
	}

	// Determine owner
	ownerID := source.OwnerID
	if req.OwnerID != nil {
		if u, uErr := s.userRepo.FindByID(ctx, *req.OwnerID); uErr != nil || u == nil {
			return nil, errBadRequest(ctx, "goal_owner_not_found")
		}
		ownerID = *req.OwnerID
	}
	if existing, exErr := s.goalRepo.FindGoalByTitleAndOwner(ctx, title, ownerID); exErr == nil && existing != nil {
		return nil, ErrGoalAlreadyExists
	}

	// Determine dates
	startDate := source.StartDate
	if req.StartDate != nil {
		startDate = req.StartDate
	}
	targetDate := source.TargetDate
	if req.TargetDate != nil {
		targetDate = req.TargetDate
	}
	reviewDate := source.ReviewDate
	if req.ReviewDate != nil {
		reviewDate = req.ReviewDate
	}

	if startDate != nil && targetDate != nil && startDate.After(*targetDate) {
		return nil, errBadRequest(ctx, "goal_start_after_target")
	}

	// Create the clone's Documenta folder (unique name); fail clearly instead
	// of creating a goal that can never receive evidence.
	cloneID := uuid.New()
	folderID, err := s.createGoalFolder(ctx, title, cloneID)
	if err != nil {
		return nil, err
	}

	clone := &models.Goal{
		ID:                cloneID,
		Title:             title,
		Description:       source.Description,
		Category:          source.Category,
		Priority:          source.Priority,
		Status:            models.GoalStatusDraft,
		OwnerID:           ownerID,
		DepartmentID:      source.DepartmentID,
		StartDate:         startDate,
		TargetDate:        targetDate,
		ReviewDate:        reviewDate,
		Progress:          0,
		DocumentaFolderID: folderID,
		Metadata:          source.Metadata,
		CreatedByID:       userID,
		CategoryID:        source.CategoryID,
		ParentGoalID:      source.ParentGoalID,
		Level:             source.Level,
	}

	if err := s.goalRepo.Create(ctx, clone); err != nil {
		return nil, fmt.Errorf("failed to create cloned goal: %w", err)
	}
	// Materialized path (sibling of the source)
	if source.ParentGoalID != nil && source.Path != "" && strings.Contains(source.Path, ".") {
		clone.Path = source.Path[:strings.LastIndex(source.Path, ".")] + "." + clone.ID.String()
	} else {
		clone.Path = clone.ID.String()
	}
	if err := s.goalRepo.Update(ctx, clone); err != nil {
		return nil, fmt.Errorf("failed to set cloned goal path: %w", err)
	}

	// Clone metrics (reset current value to baseline). Cloned metric
	// definitions start in the metric workflow's initial state, like any
	// newly created metric.
	mwf, minitial, _ := s.resolveGoalWorkflow(ctx, goalWfMetric)
	for _, m := range source.Metrics {
		clonedMetric := &models.GoalMetric{
			GoalID:        clone.ID,
			Name:          m.Name,
			MetricType:    m.MetricType,
			Unit:          m.Unit,
			BaselineValue: m.BaselineValue,
			CurrentValue:  m.BaselineValue,
			TargetValue:   m.TargetValue,
			Weight:        m.Weight,
			Formula:       m.Formula,
			Version:       1,
		}
		if mwf != nil && minitial != nil {
			clonedMetric.WorkflowID = &mwf.ID
			clonedMetric.CurrentStateID = &minitial.ID
		}
		if err := s.goalRepo.CreateMetric(ctx, clonedMetric); err != nil {
			log.Printf("[GoalService] CloneGoal: failed to clone metric %s: %v", m.Name, err)
		}
	}

	// Clone collaborators
	for _, c := range source.Collaborators {
		clonedCollab := &models.GoalCollaborator{
			GoalID: clone.ID,
			UserID: c.UserID,
			Role:   c.Role,
		}
		if err := s.goalRepo.AddCollaborator(ctx, clonedCollab); err != nil {
			log.Printf("[GoalService] CloneGoal: failed to clone collaborator %s: %v", c.UserID, err)
		}
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "clone",
		Module:      "goals",
		ResourceID:  clone.ID.String(),
		Description: fmt.Sprintf("Cloned goal from %s: %s", source.ID.String(), title),
		Status:      "success",
	})

	cloneWithRelations, err := s.goalRepo.FindByIDWithRelations(ctx, clone.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch cloned goal: %w", err)
	}

	resp := cloneWithRelations.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// ImportGoals
// ──────────────────────────────────────────────────

func (s *goalService) ImportGoals(ctx context.Context, fileData []byte, fileName string, dryRun bool, userID uuid.UUID) (*models.GoalImportResponse, error) {
	dot := strings.LastIndex(fileName, ".")
	if dot < 0 {
		return nil, errBadRequest(ctx, "unsupported_file_type", fileName)
	}
	ext := strings.ToLower(fileName[dot:])

	var rows [][]string
	var err error
	switch ext {
	case ".csv":
		rows, err = s.parseCSV(fileData)
	case ".xlsx":
		rows, err = s.parseXLSX(ctx, fileData)
	default:
		return nil, errBadRequest(ctx, "unsupported_file_type", ext)
	}
	if err != nil {
		return nil, newGoalErr(ctx, http.StatusBadRequest, "failed_to_parse_file_detail", err.Error())
	}

	if len(rows) < 2 {
		return nil, errBadRequest(ctx, "file_missing_header_or_data")
	}

	// Skip header row
	dataRows := rows[1:]

	type parsedMetric struct {
		name      string
		mtype     string
		unit      string
		baseline  float64
		current   float64
		target    float64
		weight    float64
		rowNumber int
	}
	type parsedGoal struct {
		existing    *models.Goal // non-nil → update via ID column
		title       string
		description string
		category    string
		priority    string
		ownerID     uuid.UUID
		deptID      *uuid.UUID
		startDate   *time.Time
		targetDate  *time.Time
		reviewDate  *time.Time
		metrics     []parsedMetric
		metricNames map[string]bool
		rows        []int // indexes into rowResults
	}

	parseNum := func(raw string, def float64) (float64, bool) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return def, true
		}
		v, perr := strconv.ParseFloat(raw, 64)
		if perr != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return 0, false
		}
		return v, true
	}
	parseDate := func(raw string) (*time.Time, bool) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return nil, true
		}
		t, perr := time.Parse("2006-01-02", raw)
		if perr != nil {
			return nil, false
		}
		return &t, true
	}

	goalMap := make(map[string]*parsedGoal)
	var goalOrder []string
	rowResults := make([]models.ImportRowResult, 0, len(dataRows))
	ownerCache := map[string]*uuid.UUID{}

	for i, row := range dataRows {
		rowNum := i + 2 // 1-indexed, skip header
		result := models.ImportRowResult{RowNumber: rowNum, Status: "valid"}
		addErr := func(msg string) {
			result.Status = "error"
			result.Errors = append(result.Errors, msg)
		}

		// Skip completely blank lines
		blank := true
		for _, c := range row {
			if strings.TrimSpace(c) != "" {
				blank = false
				break
			}
		}
		if blank {
			continue
		}

		// Pad row to expected column count (19 columns matching export format)
		for len(row) < 19 {
			row = append(row, "")
		}

		// Columns: 0=ID, 1=Title, 2=Description, 3=Category, 4=Priority, 5=Status,
		// 6=Owner, 7=Department, 8=Start Date, 9=Target Date, 10=Review Date, 11=Progress,
		// 12=Metric Name, 13=Metric Type, 14=Metric Unit, 15=Baseline Value,
		// 16=Current Value, 17=Target Value, 18=Weight
		title := strings.TrimSpace(row[1])
		if title == "" {
			result.GoalTitle = "(empty)"
			addErr("Title is required")
			rowResults = append(rowResults, result)
			continue
		}
		result.GoalTitle = title
		if len([]rune(title)) > 255 {
			addErr("Title must be at most 255 characters")
		}

		// Existing goal via ID column → update
		var existing *models.Goal
		if idStr := strings.TrimSpace(row[0]); idStr != "" {
			gid, perr := uuid.Parse(idStr)
			if perr != nil {
				addErr(fmt.Sprintf("Invalid ID: %s", idStr))
			} else if g, gErr := s.goalRepo.FindByID(ctx, gid); gErr != nil {
				addErr(fmt.Sprintf("Goal ID not found: %s", idStr))
			} else if !s.canModifyGoalObj(ctx, g, userID) {
				addErr("You do not have permission to update this goal")
			} else if g.Status == models.GoalStatusClosed {
				addErr("Closed goals cannot be modified")
			} else {
				existing = g
			}
		}

		description := strings.TrimSpace(row[2])
		if description == "" {
			addErr("Description is required")
		}

		// Validate priority
		priority := strings.TrimSpace(row[4])
		if priority == "" {
			priority = models.GoalPriorityMedium
			result.Warnings = append(result.Warnings, "Priority defaulted to Medium")
		} else if !models.IsValidGoalPriority(priority) {
			addErr(fmt.Sprintf("Invalid priority: %s (must be Critical, High, Medium, or Low)", priority))
		}

		// Validate + resolve owner
		var ownerID uuid.UUID
		ownerStr := strings.TrimSpace(row[6])
		if ownerStr == "" {
			addErr("Owner is required")
		} else {
			key := strings.ToLower(ownerStr)
			cached, ok := ownerCache[key]
			if !ok {
				var u *models.User
				if oid, uErr := uuid.Parse(ownerStr); uErr == nil {
					u, _ = s.userRepo.FindByID(ctx, oid)
				} else if strings.Contains(ownerStr, "@") {
					u, _ = s.userRepo.FindByEmail(ctx, ownerStr)
				}
				if u != nil {
					id := u.ID
					cached = &id
				}
				ownerCache[key] = cached
			}
			if cached == nil {
				addErr(fmt.Sprintf("Owner not found (must be an existing user's UUID or email): %s", ownerStr))
			} else {
				ownerID = *cached
			}
		}

		// Validate department (warning only, as before)
		var deptID *uuid.UUID
		deptStr := strings.TrimSpace(row[7])
		if deptStr != "" {
			if did, dErr := uuid.Parse(deptStr); dErr == nil {
				if dept, fErr := s.departmentRepo.FindByID(ctx, did); fErr == nil && dept != nil {
					deptID = &dept.ID
				}
			} else if dept, fErr := s.departmentRepo.FindByNameAndParent(ctx, deptStr, nil); fErr == nil && dept != nil {
				deptID = &dept.ID
			}
			if deptID == nil {
				result.Warnings = append(result.Warnings, fmt.Sprintf("Department not found: %s", deptStr))
			}
		}

		// Validate dates
		startDate, ok1 := parseDate(row[8])
		if !ok1 {
			addErr(fmt.Sprintf("Invalid start date format: %s (use YYYY-MM-DD)", row[8]))
		}
		targetDate, ok2 := parseDate(row[9])
		if !ok2 {
			addErr(fmt.Sprintf("Invalid target date format: %s (use YYYY-MM-DD)", row[9]))
		}
		reviewDate, ok3 := parseDate(row[10])
		if !ok3 {
			addErr(fmt.Sprintf("Invalid review date format: %s (use YYYY-MM-DD)", row[10]))
		}
		if startDate != nil && targetDate != nil && startDate.After(*targetDate) {
			addErr("Start date must be on or before the target date")
		}

		// Validate metric fields (if present)
		var pm *parsedMetric
		metricName := strings.TrimSpace(row[12])
		metricType := strings.TrimSpace(row[13])
		if metricName != "" {
			if metricType == "" {
				metricType = models.MetricTypeNumeric
				result.Warnings = append(result.Warnings, "Metric type defaulted to Numeric")
			} else if !models.IsValidMetricType(metricType) {
				addErr(fmt.Sprintf("Invalid metric type: %s (must be Numeric, Percentage, Currency, or Boolean)", metricType))
			}
			baseline, okB := parseNum(row[15], 0)
			if !okB {
				addErr(fmt.Sprintf("Invalid baseline value: %s", row[15]))
			}
			current, okC := parseNum(row[16], baseline)
			if !okC {
				addErr(fmt.Sprintf("Invalid current value: %s", row[16]))
			}
			var target float64
			if strings.TrimSpace(row[17]) == "" {
				addErr("Metric target value is required")
			} else if t, okT := parseNum(row[17], 0); !okT {
				addErr(fmt.Sprintf("Invalid target value: %s", row[17]))
			} else {
				target = t
			}
			weight, okW := parseNum(row[18], 1)
			if !okW {
				addErr(fmt.Sprintf("Invalid weight: %s", row[18]))
			} else if weight <= 0 {
				addErr(fmt.Sprintf("Weight must be greater than 0: %s", row[18]))
			}
			pm = &parsedMetric{
				name: metricName, mtype: metricType, unit: strings.TrimSpace(row[14]),
				baseline: baseline, current: current, target: target, weight: weight, rowNumber: rowNum,
			}
		}

		// Group key: existing goal ID, else title+owner
		key := strings.ToLower(title) + "\x00" + ownerID.String()
		if existing != nil {
			key = "id:" + existing.ID.String()
		}
		pg, exists := goalMap[key]
		if !exists && result.Status != "error" {
			// New goal (no ID) duplicating an existing title+owner → reject
			if existing == nil {
				if dup, dErr := s.goalRepo.FindGoalByTitleAndOwner(ctx, title, ownerID); dErr == nil && dup != nil {
					addErr(fmt.Sprintf("A goal titled %q already exists for this owner (ID %s); fill the ID column to update it", title, dup.ID))
				}
			}
		}
		if pg != nil && pm != nil && pg.metricNames[strings.ToLower(pm.name)] {
			addErr(fmt.Sprintf("Duplicate metric name %q for this goal", pm.name))
		}

		if len(result.Warnings) > 0 && result.Status == "valid" {
			result.Status = "warning"
		}
		rowResults = append(rowResults, result)

		if result.Status == "error" {
			continue
		}
		if pg == nil {
			pg = &parsedGoal{
				existing:    existing,
				title:       title,
				description: description,
				category:    strings.TrimSpace(row[3]),
				priority:    priority,
				ownerID:     ownerID,
				deptID:      deptID,
				startDate:   startDate,
				targetDate:  targetDate,
				reviewDate:  reviewDate,
				metricNames: map[string]bool{},
			}
			goalMap[key] = pg
			goalOrder = append(goalOrder, key)
		}
		if pm != nil {
			pg.metrics = append(pg.metrics, *pm)
			pg.metricNames[strings.ToLower(pm.name)] = true
		}
	}

	// Count results
	errorCount, warningCount, validCount, metricsCount := 0, 0, 0, 0
	for _, r := range rowResults {
		switch r.Status {
		case "error":
			errorCount++
		case "warning":
			warningCount++
		default:
			validCount++
		}
	}
	for _, g := range goalMap {
		metricsCount += len(g.metrics)
	}

	mode := "dry_run"
	var createdGoalIDs, updatedGoalIDs []string

	if !dryRun && errorCount == 0 {
		mode = "committed"
		mwf, minitial, _ := s.resolveGoalWorkflow(ctx, goalWfMetric)
		newMetric := func(goalID uuid.UUID, pm parsedMetric) *models.GoalMetric {
			m := &models.GoalMetric{
				ID: uuid.New(), GoalID: goalID, Name: pm.name, MetricType: pm.mtype, Unit: pm.unit,
				BaselineValue: pm.baseline, CurrentValue: pm.current, TargetValue: pm.target,
				Weight: pm.weight, Version: 1,
			}
			// Same initial workflow state as manually created metrics.
			if mwf != nil && minitial != nil {
				m.WorkflowID = &mwf.ID
				m.CurrentStateID = &minitial.ID
			}
			return m
		}

		// Provision Documenta folders for new goals first (outside the DB tx);
		// abort with a clear error if any fails.
		newIDs := map[string]uuid.UUID{}
		folderIDs := map[string]string{}
		for _, key := range goalOrder {
			pg := goalMap[key]
			if pg.existing != nil {
				continue
			}
			id := uuid.New()
			folderID, fErr := s.createGoalFolder(ctx, pg.title, id)
			if fErr != nil {
				return nil, fErr
			}
			newIDs[key] = id
			folderIDs[key] = folderID
		}

		tx := s.goalRepo.BeginTx(ctx)
		defer func() {
			if r := recover(); r != nil {
				tx.Rollback()
			}
		}()

		for _, key := range goalOrder {
			pg := goalMap[key]

			if pg.existing != nil {
				g := pg.existing
				updates := map[string]interface{}{
					"title": pg.title, "description": pg.description, "category": pg.category,
					"priority": pg.priority, "owner_id": pg.ownerID, "start_date": pg.startDate,
					"target_date": pg.targetDate, "review_date": pg.reviewDate,
				}
				if pg.deptID != nil {
					updates["department_id"] = *pg.deptID
				}
				if err := tx.Model(&models.Goal{}).Where("id = ?", g.ID).Updates(updates).Error; err != nil {
					tx.Rollback()
					return nil, fmt.Errorf("failed to update goal %q: %w", pg.title, err)
				}
				// Upsert metric definitions by name. Current values are NOT
				// overwritten — value changes go through the approval workflow.
				existingMetrics, _ := s.goalRepo.ListMetricsByGoalID(ctx, g.ID)
				byName := map[string]models.GoalMetric{}
				for _, em := range existingMetrics {
					byName[strings.ToLower(strings.TrimSpace(em.Name))] = em
				}
				for _, pm := range pg.metrics {
					if em, ok := byName[strings.ToLower(pm.name)]; ok {
						if err := tx.Model(&models.GoalMetric{}).Where("id = ?", em.ID).Updates(map[string]interface{}{
							"metric_type": pm.mtype, "unit": pm.unit, "baseline_value": pm.baseline,
							"target_value": pm.target, "weight": pm.weight,
						}).Error; err != nil {
							tx.Rollback()
							return nil, fmt.Errorf("failed to update metric %q for goal %q: %w", pm.name, pg.title, err)
						}
						continue
					}
					if err := tx.Create(newMetric(g.ID, pm)).Error; err != nil {
						tx.Rollback()
						return nil, fmt.Errorf("failed to create metric %q for goal %q: %w", pm.name, pg.title, err)
					}
				}
				updatedGoalIDs = append(updatedGoalIDs, g.ID.String())
				continue
			}

			id := newIDs[key]
			goal := &models.Goal{
				ID:                id,
				Title:             pg.title,
				Description:       pg.description,
				Category:          pg.category,
				Priority:          pg.priority,
				Status:            models.GoalStatusDraft,
				OwnerID:           pg.ownerID,
				DepartmentID:      pg.deptID,
				StartDate:         pg.startDate,
				TargetDate:        pg.targetDate,
				ReviewDate:        pg.reviewDate,
				Progress:          0,
				Path:              id.String(),
				DocumentaFolderID: folderIDs[key],
				Metadata:          "{}",
				CreatedByID:       userID,
			}
			if err := tx.WithContext(ctx).Create(goal).Error; err != nil {
				tx.Rollback()
				return nil, fmt.Errorf("failed to create goal %q: %w", pg.title, err)
			}
			if userID != pg.ownerID {
				if err := tx.Create(&models.GoalCollaborator{GoalID: id, UserID: userID, Role: models.CollaboratorRoleCollaborator}).Error; err != nil {
					log.Printf("[GoalService] ImportGoals: add importer as collaborator: %v", err)
				}
			}
			for _, pm := range pg.metrics {
				if err := tx.WithContext(ctx).Create(newMetric(id, pm)).Error; err != nil {
					tx.Rollback()
					return nil, fmt.Errorf("failed to create metric %q for goal %q: %w", pm.name, pg.title, err)
				}
			}
			createdGoalIDs = append(createdGoalIDs, id.String())
		}

		if err := tx.Commit().Error; err != nil {
			return nil, fmt.Errorf("failed to commit import transaction: %w", err)
		}

		// Recalculate progress for all touched goals (same rule as create/update)
		for _, idStr := range append(append([]string{}, createdGoalIDs...), updatedGoalIDs...) {
			gid, _ := uuid.Parse(idStr)
			_ = s.RecalculateProgress(ctx, gid)
		}

		// Audit log
		s.actionLogService.LogAction(ctx, &LogActionParams{
			UserID:      userID,
			Action:      "import",
			Module:      "goals",
			ResourceID:  fmt.Sprintf("%d goals", len(createdGoalIDs)+len(updatedGoalIDs)),
			Description: fmt.Sprintf("Imported %d new / %d updated goals with %d metrics from %s", len(createdGoalIDs), len(updatedGoalIDs), metricsCount, fileName),
			Status:      "success",
		})
	}

	return &models.GoalImportResponse{
		Mode:           mode,
		TotalRows:      len(rowResults),
		GoalsCount:     len(goalOrder),
		MetricsCount:   metricsCount,
		ValidCount:     validCount,
		ErrorCount:     errorCount,
		WarningCount:   warningCount,
		Rows:           rowResults,
		CreatedGoalIDs: createdGoalIDs,
		UpdatedGoalIDs: updatedGoalIDs,
	}, nil
}

func (s *goalService) parseCSV(data []byte) ([][]string, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	reader.FieldsPerRecord = -1 // Allow variable fields
	return reader.ReadAll()
}

func (s *goalService) parseXLSX(ctx context.Context, data []byte) ([][]string, error) {
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer f.Close()

	sheetName := f.GetSheetName(0)
	if sheetName == "" {
		return nil, fmt.Errorf("%s", i18n.T(ctx, "no_sheets_in_xlsx"))
	}

	xlRows, err := f.GetRows(sheetName)
	if err != nil {
		return nil, err
	}

	return xlRows, nil
}

// ──────────────────────────────────────────────────
// BulkAction
// ──────────────────────────────────────────────────

func (s *goalService) BulkAction(ctx context.Context, req *models.BulkActionRequest, userID uuid.UUID) (*models.BulkActionResponse, error) {
	results := make([]models.BulkActionItemResult, len(req.GoalIDs))
	successCount := 0
	failureCount := 0

	for i, goalID := range req.GoalIDs {
		var actionErr error

		switch req.Action {
		case "transition":
			if req.NewStatus == "" {
				actionErr = fmt.Errorf("%s", i18n.T(ctx, "new_status_required_transition"))
			} else {
				_, actionErr = s.TransitionGoalStatus(ctx, goalID, req.NewStatus, userID)
			}

		case "reassign":
			if req.NewOwnerID == nil {
				actionErr = fmt.Errorf("%s", i18n.T(ctx, "new_owner_id_required_reassign"))
			} else {
				ownerID := *req.NewOwnerID
				_, actionErr = s.UpdateGoal(ctx, goalID, &models.GoalUpdateRequest{
					OwnerID: &ownerID,
				}, userID)
			}

		case "close":
			_, actionErr = s.TransitionGoalStatus(ctx, goalID, models.GoalStatusClosed, userID)

		default:
			actionErr = fmt.Errorf("%s", i18n.Tf(ctx, "unknown_bulk_action", req.Action))
		}

		result := models.BulkActionItemResult{
			GoalID:  goalID,
			Success: actionErr == nil,
		}
		if actionErr != nil {
			result.Error = actionErr.Error()
			failureCount++
		} else {
			successCount++
		}
		results[i] = result
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "bulk_" + req.Action,
		Module:      "goals",
		ResourceID:  fmt.Sprintf("%d goals", len(req.GoalIDs)),
		Description: fmt.Sprintf("Bulk %s: %d succeeded, %d failed", req.Action, successCount, failureCount),
		Status:      "success",
	})

	return &models.BulkActionResponse{
		TotalRequested: len(req.GoalIDs),
		SuccessCount:   successCount,
		FailureCount:   failureCount,
		Results:        results,
	}, nil
}

// ──────────────────────────────────────────────────
// Hierarchy: GetGoalTree
// ──────────────────────────────────────────────────

func (s *goalService) GetGoalTree(ctx context.Context, rootID uuid.UUID, userID uuid.UUID) (*models.GoalResponse, error) {
	if _, err := s.requireView(ctx, rootID, userID); err != nil {
		return nil, err
	}
	goal, err := s.goalRepo.FindByIDWithRelations(ctx, rootID)
	if err != nil {
		return nil, notFoundOr(ctx, err, "goal_not_found")
	}

	// Recursively load children for each child (depth-first)
	if err := s.loadChildrenRecursive(ctx, goal, 0, userID); err != nil {
		return nil, err
	}

	resp := goal.ToResponse()
	return &resp, nil
}

// visibleChildren returns the children the caller can view (all for super admins).
func (s *goalService) visibleChildren(ctx context.Context, parentID uuid.UUID, userID uuid.UUID) ([]models.Goal, error) {
	children, err := s.goalRepo.FindChildren(ctx, parentID)
	if err != nil {
		return nil, err
	}
	if s.isSuperAdmin(ctx) {
		return children, nil
	}
	out := make([]models.Goal, 0, len(children))
	for i := range children {
		if s.canAccessGoalObj(ctx, &children[i], userID) {
			out = append(out, children[i])
		}
	}
	return out, nil
}

func (s *goalService) loadChildrenRecursive(ctx context.Context, goal *models.Goal, depth int, userID uuid.UUID) error {
	if depth > 3 {
		return nil // Safety limit
	}

	children, err := s.visibleChildren(ctx, goal.ID, userID)
	if err != nil {
		return err
	}

	goal.Children = children
	for i := range goal.Children {
		if err := s.loadChildrenRecursive(ctx, &goal.Children[i], depth+1, userID); err != nil {
			return err
		}
	}
	return nil
}

// ──────────────────────────────────────────────────
// Hierarchy: ListChildGoals
// ──────────────────────────────────────────────────

func (s *goalService) ListChildGoals(ctx context.Context, parentID uuid.UUID, userID uuid.UUID) ([]models.GoalResponse, error) {
	if _, err := s.requireView(ctx, parentID, userID); err != nil {
		return nil, err
	}
	children, err := s.visibleChildren(ctx, parentID, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list child goals: %w", err)
	}

	responses := make([]models.GoalResponse, len(children))
	for i, child := range children {
		responses[i] = child.ToResponse()
	}
	return responses, nil
}

// ──────────────────────────────────────────────────
// Check-in: CreateCheckIn
// ──────────────────────────────────────────────────

func (s *goalService) CreateCheckIn(ctx context.Context, goalID uuid.UUID, req *models.CheckInCreateRequest, userID uuid.UUID) (*models.CheckInResponse, error) {
	// Existence (404) + access (403)
	goal, err := s.requireView(ctx, goalID, userID)
	if err != nil {
		return nil, err
	}
	if err := ensureGoalOpen(ctx, goal); err != nil {
		return nil, err
	}

	// Validate check-in status
	if !models.IsValidCheckInStatus(req.Status) {
		return nil, errBadRequest(ctx, "invalid_check_in_status", req.Status)
	}

	// Proposing metric values requires edit rights on the goal (same rule as
	// PUT /goals/metrics/:id/value).
	if len(req.MetricUpdates) > 0 && !s.canModifyGoalObj(ctx, goal, userID) {
		return nil, errForbidden(ctx)
	}

	// Validate ALL metric updates before writing anything.
	type metricChange struct {
		MetricID string  `json:"metric_id"`
		Name     string  `json:"name"`
		OldValue float64 `json:"old_value"`
		NewValue float64 `json:"new_value"`
	}
	var changes []metricChange
	var valueChanges []*models.GoalMetricValueChange
	seen := map[uuid.UUID]bool{}
	for _, mu := range req.MetricUpdates {
		if mu.Value == nil {
			return nil, errBadRequest(ctx, "value_required")
		}
		if seen[mu.MetricID] {
			return nil, errBadRequest(ctx, "check_in_metric_not_in_goal", mu.MetricID.String())
		}
		seen[mu.MetricID] = true
		metric, mErr := s.goalRepo.FindMetricByID(ctx, mu.MetricID)
		if mErr != nil {
			return nil, errBadRequest(ctx, "check_in_metric_not_found", mu.MetricID.String())
		}
		if metric.GoalID != goalID {
			return nil, errBadRequest(ctx, "check_in_metric_not_in_goal", mu.MetricID.String())
		}
		vc, vErr := s.buildMetricValueChange(ctx, metric, *mu.Value, mu.Comment, userID)
		if vErr != nil {
			return nil, vErr
		}
		valueChanges = append(valueChanges, vc)
		changes = append(changes, metricChange{
			MetricID: mu.MetricID.String(),
			Name:     metric.Name,
			OldValue: metric.CurrentValue,
			NewValue: vc.ProposedValue,
		})
	}

	// Serialize metric changes to JSON
	metricUpdatesJSON := "[]"
	if len(changes) > 0 {
		jsonBytes, err := json.Marshal(changes)
		if err != nil {
			return nil, fmt.Errorf("failed to serialize metric updates: %w", err)
		}
		metricUpdatesJSON = string(jsonBytes)
	}

	checkIn := &models.GoalCheckIn{
		GoalID:        goalID,
		AuthorID:      userID,
		Status:        req.Status,
		Content:       req.Content,
		ProgressSnap:  goal.Progress,
		MetricUpdates: metricUpdatesJSON,
	}

	// Value-change proposals + the check-in are written atomically.
	tx := s.goalRepo.BeginTx(ctx)
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()
	for _, vc := range valueChanges {
		if err := tx.Create(vc).Error; err != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to create metric value change: %w", err)
		}
	}
	if err := tx.Create(checkIn).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create check-in: %w", err)
	}
	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit check-in: %w", err)
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "check_in",
		Module:      "goals",
		ResourceID:  goal.ID.String(),
		Description: fmt.Sprintf("Check-in on goal '%s': %s", goal.Title, req.Status),
		Status:      "success",
	})

	// WebSocket broadcast
	if s.wsHub != nil {
		s.wsHub.BroadcastToGoal(goalID, "check_in_created", map[string]interface{}{
			"goal_id":     goalID,
			"check_in_id": checkIn.ID,
		}, userID)
	}

	// Fetch back with author
	created, err := s.goalRepo.FindCheckInByID(ctx, checkIn.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch created check-in: %w", err)
	}

	resp := created.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// Check-in: ListCheckIns
// ──────────────────────────────────────────────────

func (s *goalService) ListCheckIns(ctx context.Context, goalID uuid.UUID, page int, limit int, userID uuid.UUID) ([]models.CheckInResponse, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 {
		limit = 10
	}
	if _, err := s.requireView(ctx, goalID, userID); err != nil {
		return nil, 0, err
	}

	checkIns, total, err := s.goalRepo.ListCheckIns(ctx, goalID, page, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list check-ins: %w", err)
	}

	responses := make([]models.CheckInResponse, len(checkIns))
	for i, ci := range checkIns {
		responses[i] = ci.ToResponse()
	}

	return responses, total, nil
}

// ──────────────────────────────────────────────────
// Check-in: DeleteCheckIn
// ──────────────────────────────────────────────────

func (s *goalService) DeleteCheckIn(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	checkIn, err := s.goalRepo.FindCheckInByID(ctx, id)
	if err != nil {
		return notFoundOr(ctx, err, "check_in_not_found")
	}

	// The author (still able to see the goal) or a goal editor may delete.
	goal, err := s.goalRepo.FindByID(ctx, checkIn.GoalID)
	if err != nil {
		return notFoundOr(ctx, err, "goal_not_found")
	}
	if !s.canModifyGoalObj(ctx, goal, userID) && !(checkIn.AuthorID == userID && s.canAccessGoalObj(ctx, goal, userID)) {
		return errForbidden(ctx)
	}

	if err := s.goalRepo.DeleteCheckIn(ctx, id); err != nil {
		return fmt.Errorf("failed to delete check-in: %w", err)
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "delete_check_in",
		Module:      "goals",
		ResourceID:  checkIn.GoalID.String(),
		Description: fmt.Sprintf("Deleted check-in on goal %s", checkIn.GoalID.String()),
		Status:      "success",
	})

	return nil
}

// ════════════════════════════════════════════════════
// METRIC IMPORT/EXPORT
// ════════════════════════════════════════════════════

// ──────────────────────────────────────────────────
// ExportMetricsTemplate
// ──────────────────────────────────────────────────

func (s *goalService) ExportMetricsTemplate(ctx context.Context, filter *models.GoalFilter, format string) ([]byte, error) {
	goals, err := s.goalRepo.ListForExport(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to list goals for export: %w", err)
	}

	type metricRow struct {
		GoalID        string
		GoalTitle     string
		MetricID      string
		MetricName    string
		MetricType    string
		Unit          string
		BaselineValue float64
		CurrentValue  float64
		TargetValue   float64
	}

	var rows []metricRow
	for _, g := range goals {
		for _, m := range g.Metrics {
			rows = append(rows, metricRow{
				GoalID:        g.ID.String(),
				GoalTitle:     g.Title,
				MetricID:      m.ID.String(),
				MetricName:    m.Name,
				MetricType:    m.MetricType,
				Unit:          m.Unit,
				BaselineValue: m.BaselineValue,
				CurrentValue:  m.CurrentValue,
				TargetValue:   m.TargetValue,
			})
		}
	}

	header := []string{
		"Goal ID", "Goal Title", "Metric ID", "Metric Name",
		"Metric Type", "Unit", "Baseline Value", "Current Value",
		"Target Value", "New Value",
	}

	if format == "xlsx" {
		f := excelize.NewFile()
		sheet := "Metrics"
		f.SetSheetName("Sheet1", sheet)

		// Header style
		headerStyle, _ := f.NewStyle(&excelize.Style{
			Font:      &excelize.Font{Bold: true, Color: "FFFFFF"},
			Fill:      excelize.Fill{Type: "pattern", Color: []string{"4472C4"}, Pattern: 1},
			Alignment: &excelize.Alignment{Horizontal: "center"},
		})

		for i, h := range header {
			cell, _ := excelize.CoordinatesToCellName(i+1, 1)
			f.SetCellValue(sheet, cell, h)
			f.SetCellStyle(sheet, cell, cell, headerStyle)
		}

		for i, r := range rows {
			row := i + 2
			f.SetCellValue(sheet, cellName(1, row), r.GoalID)
			f.SetCellValue(sheet, cellName(2, row), r.GoalTitle)
			f.SetCellValue(sheet, cellName(3, row), r.MetricID)
			f.SetCellValue(sheet, cellName(4, row), r.MetricName)
			f.SetCellValue(sheet, cellName(5, row), r.MetricType)
			f.SetCellValue(sheet, cellName(6, row), r.Unit)
			f.SetCellValue(sheet, cellName(7, row), r.BaselineValue)
			f.SetCellValue(sheet, cellName(8, row), r.CurrentValue)
			f.SetCellValue(sheet, cellName(9, row), r.TargetValue)
			f.SetCellValue(sheet, cellName(10, row), "") // New Value — blank
		}

		// Auto-fit column widths
		for i := range header {
			col, _ := excelize.ColumnNumberToName(i + 1)
			f.SetColWidth(sheet, col, col, 18)
		}

		var buf bytes.Buffer
		if err := f.Write(&buf); err != nil {
			return nil, fmt.Errorf("failed to write XLSX: %w", err)
		}
		return buf.Bytes(), nil
	}

	// Default: CSV
	var buf bytes.Buffer
	writer := csv.NewWriter(&buf)

	if err := writer.Write(header); err != nil {
		return nil, fmt.Errorf("failed to write CSV header: %w", err)
	}

	for _, r := range rows {
		csvRow := []string{
			r.GoalID, r.GoalTitle, r.MetricID, r.MetricName,
			r.MetricType, r.Unit,
			fmt.Sprintf("%.2f", r.BaselineValue),
			fmt.Sprintf("%.2f", r.CurrentValue),
			fmt.Sprintf("%.2f", r.TargetValue),
			"", // New Value — blank
		}
		if err := writer.Write(csvRow); err != nil {
			return nil, fmt.Errorf("failed to write CSV row: %w", err)
		}
	}

	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, fmt.Errorf("CSV writer error: %w", err)
	}
	return buf.Bytes(), nil
}

func cellName(col, row int) string {
	name, _ := excelize.CoordinatesToCellName(col, row)
	return name
}

// ──────────────────────────────────────────────────
// ImportMetricsDryRun
// ──────────────────────────────────────────────────

func (s *goalService) ImportMetricsDryRun(ctx context.Context, fileData []byte, fileName string, userID uuid.UUID) (*models.MetricImportDryRunResponse, error) {
	items, totalRows, err := s.parseMetricImportFile(ctx, fileData, fileName, userID)
	if err != nil {
		return nil, err
	}

	goalSet := make(map[uuid.UUID]bool)
	var validCount, errorCount, warningCount, skippedCount int
	for i := range items {
		switch items[i].Status {
		case "valid":
			validCount++
			gid, _ := uuid.Parse(items[i].GoalID)
			goalSet[gid] = true
		case "error":
			errorCount++
		case "warning":
			warningCount++
			gid, _ := uuid.Parse(items[i].GoalID)
			goalSet[gid] = true
		case "skipped":
			skippedCount++
		}
	}

	return &models.MetricImportDryRunResponse{
		TotalRows:    totalRows,
		ValidCount:   validCount,
		ErrorCount:   errorCount,
		WarningCount: warningCount,
		SkippedCount: skippedCount,
		GoalCount:    len(goalSet),
		Items:        items,
	}, nil
}

// ──────────────────────────────────────────────────
// ImportMetricsCommit
// ──────────────────────────────────────────────────

func (s *goalService) ImportMetricsCommit(ctx context.Context, fileData []byte, fileName string, title string, comment string, primaryGoalID uuid.UUID, userID uuid.UUID) (*models.MetricImportBatchResponse, error) {
	// Validate primary goal exists and the caller can edit it
	if _, err := s.requireModify(ctx, primaryGoalID, userID); err != nil {
		var ge *GoalError
		if errors.As(err, &ge) && ge.Status == http.StatusNotFound {
			return nil, errBadRequest(ctx, "primary_goal_not_found")
		}
		return nil, err
	}

	// Parse and validate
	items, _, err := s.parseMetricImportFile(ctx, fileData, fileName, userID)
	if err != nil {
		return nil, err
	}

	// Filter to only valid items
	var validItems []models.MetricImportValidationItem
	for _, item := range items {
		if item.Status == "valid" || item.Status == "warning" {
			validItems = append(validItems, item)
		}
	}

	if len(validItems) == 0 {
		return nil, errBadRequest(ctx, "no_valid_metric_values_to_import")
	}

	// Resolve workflow (batches use the evidence approval workflow)
	wf, initialState, wfErr := s.resolveGoalWorkflow(ctx, goalWfEvidence)
	if wfErr != nil || wf == nil || initialState == nil {
		return nil, fmt.Errorf("%s", i18n.T(ctx, "no_active_evidence_approval_workflow"))
	}

	// Count distinct goals
	goalSet := make(map[uuid.UUID]bool)
	var importItems []models.MetricImportItem
	for _, vi := range validItems {
		goalID, _ := uuid.Parse(vi.GoalID)
		metricID, _ := uuid.Parse(vi.MetricID)
		goalSet[goalID] = true

		importItems = append(importItems, models.MetricImportItem{
			ID:       uuid.New(),
			GoalID:   goalID,
			MetricID: metricID,
			OldValue: vi.CurrentValue,
			NewValue: vi.NewValue,
		})
	}

	// Create batch in transaction
	tx := s.goalRepo.BeginTx(ctx)
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	batch := &models.MetricImportBatch{
		Title:          title,
		Comment:        comment,
		Status:         "Draft",
		ItemCount:      len(importItems),
		GoalCount:      len(goalSet),
		FileName:       fileName,
		ImportedByID:   userID,
		PrimaryGoalID:  primaryGoalID,
		WorkflowID:     &wf.ID,
		CurrentStateID: &initialState.ID,
		Version:        1,
	}

	if err := tx.Create(batch).Error; err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create import batch: %w", err)
	}

	// Set batch ID on items
	for i := range importItems {
		importItems[i].BatchID = batch.ID
	}

	if err := s.goalRepo.CreateMetricImportItems(ctx, tx, importItems); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create import items: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit: %w", err)
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "create",
		Module:      "goals",
		ResourceID:  batch.ID.String(),
		Description: fmt.Sprintf("Created metric import batch '%s' with %d items across %d goals", title, len(importItems), len(goalSet)),
		Status:      "success",
	})

	// Reload with relations
	batchWithRelations, reloadErr := s.goalRepo.FindMetricImportBatchByIDWithRelations(ctx, batch.ID)
	if reloadErr != nil {
		resp := batch.ToResponse()
		return &resp, nil
	}
	resp := batchWithRelations.ToResponse()
	return &resp, nil
}

// parseMetricImportFile parses CSV/XLSX and validates each row
func (s *goalService) parseMetricImportFile(ctx context.Context, fileData []byte, fileName string, userID uuid.UUID) ([]models.MetricImportValidationItem, int, error) {
	var rows [][]string
	var parseErr error

	lowerName := strings.ToLower(fileName)
	if strings.HasSuffix(lowerName, ".xlsx") || strings.HasSuffix(lowerName, ".xls") {
		rows, parseErr = s.parseXLSX(ctx, fileData)
	} else if strings.HasSuffix(lowerName, ".csv") {
		rows, parseErr = s.parseCSV(fileData)
	} else {
		return nil, 0, errBadRequest(ctx, "unsupported_file_format_csv_xlsx")
	}

	if parseErr != nil {
		return nil, 0, errBadRequest(ctx, "failed_to_parse_file_detail", parseErr.Error())
	}

	if len(rows) < 2 {
		return nil, 0, errBadRequest(ctx, "file_missing_header_or_data")
	}
	goalAccess := map[uuid.UUID]bool{}

	// Skip header row
	dataRows := rows[1:]
	totalRows := len(dataRows)

	var results []models.MetricImportValidationItem

	for i, row := range dataRows {
		rowNum := i + 2 // 1-indexed, +1 for header

		item := models.MetricImportValidationItem{
			RowNumber: rowNum,
		}

		// Need at least 10 columns
		if len(row) < 10 {
			item.Status = "error"
			item.Errors = append(item.Errors, fmt.Sprintf("expected 10 columns, got %d", len(row)))
			results = append(results, item)
			continue
		}

		goalIDStr := strings.TrimSpace(row[0])
		item.GoalID = goalIDStr
		item.GoalTitle = strings.TrimSpace(row[1])
		metricIDStr := strings.TrimSpace(row[2])
		item.MetricID = metricIDStr
		item.MetricName = strings.TrimSpace(row[3])
		newValueStr := strings.TrimSpace(row[9])

		// Skip empty new value
		if newValueStr == "" {
			item.Status = "skipped"
			results = append(results, item)
			continue
		}

		var hasErrors bool

		// Validate Goal ID
		goalID, err := uuid.Parse(goalIDStr)
		if err != nil {
			item.Errors = append(item.Errors, "invalid Goal ID (not a valid UUID)")
			hasErrors = true
		}

		// Validate Metric ID
		metricID, err := uuid.Parse(metricIDStr)
		if err != nil {
			item.Errors = append(item.Errors, "invalid Metric ID (not a valid UUID)")
			hasErrors = true
		}

		// Validate New Value
		newValue, err := strconv.ParseFloat(newValueStr, 64)
		if err != nil {
			item.Errors = append(item.Errors, fmt.Sprintf("invalid New Value '%s' (must be a number)", newValueStr))
			hasErrors = true
		}

		if hasErrors {
			item.Status = "error"
			results = append(results, item)
			continue
		}

		// Verify goal exists
		goal, err := s.goalRepo.FindByID(ctx, goalID)
		if err != nil {
			item.Errors = append(item.Errors, "goal not found")
			item.Status = "error"
			results = append(results, item)
			continue
		}
		item.GoalTitle = goal.Title

		// Caller must be able to edit the goal (same rule as single value updates)
		allowed, cached := goalAccess[goalID]
		if !cached {
			allowed = s.canModifyGoalObj(ctx, goal, userID) && goal.Status != models.GoalStatusClosed
			goalAccess[goalID] = allowed
		}
		if !allowed {
			item.Errors = append(item.Errors, "access denied: you cannot update metrics of this goal")
			item.Status = "error"
			results = append(results, item)
			continue
		}

		// Verify metric exists and belongs to goal
		metric, err := s.goalRepo.FindMetricByID(ctx, metricID)
		if err != nil {
			item.Errors = append(item.Errors, "metric not found")
			item.Status = "error"
			results = append(results, item)
			continue
		}
		if metric.GoalID != goalID {
			item.Errors = append(item.Errors, "metric does not belong to the specified goal")
			item.Status = "error"
			results = append(results, item)
			continue
		}
		item.MetricName = metric.Name
		item.CurrentValue = metric.CurrentValue
		item.NewValue = newValue

		// Skip if new value equals current value
		if newValue == metric.CurrentValue {
			item.Status = "skipped"
			item.Warnings = append(item.Warnings, "new value equals current value")
			results = append(results, item)
			continue
		}

		item.Status = "valid"

		results = append(results, item)
	}

	// The same metric may appear only once per batch; otherwise the last row
	// would silently win. Mark every occurrence as an error.
	byMetric := map[string][]int{}
	for i := range results {
		if results[i].Status == "valid" || results[i].Status == "warning" {
			byMetric[results[i].MetricID] = append(byMetric[results[i].MetricID], i)
		}
	}
	for _, idxs := range byMetric {
		if len(idxs) < 2 {
			continue
		}
		rowNums := make([]string, len(idxs))
		for k, i := range idxs {
			rowNums[k] = strconv.Itoa(results[i].RowNumber)
		}
		for _, i := range idxs {
			results[i].Status = "error"
			results[i].Errors = append(results[i].Errors, "metric appears in multiple rows ("+strings.Join(rowNums, ", ")+"); keep only one value per metric")
		}
	}

	return results, totalRows, nil
}

// ──────────────────────────────────────────────────
// GetMetricImportBatch
// ──────────────────────────────────────────────────

func (s *goalService) GetMetricImportBatch(ctx context.Context, id uuid.UUID, userID uuid.UUID) (*models.MetricImportBatchResponse, error) {
	batch, err := s.goalRepo.FindMetricImportBatchByIDWithRelations(ctx, id)
	if err != nil {
		return nil, notFoundOr(ctx, err, "metric_import_batch_not_found")
	}
	if err := s.requireBatchView(ctx, batch, userID); err != nil {
		return nil, err
	}

	resp := batch.ToResponse()
	return &resp, nil
}

// requireBatchView: importer, assigned reviewer, or anyone who can view the
// batch's primary goal.
func (s *goalService) requireBatchView(ctx context.Context, batch *models.MetricImportBatch, userID uuid.UUID) error {
	if s.isSuperAdmin(ctx) || batch.ImportedByID == userID || (batch.AssignedToID != nil && *batch.AssignedToID == userID) {
		return nil
	}
	goal, err := s.goalRepo.FindByID(ctx, batch.PrimaryGoalID)
	if err != nil || !s.canAccessGoalObj(ctx, goal, userID) {
		return errForbidden(ctx)
	}
	return nil
}

// ──────────────────────────────────────────────────
// ListMetricImportBatches
// ──────────────────────────────────────────────────

func (s *goalService) ListMetricImportBatches(ctx context.Context, filter *models.MetricImportBatchFilter, userID uuid.UUID) ([]models.MetricImportBatchResponse, int64, error) {
	if filter.Page < 1 {
		filter.Page = 1
	}
	if filter.Limit < 1 {
		filter.Limit = 10
	}
	filter.UserID = nil
	if !s.isSuperAdmin(ctx) {
		filter.UserID = &userID
	}

	batches, total, err := s.goalRepo.ListMetricImportBatches(ctx, filter)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list metric import batches: %w", err)
	}

	responses := make([]models.MetricImportBatchResponse, len(batches))
	for i, b := range batches {
		responses[i] = b.ToResponse()
	}

	return responses, total, nil
}

// ──────────────────────────────────────────────────
// DeleteMetricImportBatch
// ──────────────────────────────────────────────────

func (s *goalService) DeleteMetricImportBatch(ctx context.Context, id uuid.UUID, userID uuid.UUID) error {
	batch, err := s.goalRepo.FindMetricImportBatchByID(ctx, id)
	if err != nil {
		return notFoundOr(ctx, err, "metric_import_batch_not_found")
	}

	// Importer, super admin, or an editor of the primary goal.
	if !s.isSuperAdmin(ctx) && batch.ImportedByID != userID {
		if ok, _ := s.canModifyGoal(ctx, batch.PrimaryGoalID, userID); !ok {
			return errForbidden(ctx)
		}
	}

	if batch.Status == "Approved" {
		return errConflict(ctx, "approved_batches_cannot_be_deleted")
	}

	if err := s.goalRepo.DeleteMetricImportBatch(ctx, id); err != nil {
		return fmt.Errorf("failed to delete batch: %w", err)
	}

	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "delete",
		Module:      "goals",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("Deleted metric import batch '%s'", batch.Title),
		Status:      "success",
	})

	return nil
}

// ──────────────────────────────────────────────────
// GetAvailableMetricBatchTransitions
// ──────────────────────────────────────────────────

func (s *goalService) GetAvailableMetricBatchTransitions(ctx context.Context, batchID uuid.UUID, userID uuid.UUID) ([]models.AvailableTransitionResponse, error) {
	batch, err := s.goalRepo.FindMetricImportBatchByIDWithRelations(ctx, batchID)
	if err != nil {
		return nil, notFoundOr(ctx, err, "metric_import_batch_not_found")
	}
	if err := s.requireBatchView(ctx, batch, userID); err != nil {
		return nil, err
	}

	if batch.CurrentStateID == nil || batch.WorkflowID == nil {
		return nil, errBadRequest(ctx, "batch_no_workflow")
	}

	// Get all transitions from current state
	transitions, err := s.workflowRepo.ListTransitionsFromState(ctx, *batch.CurrentStateID)
	if err != nil {
		return nil, fmt.Errorf("failed to get transitions: %w", err)
	}

	// Get collaborators from primary goal to determine L2 reviewer existence
	collaborators, err := s.goalRepo.ListCollaborators(ctx, batch.PrimaryGoalID)
	if err != nil {
		return nil, fmt.Errorf("failed to list collaborators: %w", err)
	}

	var hasL2Reviewer bool
	for _, c := range collaborators {
		if c.Role == models.CollaboratorRoleReviewerL2 {
			hasL2Reviewer = true
			break
		}
	}

	var results []models.AvailableTransitionResponse
	for _, t := range transitions {
		canExecute := true
		reason := ""

		requirements, _ := s.workflowRepo.GetTransitionRequirements(ctx, t.ID)

		switch t.Code {
		case "submit", "resubmit":
			goal, _ := s.goalRepo.FindByID(ctx, batch.PrimaryGoalID)
			if batch.ImportedByID != userID && (goal == nil || goal.OwnerID != userID) {
				canExecute = false
				reason = "Only the importer or goal owner can submit"
			}
		case "approve_l1":
			if !hasL2Reviewer {
				continue
			}
			if batch.AssignedToID == nil || *batch.AssignedToID != userID {
				canExecute = false
				reason = "Only the assigned reviewer can approve"
			}
		case "approve_l1_final":
			if hasL2Reviewer {
				continue
			}
			if batch.AssignedToID == nil || *batch.AssignedToID != userID {
				canExecute = false
				reason = "Only the assigned reviewer can approve"
			}
		case "request_changes_l1", "reject_l1", "request_changes_l2", "reject_l2":
			if batch.AssignedToID == nil || *batch.AssignedToID != userID {
				canExecute = false
				reason = "Only the assigned reviewer can perform this action"
			}
		case "approve_l2":
			if batch.AssignedToID == nil || *batch.AssignedToID != userID {
				canExecute = false
				reason = "Only the assigned L2 reviewer can approve"
			}
		case "assign_l1":
			continue
		default:
			if ok, _ := s.canModifyGoal(ctx, batch.PrimaryGoalID, userID); !ok {
				canExecute = false
				reason = "Only the goal owner or a reviewer can perform this action"
			}
		}

		reqResponses := make([]models.TransitionRequirementResponse, len(requirements))
		for i, r := range requirements {
			reqResponses[i] = models.ToTransitionRequirementResponse(&r)
		}

		results = append(results, models.AvailableTransitionResponse{
			Transition:   models.ToWorkflowTransitionResponse(&t),
			CanExecute:   canExecute,
			Requirements: reqResponses,
			Reason:       reason,
		})
	}

	return results, nil
}

// ──────────────────────────────────────────────────
// ExecuteMetricBatchTransition
// ──────────────────────────────────────────────────

func (s *goalService) ExecuteMetricBatchTransition(ctx context.Context, batchID uuid.UUID, req *models.MetricImportBatchTransitionRequest, userID uuid.UUID) (*models.MetricImportBatchResponse, error) {
	transitionID, err := uuid.Parse(req.TransitionID)
	if err != nil {
		return nil, errBadRequest(ctx, "invalid_transition_id_svc")
	}

	pre, err := s.goalRepo.FindMetricImportBatchByID(ctx, batchID)
	if err != nil {
		return nil, notFoundOr(ctx, err, "metric_import_batch_not_found")
	}
	if err := s.requireBatchView(ctx, pre, userID); err != nil {
		return nil, err
	}

	tx := s.goalRepo.BeginTx(ctx)
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	// Pessimistic lock
	batch, err := s.goalRepo.FindMetricImportBatchByIDForUpdate(ctx, tx, batchID)
	if err != nil {
		tx.Rollback()
		return nil, notFoundOr(ctx, err, "metric_import_batch_not_found")
	}

	// Optimistic concurrency check
	if batch.Version != req.Version {
		tx.Rollback()
		return nil, fmt.Errorf("%s", i18n.Tf(ctx, "record_modified_by_another_user", batch.Version, req.Version))
	}

	if batch.CurrentStateID == nil || batch.WorkflowID == nil {
		tx.Rollback()
		return nil, fmt.Errorf("%s", i18n.T(ctx, "batch_no_workflow"))
	}

	// Fetch transition
	transition, err := s.workflowRepo.FindTransitionByIDWithRelations(ctx, transitionID)
	if err != nil {
		tx.Rollback()
		return nil, errBadRequest(ctx, "transition_not_found")
	}

	if transition.WorkflowID != *batch.WorkflowID {
		tx.Rollback()
		return nil, fmt.Errorf("%s", i18n.T(ctx, "transition_not_in_batch_workflow"))
	}

	if transition.FromStateID != *batch.CurrentStateID {
		tx.Rollback()
		return nil, fmt.Errorf("%s", i18n.T(ctx, "transition_invalid_from_state"))
	}

	// Authorization check
	switch transition.Code {
	case "submit", "resubmit":
		goal, _ := s.goalRepo.FindByID(ctx, batch.PrimaryGoalID)
		if batch.ImportedByID != userID && (goal == nil || goal.OwnerID != userID) {
			tx.Rollback()
			return nil, newGoalErr(ctx, http.StatusForbidden, "only_importer_or_owner")
		}
	case "approve_l1", "approve_l1_final", "request_changes_l1", "reject_l1", "approve_l2", "request_changes_l2", "reject_l2":
		if batch.AssignedToID == nil || *batch.AssignedToID != userID {
			tx.Rollback()
			return nil, newGoalErr(ctx, http.StatusForbidden, "only_reviewer_action")
		}
	default:
		if ok, _ := s.canModifyGoal(ctx, batch.PrimaryGoalID, userID); !ok {
			tx.Rollback()
			return nil, errForbidden(ctx)
		}
	}

	// Validate requirements
	requirements, _ := s.workflowRepo.GetTransitionRequirements(ctx, transitionID)
	for _, r := range requirements {
		if r.RequirementType == "comment" && r.IsMandatory != nil && *r.IsMandatory {
			if strings.TrimSpace(req.Comment) == "" {
				tx.Rollback()
				errMsg := r.ErrorMessage
				if errMsg == "" {
					errMsg = i18n.T(ctx, "comment_required_for_transition")
				}
				return nil, fmt.Errorf("%s", errMsg)
			}
		}
	}

	fromStateID := *batch.CurrentStateID

	// Create transition history
	now := time.Now()
	history := &models.MetricImportBatchTransitionHistory{
		BatchID:        batchID,
		TransitionID:   &transitionID,
		FromStateID:    fromStateID,
		ToStateID:      transition.ToStateID,
		PerformedByID:  userID,
		Comment:        req.Comment,
		IsSystemAction: false,
		TransitionedAt: now,
	}
	if err := s.goalRepo.CreateMetricBatchTransitionHistory(ctx, tx, history); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to create transition history: %w", err)
	}

	// Update batch state
	batch.CurrentStateID = &transition.ToStateID
	batch.Version++

	targetState, _ := s.workflowRepo.FindStateByID(ctx, transition.ToStateID)
	if targetState != nil {
		switch targetState.Code {
		case "draft":
			batch.Status = "Draft"
		case "submitted":
			batch.Status = "Submitted"
		case "l1_review", "l2_review":
			batch.Status = "In_Review"
		case "approved":
			batch.Status = "Approved"
		case "rejected":
			batch.Status = "Rejected"
		case "changes_requested":
			batch.Status = "Changes_Requested"
		}
	}

	// Assignment logic — resolve collaborators from primary goal
	collaborators, _ := s.goalRepo.ListCollaborators(ctx, batch.PrimaryGoalID)

	var affectedGoalIDs []uuid.UUID

	switch transition.Code {
	case "submit", "resubmit":
		var reviewerL1 *models.GoalCollaborator
		for i := range collaborators {
			if collaborators[i].Role == models.CollaboratorRoleReviewerL1 {
				reviewerL1 = &collaborators[i]
				break
			}
		}
		if reviewerL1 == nil {
			tx.Rollback()
			return nil, fmt.Errorf("%s", i18n.T(ctx, "no_l1_reviewer"))
		}

		l1State, _ := s.workflowRepo.FindStateByCode(ctx, *batch.WorkflowID, "l1_review")
		if l1State != nil {
			autoHistory := &models.MetricImportBatchTransitionHistory{
				BatchID:        batchID,
				FromStateID:    transition.ToStateID,
				ToStateID:      l1State.ID,
				PerformedByID:  userID,
				Comment:        "Auto-assigned to L1 reviewer",
				IsSystemAction: true,
				TransitionedAt: now,
			}
			s.goalRepo.CreateMetricBatchTransitionHistory(ctx, tx, autoHistory)
			batch.CurrentStateID = &l1State.ID
			batch.Status = "In_Review"
		}

		batch.AssignedToID = &reviewerL1.UserID

	case "approve_l1":
		var reviewerL2 *models.GoalCollaborator
		for i := range collaborators {
			if collaborators[i].Role == models.CollaboratorRoleReviewerL2 {
				reviewerL2 = &collaborators[i]
				break
			}
		}
		if reviewerL2 != nil {
			batch.AssignedToID = &reviewerL2.UserID
		}

	case "request_changes_l1", "request_changes_l2", "reject_l1", "reject_l2":
		batch.AssignedToID = nil
	}

	// Final approval (dedicated codes or any approval into a terminal state)
	// applies the batch values.
	if isGoalApprovalTransition(transition) {
		batch.AssignedToID = nil
		batch.Status = "Approved"
		goalIDs, applyErr := s.applyMetricBatchValues(ctx, tx, batch, userID)
		if applyErr != nil {
			tx.Rollback()
			return nil, fmt.Errorf("failed to apply metric values: %w", applyErr)
		}
		affectedGoalIDs = goalIDs
	}

	// Save batch
	if err := s.goalRepo.UpdateMetricImportBatchInTx(ctx, tx, batch); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to update batch: %w", err)
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit transaction: %w", err)
	}

	// Post-commit: recalculate progress for affected goals
	for _, goalID := range affectedGoalIDs {
		_ = s.RecalculateProgress(ctx, goalID)
	}

	// Post-commit notifications
	primaryGoal, _ := s.goalRepo.FindByID(ctx, batch.PrimaryGoalID)
	goalTitle := "a goal"
	if primaryGoal != nil {
		goalTitle = primaryGoal.Title
	}

	switch transition.Code {
	case "submit", "resubmit":
		if batch.AssignedToID != nil {
			reviewer, rErr := s.userRepo.FindByID(ctx, *batch.AssignedToID)
			if rErr == nil && reviewer != nil {
				s.notificationService.SendNotification(ctx, "notification", nil, "en",
					[]string{reviewer.Email}, nil, nil,
					"Metric Import Submitted for Review",
					fmt.Sprintf("Metric import batch '%s' has been submitted for your review (primary goal: %s)", batch.Title, goalTitle),
					nil, nil, &userID, nil)
			}
		}
	case "approve_l1":
		if batch.AssignedToID != nil {
			reviewer, rErr := s.userRepo.FindByID(ctx, *batch.AssignedToID)
			if rErr == nil && reviewer != nil {
				s.notificationService.SendNotification(ctx, "notification", nil, "en",
					[]string{reviewer.Email}, nil, nil,
					"Metric Import Pending L2 Review",
					fmt.Sprintf("Metric import batch '%s' for goal '%s' requires your L2 review", batch.Title, goalTitle),
					nil, nil, &userID, nil)
			}
		}
	case "approve_l1_final", "approve_l2":
		importer, iErr := s.userRepo.FindByID(ctx, batch.ImportedByID)
		if iErr == nil && importer != nil {
			s.notificationService.SendNotification(ctx, "notification", nil, "en",
				[]string{importer.Email}, nil, nil,
				"Metric Import Approved",
				fmt.Sprintf("Your metric import batch '%s' for goal '%s' has been approved. %d metric values have been applied.", batch.Title, goalTitle, batch.ItemCount),
				nil, nil, &userID, nil)
		}
	case "reject_l1", "reject_l2":
		importer, iErr := s.userRepo.FindByID(ctx, batch.ImportedByID)
		if iErr == nil && importer != nil {
			s.notificationService.SendNotification(ctx, "notification", nil, "en",
				[]string{importer.Email}, nil, nil,
				"Metric Import Rejected",
				fmt.Sprintf("Your metric import batch '%s' for goal '%s' has been rejected. Comment: %s", batch.Title, goalTitle, req.Comment),
				nil, nil, &userID, nil)
		}
	case "request_changes_l1", "request_changes_l2":
		importer, iErr := s.userRepo.FindByID(ctx, batch.ImportedByID)
		if iErr == nil && importer != nil {
			s.notificationService.SendNotification(ctx, "notification", nil, "en",
				[]string{importer.Email}, nil, nil,
				"Metric Import Returned for Changes",
				fmt.Sprintf("Your metric import batch '%s' for goal '%s' has been returned for changes. Comment: %s", batch.Title, goalTitle, req.Comment),
				nil, nil, &userID, nil)
		}
	}

	// Audit log
	s.actionLogService.LogAction(ctx, &LogActionParams{
		UserID:      userID,
		Action:      "transition",
		Module:      "goals",
		ResourceID:  batchID.String(),
		Description: fmt.Sprintf("Executed '%s' on metric import batch '%s'", transition.Name, batch.Title),
		Status:      "success",
	})

	// WebSocket broadcast
	if s.wsHub != nil {
		wsData := map[string]interface{}{
			"batch_id":        batchID,
			"primary_goal_id": batch.PrimaryGoalID,
			"status":          batch.Status,
			"transition":      transition.Name,
		}
		s.wsHub.BroadcastToGoal(batch.PrimaryGoalID, "metric_batch_transitioned", wsData, userID)
		s.wsHub.BroadcastGoalToAll("metric_batch_transitioned", wsData)
	}

	// Reload with relations
	batchWithRelations, reloadErr := s.goalRepo.FindMetricImportBatchByIDWithRelations(ctx, batchID)
	if reloadErr != nil {
		resp := batch.ToResponse()
		return &resp, nil
	}
	resp := batchWithRelations.ToResponse()
	return &resp, nil
}

// applyMetricBatchValues applies all metric values from a batch atomically
func (s *goalService) applyMetricBatchValues(ctx context.Context, tx *gorm.DB, batch *models.MetricImportBatch, userID uuid.UUID) ([]uuid.UUID, error) {
	items, err := s.goalRepo.ListMetricImportItemsByBatchID(ctx, batch.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load batch items: %w", err)
	}

	goalSet := make(map[uuid.UUID]bool)

	for _, item := range items {
		// Fresh-read metric to handle drift
		metric, err := s.goalRepo.FindMetricByID(ctx, item.MetricID)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", i18n.Tf(ctx, "metric_id_not_found", item.MetricID.String()), err)
		}

		// Create metric history entry
		historyEntry := &models.MetricHistory{
			MetricID:    metric.ID,
			OldValue:    metric.CurrentValue,
			NewValue:    item.NewValue,
			ChangedByID: userID,
			Comment:     fmt.Sprintf("Bulk import: %s", batch.Title),
		}
		if err := tx.Create(historyEntry).Error; err != nil {
			return nil, fmt.Errorf("failed to create metric history: %w", err)
		}

		// Update metric current value
		if err := tx.Model(metric).Update("current_value", item.NewValue).Error; err != nil {
			return nil, fmt.Errorf("failed to update metric value: %w", err)
		}

		goalSet[item.GoalID] = true
	}

	// Mark all items as applied
	if err := s.goalRepo.MarkMetricImportItemsApplied(ctx, tx, batch.ID); err != nil {
		return nil, fmt.Errorf("failed to mark items as applied: %w", err)
	}

	goalIDs := make([]uuid.UUID, 0, len(goalSet))
	for gid := range goalSet {
		goalIDs = append(goalIDs, gid)
	}

	return goalIDs, nil
}

// ──────────────────────────────────────────────────
// GetMetricBatchTransitionHistory
// ──────────────────────────────────────────────────

func (s *goalService) GetMetricBatchTransitionHistory(ctx context.Context, batchID uuid.UUID, userID uuid.UUID) ([]models.MetricImportBatchTransitionHistoryResponse, error) {
	batch, err := s.goalRepo.FindMetricImportBatchByID(ctx, batchID)
	if err != nil {
		return nil, notFoundOr(ctx, err, "metric_import_batch_not_found")
	}
	if err := s.requireBatchView(ctx, batch, userID); err != nil {
		return nil, err
	}
	histories, err := s.goalRepo.ListMetricBatchTransitionHistory(ctx, batchID)
	if err != nil {
		return nil, fmt.Errorf("failed to get transition history: %w", err)
	}

	responses := make([]models.MetricImportBatchTransitionHistoryResponse, len(histories))
	for i, h := range histories {
		responses[i] = h.ToResponse()
	}

	return responses, nil
}

// ──────────────────────────────────────────────────
// Goal Comments
// ──────────────────────────────────────────────────

func (s *goalService) AddComment(ctx context.Context, goalID uuid.UUID, content string, userID uuid.UUID) (*models.GoalCommentResponse, error) {
	// 404 for a missing goal (also for super admins — no orphan comments)
	if _, err := s.requireView(ctx, goalID, userID); err != nil {
		return nil, err
	}

	comment := &models.GoalComment{
		GoalID:   goalID,
		AuthorID: userID,
		Content:  content,
	}

	if err := s.goalRepo.CreateComment(ctx, comment); err != nil {
		return nil, fmt.Errorf("failed to create comment: %w", err)
	}

	// Reload with author preloaded
	created, err := s.goalRepo.FindCommentByID(ctx, comment.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to load comment: %w", err)
	}

	resp := models.ToGoalCommentResponse(created)
	return &resp, nil
}

func (s *goalService) ListComments(ctx context.Context, goalID uuid.UUID, page, limit int, userID uuid.UUID) ([]models.GoalCommentResponse, int64, error) {
	if _, err := s.requireView(ctx, goalID, userID); err != nil {
		return nil, 0, err
	}
	comments, total, err := s.goalRepo.ListComments(ctx, goalID, page, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to list comments: %w", err)
	}

	responses := make([]models.GoalCommentResponse, len(comments))
	for i, c := range comments {
		responses[i] = models.ToGoalCommentResponse(&c)
	}

	return responses, total, nil
}

func (s *goalService) DeleteComment(ctx context.Context, commentID uuid.UUID, userID uuid.UUID) error {
	comment, err := s.goalRepo.FindCommentByID(ctx, commentID)
	if err != nil {
		return notFoundOr(ctx, err, "comment_not_found")
	}

	// Only the author or a super admin can delete
	if comment.AuthorID != userID && !s.isSuperAdmin(ctx) {
		return newGoalErr(ctx, http.StatusForbidden, "only_comment_author_delete")
	}

	if err := s.goalRepo.DeleteComment(ctx, commentID); err != nil {
		return fmt.Errorf("failed to delete comment: %w", err)
	}

	return nil
}

// ════════════════════════════════════════════════════
// Shared helpers (workflow selection, progress math, files, folders)
// ════════════════════════════════════════════════════

// Goal-side workflow record types and their seeded workflow codes.
const (
	goalWfMetric            = "metric"
	goalWfMetricValueChange = "metric_value_change"
	goalWfEvidence          = "evidence"
)

var goalWorkflowPreferredCodes = map[string][]string{
	goalWfMetric:            {"metric_approval"},
	goalWfMetricValueChange: {"metric_value_change_approval"},
	goalWfEvidence:          {"evidence_approval"},
}

// pickGoalWorkflow chooses the workflow for a goal record type from the list
// returned by WorkflowRepository.ListByRecordType (which also returns generic
// 'all'/'both' workflows sorted by name). Order of preference:
//  1. record_type matches exactly AND code is the dedicated goal workflow code
//  2. record_type matches exactly (default first, then oldest)
//  3. code is the dedicated goal workflow code (whatever its record_type)
//  4. generic record_type 'all' (default first, then oldest)
//
// 'both' workflows are incident/request workflows and are never used. The
// result never depends on workflow names.
func pickGoalWorkflow(wfs []models.Workflow, recordType string, preferredCodes []string) *models.Workflow {
	if len(wfs) == 0 {
		return nil
	}
	sorted := make([]models.Workflow, len(wfs))
	copy(sorted, wfs)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].IsDefault != sorted[j].IsDefault {
			return sorted[i].IsDefault
		}
		if !sorted[i].CreatedAt.Equal(sorted[j].CreatedAt) {
			return sorted[i].CreatedAt.Before(sorted[j].CreatedAt)
		}
		return sorted[i].ID.String() < sorted[j].ID.String()
	})
	isPreferred := func(code string) bool {
		for _, c := range preferredCodes {
			if c == code {
				return true
			}
		}
		return false
	}
	for _, code := range preferredCodes {
		for i := range sorted {
			if sorted[i].RecordType == recordType && sorted[i].Code == code {
				return &sorted[i]
			}
		}
	}
	for i := range sorted {
		if sorted[i].RecordType == recordType {
			return &sorted[i]
		}
	}
	for i := range sorted {
		if isPreferred(sorted[i].Code) {
			return &sorted[i]
		}
	}
	for i := range sorted {
		if sorted[i].RecordType == "all" {
			return &sorted[i]
		}
	}
	return nil
}

// resolveGoalWorkflow returns the active workflow + initial state for a goal
// record type, or (nil, nil, nil) when no workflow applies.
func (s *goalService) resolveGoalWorkflow(ctx context.Context, recordType string) (*models.Workflow, *models.WorkflowState, error) {
	wfs, err := s.workflowRepo.ListByRecordType(ctx, recordType, true)
	if err != nil {
		return nil, nil, err
	}
	wf := pickGoalWorkflow(wfs, recordType, goalWorkflowPreferredCodes[recordType])
	if wf == nil {
		return nil, nil, nil
	}
	initial, err := s.workflowRepo.GetInitialState(ctx, wf.ID)
	if err != nil || initial == nil {
		return wf, nil, fmt.Errorf("workflow %s has no initial state", wf.Code)
	}
	return wf, initial, nil
}

// isGoalApprovalTransition reports whether executing t finalises the record as
// approved: the dedicated approve codes, or any non-rejection transition into
// a terminal state that is not a "rejected"/"cancelled" state.
func isGoalApprovalTransition(t *models.WorkflowTransition) bool {
	switch t.Code {
	case "approve_l1_final", "approve_l2":
		return true
	}
	if t.IsRejection || t.ToState == nil || t.ToState.StateType != "terminal" {
		return false
	}
	switch strings.ToLower(t.ToState.Code) {
	case "rejected", "reject", "cancelled", "canceled":
		return false
	}
	return true
}

// goalReviewerTransitionCodes are transitions only the assigned reviewer may run.
var goalReviewerTransitionCodes = map[string]bool{
	"approve_l1": true, "approve_l1_final": true, "request_changes_l1": true, "reject_l1": true,
	"approve_l2": true, "request_changes_l2": true, "reject_l2": true,
}

// metricProgressFraction returns the 0..1 completion of one metric.
//
//   - Boolean: 1 when current >= 1, else 0.
//   - Otherwise progress is measured from baseline towards target, so both
//     increasing (baseline 0 → target 100) and decreasing (baseline 100 →
//     target 20) metrics work: (current − baseline) / (target − baseline),
//     clamped to [0, 1].
//   - target == baseline ("maintain" metric): 1 when current equals the
//     target, else 0.
func metricProgressFraction(m models.GoalMetric) float64 {
	if m.MetricType == models.MetricTypeBoolean {
		if m.CurrentValue >= 1 {
			return 1
		}
		return 0
	}
	span := m.TargetValue - m.BaselineValue
	if span == 0 {
		if m.CurrentValue == m.TargetValue {
			return 1
		}
		return 0
	}
	f := (m.CurrentValue - m.BaselineValue) / span
	if math.IsNaN(f) || f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// metricCountsTowardsProgress: draft / in-review / rejected metric definitions
// do not count. Legacy metrics without a workflow state count.
func metricCountsTowardsProgress(m models.GoalMetric) bool {
	if m.Weight <= 0 {
		return false
	}
	if m.CurrentStateID == nil {
		return true
	}
	if m.CurrentState == nil {
		return false
	}
	if m.CurrentState.StateType != "terminal" {
		return false
	}
	switch strings.ToLower(m.CurrentState.Code) {
	case "rejected", "reject", "cancelled", "canceled":
		return false
	}
	return true
}

// computeGoalProgress combines a goal's own (approved) metrics with its child
// goals' progress, returning a percentage 0..100.
//
// Rule: the goal's own metrics form ONE component (their weighted average) and
// every child goal is one further component; progress is the plain average of
// all components. So:
//   - only own metrics → weighted average of own metrics
//   - only children    → average of children
//   - both             → (ownAvg + Σ child) / (1 + #children)
//   - neither          → 0
func computeGoalProgress(metrics []models.GoalMetric, childProgress []float64) float64 {
	var weighted, totalWeight float64
	for _, m := range metrics {
		if !metricCountsTowardsProgress(m) {
			continue
		}
		weighted += metricProgressFraction(m) * m.Weight
		totalWeight += m.Weight
	}
	var sum float64
	var n int
	if totalWeight > 0 {
		sum += weighted / totalWeight * 100
		n++
	}
	for _, p := range childProgress {
		sum += p
		n++
	}
	if n == 0 {
		return 0
	}
	p := sum / float64(n)
	if p < 0 {
		return 0
	}
	if p > 100 {
		return 100
	}
	return p
}

// goalFolderName makes the Documenta folder name unique per goal: Documenta
// enforces unique names per parent folder, so two goals with the same title
// (different owners, or a re-created title after delete) would otherwise
// collide.
func goalFolderName(title string, id uuid.UUID) string {
	return fmt.Sprintf("%s [%s]", strings.TrimSpace(title), id.String()[:8])
}

// createGoalFolder provisions the goal's Documenta folder; failures surface as
// a clear 502 instead of leaving the goal without a folder.
func (s *goalService) createGoalFolder(ctx context.Context, title string, goalID uuid.UUID) (string, error) {
	rootID, err := s.documentaClient.EnsureFolder(ctx, s.cfg.Documenta.WorkspaceName, "", "Goal Management")
	if err != nil {
		log.Printf("[goal_service] ensure Goal Management folder failed: %v", err)
		ge := newGoalErr(ctx, http.StatusBadGateway, "goal_documenta_folder_failed")
		ge.Err = err
		return "", ge
	}
	folderID, err := s.documentaClient.CreateFolder(ctx, s.cfg.Documenta.WorkspaceName, rootID, goalFolderName(title, goalID))
	if err != nil || folderID == "" {
		log.Printf("[goal_service] create goal folder for %s failed: %v", goalID, err)
		ge := newGoalErr(ctx, http.StatusBadGateway, "goal_documenta_folder_failed")
		ge.Err = err
		return "", ge
	}
	return folderID, nil
}

// blockedEvidenceExtensions are executable / script types never accepted as
// evidence. (No shared upload allowlist exists elsewhere in the codebase.)
var blockedEvidenceExtensions = map[string]bool{
	".exe": true, ".dll": true, ".com": true, ".bat": true, ".cmd": true, ".msi": true, ".msp": true,
	".scr": true, ".pif": true, ".cpl": true, ".ps1": true, ".psm1": true, ".vbs": true, ".vbe": true,
	".js": true, ".jse": true, ".wsf": true, ".wsh": true, ".hta": true, ".jar": true, ".sh": true,
	".app": true, ".apk": true, ".lnk": true, ".reg": true, ".gadget": true, ".sys": true, ".drv": true,
	".php": true, ".asp": true, ".aspx": true, ".jsp": true, ".py": true, ".pl": true, ".rb": true,
	".elf": true, ".bin": true, ".run": true, ".dmg": true, ".deb": true, ".rpm": true,
}

// validateEvidenceFile rejects empty files and executable/script uploads
// (by extension and by Windows/ELF magic bytes).
func validateEvidenceFile(ctx context.Context, fileName string, fileData []byte) error {
	if len(fileData) == 0 {
		return errBadRequest(ctx, "evidence_empty_file")
	}
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(fileName)))
	if blockedEvidenceExtensions[ext] {
		return errBadRequest(ctx, "evidence_file_type_not_allowed")
	}
	if bytes.HasPrefix(fileData, []byte("MZ")) || bytes.HasPrefix(fileData, []byte{0x7f, 'E', 'L', 'F'}) {
		return errBadRequest(ctx, "evidence_file_type_not_allowed")
	}
	return nil
}

func isValidEvidenceType(t string) bool {
	switch t {
	case models.EvidenceTypeReport, models.EvidenceTypePhoto, models.EvidenceTypeCertificate,
		models.EvidenceTypeInvoice, models.EvidenceTypeOther:
		return true
	}
	return false
}

// validMetadataJSON accepts an empty string or a JSON object.
func validMetadataJSON(meta string) bool {
	if strings.TrimSpace(meta) == "" {
		return true
	}
	var obj map[string]interface{}
	return json.Unmarshal([]byte(meta), &obj) == nil
}
