package handlers

import (
	"fmt"

	"github.com/automax/backend/internal/middleware"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
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
}

func NewKpiEngagementHandler(db *gorm.DB, actionLogSvc services.ActionLogService) *KpiEngagementHandler {
	return &KpiEngagementHandler{
		db:           db,
		validator:    validator.New(),
		actionLogSvc: actionLogSvc,
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
	var items []models.KpiMetric
	if err := h.db.WithContext(c.UserContext()).
		Where("kpi_id = ? AND kpi_type = ?", id, kpiType).
		Order("created_at ASC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
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
	item := &models.KpiMetric{
		KpiID:         id,
		KpiType:       kpiType,
		Name:          req.Name,
		MetricType:    metricType,
		Unit:          req.Unit,
		BaselineValue: req.BaselineValue,
		CurrentValue:  req.BaselineValue,
		TargetValue:   req.TargetValue,
		Weight:        req.Weight,
		Formula:       req.Formula,
		StartDate:     req.StartDate,
		DueDate:       req.DueDate,
		CreatedByID:   userID,
	}
	if item.Weight == 0 {
		item.Weight = 1
	}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}

	// An attachment on a metric is really just evidence for this KPI — create
	// a real KpiEvidence row so it shows up as a manageable entry under the
	// Evidence tab instead of being siloed on the metric.
	if req.AttachmentFileURL != "" {
		title := req.AttachmentTitle
		if title == "" {
			title = fmt.Sprintf("Attachment for metric: %s", req.Name)
		}
		h.db.WithContext(c.UserContext()).Create(&models.KpiEvidence{
			KpiID:        id,
			KpiType:      kpiType,
			Title:        title,
			Description:  fmt.Sprintf("Uploaded with metric %q", req.Name),
			FileURL:      req.AttachmentFileURL,
			UploadedByID: userID,
		})
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
	result := h.db.WithContext(c.UserContext()).Model(&models.KpiMetric{ID: id}).Updates(map[string]interface{}{
		"name":           req.Name,
		"metric_type":    req.MetricType,
		"unit":           req.Unit,
		"baseline_value": req.BaselineValue,
		"target_value":   req.TargetValue,
		"weight":         req.Weight,
		"formula":        req.Formula,
		"start_date":     req.StartDate,
		"due_date":       req.DueDate,
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
	result := h.db.WithContext(c.UserContext()).Model(&models.KpiMetric{ID: id}).
		Update("current_value", req.Value)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	var item models.KpiMetric
	h.db.WithContext(c.UserContext()).First(&item, id)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "update",
		Module:      "kpi",
		ResourceID:  item.KpiID.String(),
		Description: fmt.Sprintf("Updated actual value for metric %q to %v", item.Name, req.Value),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", item)
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
	item := &models.KpiEvidence{
		KpiID:        id,
		KpiType:      kpiType,
		Title:        req.Title,
		Description:  req.Description,
		FileURL:      req.FileURL,
		UploadedByID: userID,
	}
	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}
	h.db.WithContext(c.UserContext()).Preload("UploadedBy").First(item, item.ID)

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
