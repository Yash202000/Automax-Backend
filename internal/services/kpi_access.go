package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// KpiAction is something a user may do on one KPI. Per-KPI access comes from
// the KPI's collaborator assignments (the Collaborators tab Permission
// Matrix); the route's role permission still applies on top, so a user needs
// both.
type KpiAction string

const (
	KpiActionView                KpiAction = "view"
	KpiActionEditKpi             KpiAction = "edit_kpi"   // definition, metrics, evidence, lifecycle, baseline
	KpiActionContribute          KpiAction = "contribute" // check-ins, comments
	KpiActionCreateEntry         KpiAction = "create_entry"
	KpiActionSubmitEntry         KpiAction = "submit_entry"
	KpiActionReview              KpiAction = "review" // review / return
	KpiActionApprove             KpiAction = "approve"
	KpiActionManageTargets       KpiAction = "manage_targets"
	KpiActionManageCollaborators KpiAction = "manage_collaborators"
)

// kpiActionsByCollaboratorType follows the Permission Matrix. "Configurable"
// cells are granted (the role permission can still withhold them); "No by
// default" cells are not.
var kpiActionsByCollaboratorType = map[string][]KpiAction{
	models.CollaboratorTypeOwner: {
		KpiActionView, KpiActionEditKpi, KpiActionContribute, KpiActionCreateEntry,
		KpiActionSubmitEntry, KpiActionReview, KpiActionManageTargets, KpiActionManageCollaborators,
	},
	models.CollaboratorTypeDataContributor: {KpiActionView, KpiActionContribute, KpiActionCreateEntry},
	models.CollaboratorTypeDataSubmitter:   {KpiActionView, KpiActionContribute, KpiActionCreateEntry, KpiActionSubmitEntry},
	models.CollaboratorTypeReviewer:        {KpiActionView, KpiActionContribute, KpiActionReview},
	models.CollaboratorTypeApprover:        {KpiActionView, KpiActionContribute, KpiActionReview, KpiActionApprove},
	models.CollaboratorTypeViewer:          {KpiActionView},
}

// KpiActionsFor lists the actions a collaborator type grants on its KPI.
func KpiActionsFor(collaboratorType string) []KpiAction {
	return kpiActionsByCollaboratorType[collaboratorType]
}

func collaboratorTypeAllows(collaboratorType string, action KpiAction) bool {
	for _, a := range kpiActionsByCollaboratorType[collaboratorType] {
		if a == action {
			return true
		}
	}
	return false
}

// activeKpiAssignmentCond keeps assignments that are switched on and inside
// their Effective From / Effective To dates (both inclusive). Evaluated on
// every request, so access changes apply without signing out.
const activeKpiAssignmentCond = "a.is_active AND a.deleted_at IS NULL" +
	" AND a.effective_from::date <= CURRENT_DATE" +
	" AND (a.effective_to IS NULL OR a.effective_to::date >= CURRENT_DATE)"

// kpiCodeUnionSQL maps every KPI (any type) to its code.
const kpiCodeUnionSQL = `(
	SELECT id, code, 'strategic' AS kpi_type FROM strategic_kpis WHERE deleted_at IS NULL
	UNION ALL
	SELECT id, code, 'operational' FROM operational_kpis WHERE deleted_at IS NULL
	UNION ALL
	SELECT id, code, 'award' FROM award_kpis WHERE deleted_at IS NULL
)`

// KpiAccessScope is the set of KPIs one user may see: every KPI for the
// Super Admin, otherwise only those they hold an active collaborator
// assignment on. Its condition builders return "" when nothing needs
// filtering; the user ID is a parsed UUID, so inlining it is safe.
type KpiAccessScope struct {
	All    bool
	UserID uuid.UUID
}

func (s KpiAccessScope) assignedSQL(cols, extra string) string {
	return fmt.Sprintf("SELECT %s FROM kpi_collaborator_assignments a WHERE a.user_id = '%s' AND %s%s",
		cols, s.UserID, activeKpiAssignmentCond, extra)
}

// IDCondition restricts idCol, the ID of a KPI of the given type
// (strategic|operational|award), to the scope.
func (s KpiAccessScope) IDCondition(kpiType, idCol string) string {
	if s.All {
		return ""
	}
	if !isKpiType(kpiType) {
		return "1 = 0"
	}
	return fmt.Sprintf("%s IN (%s)", idCol, s.assignedSQL("a.kpi_id", fmt.Sprintf(" AND a.kpi_type = '%s'", kpiType)))
}

// TypedIDCondition restricts rows carrying both a KPI type and a KPI ID.
func (s KpiAccessScope) TypedIDCondition(typeCol, idCol string) string {
	if s.All {
		return ""
	}
	return fmt.Sprintf("(%s, %s) IN (%s)", typeCol, idCol, s.assignedSQL("a.kpi_type, a.kpi_id", ""))
}

