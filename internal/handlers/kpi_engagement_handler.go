package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/middleware"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/internal/storage"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// KpiEngagementHandler covers the KPI-dictionary-item-level engagement
// features that mirror Goal's Metrics/Evidence/Collaborators/Check-ins/
// Comments/Activity tabs. Every route is scoped by (:type, :id) where :type
// is one of "strategic" | "operational" | "award" and :id is the dictionary
// row's own UUID — see kpi_engagement.go for why this composite identity is
// used instead of a single FK.
type KpiEngagementHandler struct {
	db           *gorm.DB
	validator    *validator.Validate
	actionLogSvc services.ActionLogService
	// storage (MinIO) is kept ONLY to serve evidence rows uploaded before the
	// Documenta integration existed — DownloadEvidence falls back to it when
	// an evidence row has no DocumentaFileID. All NEW uploads go through
	// documentaClient instead (see UploadAttachment).
	storage         *storage.MinIOStorage
	documentaClient storage.DocumentaClient
	documentaCfg    config.DocumentaConfig
	workflowSvc     *services.KpiWorkflowService
	// uploadKey signs the file references UploadAttachment hands out, so
	// CreateEvidence only accepts files this module uploaded for that KPI.
	uploadKey []byte
}

// NewKpiEngagementHandler builds the handler. uploadSecret (the server's JWT
// secret) keys the signature on uploaded evidence references.
func NewKpiEngagementHandler(db *gorm.DB, actionLogSvc services.ActionLogService, storage *storage.MinIOStorage, documentaClient storage.DocumentaClient, documentaCfg config.DocumentaConfig, workflowSvc *services.KpiWorkflowService, uploadSecret string) *KpiEngagementHandler {
	key := sha256.Sum256([]byte("kpi-evidence-upload|" + uploadSecret))
	return &KpiEngagementHandler{
		db:              db,
		validator:       validator.New(),
		actionLogSvc:    actionLogSvc,
		storage:         storage,
		documentaClient: documentaClient,
		documentaCfg:    documentaCfg,
		workflowSvc:     workflowSvc,
		uploadKey:       key[:],
	}
}

func isValidKpiType(t string) bool {
	switch t {
	case models.KPITypeStrategic, models.KPITypeOperational, models.KPITypeAward:
		return true
	}
	return false
}

// kpiExists checks the dictionary row identified by (kpiType, id) actually exists.
func (h *KpiEngagementHandler) kpiExists(kpiType string, id uuid.UUID) bool {
	var count int64
	switch kpiType {
	case models.KPITypeOperational:
		h.db.Model(&models.OperationalKPI{}).Where("id = ?", id).Count(&count)
	case models.KPITypeAward:
		h.db.Model(&models.AwardKPI{}).Where("id = ?", id).Count(&count)
	default:
		h.db.Model(&models.StrategicKPI{}).Where("id = ?", id).Count(&count)
	}
	return count > 0
}

// kpiCodeAndName resolves a KPI dictionary row's code+display name in one
// query — used to build the Documenta folder title for evidence uploads
// ("{code} - {name}"), mirroring how Goal evidence uploads use goal.Title.
func (h *KpiEngagementHandler) kpiCodeAndName(kpiType string, id uuid.UUID) (code string, name string, err error) {
	switch kpiType {
	case models.KPITypeOperational:
		var k models.OperationalKPI
		if err := h.db.Select("code, name_en").Where("id = ?", id).First(&k).Error; err != nil {
			return "", "", err
		}
		return k.Code, k.NameEn, nil
	case models.KPITypeAward:
		var k models.AwardKPI
		if err := h.db.Select("code, name_en").Where("id = ?", id).First(&k).Error; err != nil {
			return "", "", err
		}
		return k.Code, k.NameEn, nil
	default:
		var k models.StrategicKPI
		if err := h.db.Select("code, name_en").Where("id = ?", id).First(&k).Error; err != nil {
			return "", "", err
		}
		return k.Code, k.NameEn, nil
	}
}

// userOnBehalfContext threads the current request's authenticated user email
// into ctx as Documenta's X-On-Behalf-Of identity. Without this, every KPI
// evidence Documenta call fell back to the synthetic default identity
// ("system@automax.local"), which — verified live against mydocs.axionic.io
// — has no real membership/read access in the Documenta workspace: it
// returns 404 "file not found" for files that download fine for a real
// workspace member (e.g. admin@automax.com), which looked exactly like
// random data loss. Goal/Documents (document_authz.go, document_service.go)
// already do this; KPI evidence never did.
func (h *KpiEngagementHandler) userOnBehalfContext(c *fiber.Ctx) context.Context {
	ctx := c.UserContext()
	userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	if !ok {
		return ctx
	}
	var user models.User
	if err := h.db.Select("email").Where("id = ?", userID).First(&user).Error; err != nil || user.Email == "" {
		return ctx
	}
	return storage.ContextWithOnBehalf(ctx, user.Email)
}

func (h *KpiEngagementHandler) parseTypeAndID(c *fiber.Ctx) (string, uuid.UUID, error) {
	kpiType := c.Params("type")
	if !isValidKpiType(kpiType) {
		return "", uuid.Nil, fmt.Errorf("invalid kpi type")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return "", uuid.Nil, err
	}
	return kpiType, id, nil
}

// ─── Metrics ────────────────────────────────────────────────────────────────

func (h *KpiEngagementHandler) ListMetrics(c *fiber.Ctx) error {
	kpiType, id, err := h.parseTypeAndID(c)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	db := h.db.WithContext(c.UserContext())
	var items []models.KpiMetric
	if err := db.
		Where("kpi_id = ? AND kpi_type = ?", id, kpiType).
		Order("created_at ASC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	if kpiCode, err := getKPICode(db, kpiType, id); err == nil && kpiCode != "" {
		for i := range items {
			items[i].EffectiveTargetValue, items[i].EffectiveTargetPeriod =
				services.GetEffectiveTarget(db, kpiCode, kpiType, items[i].ID)
		}
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", items)
}

// ListMetricsByCode returns all metrics for a KPI identified by its code.
// It searches across strategic, operational, and award KPI tables to find the
// matching (kpi_id, kpi_type) pair.
// GET /kpi/metrics-by-code/:code
func (h *KpiEngagementHandler) ListMetricsByCode(c *fiber.Ctx) error {
	kpiCode := strings.TrimSpace(c.Params("code"))
	if decoded, err := url.PathUnescape(kpiCode); err == nil {
		kpiCode = strings.TrimSpace(decoded)
	}
	if kpiCode == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	kpiType := c.Query("type")
	if kpiType != "" && !isValidKpiType(kpiType) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "type must be strategic, operational or award")
	}

	// Codes are matched case-insensitively; ?type= narrows the lookup to one
	// KPI type, otherwise strategic, operational and award are tried in turn.
	type kpiLookup struct {
		ID   uuid.UUID
		Code string
		Type string
	}
	var found *kpiLookup
	for _, t := range []string{models.KPITypeStrategic, models.KPITypeOperational, models.KPITypeAward} {
		if kpiType != "" && t != kpiType {
			continue
		}
		var row struct {
			ID   uuid.UUID
			Code string
		}
		h.db.WithContext(c.UserContext()).Table(kpiDictTable(t)).Select("id, code").
			Where("upper(trim(code)) = upper(?) AND deleted_at IS NULL", kpiCode).Limit(1).Scan(&row)
		if row.ID != uuid.Nil {
			found = &kpiLookup{ID: row.ID, Code: row.Code, Type: t}
			break
		}
	}

	if found == nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	if denied, err := kpiAccessDenied(c, found.Type, found.ID, services.KpiActionView); denied {
		return err
	}

	db := h.db.WithContext(c.UserContext())
	var items []models.KpiMetric
	if err := db.
		Where("kpi_id = ? AND kpi_type = ?", found.ID, found.Type).
		Order("created_at ASC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	for i := range items {
		items[i].EffectiveTargetValue, items[i].EffectiveTargetPeriod =
			services.GetEffectiveTarget(db, found.Code, found.Type, items[i].ID)
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", items)
}

