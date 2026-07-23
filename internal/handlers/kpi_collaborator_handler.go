package handlers

import (
	"fmt"
	"time"

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

type KpiCollaboratorHandler struct {
	db           *gorm.DB
	validator    *validator.Validate
	actionLogSvc services.ActionLogService
}

func NewKpiCollaboratorHandler(db *gorm.DB, actionLogSvc services.ActionLogService) *KpiCollaboratorHandler {
	return &KpiCollaboratorHandler{
		db:           db,
		validator:    validator.New(),
		actionLogSvc: actionLogSvc,
	}
}

func (h *KpiCollaboratorHandler) parseTypeAndID(c *fiber.Ctx) (string, uuid.UUID, error) {
	kpiType := c.Params("type")
	if !isValidKpiType(kpiType) {
		return "", uuid.Nil, fmt.Errorf("invalid type")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return "", uuid.Nil, err
	}
	return kpiType, id, nil
}

func (h *KpiCollaboratorHandler) ListAssignments(c *fiber.Ctx) error {
	kpiType, id, err := h.parseTypeAndID(c)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var items []models.KpiCollaboratorAssignment
	q := h.db.WithContext(c.UserContext()).
		Preload("User").
		Preload("DelegateForUser").
		Preload("CreatedBy").
		Preload("UpdatedBy").
		Where("kpi_id = ? AND kpi_type = ?", id, kpiType)

	if active := c.Query("active"); active != "" {
		q = q.Where("is_active = ?", active == "true")
	}
	if collabType := c.Query("collaborator_type"); collabType != "" {
		q = q.Where("collaborator_type = ?", collabType)
	}

	if err := q.Order("created_at ASC").Find(&items).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_load_data"))
	}

	resp := make([]models.KpiCollaboratorAssignmentResponse, len(items))
	for i, item := range items {
		resp[i] = toAssignmentResponse(&item)
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", resp)
}

func (h *KpiCollaboratorHandler) GetAssignment(c *fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("assignmentId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var item models.KpiCollaboratorAssignment
	if err := h.db.WithContext(c.UserContext()).
		Preload("User").
		Preload("DelegateForUser").
		Preload("CreatedBy").
		Preload("UpdatedBy").
		First(&item, id).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", toAssignmentResponse(&item))
}

func (h *KpiCollaboratorHandler) CreateAssignment(c *fiber.Ctx) error {
	kpiType, id, err := h.parseTypeAndID(c)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.KpiCollaboratorAssignmentRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{"success": false, "errors": validationErrors})
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	effectiveFrom, err := time.Parse("2006-01-02", req.EffectiveFrom)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid effective_from date format (use YYYY-MM-DD)")
	}
	var effectiveTo *time.Time
	if req.EffectiveTo != "" {
		t, err := time.Parse("2006-01-02", req.EffectiveTo)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid effective_to date format (use YYYY-MM-DD)")
		}
		effectiveTo = &t
	}
	if effectiveTo != nil && effectiveTo.Before(effectiveFrom) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "effective_to must be on or after effective_from")
	}

	metricScope := req.MetricScope
	if metricScope == "" {
		metricScope = "All Metrics"
	}
	periodScope := req.PeriodScope
	if periodScope == "" {
		periodScope = "All Periods"
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	item := &models.KpiCollaboratorAssignment{
		KpiID:              id,
		KpiType:            kpiType,
		UserID:             req.UserID,
		UserCategory:       req.UserCategory,
		CollaboratorType:   req.CollaboratorType,
		OrganizationScope:  req.OrganizationScope,
		MetricScope:        metricScope,
		MetricScopeIDs:     req.MetricScopeIDs,
		PeriodScope:        periodScope,
		PeriodScopeYear:    req.PeriodScopeYear,
		PeriodScopePeriods: req.PeriodScopePeriods,
		EffectiveFrom:      effectiveFrom,
		EffectiveTo:        effectiveTo,
		IsActive:           isActive,
		DelegateForUserID:  req.DelegateForUserID,
		DelegationReason:   req.DelegationReason,
		NotificationPrefs:  req.NotificationPrefs,
		CreatedByID:        userID,
		UpdatedByID:        userID,
	}

	if err := h.db.WithContext(c.UserContext()).Create(item).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create"))
	}

	h.db.WithContext(c.UserContext()).
		Preload("User").
		Preload("DelegateForUser").
		Preload("CreatedBy").
		Preload("UpdatedBy").
		First(item, item.ID)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "create",
		Module:      "kpi",
		ResourceID:  id.String(),
		Description: fmt.Sprintf("Assigned user %s as %s for KPI", req.UserID.String(), req.CollaboratorType),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusCreated, "", toAssignmentResponse(item))
}

