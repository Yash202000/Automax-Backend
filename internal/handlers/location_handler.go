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

type LocationHandler struct {
	repo      repository.LocationRepository
	validator *validator.Validate
	cfg       *config.Config
}

func NewLocationHandler(repo repository.LocationRepository, cfg *config.Config) *LocationHandler {
	return &LocationHandler{
		repo:      repo,
		validator: validator.New(),
		cfg:       cfg,
	}
}

func (h *LocationHandler) Create(c *fiber.Ctx) error {
	var req models.LocationCreateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}
	req.Name = strings.TrimSpace(req.Name)
	req.NameAr = strings.TrimSpace(req.NameAr)
	req.Code = strings.TrimSpace(req.Code)

	// EPM940 auto-generates the Location Code (loc-######) and ignores any
	// supplied value; other clients must supply the code in the payload.
	isEPM940 := strings.EqualFold(strings.TrimSpace(h.cfg.ClientCode), constants.CLIENT_CODE.EPM940)

	validationErrors := validation.ValidateStruct(c.UserContext(), &req)
	if !isEPM940 && req.Code == "" {
		if validationErrors == nil {
			validationErrors = map[string]string{}
		}
		validationErrors["code"] = i18n.T(c.UserContext(), "location_code_required")
	}
	if len(validationErrors) != 0 {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"success": false,
			"errors":  validationErrors,
		})
	}

	existing, err := h.repo.FindByNameOrNameArAndParent(c.UserContext(), req.Name, req.NameAr, req.ParentID)
	if err == nil && existing != nil {
		existingPath, _ := h.repo.FetchLocationFullPathByID(c.UserContext(), existing.ID)
		msg := fmt.Sprintf("Location '%s' already exists", existing.Name)
		if existingPath != "" {
			msg += fmt.Sprintf(" at '%s'", existingPath)
		}
		return utils.ErrorResponse(c, fiber.StatusConflict, msg)
	}

	source := req.Source
	if source == "" {
		source = "master"
	}

	location := &models.Location{
		Name:          req.Name,
		NameAr:        req.NameAr,
		Description:   req.Description,
		DescriptionAr: req.DescriptionAr,
		Type:          req.Type,
		ParentID:      req.ParentID,
		Address:       req.Address,
		Latitude:      req.Latitude,
		Longitude:     req.Longitude,
		SortOrder:     req.SortOrder,
		Source:        source,
		IsActive:      true,
	}

	// Non-EPM940: use the payload code verbatim. EPM940: leave empty so the
	// repository generates the next loc-###### code.
	if !isEPM940 {
		location.Code = req.Code
	}

	if err := h.repo.Create(c.UserContext(), location); err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "location_code_exists"))
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusCreated, i18n.T(c.UserContext(), "location_created"), models.ToLocationResponse(location))
}

func (h *LocationHandler) GetByID(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	location, err := h.repo.FindByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "location_not_found"))
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "location_retrieved"), models.ToLocationResponse(location))
}