const duplicateMetricNameMsg = "Metric name already exists under this KPI. Please use a unique metric name."

// metricNameTaken reports whether another (non-deleted) metric of the same
// KPI already uses name — compared trimmed and case-insensitively, so
// "Roads Resurfaced" and " roads resurfaced" count as the same name. The
// same name under a different KPI is allowed.
func (h *KpiEngagementHandler) metricNameTaken(db *gorm.DB, kpiType string, kpiID uuid.UUID, name string, excludeID uuid.UUID) bool {
	var count int64
	db.Model(&models.KpiMetric{}).
		Where("kpi_type = ? AND kpi_id = ? AND id <> ? AND lower(trim(name)) = lower(trim(?))", kpiType, kpiID, excludeID, name).
		Count(&count)
	return count > 0
}

var baselinePeriodPattern = regexp.MustCompile(`^\d{4}-(0[1-9]|1[0-2])$`)

// validBaselinePeriod accepts an empty period (not recorded) or "YYYY-MM".
func validBaselinePeriod(p string) bool {
	return p == "" || baselinePeriodPattern.MatchString(p)
}

// metricConfigLocked reports whether the KPI has passed approval (Approved,
// Active or Closed), after which a metric's Weight and Baseline are fixed.
func (h *KpiEngagementHandler) metricConfigLocked(c *fiber.Ctx, kpiType string, kpiID uuid.UUID) (bool, error) {
	status, err := services.GetKpiActivationStatus(h.db.WithContext(c.UserContext()), kpiType, kpiID)
	if err != nil {
		return false, err
	}
	return status == "approved" || status == "active" || status == "closed", nil
}

// metricWeightExceeds reports whether saving an Active metric with this weight
// would push the KPI's Active metric weights past 100% (BR-07). Inactive
// metrics don't count, matching the activation gate. Returns the weight
// already used by the KPI's other Active metrics, for the error message.
func (h *KpiEngagementHandler) metricWeightExceeds(db *gorm.DB, kpiType string, kpiID, excludeID uuid.UUID, status string, weight float64) (float64, bool, error) {
	if status != "Active" {
		return 0, false, nil
	}
	var used float64
	err := db.Model(&models.KpiMetric{}).
		Where("kpi_type = ? AND kpi_id = ? AND id <> ? AND metric_status = ?", kpiType, kpiID, excludeID, "Active").
		Select("COALESCE(SUM(weight), 0)").Scan(&used).Error
	if err != nil {
		return 0, false, err
	}
	used = math.Round(used*100) / 100
	return used, used+weight > 100.005, nil
}

// errMetricRejected carries a 4xx answer out of a metric transaction.
type errMetricRejected struct {
	status int
	msg    string
}

func (e *errMetricRejected) Error() string { return e.msg }

// lockKpiForMetrics takes the KPI row lock that serializes metric writes on
// one KPI, so concurrent creates/updates cannot each pass the 100% weight
// check against the same stale total, or both take the same name.
func lockKpiForMetrics(tx *gorm.DB, kpiType string, kpiID uuid.UUID) error {
	var row struct{ ID uuid.UUID }
	if err := tx.Table(kpiDictTable(kpiType)).Select("id").
		Where("id = ? AND deleted_at IS NULL", kpiID).
		Clauses(clause.Locking{Strength: "UPDATE"}).Limit(1).Scan(&row).Error; err != nil {
		return err
	}
	if row.ID == uuid.Nil {
		return &errMetricRejected{status: fiber.StatusNotFound, msg: "KPI not found"}
	}
	return nil
}

// metricBodyKeys returns the top-level JSON keys present in the request body,
// so an update can tell an omitted field (keep) from an explicit zero/false
// (write). ok=false when the body is not a JSON object.
func metricBodyKeys(c *fiber.Ctx) (map[string]bool, bool) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(c.Body(), &raw); err != nil {
		return nil, false
	}
	keys := make(map[string]bool, len(raw))
	for k := range raw {
		keys[k] = true
	}
	return keys, true
}

// parseMetricOwner validates metric_owner_id: empty clears it, a malformed
// UUID or a user that does not exist is a 400.
func (h *KpiEngagementHandler) parseMetricOwner(db *gorm.DB, raw *string) (*uuid.UUID, string) {
	if raw == nil || strings.TrimSpace(*raw) == "" {
		return nil, ""
	}
	id, err := uuid.Parse(strings.TrimSpace(*raw))
	if err != nil {
		return nil, fmt.Sprintf("metric_owner_id %q is not a valid user ID", *raw)
	}
	var n int64
	db.Model(&models.User{}).Where("id = ?", id).Count(&n)
	if n == 0 {
		return nil, fmt.Sprintf("Metric owner %s was not found", id)
	}
	return &id, ""
}

// metricIssuesMessage joins validation issues into one error message.
func metricIssuesMessage(issues []services.KpiMetricIssue) string {
	msgs := make([]string, len(issues))
	for i, is := range issues {
		msgs[i] = is.Message
	}
	return strings.Join(msgs, " ")
}

// checkMetricFormula validates a Formula metric against the names of the
// KPI's other metrics.
func (h *KpiEngagementHandler) checkMetricFormula(db *gorm.DB, m *models.KpiMetric, excludeID uuid.UUID) string {
	if m.CalculationType != "Formula" {
		return ""
	}
	var names []string
	db.Model(&models.KpiMetric{}).Where("kpi_type = ? AND kpi_id = ? AND id <> ?", m.KpiType, m.KpiID, excludeID).Pluck("name", &names)
	return services.ValidateKpiMetricFormula(m.Formula, m.Name, names)
}

// checkKpiDefinitionEditable answers 403 unless the user may change the
// KPI's definition (its metrics included) in the KPI's current workflow
// state. ok=false means the response is already written.
func (h *KpiEngagementHandler) checkKpiDefinitionEditable(c *fiber.Ctx, kpiType string, kpiID uuid.UUID) (bool, error) {
	if h.workflowSvc == nil {
		return true, nil
	}
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	ok, state, err := h.workflowSvc.CanEditKpiDefinition(c.UserContext(), kpiType, kpiID, userID)
	if errors.Is(err, services.ErrKpiDictionaryNotFound) {
		return false, utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	if err != nil {
		return false, utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "internal_server_error"))
	}
	if !ok {
		return false, utils.ErrorResponse(c, fiber.StatusForbidden,
			fmt.Sprintf("This KPI is %s; your role cannot change its metrics in this workflow state", state))
	}
	return true, nil
}

// metricEntryCount counts the KPI entries of a metric in the given statuses
// (all statuses when none are given).
func metricEntryCount(db *gorm.DB, metricID uuid.UUID, statuses ...string) int64 {
	q := db.Model(&models.KpiEntry{}).Where("metric_id = ?", metricID)
	if len(statuses) > 0 {
		q = q.Where("status IN ?", statuses)
	}
	var n int64
	q.Count(&n)
	return n
}

// metricTxError answers for an error returned from a metric transaction.
func metricTxError(c *fiber.Ctx, err error, fallbackKey string) error {
	var rej *errMetricRejected
	if errors.As(err, &rej) {
		return utils.ErrorResponse(c, rej.status, rej.msg)
	}
	log.Printf("[kpi_engagement] metric write failed: %v", err)
	return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), fallbackKey))
}

