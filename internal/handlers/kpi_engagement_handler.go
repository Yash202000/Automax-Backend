package handlers

import (
	"context"
	"fmt"
	"io"
	"log"
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
}

func NewKpiEngagementHandler(db *gorm.DB, actionLogSvc services.ActionLogService, storage *storage.MinIOStorage, documentaClient storage.DocumentaClient, documentaCfg config.DocumentaConfig) *KpiEngagementHandler {
	return &KpiEngagementHandler{
		db:              db,
		validator:       validator.New(),
		actionLogSvc:    actionLogSvc,
		storage:         storage,
		documentaClient: documentaClient,
		documentaCfg:    documentaCfg,
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
	kpiCode := c.Params("code")
	if kpiCode == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	// Search across all three KPI dictionary tables
	type kpiLookup struct {
		ID   uuid.UUID
		Type string
	}
	var found *kpiLookup

	var skpi models.StrategicKPI
	if err := h.db.WithContext(c.UserContext()).Where("code = ?", kpiCode).First(&skpi).Error; err == nil {
		found = &kpiLookup{ID: skpi.ID, Type: models.KPITypeStrategic}
	}
	if found == nil {
		var okpi models.OperationalKPI
		if err := h.db.WithContext(c.UserContext()).Where("code = ?", kpiCode).First(&okpi).Error; err == nil {
			found = &kpiLookup{ID: okpi.ID, Type: models.KPITypeOperational}
		}
	}
	if found == nil {
		var akpi models.AwardKPI
		if err := h.db.WithContext(c.UserContext()).Where("code = ?", kpiCode).First(&akpi).Error; err == nil {
			found = &kpiLookup{ID: akpi.ID, Type: models.KPITypeAward}
		}
	}

	if found == nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
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
			services.GetEffectiveTarget(db, kpiCode, found.Type, items[i].ID)
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", items)
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
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	metricType := req.MetricType
	if metricType == "" {
		metricType = "Numeric"
	}
	calcType := req.CalculationType
	if calcType == "" {
		calcType = "Direct Value"
	}
	direction := req.Direction
	if direction == "" {
		direction = "Higher is Better"
	}
	aggMethod := req.AggregationMethod
	if aggMethod == "" {
		aggMethod = "Sum"
	}
	metricStatus := req.MetricStatus
	if metricStatus == "" {
		metricStatus = "Active"
	}
	divideByZero := req.DivideByZeroHandling
	if divideByZero == "" {
		divideByZero = "Block Submission"
	}
	roundingRule := req.RoundingRule
	if roundingRule == "" {
		roundingRule = "Standard Round"
	}
	var metricOwnerID *uuid.UUID
	if req.MetricOwnerID != nil && *req.MetricOwnerID != "" {
		if mid, err := uuid.Parse(*req.MetricOwnerID); err == nil {
			metricOwnerID = &mid
		}
	}
	item := &models.KpiMetric{
		KpiID:                    id,
		KpiType:                  kpiType,
		Name:                     req.Name,
		MetricCode:               req.MetricCode,
		MetricDescription:        req.MetricDescription,
		MetricStatus:             metricStatus,
		DisplayOrder:             req.DisplayOrder,
		MetricType:               metricType,
		Unit:                     req.Unit,
		CustomUnitLabel:          req.CustomUnitLabel,
		BaselineValue:            req.BaselineValue,
		CurrentValue:             req.BaselineValue,
		Weight:                   req.Weight,
		Formula:                  req.Formula,
		CalculationType:          calcType,
		Direction:                direction,
		DecimalPrecision:         req.DecimalPrecision,
		AggregationMethod:        aggMethod,
		ReportingFrequency:       req.ReportingFrequency,
		NumeratorLabel:           req.NumeratorLabel,
		NumeratorVariableCode:    req.NumeratorVariableCode,
		DenominatorLabel:         req.DenominatorLabel,
		DenominatorVariableCode:  req.DenominatorVariableCode,
		DirectActualLabel:        req.DirectActualLabel,
		AllowManualActualOverride: req.AllowManualActualOverride,
		AdvancedFormulaEnabled:   req.AdvancedFormulaEnabled,
		FormulaCode:              req.FormulaCode,
		DivideByZeroHandling:     divideByZero,
		RoundingRule:             roundingRule,
		CalculationTraceRequired: req.CalculationTraceRequired,
		MetricOwnerID:            metricOwnerID,
		DataSource:               req.DataSource,
		EvidenceRequired:         req.EvidenceRequired,
		StartDate:                req.StartDate,
		DueDate:                  req.DueDate,
		CreatedByID:              userID,
	}
	if item.Weight == 0 {
		item.Weight = 1
	}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
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

	ctx := c.UserContext()

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

	return utils.SuccessResponse(c, fiber.StatusCreated, "", fiber.Map{
		"documenta_file_id": documentaFileID,
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
		awardSubCriterionID = &k.AwardSubCriterionID
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
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}
	calcType := req.CalculationType
	if calcType == "" {
		calcType = "Direct Value"
	}
	direction := req.Direction
	if direction == "" {
		direction = "Higher is Better"
	}
	aggMethod := req.AggregationMethod
	if aggMethod == "" {
		aggMethod = "Sum"
	}
	metricStatus := req.MetricStatus
	if metricStatus == "" {
		metricStatus = "Active"
	}
	divideByZero := req.DivideByZeroHandling
	if divideByZero == "" {
		divideByZero = "Block Submission"
	}
	roundingRule := req.RoundingRule
	if roundingRule == "" {
		roundingRule = "Standard Round"
	}
	var metricOwnerID *uuid.UUID
	if req.MetricOwnerID != nil && *req.MetricOwnerID != "" {
		if mid, err := uuid.Parse(*req.MetricOwnerID); err == nil {
			metricOwnerID = &mid
		}
	}
	result := h.db.WithContext(c.UserContext()).Model(&models.KpiMetric{ID: id}).Updates(map[string]interface{}{
		"name":                        req.Name,
		"metric_code":                 req.MetricCode,
		"metric_description":          req.MetricDescription,
		"metric_status":               metricStatus,
		"display_order":               req.DisplayOrder,
		"metric_type":                 req.MetricType,
		"unit":                        req.Unit,
		"custom_unit_label":           req.CustomUnitLabel,
		"baseline_value":              req.BaselineValue,
		"weight":                      req.Weight,
		"formula":                     req.Formula,
		"calculation_type":            calcType,
		"direction":                   direction,
		"decimal_precision":           req.DecimalPrecision,
		"aggregation_method":          aggMethod,
		"reporting_frequency":         req.ReportingFrequency,
		"numerator_label":             req.NumeratorLabel,
		"numerator_variable_code":     req.NumeratorVariableCode,
		"denominator_label":           req.DenominatorLabel,
		"denominator_variable_code":   req.DenominatorVariableCode,
		"direct_actual_label":         req.DirectActualLabel,
		"allow_manual_actual_override": req.AllowManualActualOverride,
		"advanced_formula_enabled":    req.AdvancedFormulaEnabled,
		"formula_code":                req.FormulaCode,
		"divide_by_zero_handling":     divideByZero,
		"rounding_rule":               roundingRule,
		"calculation_trace_required":  req.CalculationTraceRequired,
		"metric_owner_id":             metricOwnerID,
		"data_source":                 req.DataSource,
		"evidence_required":           req.EvidenceRequired,
		"start_date":                  req.StartDate,
		"due_date":                    req.DueDate,
	})
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var item models.KpiMetric
	h.db.WithContext(c.UserContext()).First(&item, id)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "update",
		Module:      "kpi",
		ResourceID:  item.KpiID.String(),
		Description: fmt.Sprintf("Updated metric %q", item.Name),
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

	db := h.db.WithContext(c.UserContext())

	// REL-11: Check if there are approved performance entries for any metric
	// that would make direct current_value updates inconsistent.
	var metric models.KpiMetric
	if err := db.First(&metric, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
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
		Update("current_value", req.Value)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	h.db.WithContext(c.UserContext()).First(&metric, id)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "update",
		Module:      "kpi",
		ResourceID:  metric.KpiID.String(),
		Description: fmt.Sprintf("Updated actual value for metric %q to %v", metric.Name, req.Value),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", metric)
}

func (h *KpiEngagementHandler) DeleteMetric(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var item models.KpiMetric
	h.db.WithContext(c.UserContext()).First(&item, id)
	result := h.db.WithContext(c.UserContext()).Delete(&models.KpiMetric{}, id)
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

func (h *KpiEngagementHandler) CreateEvidence(c *fiber.Ctx) error {
	kpiType, id, err := h.parseTypeAndID(c)
	if err != nil || !h.kpiExists(kpiType, id) {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var req models.KpiEvidenceRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
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
		FileURL:         req.FileURL,
		DocumentaFileID: req.DocumentaFileID,
		FileName:        req.FileName,
		FileSize:        req.FileSize,
		MimeType:        req.MimeType,
		UploadedByID:    userID,
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

func (h *KpiEngagementHandler) DeleteEvidence(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}
	var item models.KpiEvidence
	h.db.WithContext(c.UserContext()).First(&item, id)

	// Best-effort cleanup in Documenta — mirrors Goal evidence deletion: a
	// failed remote delete never blocks removing the local record, it's just
	// logged (matches models.Evidence's DocumentaFileID handling in
	// goal_service.go DeleteEvidence).
	if item.DocumentaFileID != "" {
		if delErr := h.documentaClient.DeleteFile(c.UserContext(), item.DocumentaFileID); delErr != nil {
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
		reader, info, err := h.documentaClient.DownloadFile(c.UserContext(), item.DocumentaFileID)
		if err != nil {
			log.Printf("[kpi_engagement] DownloadEvidence: DownloadFile failed: %v", err)
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
		c.Set("Content-Type", mimeType)
		c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", item.FileName))
		return c.Send(data)
	}

	file, err := h.storage.GetFile(c.UserContext(), item.FileURL)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_retrieve_file"))
	}
	c.Set("Content-Type", item.MimeType)
	c.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%s", item.FileName))
	return c.SendStream(file)
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
	item := &models.KpiCollaborator{
		KpiID:   id,
		KpiType: kpiType,
		UserID:  req.UserID,
		Role:    role,
	}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
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
	result := h.db.WithContext(c.UserContext()).Delete(&models.KpiCheckIn{}, id)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
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
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	user, _ := c.Locals(constants.ContextKeys.User).(*models.User)
	isSuperAdmin := user != nil && user.IsSuperAdmin
	if comment.AuthorID != userID && !isSuperAdmin {
		return utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(c.UserContext(), "access_denied"))
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
	return c.JSON(fiber.Map{"success": true, "data": logs, "total": total, "page": page, "limit": limit})
}