func (h *KpiCollaboratorHandler) UpdateAssignment(c *fiber.Ctx) error {
	assignmentID, err := uuid.Parse(c.Params("assignmentId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var item models.KpiCollaboratorAssignment
	if err := h.db.WithContext(c.UserContext()).First(&item, assignmentID).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	var req models.KpiCollaboratorAssignmentRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	effectiveFrom, err := time.Parse("2006-01-02", req.EffectiveFrom)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid effective_from date format (use YYYY-MM-DD)")
	}
	var effectiveTo *time.Time
	if req.EffectiveTo != "" {
		t, err := time.Parse("2006-01-02", req.EffectiveTo)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid effective_to date format (use YYYY-MM-DD)")
		}
		effectiveTo = &t
	}
	if effectiveTo != nil && effectiveTo.Before(effectiveFrom) {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "effective_to must be on or after effective_from")
	}

	metricScope := req.MetricScope
	if metricScope == "" {
		metricScope = "All Metrics"
	}
	periodScope := req.PeriodScope
	if periodScope == "" {
		periodScope = "All Periods"
	}
	isActive := true
	if req.IsActive != nil {
		isActive = *req.IsActive
	}

	updates := map[string]interface{}{
		"user_category":        req.UserCategory,
		"collaborator_type":    req.CollaboratorType,
		"organization_scope":   req.OrganizationScope,
		"metric_scope":         metricScope,
		"metric_scope_ids":     req.MetricScopeIDs,
		"period_scope":         periodScope,
		"period_scope_year":    req.PeriodScopeYear,
		"period_scope_periods": req.PeriodScopePeriods,
		"effective_from":       effectiveFrom,
		"effective_to":         effectiveTo,
		"is_active":            isActive,
		"delegate_for_user_id": req.DelegateForUserID,
		"delegation_reason":    req.DelegationReason,
		"notification_prefs":   req.NotificationPrefs,
		"updated_by_id":        userID,
	}

	result := h.db.WithContext(c.UserContext()).Model(&item).Updates(updates)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	h.db.WithContext(c.UserContext()).
		Preload("User").
		Preload("DelegateForUser").
		Preload("CreatedBy").
		Preload("UpdatedBy").
		First(&item, assignmentID)

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "update",
		Module:      "kpi",
		ResourceID:  item.KpiID.String(),
		Description: fmt.Sprintf("Updated assignment for user %s", item.UserID.String()),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", toAssignmentResponse(&item))
}

func (h *KpiCollaboratorHandler) DeleteAssignment(c *fiber.Ctx) error {
	assignmentID, err := uuid.Parse(c.Params("assignmentId"))
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var item models.KpiCollaboratorAssignment
	if err := h.db.WithContext(c.UserContext()).First(&item, assignmentID).Error; err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	result := h.db.WithContext(c.UserContext()).Delete(&item)
	if result.RowsAffected == 0 {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "not_found"))
	}

	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action:      "delete",
		Module:      "kpi",
		ResourceID:  item.KpiID.String(),
		Description: fmt.Sprintf("Removed assignment for user %s (deleted by %s)", item.UserID.String(), userID.String()),
		Status:      "success",
	})

	return utils.SuccessResponse(c, fiber.StatusOK, "", nil)
}

func (h *KpiCollaboratorHandler) GetPermissionMatrix(c *fiber.Ctx) error {
	matrix := h.buildPermissionMatrix()
	return utils.SuccessResponse(c, fiber.StatusOK, "", matrix)
}