func (h *KpiEngagementHandler) CreateMetric(c *fiber.Ctx) error {
	kpiType, id, err := h.parseTypeAndID(c)
	if err != nil || !h.kpiExists(kpiType, id) {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var req models.KpiMetricRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	present, _ := metricBodyKeys(c)
	req.Name = strings.TrimSpace(req.Name)
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	req.BaselinePeriod = strings.TrimSpace(req.BaselinePeriod)
	if !validBaselinePeriod(req.BaselinePeriod) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "kpi_baseline_period_invalid"))
	}
	if ok, resp := h.checkKpiDefinitionEditable(c, kpiType, id); !ok {
		return resp
	}
	db := h.db.WithContext(c.UserContext())
	metricOwnerID, ownerErr := h.parseMetricOwner(db, req.MetricOwnerID)
	if ownerErr != "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, ownerErr)
	}
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	or := func(v, def string) string {
		if strings.TrimSpace(v) == "" {
			return def
		}
		return v
	}
	item := &models.KpiMetric{
		KpiID:                     id,
		KpiType:                   kpiType,
		Name:                      req.Name,
		MetricCode:                strings.TrimSpace(req.MetricCode),
		MetricDescription:         req.MetricDescription,
		MetricStatus:              or(req.MetricStatus, "Active"),
		DisplayOrder:              req.DisplayOrder,
		MetricType:                or(req.MetricType, "Numeric"),
		Unit:                      req.Unit,
		CustomUnitLabel:           req.CustomUnitLabel,
		BaselineValue:             req.BaselineValue,
		BaselinePeriod:            req.BaselinePeriod,
		CurrentValue:              req.BaselineValue,
		Weight:                    req.Weight,
		Formula:                   req.Formula,
		CalculationType:           or(req.CalculationType, "Direct Value"),
		Direction:                 or(req.Direction, "Higher is Better"),
		DecimalPrecision:          req.DecimalPrecision,
		AggregationMethod:         or(req.AggregationMethod, "Sum"),
		ReportingFrequency:        req.ReportingFrequency,
		NumeratorLabel:            req.NumeratorLabel,
		NumeratorVariableCode:     req.NumeratorVariableCode,
		DenominatorLabel:          req.DenominatorLabel,
		DenominatorVariableCode:   req.DenominatorVariableCode,
		DirectActualLabel:         req.DirectActualLabel,
		AllowManualActualOverride: req.AllowManualActualOverride,
		AdvancedFormulaEnabled:    req.AdvancedFormulaEnabled,
		FormulaCode:               req.FormulaCode,
		DivideByZeroHandling:      or(req.DivideByZeroHandling, "Block Submission"),
		RoundingRule:              or(req.RoundingRule, "Standard Round"),
		CalculationTraceRequired:  req.CalculationTraceRequired,
		MetricOwnerID:             metricOwnerID,
		DataSource:                req.DataSource,
		EvidenceRequired:          req.EvidenceRequired,
		StartDate:                 req.StartDate,
		DueDate:                   req.DueDate,
		CreatedByID:               userID,
	}
	// Omitted fields take the form's defaults; an explicit weight of 0 or
	// calculation_trace_required=false is kept (and stored, see
	// models.CreateKpiMetric).
	if !present["weight"] {
		item.Weight = 1
	}
	if !present["calculation_trace_required"] {
		item.CalculationTraceRequired = true
	}
	if issues := services.NormalizeKpiMetricDefinition(item); len(issues) > 0 {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, metricIssuesMessage(issues))
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockKpiForMetrics(tx, kpiType, id); err != nil {
			return err
		}
		if h.metricNameTaken(tx, kpiType, id, item.Name, uuid.Nil) {
			return &errMetricRejected{fiber.StatusConflict, duplicateMetricNameMsg}
		}
		if msg := h.checkMetricFormula(tx, item, uuid.Nil); msg != "" {
			return &errMetricRejected{fiber.StatusBadRequest, msg}
		}
		if used, over, err := h.metricWeightExceeds(tx, kpiType, id, uuid.Nil, item.MetricStatus, item.Weight); err != nil {
			return err
		} else if over {
			return &errMetricRejected{fiber.StatusBadRequest, i18n.Tf(c.UserContext(), "kpi_metric_weight_exceeds", used, 100-used)}
		}
		return models.CreateKpiMetric(tx, item)
	})
	if err != nil {
		return metricTxError(c, err, "failed_to_create")
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "create",
		Module:      "kpi",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("Added metric %q", req.Name),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusCreated, "", item)
}

// UploadAttachment uploads a real file for this KPI (used for a metric's
// attachment or a standalone evidence upload) to the KPI Documenta workspace
// and hands back a reference — it does not create a KpiMetric/KpiEvidence
// row itself, callers pass the returned fields through to
// CreateMetric/CreateEvidence.
//
// The base folder is the KPI's own configured DocumentaFolderID (set once,
// during KPI creation/edit, via the "KPI Evidence Folder Configuration"
// feature — see kpi_documenta_handler.go) — every evidence upload for a KPI
// goes straight into that one folder, nested one level by Evidence Type
// (Report/Photo/Certificate/Invoice/Other):
//
//	{Configured Folder} / {Evidence Type} / file
//
// KPIs that predate this feature (DocumentaFolderID not yet set) fall back
// to deriving one on the fly — same rule as before (Pillar → "{Code} -
// {Name}" for Strategic/Operational; NEW: Criterion → Sub-Criterion →
// "{Code} - {Name}" for Award, which used to hard-block entirely) — and
// persist the result, mirroring goal_service.go's CreateEvidence self-healing
// backfill for goals that predate Documenta.
func (h *KpiEngagementHandler) UploadAttachment(c *fiber.Ctx) error {
	kpiType, id, err := h.parseTypeAndID(c)
	if err != nil || !h.kpiExists(kpiType, id) {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	file, err := c.FormFile("file")
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "no_file_uploaded"))
	}
	src, err := file.Open()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_read_file"))
	}
	defer src.Close()

	ctx := h.userOnBehalfContext(c)

	kpiCode, kpiName, err := h.kpiCodeAndName(kpiType, id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(ctx, "not_found"))
	}

	baseFolderID, resolveErr := h.resolveOrBackfillEvidenceFolder(ctx, kpiType, id, kpiCode, kpiName)
	if resolveErr != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, resolveErr.Error())
	}

	evidenceType := c.FormValue("evidence_type")
	if evidenceType == "" {
		evidenceType = "Report"
	}

	uploadFolderID, err := h.documentaClient.EnsureFolder(ctx, h.documentaCfg.WorkspaceName, baseFolderID, evidenceType)
	if err != nil {
		log.Printf("[kpi_engagement] UploadAttachment: EnsureFolder(%s) failed: %v", evidenceType, err)
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_upload_file"))
	}

	metadata := map[string]string{
		"kpi_id":        id.String(),
		"kpi_code":      kpiCode,
		"kpi_type":      kpiType,
		"kpi_name":      kpiName,
		"evidence_type": evidenceType,
		"metric_id":     "",
		"metric_name":   "",
		"source_system": "automax",
	}

	// metric_id is optional — the standalone Evidence Upload modal lets the
	// user pick "No specific metric", and the inline Metric-creation
	// attachment step always has one since it's uploaded for that exact
	// metric. Recorded as metadata only — the approved hierarchy has no
	// per-metric folder level.
	if metricIDStr := c.FormValue("metric_id"); metricIDStr != "" {
		if metricID, parseErr := uuid.Parse(metricIDStr); parseErr == nil {
			var metric models.KpiMetric
			if h.db.Select("name").Where("id = ?", metricID).First(&metric).Error == nil {
				metadata["metric_id"] = metricID.String()
				metadata["metric_name"] = metric.Name
			}
		}
	}

	if userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID); ok {
		var uploader models.User
		if h.db.Select("email").Where("id = ?", userID).First(&uploader).Error == nil {
			metadata["uploaded_by"] = uploader.Email
		}
	}
	metadata["uploaded_at"] = time.Now().UTC().Format(time.RFC3339)

	documentaFileID, err := h.documentaClient.UploadFile(ctx, uploadFolderID, file.Filename, src, file.Size, metadata)
	if err != nil {
		log.Printf("[kpi_engagement] UploadAttachment: UploadFile failed: %v", err)
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_upload_file"))
	}
	if tagErr := h.documentaClient.SetTags(ctx, documentaFileID, metadata); tagErr != nil {
		log.Printf("[kpi_engagement] UploadAttachment: SetTags failed for file %s: %v", documentaFileID, tagErr)
	}

	// The reference handed back is signed for this KPI; CreateEvidence only
	// accepts references signed here, so evidence can never point at a file
	// another module (or another KPI) owns.
	return utils.SuccessResponse(c, fiber.StatusCreated, "", fiber.Map{
		"documenta_file_id": h.signUploadRef(kpiType, id, documentaFileID),
		"file_name":         file.Filename,
		"file_size":         file.Size,
		"mime_type":         file.Header.Get("Content-Type"),
	})
}