func (h *LocationHandler) Update(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	var req models.LocationUpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
	}

	req.Name = strings.TrimSpace(req.Name)
	req.NameAr = strings.TrimSpace(req.NameAr)
	req.Code = strings.TrimSpace(req.Code)

	location, err := h.repo.FindByID(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusNotFound, i18n.T(c.UserContext(), "location_not_found"))
	}

	// EPM940's code is system-generated and immutable; other clients may edit it,
	// subject to a uniqueness check.
	isEPM940 := strings.EqualFold(strings.TrimSpace(h.cfg.ClientCode), constants.CLIENT_CODE.EPM940)

	checkName := req.Name
	if checkName == "" {
		checkName = location.Name
	}
	checkNameAr := req.NameAr
	if checkNameAr == "" {
		checkNameAr = location.NameAr
	}

	if (req.Name != "" && req.Name != location.Name) || (req.NameAr != "" && req.NameAr != location.NameAr) {
		existing, err := h.repo.FindByNameOrNameAr(c.UserContext(), checkName, checkNameAr)
		if err == nil && existing != nil && existing.ID != id {
			existingPath, _ := h.repo.FetchLocationFullPathByID(c.UserContext(), existing.ID)
			msg := fmt.Sprintf("Location '%s' already exists", existing.Name)
			if existingPath != "" {
				msg += fmt.Sprintf(" at '%s'", existingPath)
			}
			return utils.ErrorResponse(c, fiber.StatusConflict, msg)
		}
	}

	if req.Name != "" {
		location.Name = req.Name
	}
	if req.NameAr != "" {
		location.NameAr = req.NameAr
	}
	if !isEPM940 && req.Code != "" && req.Code != location.Code {
		if existing, err := h.repo.FindByCode(c.UserContext(), req.Code); err == nil && existing != nil && existing.ID != id {
			return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "location_code_exists"))
		}
		location.Code = req.Code
	}
	if req.Description != "" {
		location.Description = req.Description
	}
	if req.DescriptionAr != "" {
		location.DescriptionAr = req.DescriptionAr
	}
	if req.Type != "" {
		location.Type = req.Type
	}
	if req.Address != "" {
		location.Address = req.Address
	}
	if req.Latitude != nil {
		location.Latitude = req.Latitude
	}
	if req.Longitude != nil {
		location.Longitude = req.Longitude
	}
	if req.IsActive != nil {
		if !*req.IsActive && location.IsActive {
			hasActive, err := h.repo.HasActiveChildren(c.UserContext(), id)
			if err != nil {
				return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
			}
			if hasActive {
				return utils.ErrorResponse(c, fiber.StatusConflict, "Cannot deactivate this location because it has active sub-locations. Please deactivate all child locations first.")
			}
		}
		location.IsActive = *req.IsActive
	}
	if req.SortOrder != nil {
		location.SortOrder = *req.SortOrder
	}

	if err := h.repo.Update(c.UserContext(), location); err != nil {
		if strings.Contains(err.Error(), "duplicate key") || strings.Contains(err.Error(), "unique constraint") {
			return utils.ErrorResponse(c, fiber.StatusConflict, i18n.T(c.UserContext(), "location_code_exists"))
		}
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "location_updated"), models.ToLocationResponse(location))
}

func (h *LocationHandler) Delete(c *fiber.Ctx) error {
	idStr := c.Params("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
	}

	children, users, incidents, err := h.repo.CheckDeleteDependencies(c.UserContext(), id)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	isAr := strings.HasPrefix(strings.ToLower(strings.TrimSpace(c.Get("Accept-Language"))), "ar")

	var reasons []string
	if isAr {
		if children > 0 {
			reasons = append(reasons, "له مواقع فرعية")
		}
		if users > 0 {
			reasons = append(reasons, "مرتبط بمستخدم أو أكثر")
		}
		if incidents > 0 {
			reasons = append(reasons, fmt.Sprintf("مرتبط بـ%d بلاغ", incidents))
		}
		if len(reasons) > 0 {
			return utils.ErrorResponse(c, fiber.StatusConflict, "لا يمكن حذف هذا الموقع لأنه "+strings.Join(reasons, "، و"))
		}
	} else {
		if children > 0 {
			reasons = append(reasons, "it has sub-locations")
		}
		if users > 0 {
			reasons = append(reasons, "it is assigned to one or more users")
		}
		if incidents > 0 {
			reasons = append(reasons, fmt.Sprintf("%d incident(s) are associated with this location", incidents))
		}
		if len(reasons) > 0 {
			return utils.ErrorResponse(c, fiber.StatusConflict, "Cannot delete this location: "+strings.Join(reasons, "; "))
		}
	}

	if err := h.repo.Delete(c.UserContext(), id); err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "location_deleted"), nil)
}

func (h *LocationHandler) List(c *fiber.Ctx) error {
	locations, err := h.repo.List(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	responses := make([]models.LocationResponse, len(locations))
	for i, loc := range locations {
		responses[i] = models.ToLocationResponse(&loc)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "locations_retrieved"), responses)
}

func (h *LocationHandler) GetTree(c *fiber.Ctx) error {
	tree, err := h.repo.GetTree(c.UserContext())
	if err != nil {
		return ErrorResponseWithKey(c, fiber.StatusInternalServerError, "internal_server_error")
	}

	responses := make([]models.LocationResponse, len(tree))
	for i, loc := range tree {
		responses[i] = models.ToLocationResponse(&loc)
	}

	// Return portal-trimmed tree when source=epmportal
	if strings.EqualFold(c.Query("source"), "epmportal") {
		portalTree := make([]models.EpmPortalTreeNode, len(responses))
		for i := range responses {
			portalTree[i] = models.ToEpmPortalLocationTree(&responses[i])
		}
		return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "location_tree_retrieved"), portalTree)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "location_tree_retrieved"), responses)
}

func (h *LocationHandler) GetChildren(c *fiber.Ctx) error {
	parentIDStr := c.Query("parent_id")

	var children []models.Location
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
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	responses := make([]models.LocationResponse, len(children))
	for i, loc := range children {
		responses[i] = models.ToLocationResponse(&loc)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "children_retrieved"), responses)
}

