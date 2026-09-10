package handlers

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type ClassificationHandler struct {
	repo      repository.ClassificationRepository
	validator *validator.Validate
	cfg       *config.Config
}

func NewClassificationHandler(repo repository.ClassificationRepository, cfg *config.Config) *ClassificationHandler {
	return &ClassificationHandler{
		repo:      repo,
		validator: validator.New(),
		cfg:       cfg,
	}
}

// parseTypeParam splits a comma-separated ?type= query param into a []string.
// e.g. "incident,mobile,ivr" → ["incident","mobile","ivr"]
// Empty string → nil (no filter).
func parseTypeParam(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (h *ClassificationHandler) Create(c *fiber.Ctx) error {
	var req models.ClassificationCreateRequestWithCriticalities
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	req.Name = strings.TrimSpace(req.Name)
	req.NameAr = strings.TrimSpace(req.NameAr)

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	// Default to incident+request when no types provided
	classTypes := req.Types
	if len(classTypes) == 0 {
		classTypes = []string{"incident", "request"}
	}

	existing, fields, err := h.repo.CheckDuplicate(c.UserContext(), req.Name, req.NameAr, "", req.ParentID, nil)
	if err == nil && existing != nil && fields.Any() {
		existingPath, _ := h.repo.FetchClassificationFullPathByID(c.UserContext(), existing.ID)
		return utils.ErrorResponse(c, fiber.StatusConflict, duplicateConflictMessage(c.UserContext(), fields, existing.Name, existing.NameAr, existingPath,
			"classification_code_exists", "classification_exists", "classification_exists_at"))
	}

	// Build ClassificationType associations so GORM creates them with the classification
	typeRecords := make([]models.ClassificationType, len(classTypes))
	for i, t := range classTypes {
		typeRecords[i] = models.ClassificationType{Type: t}
	}

	classification := &models.Classification{
		Name:          req.Name,
		NameAr:        req.NameAr,
		Description:   req.Description,
		DescriptionAr: req.DescriptionAr,
		Types:         typeRecords,
		ParentID:      req.ParentID,
		SortOrder:     req.SortOrder,
		IsActive:      true,
	}

	if err := h.repo.Create(c.UserContext(), classification); err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "classification_name_exists"))
		}
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	// Create criticalities if provided
	if len(req.Criticalities) > 0 {
		for _, critReq := range req.Criticalities {
			criticalityID, err := uuid.Parse(critReq.CriticalityID)
			if err != nil {
				// Rollback: delete the classification if criticality creation fails
				h.repo.Delete(c.UserContext(), classification.ID)
				return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.Tf(c.UserContext(), "invalid_criticality_id_detail", critReq.CriticalityID))
			}

			criticality := &models.ClassificationCriticality{
				ClassificationID:  classification.ID,
				CriticalityID:     criticalityID,
				MaxClosingHours:   critReq.MaxClosingHours,
				MaxClosingMinutes: critReq.MaxClosingMinutes,
				IsActive:          true,
			}

			if err := h.repo.CreateCriticality(c.UserContext(), criticality); err != nil {
				// Rollback: delete the classification if criticality creation fails
				h.repo.Delete(c.UserContext(), classification.ID)
				return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create_criticality")+": "+err.Error())
			}
		}
	}

	// Reload to get full response with types
	created, err := h.repo.FindByID(c.UserContext(), classification.ID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_reload_class"))
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "classification_created"), models.ToClassificationResponse(created))
}

func (h *ClassificationHandler) GetByID(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	classification, err := h.repo.FindByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "classification_not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "classification_retrieved"), models.ToClassificationResponse(classification))
}