// resolveOrBackfillEvidenceFolder returns the KPI's configured evidence
// folder (DocumentaFolderID), deriving and persisting one on the fly if it
// doesn't have one yet — self-healing backfill for KPIs that predate the
// "KPI Evidence Folder Configuration" feature, mirroring goal_service.go's
// CreateEvidence pattern for Goal.DocumentaFolderID.
func (h *KpiEngagementHandler) resolveOrBackfillEvidenceFolder(ctx context.Context, kpiType string, id uuid.UUID, kpiCode, kpiName string) (string, error) {
	folderID, pillarID, objectiveID, processID, awardSubCriterionID, err := h.loadFolderAndTaxonomy(kpiType, id)
	if err != nil {
		return "", fmt.Errorf("failed to load KPI")
	}
	if folderID != "" {
		return folderID, nil
	}

	anchor, err := services.ResolveTaxonomyAnchor(ctx, h.db, h.documentaClient, h.documentaCfg.WorkspaceName, kpiType, pillarID, objectiveID, processID, awardSubCriterionID)
	if err != nil {
		log.Printf("[kpi_engagement] resolveOrBackfillEvidenceFolder: ResolveTaxonomyAnchor failed: %v", err)
		return "", fmt.Errorf("failed to resolve an evidence folder")
	}
	if anchor == nil {
		return "", fmt.Errorf("%s (%s) has no Evidence Folder configured, and its taxonomy doesn't resolve to one automatically — configure an Evidence Folder for this KPI first", kpiName, kpiCode)
	}

	kpiFolderID, err := h.documentaClient.EnsureFolder(ctx, h.documentaCfg.WorkspaceName, anchor.FolderID, fmt.Sprintf("%s - %s", kpiCode, kpiName))
	if err != nil {
		log.Printf("[kpi_engagement] resolveOrBackfillEvidenceFolder: EnsureFolder(%s - %s) failed: %v", kpiCode, kpiName, err)
		return "", fmt.Errorf("failed to create an evidence folder")
	}

	h.persistDocumentaFolderID(kpiType, id, kpiFolderID)
	return kpiFolderID, nil
}

// loadFolderAndTaxonomy loads a KPI's configured folder id plus whichever
// taxonomy IDs its type carries, for use by resolveOrBackfillEvidenceFolder.
func (h *KpiEngagementHandler) loadFolderAndTaxonomy(kpiType string, id uuid.UUID) (folderID string, pillarID, objectiveID, processID, awardSubCriterionID *uuid.UUID, err error) {
	switch kpiType {
	case models.KPITypeOperational:
		var k models.OperationalKPI
		if err = h.db.Select("documenta_folder_id, operational_objective_id, process_id").Where("id = ?", id).First(&k).Error; err != nil {
			return
		}
		folderID = k.DocumentaFolderID
		objectiveID = &k.OperationalObjectiveID
		processID = &k.ProcessID
	case models.KPITypeAward:
		var k models.AwardKPI
		if err = h.db.Select("documenta_folder_id, award_sub_criterion_id").Where("id = ?", id).First(&k).Error; err != nil {
			return
		}
		folderID = k.DocumentaFolderID
		awardSubCriterionID = k.AwardSubCriterionID
	default:
		var k models.StrategicKPI
		if err = h.db.Select("documenta_folder_id, pillar_id").Where("id = ?", id).First(&k).Error; err != nil {
			return
		}
		folderID = k.DocumentaFolderID
		pillarID = k.PillarID
	}
	return
}

// persistDocumentaFolderID writes a freshly-derived folder id back onto the
// KPI row so subsequent uploads take the fast (already-configured) path.
// KPI dictionary handlers access the DB directly rather than through a
// repository layer, so this does too.
func (h *KpiEngagementHandler) persistDocumentaFolderID(kpiType string, id uuid.UUID, folderID string) {
	var result *gorm.DB
	switch kpiType {
	case models.KPITypeOperational:
		result = h.db.Model(&models.OperationalKPI{}).Where("id = ?", id).Update("documenta_folder_id", folderID)
	case models.KPITypeAward:
		result = h.db.Model(&models.AwardKPI{}).Where("id = ?", id).Update("documenta_folder_id", folderID)
	default:
		result = h.db.Model(&models.StrategicKPI{}).Where("id = ?", id).Update("documenta_folder_id", folderID)
	}
	if result.Error != nil {
		log.Printf("[kpi_engagement] persistDocumentaFolderID: failed to persist folder for %s %s: %v", kpiType, id, result.Error)
	}
}

