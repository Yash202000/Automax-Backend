package handlers

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type ClassificationHandler struct {
	repo      repository.ClassificationRepository
	validator *validator.Validate
}

func NewClassificationHandler(repo repository.ClassificationRepository) *ClassificationHandler {
	return &ClassificationHandler{
		repo:      repo,
		validator: validator.New(),
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

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
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Create criticalities if provided
	if len(req.Criticalities) > 0 {
		for _, critReq := range req.Criticalities {
			criticalityID, err := uuid.Parse(critReq.CriticalityID)
			if err != nil {
				// Rollback: delete the classification if criticality creation fails
				h.repo.Delete(c.UserContext(), classification.ID)
				return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid criticality ID: "+critReq.CriticalityID)
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
				return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create criticality: "+err.Error())
			}
		}
	}

	// Reload to get full response with types
	created, err := h.repo.FindByID(c.UserContext(), classification.ID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to reload classification")
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Classification created", models.ToClassificationResponse(created))
}

func (h *ClassificationHandler) GetByID(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	classification, err := h.repo.FindByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Classification not found")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Classification retrieved", models.ToClassificationResponse(classification))
}

func (h *ClassificationHandler) Update(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	var req models.ClassificationCreateRequestWithCriticalities
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	classification, err := h.repo.FindByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Classification not found")
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
	classification.IsActive = true // Keep active on update
	if req.SortOrder >= 0 {
		classification.SortOrder = req.SortOrder
	}

	// Update classification fields
	if err := h.repo.Update(c.UserContext(), classification); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Update types if provided
	if len(req.Types) > 0 {
		if err := h.repo.SetTypes(c.UserContext(), id, req.Types); err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to update types: "+err.Error())
		}
	}

	// Update criticalities if provided
	if len(req.Criticalities) > 0 {
		existingCriticalities, err := h.repo.GetCriticalitiesByClassificationID(c.UserContext(), id)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to get existing criticalities")
		}

		existingMap := make(map[uuid.UUID]*models.ClassificationCriticality)
		for i := range existingCriticalities {
			existingMap[existingCriticalities[i].CriticalityID] = &existingCriticalities[i]
		}

		for _, critReq := range req.Criticalities {
			criticalityID, err := uuid.Parse(critReq.CriticalityID)
			if err != nil {
				return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid criticality ID: "+critReq.CriticalityID)
			}

			if existing, ok := existingMap[criticalityID]; ok {
				existing.MaxClosingHours = critReq.MaxClosingHours
				existing.MaxClosingMinutes = critReq.MaxClosingMinutes
				if err := h.repo.UpdateCriticality(c.UserContext(), existing); err != nil {
					return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to update criticality: "+err.Error())
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
					return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create criticality: "+err.Error())
				}
			}
		}
	}

	// Reload classification with types and criticalities
	updatedClassification, err := h.repo.FindByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to reload classification")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Classification updated", models.ToClassificationResponse(updatedClassification))
}

func (h *ClassificationHandler) Delete(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	if err := h.repo.Delete(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Classification deleted", nil)
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
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	responses := make([]models.ClassificationResponse, len(classifications))
	for i, cls := range classifications {
		responses[i] = models.ToClassificationResponse(&cls)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Classifications retrieved", responses)
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
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	responses := make([]models.ClassificationResponse, len(tree))
	for i, cls := range tree {
		responses[i] = models.ToClassificationResponse(&cls)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Classification tree retrieved", responses)
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
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid parent ID")
		}
		children, err = h.repo.GetByParentID(c.UserContext(), &parentID)
	}

	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	responses := make([]models.ClassificationResponse, len(children))
	for i, cls := range children {
		responses[i] = models.ToClassificationResponse(&cls)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Children retrieved", responses)
}

// Export exports all classifications as JSON
func (h *ClassificationHandler) Export(c *fiber.Ctx) error {
	classifications, err := h.repo.List(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
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
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "No file uploaded")
	}

	fileContent, err := file.Open()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to read file")
	}
	defer fileContent.Close()

	var importData []struct {
		ID          uuid.UUID  `json:"id"`
		Name        string     `json:"name"`
		Description string     `json:"description"`
		Types       []string   `json:"types"`
		ParentID    *uuid.UUID `json:"parent_id"`
		Level       int        `json:"level"`
		Path        string     `json:"path"`
		IsActive    bool       `json:"is_active"`
		SortOrder   int        `json:"sort_order"`
	}

	decoder := json.NewDecoder(fileContent)
	if err := decoder.Decode(&importData); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid JSON format: "+err.Error())
	}

	// Sort by level to ensure parents are imported before children
	sort.Slice(importData, func(i, j int) bool {
		return importData[i].Level < importData[j].Level
	})

	idMapping := make(map[uuid.UUID]uuid.UUID)
	imported := 0
	skipped := 0
	errors := []string{}

	for _, data := range importData {
		var newParentID *uuid.UUID

		if data.ParentID != nil {
			mappedParentID, exists := idMapping[*data.ParentID]
			if exists {
				newParentID = &mappedParentID
			} else {
				newParentID = nil
			}
		}

		// Default types if none provided in import file
		importTypes := data.Types
		if len(importTypes) == 0 {
			importTypes = []string{"incident", "request"}
		}
		typeRecords := make([]models.ClassificationType, len(importTypes))
		for i, t := range importTypes {
			typeRecords[i] = models.ClassificationType{Type: t}
		}

		newID := uuid.New()
		classification := &models.Classification{
			ID:          newID,
			Name:        data.Name,
			Description: data.Description,
			Types:       typeRecords,
			ParentID:    newParentID,
			IsActive:    data.IsActive,
			SortOrder:   data.SortOrder,
		}

		if err := h.repo.Create(c.UserContext(), classification); err != nil {
			skipped++
			errors = append(errors, data.Name+" (Level "+fmt.Sprintf("%d", data.Level)+") - "+err.Error())
		} else {
			imported++
			idMapping[data.ID] = newID
		}
	}

	result := map[string]interface{}{
		"imported": imported,
		"skipped":  skipped,
		"errors":   errors,
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Import completed", result)
}