func (h *KpiCollaboratorHandler) buildPermissionMatrix() []models.CollaboratorPermissionMatrix {
	return []models.CollaboratorPermissionMatrix{
		{
			CollaboratorType:   models.CollaboratorTypeOwner,
			ViewKPI:            true,
			ViewEntries:        true,
			CreateDraft:        true,
			EditOwnDraft:       true,
			EditOthersDraft:    "Configurable",
			SubmitEntry:        true,
			Review:             "Configurable",
			Return:             true,
			ApproveReject:      "No by default",
			ManageTargets:      "Configurable",
			ManageCollabs:      true,
			ScopeRule:          "Assigned KPI",
			CriticalConstraint: "Ownership does not automatically grant approval; separation of duties may apply.",
		},
		{
			CollaboratorType:   models.CollaboratorTypeDataContributor,
			ViewKPI:            true,
			ViewEntries:        true,
			CreateDraft:        true,
			EditOwnDraft:       true,
			EditOthersDraft:    "No",
			SubmitEntry:        false,
			Review:             "No",
			Return:             false,
			ApproveReject:      "No",
			ManageTargets:      "No",
			ManageCollabs:      false,
			ScopeRule:          "Assigned Metrics/Org/Periods",
			CriticalConstraint: "Prepares data but cannot submit.",
		},
		{
			CollaboratorType:   models.CollaboratorTypeDataSubmitter,
			ViewKPI:            true,
			ViewEntries:        true,
			CreateDraft:        true,
			EditOwnDraft:       true,
			EditOthersDraft:    "Configurable",
			SubmitEntry:        true,
			Review:             "No",
			Return:             false,
			ApproveReject:      "No",
			ManageTargets:      "No",
			ManageCollabs:      false,
			ScopeRule:          "Assigned Metrics/Org/Periods",
			CriticalConstraint: "Can submit only when active and within scope.",
		},
		{
			CollaboratorType:   models.CollaboratorTypeReviewer,
			ViewKPI:            true,
			ViewEntries:        true,
			CreateDraft:        false,
			EditOwnDraft:       false,
			EditOthersDraft:    "No",
			SubmitEntry:        false,
			Review:             "Yes",
			Return:             true,
			ApproveReject:      "No",
			ManageTargets:      "No",
			ManageCollabs:      false,
			ScopeRule:          "Assigned review scope",
			CriticalConstraint: "May review/return; cannot approve unless separately assigned as Approver.",
		},
		{
			CollaboratorType:   models.CollaboratorTypeApprover,
			ViewKPI:            true,
			ViewEntries:        true,
			CreateDraft:        false,
			EditOwnDraft:       false,
			EditOthersDraft:    "No",
			SubmitEntry:        false,
			Review:             "Yes",
			Return:             true,
			ApproveReject:      "Yes",
			ManageTargets:      "No",
			ManageCollabs:      false,
			ScopeRule:          "Assigned approval scope",
			CriticalConstraint: "Approval should respect separation-of-duties policy.",
		},
		{
			CollaboratorType:   models.CollaboratorTypeViewer,
			ViewKPI:            true,
			ViewEntries:        true,
			CreateDraft:        false,
			EditOwnDraft:       false,
			EditOthersDraft:    "No",
			SubmitEntry:        false,
			Review:             "No",
			Return:             false,
			ApproveReject:      "No",
			ManageTargets:      "No",
			ManageCollabs:      false,
			ScopeRule:          "Assigned KPI scope",
			CriticalConstraint: "Read-only access.",
		},
	}
}

func toAssignmentResponse(item *models.KpiCollaboratorAssignment) models.KpiCollaboratorAssignmentResponse {
	resp := models.KpiCollaboratorAssignmentResponse{
		ID:                 item.ID,
		KpiID:              item.KpiID,
		KpiType:            item.KpiType,
		UserID:             item.UserID,
		UserCategory:       item.UserCategory,
		CollaboratorType:   item.CollaboratorType,
		OrganizationScope:  item.OrganizationScope,
		MetricScope:        item.MetricScope,
		MetricScopeIDs:     item.MetricScopeIDs,
		PeriodScope:        item.PeriodScope,
		PeriodScopeYear:    item.PeriodScopeYear,
		PeriodScopePeriods: item.PeriodScopePeriods,
		EffectiveFrom:      &item.EffectiveFrom,
		EffectiveTo:        item.EffectiveTo,
		IsActive:           item.IsActive,
		DelegateForUserID:  item.DelegateForUserID,
		DelegationReason:   item.DelegationReason,
		NotificationPrefs:  item.NotificationPrefs,
		CreatedByID:        item.CreatedByID,
		CreatedAt:          item.CreatedAt,
		UpdatedAt:          item.UpdatedAt,
	}
	if item.User != nil {
		resp.User = &models.UserBrief{
			ID:        item.User.ID,
			FirstName: item.User.FirstName,
			LastName:  item.User.LastName,
			Email:     item.User.Email,
			IsActive:  item.User.IsActive,
		}
	}
	if item.DelegateForUser != nil {
		resp.DelegateForUser = &models.UserBrief{
			ID:        item.DelegateForUser.ID,
			FirstName: item.DelegateForUser.FirstName,
			LastName:  item.DelegateForUser.LastName,
			Email:     item.DelegateForUser.Email,
			IsActive:  item.DelegateForUser.IsActive,
		}
	}
	if item.CreatedBy != nil {
		resp.CreatedBy = &models.UserBrief{
			ID:        item.CreatedBy.ID,
			FirstName: item.CreatedBy.FirstName,
			LastName:  item.CreatedBy.LastName,
			Email:     item.CreatedBy.Email,
			IsActive:  item.CreatedBy.IsActive,
		}
	}
	return resp
}