func (h *KpiEngagementHandler) UpdateMetric(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var req models.KpiMetricRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	// Partial update: only the fields present in the body change; omitted
	// fields keep their stored value (an explicit 0/false/"" is written).
	present, ok := metricBodyKeys(c)
	if !ok {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	db := h.db.WithContext(c.UserContext())
	var current models.KpiMetric
	if err := db.First(&current, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	if ok, resp := h.checkKpiDefinitionEditable(c, current.KpiType, current.KpiID); !ok {
		return resp
	}

	m := current
	set := func(key string, apply func()) {
		if present[key] {
			apply()
		}
	}
	set("name", func() { m.Name = strings.TrimSpace(req.Name) })
	set("metric_code", func() { m.MetricCode = strings.TrimSpace(req.MetricCode) })
	set("metric_description", func() { m.MetricDescription = req.MetricDescription })
	set("metric_status", func() { m.MetricStatus = req.MetricStatus })
	set("display_order", func() { m.DisplayOrder = req.DisplayOrder })
	set("metric_type", func() { m.MetricType = req.MetricType })
	set("unit", func() { m.Unit = req.Unit })
	set("custom_unit_label", func() { m.CustomUnitLabel = req.CustomUnitLabel })
	set("baseline_value", func() { m.BaselineValue = req.BaselineValue })
	set("baseline_period", func() { m.BaselinePeriod = strings.TrimSpace(req.BaselinePeriod) })
	set("weight", func() { m.Weight = req.Weight })
	set("formula", func() { m.Formula = req.Formula })
	set("calculation_type", func() { m.CalculationType = req.CalculationType })
	set("direction", func() { m.Direction = req.Direction })
	set("decimal_precision", func() { m.DecimalPrecision = req.DecimalPrecision })
	set("aggregation_method", func() { m.AggregationMethod = req.AggregationMethod })
	set("reporting_frequency", func() { m.ReportingFrequency = req.ReportingFrequency })
	set("numerator_label", func() { m.NumeratorLabel = req.NumeratorLabel })
	set("numerator_variable_code", func() { m.NumeratorVariableCode = req.NumeratorVariableCode })
	set("denominator_label", func() { m.DenominatorLabel = req.DenominatorLabel })
	set("denominator_variable_code", func() { m.DenominatorVariableCode = req.DenominatorVariableCode })
	set("direct_actual_label", func() { m.DirectActualLabel = req.DirectActualLabel })
	set("allow_manual_actual_override", func() { m.AllowManualActualOverride = req.AllowManualActualOverride })
	set("advanced_formula_enabled", func() { m.AdvancedFormulaEnabled = req.AdvancedFormulaEnabled })
	set("formula_code", func() { m.FormulaCode = req.FormulaCode })
	set("divide_by_zero_handling", func() { m.DivideByZeroHandling = req.DivideByZeroHandling })
	set("rounding_rule", func() { m.RoundingRule = req.RoundingRule })
	set("calculation_trace_required", func() { m.CalculationTraceRequired = req.CalculationTraceRequired })
	set("data_source", func() { m.DataSource = req.DataSource })
	set("evidence_required", func() { m.EvidenceRequired = req.EvidenceRequired })
	set("start_date", func() { m.StartDate = req.StartDate })
	set("due_date", func() { m.DueDate = req.DueDate })
	if present["metric_owner_id"] {
		ownerID, ownerErr := h.parseMetricOwner(db, req.MetricOwnerID)
		if ownerErr != "" {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, ownerErr)
		}
		m.MetricOwnerID = ownerID
	}

	if m.Name == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Metric name is required")
	}
	if !validBaselinePeriod(m.BaselinePeriod) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "kpi_baseline_period_invalid"))
	}
	// Blank enum fields fall back to the same defaults as on create.
	defaults := []struct {
		field *string
		def   string
	}{
		{&m.MetricStatus, "Active"}, {&m.MetricType, "Numeric"}, {&m.CalculationType, "Direct Value"},
		{&m.Direction, "Higher is Better"}, {&m.AggregationMethod, "Sum"},
		{&m.DivideByZeroHandling, "Block Submission"}, {&m.RoundingRule, "Standard Round"},
	}
	for _, d := range defaults {
		if strings.TrimSpace(*d.field) == "" {
			*d.field = d.def
		}
	}
	if issues := services.NormalizeKpiMetricDefinition(&m); len(issues) > 0 {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, metricIssuesMessage(issues))
	}

	// Weight and Baseline are only editable before approval; once the KPI is
	// Approved, Active or Closed they are what was signed off. Baseline then
	// changes only through an approved baseline revision.
	weightChanged := math.Abs(m.Weight-current.Weight) >= 0.005
	baselineChanged := math.Abs(m.BaselineValue-current.BaselineValue) >= 0.00005 ||
		m.BaselinePeriod != current.BaselinePeriod
	if weightChanged || baselineChanged {
		locked, err := h.metricConfigLocked(c, current.KpiType, current.KpiID)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "internal_server_error"))
		}
		if locked && weightChanged {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "kpi_metric_weight_locked"))
		}
		if locked && baselineChanged {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "kpi_metric_baseline_locked"))
		}
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		if err := lockKpiForMetrics(tx, current.KpiType, current.KpiID); err != nil {
			return err
		}
		if h.metricNameTaken(tx, current.KpiType, current.KpiID, m.Name, id) {
			return &errMetricRejected{fiber.StatusConflict, duplicateMetricNameMsg}
		}
		if msg := h.checkMetricFormula(tx, &m, id); msg != "" {
			return &errMetricRejected{fiber.StatusBadRequest, msg}
		}
		if used, over, err := h.metricWeightExceeds(tx, current.KpiType, current.KpiID, id, m.MetricStatus, m.Weight); err != nil {
			return err
		} else if over {
			return &errMetricRejected{fiber.StatusBadRequest, i18n.Tf(c.UserContext(), "kpi_metric_weight_exceeds", used, 100-used)}
		}
		result := tx.Model(&models.KpiMetric{ID: id}).Updates(map[string]interface{}{
			"name":                         m.Name,
			"metric_code":                  m.MetricCode,
			"metric_description":           m.MetricDescription,
			"metric_status":                m.MetricStatus,
			"display_order":                m.DisplayOrder,
			"metric_type":                  m.MetricType,
			"unit":                         m.Unit,
			"custom_unit_label":            m.CustomUnitLabel,
			"baseline_value":               m.BaselineValue,
			"baseline_period":              m.BaselinePeriod,
			"weight":                       m.Weight,
			"formula":                      m.Formula,
			"calculation_type":             m.CalculationType,
			"direction":                    m.Direction,
			"decimal_precision":            m.DecimalPrecision,
			"aggregation_method":           m.AggregationMethod,
			"reporting_frequency":          m.ReportingFrequency,
			"numerator_label":              m.NumeratorLabel,
			"numerator_variable_code":      m.NumeratorVariableCode,
			"denominator_label":            m.DenominatorLabel,
			"denominator_variable_code":    m.DenominatorVariableCode,
			"direct_actual_label":          m.DirectActualLabel,
			"allow_manual_actual_override": m.AllowManualActualOverride,
			"advanced_formula_enabled":     m.AdvancedFormulaEnabled,
			"formula_code":                 m.FormulaCode,
			"divide_by_zero_handling":      m.DivideByZeroHandling,
			"rounding_rule":                m.RoundingRule,
			"calculation_trace_required":   m.CalculationTraceRequired,
			"metric_owner_id":              m.MetricOwnerID,
			"data_source":                  m.DataSource,
			"evidence_required":            m.EvidenceRequired,
			"start_date":                   m.StartDate,
			"due_date":                     m.DueDate,
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return &errMetricRejected{fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found")}
		}
		return nil
	})
	if err != nil {
		return metricTxError(c, err, "failed_to_update")
	}
	var item models.KpiMetric
	db.First(&item, id)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "update",
		Module:      "kpi",
		ResourceID:  item.KpiID.String(),
		Description: fmt.Sprintf("Updated metric %q", item.Name),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", item)
}

// ListBaselineRevisions returns a metric's baseline revision requests, newest
// first. GET /kpi/metrics/:id/baseline-revisions
func (h *KpiEngagementHandler) ListBaselineRevisions(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var items []models.KpiBaselineRevision
	if err := h.db.WithContext(c.UserContext()).
		Preload("RequestedBy").Preload("ReviewedBy").
		Where("metric_id = ?", id).Order("created_at DESC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "internal_server_error"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", items)
}

// RequestBaselineRevision asks to change a locked metric's Baseline. Only one
// request per metric can be pending at a time.
// POST /kpi/metrics/:id/baseline-revisions
func (h *KpiEngagementHandler) RequestBaselineRevision(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var req models.KpiBaselineRevisionRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	req.Reason = strings.TrimSpace(req.Reason)
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	db := h.db.WithContext(c.UserContext())

	var metric models.KpiMetric
	if err := db.Select("id, kpi_id, kpi_type, name, baseline_value, baseline_period").First(&metric, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	locked, err := h.metricConfigLocked(c, metric.KpiType, metric.KpiID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "internal_server_error"))
	}
	if !locked {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "kpi_baseline_not_locked"))
	}
	req.NewPeriod = strings.TrimSpace(req.NewPeriod)
	if req.NewPeriod == "" {
		req.NewPeriod = metric.BaselinePeriod
	}
	if !validBaselinePeriod(req.NewPeriod) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "kpi_baseline_period_invalid"))
	}
	if math.Abs(*req.NewValue-metric.BaselineValue) < 0.00005 && req.NewPeriod == metric.BaselinePeriod {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "kpi_baseline_unchanged"))
	}
	var pending int64
	db.Model(&models.KpiBaselineRevision{}).
		Where("metric_id = ? AND status = ?", id, models.BaselineRevisionPending).Count(&pending)
	if pending > 0 {
		return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "kpi_baseline_revision_pending"))
	}

	item := &models.KpiBaselineRevision{
		MetricID:      metric.ID,
		KpiID:         metric.KpiID,
		KpiType:       metric.KpiType,
		OldValue:      metric.BaselineValue,
		NewValue:      *req.NewValue,
		OldPeriod:     metric.BaselinePeriod,
		NewPeriod:     req.NewPeriod,
		Reason:        req.Reason,
		Status:        models.BaselineRevisionPending,
		RequestedByID: userID,
	}
	if err := db.Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "internal_server_error"))
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "update",
		Module:      "kpi",
		ResourceID:  metric.KpiID.String(),
		Description: fmt.Sprintf("Requested baseline revision for metric %q: %g -> %g", metric.Name, metric.BaselineValue, *req.NewValue),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusCreated, "", item)
}