func (h *LocationHandler) GetByType(c *fiber.Ctx) error {
	locationType := c.Query("type")
	if locationType == "" {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "type_required"))
	}

	locations, err := h.repo.GetByType(c.UserContext(), locationType)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	responses := make([]models.LocationResponse, len(locations))
	for i, loc := range locations {
		responses[i] = models.ToLocationResponse(&loc)
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "locations_retrieved"), responses)
}

// Export exports all locations as JSON
func (h *LocationHandler) Export(c *fiber.Ctx) error {
	locations, err := h.repo.List(c.UserContext())
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	// Convert to export format - NO FILTERING to match classification behavior
	exportData := make([]map[string]interface{}, len(locations))
	for i, loc := range locations {
		exportData[i] = map[string]interface{}{
			"id":          loc.ID,
			"name":        loc.Name,
			"code":        loc.Code,
			"description": loc.Description,
			"type":        loc.Type,
			"parent_id":   loc.ParentID,
			"level":       loc.Level,
			"path":        loc.Path,
			"address":     loc.Address,
			"latitude":    loc.Latitude,
			"longitude":   loc.Longitude,
			"is_active":   loc.IsActive,
			"sort_order":  loc.SortOrder,
		}
	}

	c.Set("Content-Type", "application/json")
	c.Set("Content-Disposition", "attachment; filename=locations_export.json")
	return c.JSON(exportData)
}

// Import imports locations from JSON
func (h *LocationHandler) Import(c *fiber.Ctx) error {
	file, err := c.FormFile("file")
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "no_file_uploaded"))
	}

	// Open and read file
	fileContent, err := file.Open()
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "failed_to_read_file"))
	}
	defer fileContent.Close()

	// Read file content
	var importData []struct {
		ID          uuid.UUID  `json:"id"`
		Name        string     `json:"name"`
		Code        string     `json:"code"`
		Description string     `json:"description"`
		Type        string     `json:"type"`
		ParentID    *uuid.UUID `json:"parent_id"`
		Level       int        `json:"level"`
		Path        string     `json:"path"`
		Address     string     `json:"address"`
		Latitude    *float64   `json:"latitude"`
		Longitude   *float64   `json:"longitude"`
		IsActive    bool       `json:"is_active"`
		SortOrder   int        `json:"sort_order"`
	}

	// Parse JSON from file
	decoder := json.NewDecoder(fileContent)
	if err := decoder.Decode(&importData); err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.Tf(c.UserContext(), "invalid_json_format_detail", err.Error()))
	}

	// Sort by level to ensure parents are imported before children
	sort.Slice(importData, func(i, j int) bool {
		return importData[i].Level < importData[j].Level
	})

	// Create a map from old IDs to new IDs for maintaining parent-child relationships
	idMapping := make(map[uuid.UUID]uuid.UUID)
	imported := 0
	skipped := 0
	errors := []string{}

	isEPM940 := strings.EqualFold(strings.TrimSpace(h.cfg.ClientCode), constants.CLIENT_CODE.EPM940)

	// Import all locations in level order
	for _, data := range importData {
		var newParentID *uuid.UUID

		// If has parent, get the new parent ID from mapping
		if data.ParentID != nil {
			mappedParentID, exists := idMapping[*data.ParentID]
			if exists {
				newParentID = &mappedParentID
			} else {
				// Parent not found in import data, import as root node
				newParentID = nil
			}
		}

		// Create new location (no duplicate check)
		newID := uuid.New()
		location := &models.Location{
			ID:          newID,
			Name:        data.Name,
			Description: data.Description,
			Type:        data.Type,
			ParentID:    newParentID,
			Address:     data.Address,
			Latitude:    data.Latitude,
			Longitude:   data.Longitude,
			IsActive:    data.IsActive,
			SortOrder:   data.SortOrder,
		}
		if !isEPM940 {
			location.Code = strings.TrimSpace(data.Code)
		}

		if err := h.repo.Create(c.UserContext(), location); err != nil {
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

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "import_completed"), result)
}

// GetTreeWithStats returns location tree with incident counts
func (h *LocationHandler) GetTreeWithStats(c *fiber.Ctx) error {
	recordType := c.Query("type", "")

	tree, err := h.repo.GetTreeWithStats(c.UserContext(), recordType)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	return utils.SuccessResponse(c, fiber.StatusOK, i18n.T(c.UserContext(), "location_tree_stats"), tree)
}