func (h *ClassificationHandler) Update(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.ClassificationCreateRequestWithCriticalities
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	req.Name = strings.TrimSpace(req.Name)
	req.NameAr = strings.TrimSpace(req.NameAr)

	classification, err := h.repo.FindByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "classification_not_found"))
	}

	checkName := req.Name
	if checkName == "" {
		checkName = classification.Name
	}
	checkNameAr := req.NameAr
	if checkNameAr == "" {
		checkNameAr = classification.NameAr
	}

	existing, fields, err := h.repo.CheckDuplicate(c.UserContext(), checkName, checkNameAr, "", classification.ParentID, &id)
	if err == nil && existing != nil && fields.Any() {
		existingPath, _ := h.repo.FetchClassificationFullPathByID(c.UserContext(), existing.ID)
		return utils.ErrorResponse(c, fiber.StatusConflict, duplicateConflictMessage(c.UserContext(), fields, existing.Name, existing.NameAr, existingPath,
			"classification_code_exists", "classification_exists", "classification_exists_at"))
	}

	if req.Name != "" {
		classification.Name = req.Name
	}
	if req.NameAr != "" {
		classification.NameAr = req.NameAr
	}
	if req.Description != "" {
		classification.Description = req.Description
	}
	if req.DescriptionAr != "" {
		classification.DescriptionAr = req.DescriptionAr
	}
	if req.IsActive != nil {
		if !*req.IsActive && classification.IsActive {
			hasActive, err := h.repo.HasActiveChildren(c.UserContext(), id)
			if err != nil {
				return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
			}
			if hasActive {
				return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "classification_has_active_children"))
			}
		}
		classification.IsActive = *req.IsActive
	}
	if req.SortOrder >= 0 {
		classification.SortOrder = req.SortOrder
	}

	// Update classification fields
	if err := h.repo.Update(c.UserContext(), classification); err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "classification_name_exists"))
		}
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	// Update types if provided
	if len(req.Types) > 0 {
		if err := h.repo.SetTypes(c.UserContext(), id, req.Types); err != nil {
			return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "failed_to_update_types"))
		}
	}

	// Update criticalities if provided
	if len(req.Criticalities) > 0 {
		existingCriticalities, err := h.repo.GetCriticalitiesByClassificationID(c.UserContext(), id)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_get_criticalities"))
		}

		existingMap := make(map[uuid.UUID]*models.ClassificationCriticality)
		for i := range existingCriticalities {
			existingMap[existingCriticalities[i].CriticalityID] = &existingCriticalities[i]
		}

		for _, critReq := range req.Criticalities {
			criticalityID, err := uuid.Parse(critReq.CriticalityID)
			if err != nil {
				return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.Tf(c.UserContext(), "invalid_criticality_id_detail", critReq.CriticalityID))
			}

			if existing, ok := existingMap[criticalityID]; ok {
				existing.MaxClosingHours = critReq.MaxClosingHours
				existing.MaxClosingMinutes = critReq.MaxClosingMinutes
				if err := h.repo.UpdateCriticality(c.UserContext(), existing); err != nil {
					return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_update_criticality")+": "+err.Error())
				}
				delete(existingMap, criticalityID)
			} else {
				criticality := &models.ClassificationCriticality{
					ClassificationID:  id,
					CriticalityID:     criticalityID,
					MaxClosingHours:   critReq.MaxClosingHours,
					MaxClosingMinutes: critReq.MaxClosingMinutes,
					IsActive:          true,
				}
				if err := h.repo.CreateCriticality(c.UserContext(), criticality); err != nil {
					return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create_criticality")+": "+err.Error())
				}
			}
		}
	}

	// Reload classification with types and criticalities
	updatedClassification, err := h.repo.FindByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_reload_class"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "classification_updated"), models.ToClassificationResponse(updatedClassification))
}

func (h *ClassificationHandler) Delete(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	children, childErr := h.repo.CountChildren(c.UserContext(), id)
	if childErr != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_check_deps"))
	}

	incidents, workflows, users, departments, err := h.repo.CheckDependencies(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_check_deps"))
	}

	isAr := strings.HasPrefix(strings.ToLower(strings.TrimSpace(c.Get("Accept-Language"))), "ar")

	var reasons []string
	if isAr {
		if children > 0 {
			reasons = append(reasons, "له تصنيفات فرعية")
		}
		if incidents > 0 {
			reasons = append(reasons, fmt.Sprintf("مرتبط بـ%d بلاغ", incidents))
		}
		if workflows > 0 {
			reasons = append(reasons, "مرتبط بسير اجراء او اكثر")
		}
		if users > 0 {
			reasons = append(reasons, "مرتبط بمستخدم او اكثر")
		}
		if departments > 0 {
			reasons = append(reasons, "مرتبط بادارة او اكثر")
		}
		if len(reasons) > 0 {
			return utils.ErrorResponse(c, fiber.StatusConflict, "لا يمكن حذف هذا التصنيف لانه "+strings.Join(reasons, "، و"))
		}
	} else {
		if children > 0 {
			reasons = append(reasons, "it has sub-classifications")
		}
		if incidents > 0 {
			reasons = append(reasons, fmt.Sprintf("%d incident(s) are associated with this classification", incidents))
		}
		if workflows > 0 {
			reasons = append(reasons, "it is linked to one or more workflows")
		}
		if users > 0 {
			reasons = append(reasons, "it is assigned to one or more users")
		}
		if departments > 0 {
			reasons = append(reasons, "it is assigned to one or more departments")
		}
		if len(reasons) > 0 {
			return utils.ErrorResponse(c, fiber.StatusConflict, "Cannot delete this classification: "+strings.Join(reasons, "; "))
		}
	}

	if err := h.repo.Delete(c.UserContext(), id); err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "classification_deleted"), nil)
}