// ReviewBaselineRevision approves or rejects a pending baseline revision.
// Approving writes the new Baseline onto the metric in the same transaction.
// The requester cannot review their own request.
// POST /kpi/baseline-revisions/:revisionId/review
func (h *KpiEngagementHandler) ReviewBaselineRevision(c *fiber.Ctx) error {
	revisionID, err := uuid.Parse(c.Params("revisionId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var req models.KpiBaselineRevisionReview
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	req.Comment = strings.TrimSpace(req.Comment)
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	var item models.KpiBaselineRevision
	var metricName string
	errStatus, errKey := 0, ""
	txErr := h.db.WithContext(c.UserContext()).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, revisionID).Error; err != nil {
			errStatus, errKey = fiber.StatusNotFound, "not_found"
			return err
		}
		if item.Status != models.BaselineRevisionPending {
			errStatus, errKey = fiber.StatusConflict, "kpi_baseline_revision_reviewed"
			return errors.New(errKey)
		}
		if item.RequestedByID == userID {
			errStatus, errKey = fiber.StatusForbidden, "kpi_baseline_revision_self"
			return errors.New(errKey)
		}
		var metric models.KpiMetric
		if err := tx.Select("id, name").First(&metric, item.MetricID).Error; err != nil {
			errStatus, errKey = fiber.StatusNotFound, "not_found"
			return err
		}
		metricName = metric.Name

		now := time.Now()
		item.ReviewedByID = &userID
		item.ReviewedAt = &now
		item.ReviewComment = req.Comment
		item.Status = models.BaselineRevisionRejected
		if req.Decision == "approve" {
			item.Status = models.BaselineRevisionApproved
			if err := tx.Model(&models.KpiMetric{ID: item.MetricID}).
				Updates(map[string]interface{}{"baseline_value": item.NewValue, "baseline_period": item.NewPeriod}).Error; err != nil {
				return err
			}
		}
		return tx.Save(&item).Error
	})
	if txErr != nil {
		if errStatus != 0 {
			return utils.ErrorResponse(c, errStatus, i18n.T(c.UserContext(), errKey))
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "internal_server_error"))
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "update",
		Module:      "kpi",
		ResourceID:  item.KpiID.String(),
		Description: fmt.Sprintf("Baseline revision for metric %q %s: %g -> %g", metricName, item.Status, item.OldValue, item.NewValue),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", item)
}

func (h *KpiEngagementHandler) UpdateMetricValue(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var req models.KpiMetricValueRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if req.Value == nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "value is required")
	}
	if math.IsNaN(*req.Value) || math.IsInf(*req.Value, 0) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "value must be a finite number")
	}

	db := h.db.WithContext(c.UserContext())

	// REL-11: once approved data exists — approved KPI entries for this
	// metric, or approved legacy performance records for its KPI — the
	// metric's actual comes from that workflow, not a direct overwrite.
	var metric models.KpiMetric
	if err := db.First(&metric, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	if metricEntryCount(db, id, models.KpiEntryStatusApproved, models.KpiEntryStatusSuperseded,
		models.KpiEntryStatusReversed, models.KpiEntryStatusReversal) > 0 {
		return utils.ErrorResponse(c, fiber.StatusForbidden,
			"Cannot update metric current_value directly when approved KPI entries exist. Use the entry workflow instead.")
	}
	var approvedCount int64
	db.Model(&models.KpiPerformance{}).
		Where("kpi_code IN (SELECT code FROM strategic_kpis WHERE id = ? UNION SELECT code FROM operational_kpis WHERE id = ? UNION SELECT code FROM award_kpis WHERE id = ?)",
			metric.KpiID, metric.KpiID, metric.KpiID).
		Where("status = ?", models.KPIPerfStatusApproved).
		Count(&approvedCount)
	if approvedCount > 0 {
		return utils.ErrorResponse(c, fiber.StatusForbidden,
			"Cannot update metric current_value directly when approved performance entries exist. Use the performance entry workflow instead.")
	}

	result := db.Model(&models.KpiMetric{ID: id}).
		Update("current_value", *req.Value)
	if result.Error != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update"))
	}
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	db.First(&metric, id)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "update",
		Module:      "kpi",
		ResourceID:  metric.KpiID.String(),
		Description: fmt.Sprintf("Updated actual value for metric %q to %v", metric.Name, *req.Value),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", metric)
}

func (h *KpiEngagementHandler) DeleteMetric(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	db := h.db.WithContext(c.UserContext())
	var item models.KpiMetric
	if err := db.First(&item, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	if ok, resp := h.checkKpiDefinitionEditable(c, item.KpiType, item.KpiID); !ok {
		return resp
	}
	// Entries (and their approvals) point at the metric; deleting it would
	// orphan them. Such a metric is retired by setting its status to
	// Inactive or Archived instead.
	if n := metricEntryCount(db, id); n > 0 {
		return utils.ErrorResponse(c, fiber.StatusConflict,
			fmt.Sprintf("Metric %q has %d KPI entr(ies) and cannot be deleted; set its status to Inactive or Archived instead.", item.Name, n))
	}
	result := db.Delete(&models.KpiMetric{}, id)
	if result.Error != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "internal_server_error"))
	}
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "delete",
		Module:      "kpi",
		ResourceID:  item.KpiID.String(),
		Description: fmt.Sprintf("Deleted metric %q", item.Name),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

// ─── Evidence ───────────────────────────────────────────────────────────────

func (h *KpiEngagementHandler) ListEvidence(c *fiber.Ctx) error {
	kpiType, id, err := h.parseTypeAndID(c)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var items []models.KpiEvidence
	if err := h.db.WithContext(c.UserContext()).
		Preload("UploadedBy").
		Preload("Metric").
		Where("kpi_id = ? AND kpi_type = ?", id, kpiType).
		Order("created_at DESC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", items)
}

// uploadRefSep separates a file ID from its signature in the reference
// UploadAttachment returns.
const uploadRefSep = "~"

func (h *KpiEngagementHandler) uploadRefMAC(kpiType string, kpiID uuid.UUID, fileID string) string {
	mac := hmac.New(sha256.New, h.uploadKey)
	mac.Write([]byte(kpiType + "|" + kpiID.String() + "|" + fileID))
	return hex.EncodeToString(mac.Sum(nil))
}

// signUploadRef returns "fileID~signature" for a file uploaded to this KPI.
func (h *KpiEngagementHandler) signUploadRef(kpiType string, kpiID uuid.UUID, fileID string) string {
	return fileID + uploadRefSep + h.uploadRefMAC(kpiType, kpiID, fileID)
}

// verifyUploadRef returns the file ID of a reference signed by
// signUploadRef for this same KPI, or ok=false.
func (h *KpiEngagementHandler) verifyUploadRef(kpiType string, kpiID uuid.UUID, ref string) (string, bool) {
	i := strings.LastIndex(ref, uploadRefSep)
	if i <= 0 {
		return "", false
	}
	fileID, sig := ref[:i], ref[i+len(uploadRefSep):]
	if !hmac.Equal([]byte(sig), []byte(h.uploadRefMAC(kpiType, kpiID, fileID))) {
		return "", false
	}
	return fileID, true
}

// isExternalLink reports whether s is an absolute http(s) URL — the only
// kind of file_url evidence may carry. It is shown as a link and never
// fetched or streamed by the server.
func isExternalLink(s string) bool {
	u, err := url.Parse(strings.TrimSpace(s))
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func (h *KpiEngagementHandler) CreateEvidence(c *fiber.Ctx) error {
	kpiType, id, err := h.parseTypeAndID(c)
	if err != nil || !h.kpiExists(kpiType, id) {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var req models.KpiEvidenceRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	req.Title = strings.TrimSpace(req.Title)
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}

	// A file must come from this module's own upload for this KPI (signed
	// reference); file_url may only be an external http(s) link. Anything
	// else — e.g. an object-storage key of another module's attachment —
	// is rejected, since DownloadEvidence would otherwise stream it.
	documentaFileID := ""
	if strings.TrimSpace(req.DocumentaFileID) != "" {
		fileID, ok := h.verifyUploadRef(kpiType, id, strings.TrimSpace(req.DocumentaFileID))
		if !ok {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "documenta_file_id must be the reference returned by this KPI's attachment upload")
		}
		documentaFileID = fileID
	}
	fileURL := strings.TrimSpace(req.FileURL)
	if fileURL != "" && !isExternalLink(fileURL) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "file_url must be an http(s) link; upload files through the KPI attachment upload")
	}
	fileName, fileSize, mimeType := req.FileName, req.FileSize, req.MimeType
	if documentaFileID == "" {
		// Nothing was uploaded: there is no file to describe or download.
		fileName, fileSize, mimeType = "", 0, ""
	}
	if req.MetricID != nil {
		var n int64
		h.db.WithContext(c.UserContext()).Model(&models.KpiMetric{}).
			Where("id = ? AND kpi_id = ? AND kpi_type = ?", *req.MetricID, id, kpiType).Count(&n)
		if n == 0 {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "metric_id is not a metric of this KPI")
		}
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	evidenceType := req.EvidenceType
	if evidenceType == "" {
		evidenceType = "Report"
	}
	item := &models.KpiEvidence{
		KpiID:           id,
		KpiType:         kpiType,
		Title:           req.Title,
		EvidenceType:    evidenceType,
		Description:     req.Description,
		MetricID:        req.MetricID,
		FileURL:         fileURL,
		DocumentaFileID: documentaFileID,
		FileName:        filepath.Base(strings.ReplaceAll(fileName, "\\", "/")),
		FileSize:        fileSize,
		MimeType:        mimeType,
		UploadedByID:    userID,
	}
	if fileName == "" {
		item.FileName = ""
	}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}
	h.db.WithContext(c.UserContext()).Preload("UploadedBy").Preload("Metric").First(item, item.ID)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "create",
		Module:      "kpi",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("Added evidence %q", item.Title),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusCreated, "", item)
}