// GetTreeWithStats returns classification tree with incident counts
func (h *ClassificationHandler) GetTreeWithStats(c *fiber.Ctx) error {
	types := parseTypeParam(c.Query("type"))

	tree, err := h.repo.GetTreeWithStats(c.UserContext(), types)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Classification tree with stats retrieved", tree)
}

// GetCriticalities returns all criticalities for a classification
func (h *ClassificationHandler) GetCriticalities(c *fiber.Ctx) error {
	classificationIDStr := c.Params("classification_id")
	classificationID, err := uuid.Parse(classificationIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid classification ID")
	}

	criticalities, err := h.repo.GetCriticalitiesByClassificationID(c.UserContext(), classificationID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	responses := make([]models.ClassificationCriticalityResponse, len(criticalities))
	for i, crit := range criticalities {
		responses[i] = models.ToClassificationCriticalityResponse(&crit)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Classification criticalities retrieved", responses)
}

// CreateCriticality creates a new criticality setting for a classification
func (h *ClassificationHandler) CreateCriticality(c *fiber.Ctx) error {
	classificationIDStr := c.Params("classification_id")
	classificationID, err := uuid.Parse(classificationIDStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid classification ID")
	}

	var req models.ClassificationCriticalityCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	_, err = h.repo.FindByID(c.UserContext(), classificationID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Classification not found")
	}

	criticalityID, err := uuid.Parse(req.CriticalityID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid criticality ID")
	}

	_, err = h.repo.GetCriticalityByClassificationAndCriticalityID(c.UserContext(), classificationID, criticalityID)
	if err == nil {
		return utils.ErrorResponse(c, fiber.StatusConflict, "Criticality already exists for this classification")
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
			return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid escalation_policy_id")
		}
		criticality.EscalationPolicyID = &policyID
	}

	if err := h.repo.CreateCriticality(c.UserContext(), criticality); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to create criticality: "+err.Error())
	}

	created, err := h.repo.GetCriticalityByID(c.UserContext(), criticality.ID)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, "Failed to retrieve created criticality")
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, "Classification criticality created", models.ToClassificationCriticalityResponse(created))
}

// UpdateCriticality updates a criticality setting for a classification
func (h *ClassificationHandler) UpdateCriticality(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	var req models.ClassificationCriticalityUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if validationErrors := validation.ValidateStruct(c.UserContext(), &req); len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	criticality, err := h.repo.GetCriticalityByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Classification criticality not found")
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
				return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid escalation_policy_id")
			}
			criticality.EscalationPolicyID = &policyID
		}
	}

	if err := h.repo.UpdateCriticality(c.UserContext(), criticality); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Classification criticality updated", models.ToClassificationCriticalityResponse(criticality))
}

// DeleteCriticality deletes a criticality setting for a classification
func (h *ClassificationHandler) DeleteCriticality(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	if err := h.repo.DeleteCriticality(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Classification criticality deleted", nil)
}

// GetCriticalityByID returns a single criticality setting by ID
func (h *ClassificationHandler) GetCriticalityByID(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, "Invalid ID")
	}

	criticality, err := h.repo.GetCriticalityByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, "Classification criticality not found")
	}

	return utils.SuccessResponse(c, fiber.StatusOK, "Classification criticality retrieved", models.ToClassificationCriticalityResponse(criticality))
}