// CodeCondition restricts rows that reference their KPI by type + code
// (targets, legacy performance, benchmarks, segmentation).
func (s KpiAccessScope) CodeCondition(typeCol, codeCol string) string {
	if s.All {
		return ""
	}
	return fmt.Sprintf("(%s, %s) IN (SELECT k.kpi_type, k.code FROM %s k WHERE (k.kpi_type, k.id) IN (%s))",
		typeCol, codeCol, kpiCodeUnionSQL, s.assignedSQL("a.kpi_type, a.kpi_id", ""))
}

// Apply adds cond to q when it is non-empty.
func (s KpiAccessScope) Apply(q *gorm.DB, cond string) *gorm.DB {
	if cond == "" {
		return q
	}
	return q.Where(cond)
}

func isKpiType(t string) bool {
	return t == models.KPITypeStrategic || t == models.KPITypeOperational || t == models.KPITypeAward
}

// KpiAccessService decides per-KPI access from collaborator assignments.
type KpiAccessService struct {
	db *gorm.DB
}

func NewKpiAccessService(db *gorm.DB) *KpiAccessService {
	return &KpiAccessService{db: db}
}

func (s *KpiAccessService) isSuperAdmin(ctx context.Context, userID uuid.UUID) (bool, error) {
	var superAdmin bool
	err := s.db.WithContext(ctx).Model(&models.User{}).
		Select("is_super_admin").Where("id = ?", userID).Scan(&superAdmin).Error
	return superAdmin, err
}

// Scope returns which KPIs the user may see.
func (s *KpiAccessService) Scope(ctx context.Context, userID uuid.UUID) (KpiAccessScope, error) {
	superAdmin, err := s.isSuperAdmin(ctx, userID)
	if err != nil {
		return KpiAccessScope{}, err
	}
	return KpiAccessScope{All: superAdmin, UserID: userID}, nil
}

// CollaboratorType returns the user's active collaborator type on the KPI,
// or "" when they have none.
func (s *KpiAccessService) CollaboratorType(ctx context.Context, userID uuid.UUID, kpiType string, kpiID uuid.UUID) (string, error) {
	var collaboratorType string
	err := s.db.WithContext(ctx).Table("kpi_collaborator_assignments a").
		Select("a.collaborator_type").
		Where("a.user_id = ? AND a.kpi_type = ? AND a.kpi_id = ? AND "+activeKpiAssignmentCond, userID, kpiType, kpiID).
		Limit(1).Scan(&collaboratorType).Error
	return collaboratorType, err
}

// Can reports whether the user may perform action on the KPI.
func (s *KpiAccessService) Can(ctx context.Context, userID uuid.UUID, kpiType string, kpiID uuid.UUID, action KpiAction) (bool, error) {
	superAdmin, err := s.isSuperAdmin(ctx, userID)
	if err != nil || superAdmin {
		return superAdmin, err
	}
	collaboratorType, err := s.CollaboratorType(ctx, userID, kpiType, kpiID)
	if err != nil {
		return false, err
	}
	return collaboratorTypeAllows(collaboratorType, action), nil
}

// KpiAssignmentAccess is one KPI the user is assigned to and what they may do.
type KpiAssignmentAccess struct {
	KpiType          string      `json:"kpi_type"`
	KpiID            uuid.UUID   `json:"kpi_id"`
	KpiCode          string      `json:"kpi_code"`
	CollaboratorType string      `json:"collaborator_type"`
	Actions          []KpiAction `json:"actions" gorm:"-"`
}

// MyKpiAccess is the current user's KPI access, for the client to decide
// which KPI actions to offer.
type MyKpiAccess struct {
	IsSuperAdmin bool                  `json:"is_super_admin"`
	Kpis         []KpiAssignmentAccess `json:"kpis"`
}

func (s *KpiAccessService) MyAccess(ctx context.Context, userID uuid.UUID) (*MyKpiAccess, error) {
	superAdmin, err := s.isSuperAdmin(ctx, userID)
	if err != nil {
		return nil, err
	}
	resp := &MyKpiAccess{IsSuperAdmin: superAdmin, Kpis: []KpiAssignmentAccess{}}
	if superAdmin {
		return resp, nil
	}
	if err := s.db.WithContext(ctx).Table("kpi_collaborator_assignments a").
		Joins("JOIN "+kpiCodeUnionSQL+" k ON k.id = a.kpi_id AND k.kpi_type = a.kpi_type").
		Select("a.kpi_type, a.kpi_id, k.code AS kpi_code, a.collaborator_type").
		Where("a.user_id = ? AND "+activeKpiAssignmentCond, userID).
		Scan(&resp.Kpis).Error; err != nil {
		return nil, err
	}
	for i := range resp.Kpis {
		resp.Kpis[i].Actions = KpiActionsFor(resp.Kpis[i].CollaboratorType)
	}
	return resp, nil
}

// KpiRef identifies one KPI.
type KpiRef struct {
	KpiType string
	KpiID   uuid.UUID
}

// ErrKpiNotFound is returned when a record's KPI cannot be resolved.
var ErrKpiNotFound = errors.New("kpi not found")