// canRemoveContribution reports whether the current user may delete a
// check-in/evidence/comment authored by authorID on the KPI: its author, the
// KPI's Owner (collaborator type), an Administrator or the Super Admin.
func (h *KpiEngagementHandler) canRemoveContribution(c *fiber.Ctx, kpiType string, kpiID, authorID uuid.UUID) (bool, error) {
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	if userID == authorID {
		return true, nil
	}
	var user models.User
	if err := h.db.WithContext(c.UserContext()).Preload("Roles", "is_active = ?", true).First(&user, "id = ?", userID).Error; err != nil {
		return false, err
	}
	if user.IsSuperAdmin || user.HasRole("admin") {
		return true, nil
	}
	collaboratorType, err := services.NewKpiAccessService(h.db).CollaboratorType(c.UserContext(), userID, kpiType, kpiID)
	if err != nil {
		return false, err
	}
	return collaboratorType == models.CollaboratorTypeOwner, nil
}

// requireRemovalRight writes 403 (or 500) unless canRemoveContribution.
func (h *KpiEngagementHandler) requireRemovalRight(c *fiber.Ctx, kpiType string, kpiID, authorID uuid.UUID) (bool, error) {
	ok, err := h.canRemoveContribution(c, kpiType, kpiID, authorID)
	if err != nil {
		return false, utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "internal_server_error"))
	}
	if !ok {
		return false, utils.ErrorResponse(c, fiber.StatusForbidden, "Only the author, the KPI Owner or an administrator can delete this")
	}
	return true, nil
}

func (h *KpiEngagementHandler) DeleteEvidence(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var item models.KpiEvidence
	if err := h.db.WithContext(c.UserContext()).First(&item, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	if ok, resp := h.requireRemovalRight(c, item.KpiType, item.KpiID, item.UploadedByID); !ok {
		return resp
	}

	// Best-effort cleanup in Documenta — mirrors Goal evidence deletion: a
	// failed remote delete never blocks removing the local record, it's just
	// logged (matches models.Evidence's DocumentaFileID handling in
	// goal_service.go DeleteEvidence).
	if item.DocumentaFileID != "" {
		if delErr := h.documentaClient.DeleteFile(h.userOnBehalfContext(c), item.DocumentaFileID); delErr != nil {
			log.Printf("[kpi_engagement] DeleteEvidence: failed to delete file from Documenta: %v", delErr)
		}
	}

	result := h.db.WithContext(c.UserContext()).Delete(&models.KpiEvidence{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "delete",
		Module:      "kpi",
		ResourceID:  item.KpiID.String(),
		Description: fmt.Sprintf("Deleted evidence %q", item.Title),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

// DownloadEvidence streams a real-uploaded evidence file back from storage.
// Evidence rows created the legacy way (a user-typed external URL, no
// FileName) have nothing in object storage to stream — callers should just
// link to file_url directly in that case. Rows uploaded after the Documenta
// integration (DocumentaFileID set) stream from there; older rows (FileURL
// only, from before the integration) keep streaming from MinIO — no
// historical files were migrated, per product decision.
// evidenceSafeMimeTypes are the content types DownloadEvidence serves as-is;
// anything else (e.g. text/html, image/svg+xml) goes out as
// application/octet-stream so a stored file can never render as active
// content in the app's origin.
var evidenceSafeMimeTypes = map[string]bool{
	"application/pdf": true, "image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true,
	"text/plain": true, "text/csv": true, "application/zip": true,
	"application/msword": true, "application/vnd.ms-excel": true, "application/vnd.ms-powerpoint": true,
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   true,
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         true,
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": true,
}

// evidenceContentType picks the response Content-Type from the stored type,
// falling back to sniffing the bytes, limited to evidenceSafeMimeTypes.
func evidenceContentType(declared string, data []byte) string {
	for _, candidate := range []string{declared, http.DetectContentType(data)} {
		base, _, err := mime.ParseMediaType(candidate)
		if err == nil && evidenceSafeMimeTypes[strings.ToLower(base)] {
			return strings.ToLower(base)
		}
	}
	return "application/octet-stream"
}

// setEvidenceDownloadHeaders sets a safe Content-Type and a properly quoted
// (RFC 6266 / 2231) attachment filename.
func setEvidenceDownloadHeaders(c *fiber.Ctx, contentType, fileName string) {
	name := filepath.Base(strings.ReplaceAll(fileName, "\\", "/"))
	if name == "." || name == "/" || name == "" {
		name = "evidence"
	}
	disposition := mime.FormatMediaType("attachment", map[string]string{"filename": name})
	if disposition == "" {
		disposition = "attachment"
	}
	c.Set(fiber.HeaderContentType, contentType)
	c.Set(fiber.HeaderContentDisposition, disposition)
	c.Set(fiber.HeaderXContentTypeOptions, "nosniff")
}

// DownloadEvidence streams an uploaded evidence file. Only files this module
// stored for the evidence's own KPI are served: Documenta files (referenced
// by a signed upload, see CreateEvidence) and legacy MinIO objects whose key
// lies under a KPI evidence folder of this KPI. Evidence that is only an
// external link has nothing to stream (400).
func (h *KpiEngagementHandler) DownloadEvidence(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var item models.KpiEvidence
	if err := h.db.WithContext(c.UserContext()).First(&item, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	if item.FileName == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "not_found"))
	}

	if item.DocumentaFileID != "" {
		reader, info, err := h.documentaClient.DownloadFile(h.userOnBehalfContext(c), item.DocumentaFileID)
		if err != nil {
			log.Printf("[kpi_engagement] DownloadEvidence: DownloadFile failed: %v", err)
			if errors.Is(err, storage.ErrFileNotFound) {
				return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
			}
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_retrieve_file"))
		}
		defer reader.Close()

		// Buffered rather than c.SendStream(reader) — piping a net/http
		// response body straight into fasthttp's stream writer produced an
		// empty/reset connection when verified live against the real
		// Documenta server (curl: "Empty reply from server"), even though
		// Fiber's own access log recorded 200 and Documenta's endpoint
		// itself streams correctly when called directly. Evidence files
		// (reports, certificates, photos) are never large enough for
		// buffering to matter — this sidesteps the incompatibility entirely.
		data, readErr := io.ReadAll(reader)
		if readErr != nil {
			log.Printf("[kpi_engagement] DownloadEvidence: reading Documenta file body failed: %v", readErr)
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_retrieve_file"))
		}

		mimeType := item.MimeType
		if mimeType == "" && info != nil {
			mimeType = info.MimeType
		}
		setEvidenceDownloadHeaders(c, evidenceContentType(mimeType, data), item.FileName)
		return c.Send(data)
	}

	if !legacyEvidenceKeyOwned(item.FileURL, item.KpiID) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "This evidence has no uploaded file to download")
	}
	file, err := h.storage.GetFile(c.UserContext(), item.FileURL)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_retrieve_file"))
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, 100<<20))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_retrieve_file"))
	}
	setEvidenceDownloadHeaders(c, evidenceContentType(item.MimeType, data), item.FileName)
	return c.Send(data)
}