func (h *ClassificationHandler) List(c *fiber.Ctx) error {
	var classifications []models.Classification
	var err error

	types := parseTypeParam(c.Query("type"))
	if len(types) > 0 {
		classifications, err = h.repo.ListByType(c.UserContext(), types)
	} else {
		classifications, err = h.repo.List(c.UserContext())
	}
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	responses := make([]models.ClassificationResponse, len(classifications))
	for i, cls := range classifications {
		responses[i] = models.ToClassificationResponse(&cls)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "classifications_retrieved"), responses)
}

func (h *ClassificationHandler) GetTree(c *fiber.Ctx) error {
	var tree []models.Classification
	var err error

	types := parseTypeParam(c.Query("type"))
	if len(types) > 0 {
		tree, err = h.repo.GetTreeByType(c.UserContext(), types)
	} else {
		tree, err = h.repo.GetTree(c.UserContext())
	}
	if err != nil {
		return ErrorResponseWithKey(c, fiber.StatusInternalServerError, "internal_server_error")
	}

	responses := make([]models.ClassificationResponse, len(tree))
	for i, cls := range tree {
		responses[i] = models.ToClassificationResponse(&cls)
	}

	// Return portal-trimmed tree when source=epmportal
	if strings.EqualFold(c.Query("source"), "epmportal") {
		portalTree := make([]models.EpmPortalTreeNode, len(responses))
		for i := range responses {
			portalTree[i] = models.ToEpmPortalClassificationTree(&responses[i])
		}
		return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "classification_tree_retrieved"), portalTree)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "classification_tree_retrieved"), responses)
}

func (h *ClassificationHandler) GetChildren(c *fiber.Ctx) error {
	parentIDStr := c.Query("parent_id")

	var children []models.Classification
	var err error

	if parentIDStr == "" {
		children, err = h.repo.GetByParentID(c.UserContext(), nil)
	} else {
		parentID, parseErr := uuid.Parse(parentIDStr)
		if parseErr != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_parent_id"))
		}
		children, err = h.repo.GetByParentID(c.UserContext(), &parentID)
	}

	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	responses := make([]models.ClassificationResponse, len(children))
	for i, cls := range children {
		responses[i] = models.ToClassificationResponse(&cls)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "children_retrieved"), responses)
}

// Export exports all classifications as JSON
func (h *ClassificationHandler) Export(c *fiber.Ctx) error {
	classifications, err := h.repo.List(c.UserContext())
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	// Filter out invalid records (with corrupted paths or invalid UUIDs)
	validClassifications := make([]models.Classification, 0)
	invalidUUID := "00000000-0000-0000-0000-000000000000"

	for _, cls := range classifications {
		if cls.ID.String() == invalidUUID {
			continue
		}
		validClassifications = append(validClassifications, cls)
	}

	exportData := make([]map[string]interface{}, len(validClassifications))
	for i, cls := range validClassifications {
		typeStrings := make([]string, len(cls.Types))
		for j, t := range cls.Types {
			typeStrings[j] = t.Type
		}
		exportData[i] = map[string]interface{}{
			"id":          cls.ID,
			"name":        cls.Name,
			"description": cls.Description,
			"types":       typeStrings,
			"parent_id":   cls.ParentID,
			"level":       cls.Level,
			"path":        cls.Path,
			"is_active":   cls.IsActive,
			"sort_order":  cls.SortOrder,
		}
	}

	c.Set("Content-Type", "application/json")
	c.Set("Content-Disposition", "attachment; filename=classifications_export.json")
	return c.JSON(exportData)
}