// KpiOfRecord resolves the KPI that owns a row of a table carrying
// kpi_type + kpi_id (entries, metrics, evidence, comments, ...).
func (s *KpiAccessService) KpiOfRecord(ctx context.Context, table string, id uuid.UUID) (KpiRef, error) {
	var ref struct {
		KpiType string
		KpiID   uuid.UUID
	}
	if err := s.db.WithContext(ctx).Table(table).Select("kpi_type, kpi_id").
		Where("id = ?", id).Limit(1).Scan(&ref).Error; err != nil {
		return KpiRef{}, err
	}
	if ref.KpiID == uuid.Nil {
		return KpiRef{}, ErrKpiNotFound
	}
	return KpiRef{KpiType: ref.KpiType, KpiID: ref.KpiID}, nil
}

// KpiOfCodedRecord resolves the KPI of a row that references it by
// kpi_type + kpi_code (targets, legacy performance).
func (s *KpiAccessService) KpiOfCodedRecord(ctx context.Context, table string, id uuid.UUID) (KpiRef, error) {
	var ref struct {
		KpiType string
		KpiCode string
	}
	if err := s.db.WithContext(ctx).Table(table).Select("kpi_type, kpi_code").
		Where("id = ?", id).Limit(1).Scan(&ref).Error; err != nil {
		return KpiRef{}, err
	}
	if ref.KpiCode == "" {
		return KpiRef{}, ErrKpiNotFound
	}
	return s.KpiByCode(ctx, ref.KpiType, ref.KpiCode)
}

// KpiByCode resolves a KPI from its type and code.
func (s *KpiAccessService) KpiByCode(ctx context.Context, kpiType, code string) (KpiRef, error) {
	var id uuid.UUID
	if err := s.db.WithContext(ctx).Table(kpiCodeUnionSQL+" k").Select("k.id").
		Where("k.kpi_type = ? AND k.code = ?", kpiType, code).Limit(1).Scan(&id).Error; err != nil {
		return KpiRef{}, err
	}
	if id == uuid.Nil {
		return KpiRef{}, ErrKpiNotFound
	}
	return KpiRef{KpiType: kpiType, KpiID: id}, nil
}

// AssignCreatorAsOwner makes the creator of a new KPI its KPI Owner so they
// keep access to it. The Super Admin sees every KPI and is not assigned.
func (s *KpiAccessService) AssignCreatorAsOwner(ctx context.Context, userID uuid.UUID, kpiType string, kpiID uuid.UUID) error {
	superAdmin, err := s.isSuperAdmin(ctx, userID)
	if err != nil || superAdmin {
		return err
	}
	return s.db.WithContext(ctx).Create(&models.KpiCollaboratorAssignment{
		KpiID:            kpiID,
		KpiType:          kpiType,
		UserID:           userID,
		UserCategory:     models.UserCategoryInternalEmployee,
		CollaboratorType: models.CollaboratorTypeOwner,
		MetricScope:      "All Metrics",
		PeriodScope:      models.PeriodScopeAllPeriods,
		EffectiveFrom:    time.Now(),
		IsActive:         true,
		CreatedByID:      userID,
		UpdatedByID:      userID,
	}).Error
}

// KpiTransitionAction maps a workflow transition code (entry approval, KPI
// lifecycle, target review action) to the KPI action it needs.
func KpiTransitionAction(code string) KpiAction {
	switch code {
	case "submit":
		return KpiActionSubmitEntry
	case "review", "review_done", "request_changes", "return":
		return KpiActionReview
	case "activate", "close":
		return KpiActionEditKpi
	default: // approve*, reject, reviewed (-> approved), publish
		return KpiActionApprove
	}
}

// TransitionCode returns the code of a workflow transition, or "" when it
// does not exist.
func (s *KpiAccessService) TransitionCode(ctx context.Context, transitionID uuid.UUID) (string, error) {
	var code string
	err := s.db.WithContext(ctx).Table("workflow_transitions").Select("code").
		Where("id = ?", transitionID).Limit(1).Scan(&code).Error
	return code, err
}

// Allowed returns a check of the actions the user may perform on the KPI,
// for filtering what a response offers.
func (s *KpiAccessService) Allowed(ctx context.Context, userID uuid.UUID, kpiType string, kpiID uuid.UUID) (allows func(KpiAction) bool, err error) {
	superAdmin, err := s.isSuperAdmin(ctx, userID)
	if err != nil {
		return nil, err
	}
	if superAdmin {
		return func(KpiAction) bool { return true }, nil
	}
	collaboratorType, err := s.CollaboratorType(ctx, userID, kpiType, kpiID)
	if err != nil {
		return nil, err
	}
	return func(a KpiAction) bool { return collaboratorTypeAllows(collaboratorType, a) }, nil
}

// CorrectiveActionPerformance returns the legacy performance record a
// corrective action belongs to.
func (s *KpiAccessService) CorrectiveActionPerformance(ctx context.Context, id uuid.UUID) (uuid.UUID, error) {
	var perfID uuid.UUID
	if err := s.db.WithContext(ctx).Table("kpi_corrective_actions").Select("kpi_performance_id").
		Where("id = ?", id).Limit(1).Scan(&perfID).Error; err != nil {
		return uuid.Nil, err
	}
	if perfID == uuid.Nil {
		return uuid.Nil, ErrKpiNotFound
	}
	return perfID, nil
}