// legacyEvidenceKeyOwned accepts only pre-Documenta MinIO keys this module
// wrote for the evidence's own KPI: a relative "kpi…/" key containing the
// KPI's ID as a path segment. URLs, absolute or traversing paths, and keys
// of other modules (incident attachments, ...) are refused.
func legacyEvidenceKeyOwned(key string, kpiID uuid.UUID) bool {
	key = strings.TrimSpace(key)
	if key == "" || strings.Contains(key, "://") || strings.HasPrefix(key, "/") || strings.Contains(key, "..") {
		return false
	}
	if !strings.HasPrefix(strings.ToLower(key), "kpi") {
		return false
	}
	for _, seg := range strings.Split(key, "/") {
		if seg == kpiID.String() {
			return true
		}
	}
	return false
}

// ─── Collaborators ──────────────────────────────────────────────────────────

func (h *KpiEngagementHandler) ListCollaborators(c *fiber.Ctx) error {
	kpiType, id, err := h.parseTypeAndID(c)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var items []models.KpiCollaborator
	if err := h.db.WithContext(c.UserContext()).
		Preload("User").
		Where("kpi_id = ? AND kpi_type = ?", id, kpiType).
		Order("created_at ASC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", items)
}

func (h *KpiEngagementHandler) AddCollaborator(c *fiber.Ctx) error {
	kpiType, id, err := h.parseTypeAndID(c)
	if err != nil || !h.kpiExists(kpiType, id) {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var req models.KpiCollaboratorAddRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	role := req.Role
	if role == "" {
		role = models.KpiCollaboratorRoleCollaborator
	}
	db := h.db.WithContext(c.UserContext())
	var userCount, dupCount int64
	db.Model(&models.User{}).Where("id = ?", req.UserID).Count(&userCount)
	if userCount == 0 {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, fmt.Sprintf("User %s was not found", req.UserID))
	}
	db.Model(&models.KpiCollaborator{}).Where("kpi_id = ? AND user_id = ?", id, req.UserID).Count(&dupCount)
	if dupCount > 0 {
		return utils.ErrorResponse(c, fiber.StatusConflict, "This user is already a collaborator on this KPI")
	}
	item := &models.KpiCollaborator{
		KpiID:   id,
		KpiType: kpiType,
		UserID:  req.UserID,
		Role:    role,
	}
	if err := db.Create(item).Error; err != nil {
		if strings.Contains(err.Error(), "23505") || strings.Contains(err.Error(), "duplicate key") {
			return utils.ErrorResponse(c, fiber.StatusConflict, "This user is already a collaborator on this KPI")
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}
	h.db.WithContext(c.UserContext()).Preload("User").First(item, item.ID)
	return utils.SuccessResponse(c, fiber.StatusCreated, "", item)
}

func (h *KpiEngagementHandler) RemoveCollaborator(c *fiber.Ctx) error {
	kpiType, id, err := h.parseTypeAndID(c)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	userID, err := uuid.Parse(c.Params("user_id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	result := h.db.WithContext(c.UserContext()).
		Where("kpi_id = ? AND kpi_type = ? AND user_id = ?", id, kpiType, userID).
		Delete(&models.KpiCollaborator{})
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

// ─── Check-ins ──────────────────────────────────────────────────────────────

func (h *KpiEngagementHandler) ListCheckIns(c *fiber.Ctx) error {
	kpiType, id, err := h.parseTypeAndID(c)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 10)
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 10
	}
	q := h.db.WithContext(c.UserContext()).Model(&models.KpiCheckIn{}).
		Where("kpi_id = ? AND kpi_type = ?", id, kpiType)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	var items []models.KpiCheckIn
	if err := q.Preload("Author").Order("created_at DESC").
		Offset((page - 1) * limit).Limit(limit).Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	return c.JSON(fiber.Map{"success": true, "data": items, "total": total, "page": page, "limit": limit})
}

func (h *KpiEngagementHandler) CreateCheckIn(c *fiber.Ctx) error {
	kpiType, id, err := h.parseTypeAndID(c)
	if err != nil || !h.kpiExists(kpiType, id) {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var req models.KpiCheckInRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	if !models.IsValidKpiCheckInStatus(req.Status) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	item := &models.KpiCheckIn{
		KpiID:    id,
		KpiType:  kpiType,
		AuthorID: userID,
		Status:   req.Status,
		Content:  req.Content,
	}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}
	h.db.WithContext(c.UserContext()).Preload("Author").First(item, item.ID)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "check_in",
		Module:      "kpi",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("Checked in on KPI (%s)", req.Status),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusCreated, "", item)
}

func (h *KpiEngagementHandler) DeleteCheckIn(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var item models.KpiCheckIn
	if err := h.db.WithContext(c.UserContext()).First(&item, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	if ok, resp := h.requireRemovalRight(c, item.KpiType, item.KpiID, item.AuthorID); !ok {
		return resp
	}
	result := h.db.WithContext(c.UserContext()).Delete(&models.KpiCheckIn{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "delete",
		Module:      "kpi",
		ResourceID:  item.KpiID.String(),
		Description: "Deleted a check-in",
		Status:      "success",
	})
	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

// ─── Comments ───────────────────────────────────────────────────────────────

func (h *KpiEngagementHandler) ListComments(c *fiber.Ctx) error {
	kpiType, id, err := h.parseTypeAndID(c)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 20)
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	q := h.db.WithContext(c.UserContext()).Model(&models.KpiComment{}).
		Where("kpi_id = ? AND kpi_type = ?", id, kpiType)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	var items []models.KpiComment
	if err := q.Preload("Author").Order("created_at ASC").
		Offset((page - 1) * limit).Limit(limit).Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	return c.JSON(fiber.Map{"success": true, "data": items, "total": total, "page": page, "limit": limit})
}

func (h *KpiEngagementHandler) AddComment(c *fiber.Ctx) error {
	kpiType, id, err := h.parseTypeAndID(c)
	if err != nil || !h.kpiExists(kpiType, id) {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var req models.KpiCommentRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	item := &models.KpiComment{
		KpiID:    id,
		KpiType:  kpiType,
		AuthorID: userID,
		Content:  req.Content,
	}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}
	h.db.WithContext(c.UserContext()).Preload("Author").First(item, item.ID)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "comment",
		Module:      "kpi",
		ResourceID:  id.String(),
		Description: "Added a comment",
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusCreated, "", item)
}

func (h *KpiEngagementHandler) DeleteComment(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var comment models.KpiComment
	if err := h.db.WithContext(c.UserContext()).First(&comment, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	if ok, resp := h.requireRemovalRight(c, comment.KpiType, comment.KpiID, comment.AuthorID); !ok {
		return resp
	}
	if err := h.db.WithContext(c.UserContext()).Delete(&models.KpiComment{}, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_delete_comment"))
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "delete",
		Module:      "kpi",
		ResourceID:  comment.KpiID.String(),
		Description: "Deleted a comment",
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

// ─── Activity ───────────────────────────────────────────────────────────────

func (h *KpiEngagementHandler) GetActivity(c *fiber.Ctx) error {
	_, id, err := h.parseTypeAndID(c)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	page := c.QueryInt("page", 1)
	limit := c.QueryInt("limit", 20)

	filter := &models.ActionLogFilter{
		Module:     "kpi",
		ResourceID: id.String(),
		Page:       page,
		Limit:      limit,
	}
	logs, total, err := h.actionLogSvc.ListActionLogs(c.UserContext(), filter)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}
	humanizeKpiActivity(h.db.WithContext(c.UserContext()), logs)
	return c.JSON(fiber.Map{"success": true, "data": logs, "total": total, "page": page, "limit": limit})
}