// Import imports classifications from JSON
func (h *ClassificationHandler) Import(c *fiber.Ctx) error {
	file, err := c.FormFile("file")
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "no_file_uploaded"))
	}

	fileContent, err := file.Open()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_read_file"))
	}
	defer fileContent.Close()

	var importData []struct {
		ID            uuid.UUID  `json:"id"`
		Name          string     `json:"name"`
		NameAr        string     `json:"name_ar"`
		Code          string     `json:"code"`
		Description   string     `json:"description"`
		DescriptionAr string     `json:"description_ar"`
		Types         []string   `json:"types"`
		ParentID      *uuid.UUID `json:"parent_id"`
		Level         int        `json:"level"`
		Path          string     `json:"path"`
		IsActive      bool       `json:"is_active"`
		SortOrder     int        `json:"sort_order"`
		Criticalities []struct {
			CriticalityID      uuid.UUID  `json:"criticality_id"`
			MaxClosingHours    int        `json:"max_closing_hours"`
			MaxClosingMinutes  int        `json:"max_closing_minutes"`
			EscalationPolicyID *uuid.UUID `json:"escalation_policy_id"`
		} `json:"criticalities"`
	}

	decoder := json.NewDecoder(fileContent)
	if err := decoder.Decode(&importData); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.Tf(c.UserContext(), "invalid_json_format_detail", err.Error()))
	}

	// Sort by level to ensure parents are imported before children
	sort.Slice(importData, func(i, j int) bool {
		return importData[i].Level < importData[j].Level
	})

	idMapping := make(map[uuid.UUID]uuid.UUID)
	imported := 0
	skipped := 0
	errors := []string{}

	isEPM940 := strings.EqualFold(strings.TrimSpace(h.cfg.ClientCode), constants.CLIENT_CODE.EPM940)

	for _, data := range importData {
		// var newParentID *uuid.UUID

		// if data.ParentID != nil {
		// 	mappedParentID, exists := idMapping[*data.ParentID]
		// 	if exists {
		// 		newParentID = &mappedParentID
		// 	} else {
		// 		// Parent was skipped/failed (or hasn't been processed yet due to
		// 		// bad level ordering) - do NOT silently create this as a root
		// 		// classification. Skip it and cascade the failure to its children.
		// 		skipped++
		// 		errors = append(errors, data.Name+" (Level "+fmt.Sprintf("%d", data.Level)+") - parent classification failed to import")
		// 		continue
		// 	}
		// }

		// Default types if none provided in import file
		importTypes := data.Types
		if len(importTypes) == 0 {
			importTypes = []string{"incident", "request"}
		}
		typeRecords := make([]models.ClassificationType, len(importTypes))
		for i, t := range importTypes {
			typeRecords[i] = models.ClassificationType{Type: t}
		}

		// VD2 (non-EPM940) has no auto-generated code, so the import file must supply
		// one; EPM940 always ignores the file's code and lets the repository generate
		// a fresh one.
		if !isEPM940 && strings.TrimSpace(data.Code) == "" {
			skipped++
			errors = append(errors, data.Name+" (Level "+fmt.Sprintf("%d", data.Level)+") - code is required, skipped")
			continue
		}

		// Skip classifications that already exist under the same parent instead
		// of creating a duplicate, and reuse the existing ID so children still
		// attach to the right place.
		if existing, err := h.repo.FindByNameOrNameArAndParent(c.UserContext(), data.Name, data.NameAr, data.ParentID); err == nil && existing != nil {
			skipped++
			idMapping[data.ID] = existing.ID
			errors = append(errors, data.Name+" (Level "+fmt.Sprintf("%d", data.Level)+") - already exists, skipped")
			continue
		}

		newID := uuid.New()
		classification := &models.Classification{
			ID:            newID,
			Name:          data.Name,
			NameAr:        data.NameAr,
			Description:   data.Description,
			DescriptionAr: data.DescriptionAr,
			Types:         typeRecords,
			ParentID:      data.ParentID,
			IsActive:      data.IsActive,
			SortOrder:     data.SortOrder,
		}
		if !isEPM940 {
			classification.Code = strings.TrimSpace(data.Code)
		}

		if err := h.repo.Create(c.UserContext(), classification); err != nil {
			skipped++
			if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
				// Lost a race with a concurrent/duplicate import of the same row: the
				// pre-check above passed, but another request inserted it first.
				if existing, findErr := h.repo.FindByNameOrNameArAndParent(c.UserContext(), data.Name, data.NameAr, data.ParentID); findErr == nil && existing != nil {
					idMapping[data.ID] = existing.ID
				}
				errors = append(errors, data.Name+" (Level "+fmt.Sprintf("%d", data.Level)+") - already exists (concurrent import), skipped")
			} else {
				errors = append(errors, data.Name+" (Level "+fmt.Sprintf("%d", data.Level)+") - "+err.Error())
			}
		} else {
			imported++
			idMapping[data.ID] = newID

			// Create criticalities if provided
			for _, critData := range data.Criticalities {
				criticality := &models.ClassificationCriticality{
					ClassificationID:   newID,
					CriticalityID:      critData.CriticalityID,
					MaxClosingHours:    critData.MaxClosingHours,
					MaxClosingMinutes:  critData.MaxClosingMinutes,
					EscalationPolicyID: critData.EscalationPolicyID,
					IsActive:           true,
				}
				if err := h.repo.CreateCriticality(c.UserContext(), criticality); err != nil {
					errors = append(errors, data.Name+" (Level "+fmt.Sprintf("%d", data.Level)+") - failed to create criticality: "+err.Error())
				}
			}
		}
	}

	result := map[string]interface{}{
		"imported": imported,
		"skipped":  skipped,
		"errors":   errors,
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "import_completed"), result)
}

// GetTreeWithStats returns classification tree with incident counts
func (h *ClassificationHandler) GetTreeWithStats(c *fiber.Ctx) error {
	types := parseTypeParam(c.Query("type"))

	tree, err := h.repo.GetTreeWithStats(c.UserContext(), types)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "classification_tree_stats"), tree)
}

// GetCriticalities returns all criticalities for a classification
func (h *ClassificationHandler) GetCriticalities(c *fiber.Ctx) error {
	classificationIDStr := c.Params("classification_id")
	classificationID, err := uuid.Parse(classificationIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_classification_id2"))
	}

	criticalities, err := h.repo.GetCriticalitiesByClassificationID(c.UserContext(), classificationID)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	responses := make([]models.ClassificationCriticalityResponse, len(criticalities))
	for i, crit := range criticalities {
		responses[i] = models.ToClassificationCriticalityResponse(&crit)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "classification_criticalities_retrieved"), responses)
}

// CreateCriticality creates a new criticality setting for a classification
func (h *ClassificationHandler) CreateCriticality(c *fiber.Ctx) error {
	classificationIDStr := c.Params("classification_id")
	classificationID, err := uuid.Parse(classificationIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_classification_id2"))
	}

	var req models.ClassificationCriticalityCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	_, err = h.repo.FindByID(c.UserContext(), classificationID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "classification_not_found"))
	}

	criticalityID, err := uuid.Parse(req.CriticalityID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_criticality_id"))
	}

	_, err = h.repo.GetCriticalityByClassificationAndCriticalityID(c.UserContext(), classificationID, criticalityID)
	if err == nil {
		return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "criticality_already_exists"))
	}

	criticality := &models.ClassificationCriticality{
		ClassificationID:  classificationID,
		CriticalityID:     criticalityID,
		MaxClosingHours:   req.MaxClosingHours,
		MaxClosingMinutes: req.MaxClosingMinutes,
		IsActive:          true,
	}
	if req.EscalationPolicyID != nil && *req.EscalationPolicyID != "" {
		policyID, err := uuid.Parse(*req.EscalationPolicyID)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_escalation_policy_id"))
		}
		criticality.EscalationPolicyID = &policyID
	}

	if err := h.repo.CreateCriticality(c.UserContext(), criticality); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_create_criticality")+": "+err.Error())
	}

	created, err := h.repo.GetCriticalityByID(c.UserContext(), criticality.ID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_reload_class"))
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "classification_criticality_created"), models.ToClassificationCriticalityResponse(created))
}

// UpdateCriticality updates a criticality setting for a classification
func (h *ClassificationHandler) UpdateCriticality(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.ClassificationCriticalityUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	criticality, err := h.repo.GetCriticalityByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "classification_criticality_not_found"))
	}

	if req.MaxClosingHours != nil {
		criticality.MaxClosingHours = *req.MaxClosingHours
	}
	if req.MaxClosingMinutes != nil {
		criticality.MaxClosingMinutes = *req.MaxClosingMinutes
	}
	if req.IsActive != nil {
		criticality.IsActive = *req.IsActive
	}
	if req.EscalationPolicyID != nil {
		if *req.EscalationPolicyID == "" {
			criticality.EscalationPolicyID = nil
		} else {
			policyID, err := uuid.Parse(*req.EscalationPolicyID)
			if err != nil {
				return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_escalation_policy_id"))
			}
			criticality.EscalationPolicyID = &policyID
		}
	}

	if err := h.repo.UpdateCriticality(c.UserContext(), criticality); err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "classification_criticality_updated"), models.ToClassificationCriticalityResponse(criticality))
}

// DeleteCriticality deletes a criticality setting for a classification
func (h *ClassificationHandler) DeleteCriticality(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	if err := h.repo.DeleteCriticality(c.UserContext(), id); err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "classification_criticality_deleted"), nil)
}

// GetCriticalityByID returns a single criticality setting by ID
func (h *ClassificationHandler) GetCriticalityByID(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	criticality, err := h.repo.GetCriticalityByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "classification_criticality_not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "classification_criticality_retrieved"), models.ToClassificationCriticalityResponse(criticality))
}
