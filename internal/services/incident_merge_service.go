package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/utils"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type IncidentMergeService interface {
	// Validation
	ValidateMerge(ctx context.Context, incidentIDs []string) (*models.IncidentMergeValidationResponse, error)

	// Merge operations - creates NEW master incident
	MergeIncidents(ctx context.Context, req *models.IncidentMergeRequest, userID uuid.UUID, userRoleIDs []uuid.UUID) (*models.IncidentMergeResponse, error)
	UnmergeIncident(ctx context.Context, req *models.IncidentUnmergeRequest, userID uuid.UUID, userRoleIDs []uuid.UUID) (*models.IncidentUnmergeResponse, error)
	BulkUnmergeIncidents(ctx context.Context, req *models.IncidentBulkUnmergeRequest, userID uuid.UUID, userRoleIDs []uuid.UUID) (*models.IncidentBulkUnmergeResponse, error)

	// Automatic operations
	HandleStatusChange(ctx context.Context, incidentID uuid.UUID, newStateID uuid.UUID, isTerminalState bool) error
	HandleReopen(ctx context.Context, incidentID uuid.UUID) error
	HandleClose(ctx context.Context, incidentID uuid.UUID) error

	// Query operations
	GetMergedIncidents(ctx context.Context, masterIncidentID string) ([]models.IncidentResponse, error)
	CanUserMerge(ctx context.Context, workflowID uuid.UUID, userRoleIDs []uuid.UUID) (bool, error)
}

type incidentMergeService struct {
	mergeRepo          repository.IncidentMergeRepository
	incidentRepo       repository.IncidentRepository
	workflowRepo       repository.WorkflowRepository
	roleRepo           repository.RoleRepository
	locationRepo       repository.LocationRepository
	classificationRepo repository.ClassificationRepository
	db                 *gorm.DB
	wsHub              *WSHub
}

func NewIncidentMergeService(
	mergeRepo repository.IncidentMergeRepository,
	incidentRepo repository.IncidentRepository,
	workflowRepo repository.WorkflowRepository,
	roleRepo repository.RoleRepository,
	locationRepo repository.LocationRepository,
	classificationRepo repository.ClassificationRepository,
	db *gorm.DB,
	wsHub *WSHub,
) IncidentMergeService {
	return &incidentMergeService{
		mergeRepo:          mergeRepo,
		incidentRepo:       incidentRepo,
		workflowRepo:       workflowRepo,
		roleRepo:           roleRepo,
		locationRepo:       locationRepo,
		classificationRepo: classificationRepo,
		db:                 db,
		wsHub:              wsHub,
	}
}

// CanUserMerge checks if the user has permission to merge incidents for a specific workflow
// If workflow has merge roles configured, only those roles can merge
// If workflow has NO merge roles configured, all users can merge (backward compatible)
func (s *incidentMergeService) CanUserMerge(ctx context.Context, workflowID uuid.UUID, userRoleIDs []uuid.UUID) (bool, error) {
	// Get merge allowed roles for this workflow
	mergeRoles, err := s.workflowRepo.GetMergeAllowedRoles(ctx, workflowID)
	if err != nil {
		return false, fmt.Errorf("error fetching workflow merge roles: %v", err)
	}

	// If no roles configured, allow all users (backward compatible)
	if len(mergeRoles) == 0 {
		return true, nil
	}

	// Check if user has any of the allowed roles
	for _, userRoleID := range userRoleIDs {
		for _, allowedRole := range mergeRoles {
			if userRoleID == allowedRole.ID {
				return true, nil
			}
		}
	}

	return false, nil
}

// ValidateMerge validates if the selected incidents can be merged
func (s *incidentMergeService) ValidateMerge(ctx context.Context, incidentIDStrs []string) (*models.IncidentMergeValidationResponse, error) {
	response := &models.IncidentMergeValidationResponse{
		CanMerge:      false,
		Errors:        []string{},
		MasterOptions: []models.IncidentMergeOption{},
	}

	if len(incidentIDStrs) < 2 {
		response.Errors = append(response.Errors, "At least 2 incidents are required for merging")
		return response, nil
	}

	// Parse incident IDs
	incidentIDs := make([]uuid.UUID, 0, len(incidentIDStrs))
	for _, idStr := range incidentIDStrs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			response.Errors = append(response.Errors, fmt.Sprintf("Invalid incident ID: %s", idStr))
			return response, nil
		}
		incidentIDs = append(incidentIDs, id)
	}

	// Fetch all incidents with relations
	incidents := make([]*models.Incident, 0, len(incidentIDs))
	for i, id := range incidentIDs {
		incident, err := s.incidentRepo.FindByIDWithRelations(ctx, id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				response.Errors = append(response.Errors, fmt.Sprintf("Incident not found: %s", incidentIDStrs[i]))
			} else {
				response.Errors = append(response.Errors, fmt.Sprintf("Error fetching incident %s: %v", incidentIDStrs[i], err))
			}
			return response, nil
		}
		incidents = append(incidents, incident)
	}

	// Check for already merged incidents
	for _, incident := range incidents {
		// Check if incident is already merged into another
		if incident.IsMerged && incident.MasterIncidentID != nil {
			// Fetch master incident to get its number (MasterIncident is not preloaded)
			masterIncidentNumber := "unknown"
			if incident.MasterIncident != nil {
				masterIncidentNumber = incident.MasterIncident.IncidentNumber
			} else {
				masterInc, err := s.incidentRepo.FindByID(ctx, *incident.MasterIncidentID)
				if err == nil && masterInc != nil {
					masterIncidentNumber = masterInc.IncidentNumber
				}
			}
			response.Errors = append(response.Errors, fmt.Sprintf("Incident %s is already merged into %s", incident.IncidentNumber, masterIncidentNumber))
			return response, nil
		}

		// Check if incident has merged children
		hasChildren, _ := s.mergeRepo.HasMergedIncidents(ctx, incident.ID)
		if hasChildren {
			response.Errors = append(response.Errors, fmt.Sprintf("Incident %s already has merged incidents", incident.IncidentNumber))
			return response, nil
		}
	}

	// Validate all incidents have the same status
	firstStateID := incidents[0].CurrentStateID
	for i, incident := range incidents {
		if i > 0 && incident.CurrentStateID != firstStateID {
			response.Errors = append(response.Errors, "All incidents must have the same status")
			return response, nil
		}
	}

	// Validate status is mergable
	firstIncident := incidents[0]
	state, err := s.workflowRepo.FindStateByID(ctx, firstIncident.CurrentStateID)
	if err != nil {
		response.Errors = append(response.Errors, fmt.Sprintf("Error fetching state: %v", err))
		return response, nil
	}
	if !state.IsMergable {
		response.Errors = append(response.Errors, fmt.Sprintf("Current status '%s' is not mergable", state.Name))
		return response, nil
	}

	// Validate all incidents have the same parent location
	// Resolve each incident's location to its parent (or itself if no parent)
	getParentLocationID := func(locationID *uuid.UUID) *uuid.UUID {
		if locationID == nil {
			return nil
		}
		loc, err := s.locationRepo.FindByID(ctx, *locationID)
		if err != nil {
			return locationID // fallback to exact match
		}
		// Return parent ID if exists, otherwise return self (top-level)
		return loc.ParentID
	}

	firstParentLocationID := getParentLocationID(firstIncident.LocationID)
	for i, incident := range incidents {
		if i > 0 {
			parentLocID := getParentLocationID(incident.LocationID)
			if firstParentLocationID == nil && parentLocID != nil {
				response.Errors = append(response.Errors, "All incidents must have the same parent location")
				return response, nil
			}
			if firstParentLocationID != nil && parentLocID == nil {
				response.Errors = append(response.Errors, "All incidents must have the same parent location")
				return response, nil
			}
			if firstParentLocationID != nil && parentLocID != nil && *firstParentLocationID != *parentLocID {
				response.Errors = append(response.Errors, "All incidents must have the same parent location")
				return response, nil
			}
			// Both nil is OK - both incidents have no location
		}
	}

	// Validate all incidents have the same parent classification
	// Resolve each incident's classification to its parent (or itself if no parent)
	getParentClassificationID := func(classificationID *uuid.UUID) *uuid.UUID {
		if classificationID == nil {
			return nil
		}
		cls, err := s.classificationRepo.FindByID(ctx, *classificationID)
		if err != nil {
			return classificationID // fallback to exact match
		}
		// Return parent ID if exists, otherwise return nil (top-level)
		return cls.ParentID
	}

	firstParentClassificationID := getParentClassificationID(firstIncident.ClassificationID)
	for i, incident := range incidents {
		if i > 0 {
			parentClsID := getParentClassificationID(incident.ClassificationID)
			if firstParentClassificationID == nil && parentClsID != nil {
				response.Errors = append(response.Errors, "All incidents must have the same parent classification")
				return response, nil
			}
			if firstParentClassificationID != nil && parentClsID == nil {
				response.Errors = append(response.Errors, "All incidents must have the same parent classification")
				return response, nil
			}
			if firstParentClassificationID != nil && parentClsID != nil && *firstParentClassificationID != *parentClsID {
				response.Errors = append(response.Errors, "All incidents must have the same parent classification")
				return response, nil
			}
			// Both nil is OK - both incidents have same top-level classification
		}
	}

	// Validate all incidents are within configured radius of each other
	maxMergeDistanceStr := os.Getenv("MAX_INCIDENT_DISTANCE")
	maxMergeDistance, err := strconv.ParseFloat(maxMergeDistanceStr, 64)
	if err != nil || maxMergeDistance <= 0 {
		maxMergeDistance = 500 // Default to 500 meters
	}

	// Check if all incidents have coordinates
	allHaveCoordinates := true
	for _, incident := range incidents {
		if incident.Latitude == nil || incident.Longitude == nil {
			allHaveCoordinates = false
			break
		}
	}

	if allHaveCoordinates {
		for i := 0; i < len(incidents); i++ {
			for j := i + 1; j < len(incidents); j++ {
				distance := utils.CalculateDistance(
					*incidents[i].Latitude, *incidents[i].Longitude,
					*incidents[j].Latitude, *incidents[j].Longitude,
				)
				if distance > maxMergeDistance {
					response.Errors = append(response.Errors,
						fmt.Sprintf("Incidents must be within %.0f meters of each other (incidents %s and %s are %.0f meters apart)",
							maxMergeDistance, incidents[i].IncidentNumber, incidents[j].IncidentNumber, distance))
					return response, nil
				}
			}
		}
	}

	// Build master options (show all incidents as potential masters for reference)
	for _, incident := range incidents {
		option := models.IncidentMergeOption{
			ID:             incident.ID,
			IncidentNumber: incident.IncidentNumber,
			Title:          incident.Title,
			CurrentStateID: incident.CurrentStateID,
			CurrentState:   incident.CurrentState.Name,
		}
		response.MasterOptions = append(response.MasterOptions, option)
	}

	response.CanMerge = true
	return response, nil
}

// MergeIncidents merges selected incidents into a master incident chosen from the selected list
func (s *incidentMergeService) MergeIncidents(ctx context.Context, req *models.IncidentMergeRequest, userID uuid.UUID, userRoleIDs []uuid.UUID) (*models.IncidentMergeResponse, error) {
	// Parse incident IDs to get workflow ID
	if len(req.IncidentIDs) == 0 {
		return nil, errors.New("at least one incident ID is required")
	}

	firstIncidentID, err := uuid.Parse(req.IncidentIDs[0])
	if err != nil {
		return nil, fmt.Errorf("invalid incident ID: %s", req.IncidentIDs[0])
	}

	// Get the incident to determine its workflow
	firstIncident, err := s.incidentRepo.FindByID(ctx, firstIncidentID)
	if err != nil {
		return nil, fmt.Errorf("error fetching incident: %v", err)
	}

	// Check user permission for this workflow
	canMerge, err := s.CanUserMerge(ctx, firstIncident.WorkflowID, userRoleIDs)
	if err != nil {
		return nil, err
	}
	if !canMerge {
		return nil, errors.New("you do not have permission to merge incidents for this workflow")
	}

	// Master incident ID is required - must be one of the selected incidents
	if req.MasterIncidentID == "" {
		return nil, errors.New("master incident ID is required: select one of the incidents as the master")
	}

	// Validate master incident ID is in the selected list
	masterInList := false
	for _, idStr := range req.IncidentIDs {
		if idStr == req.MasterIncidentID {
			masterInList = true
			break
		}
	}
	if !masterInList {
		return nil, errors.New("master incident must be one of the selected incidents")
	}

	// Validate the merge first
	validationResp, err := s.ValidateMerge(ctx, req.IncidentIDs)
	if err != nil {
		return nil, err
	}
	if !validationResp.CanMerge {
		return nil, errors.New(validationResp.Errors[0])
	}

	// Parse IDs
	masterID, err := uuid.Parse(req.MasterIncidentID)
	if err != nil {
		return nil, fmt.Errorf("invalid master incident ID: %s", req.MasterIncidentID)
	}

	incidentIDs := make([]uuid.UUID, 0, len(req.IncidentIDs))
	for _, idStr := range req.IncidentIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			return nil, fmt.Errorf("invalid incident ID: %s", idStr)
		}
		incidentIDs = append(incidentIDs, id)
	}

	// Begin transaction
	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	txMergeRepo := s.mergeRepo.WithTx(tx)
	txIncidentRepo := s.incidentRepo.WithTx(tx)

	// Link all non-master incidents to the chosen master
	if err := txMergeRepo.LinkIncidentsToMaster(ctx, masterID, incidentIDs, userID, req.Comment); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to merge incidents: %v", err)
	}

	// Build list of related incident numbers for the revision
	relatedNumbers := []string{}
	for _, id := range incidentIDs {
		if id != masterID {
			inc, err := txIncidentRepo.FindByID(ctx, id)
			if err == nil {
				relatedNumbers = append(relatedNumbers, inc.IncidentNumber)
			}
		}
	}

	// Get master incident number for child revisions
	masterIncident, err := txIncidentRepo.FindByID(ctx, masterID)
	if err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to fetch master incident: %v", err)
	}
	masterIncidentNumber := masterIncident.IncidentNumber

	// Create revision on master incident
	changes := []models.IncidentFieldChange{
		{
			FieldName:  "merged_incidents",
			FieldLabel: "Merged Incidents",
			OldValue:   strPtr("0"),
			NewValue:   strPtr(fmt.Sprintf("%d", len(incidentIDs)-1)),
		},
	}

	revision := &models.IncidentRevision{
		IncidentID:        masterID,
		RevisionNumber:    0,
		ActionType:        models.RevisionActionStatusChanged,
		ActionDescription: fmt.Sprintf("%d ticket(s) merged into this master ticket: %s", len(incidentIDs)-1, strings.Join(relatedNumbers, ", ")),
		Changes:           marshalJSON(changes),
		PerformedByID:     userID,
	}

	nextRevNum, _ := txIncidentRepo.GetNextRevisionNumber(ctx, masterID)
	revision.RevisionNumber = nextRevNum
	txIncidentRepo.CreateRevision(ctx, revision)

	// Create revision on each child incident
	for _, id := range incidentIDs {
		if id != masterID {
			childChanges := []models.IncidentFieldChange{
				{
					FieldName:  "master_incident_id",
					FieldLabel: "Master Incident",
					OldValue:   nil,
					NewValue:   strPtr(masterIncidentNumber),
				},
			}

			childRevision := &models.IncidentRevision{
				IncidentID:        id,
				RevisionNumber:    0,
				ActionType:        models.RevisionActionStatusChanged,
				ActionDescription: fmt.Sprintf("This ticket is merged into the Master ticket %s", masterIncidentNumber),
				Changes:           marshalJSON(childChanges),
				PerformedByID:     userID,
			}

			childRevNum, _ := txIncidentRepo.GetNextRevisionNumber(ctx, id)
			childRevision.RevisionNumber = childRevNum
			txIncidentRepo.CreateRevision(ctx, childRevision)
		}
	}

	// Commit transaction
	if err := tx.Commit().Error; err != nil {
		return nil, err
	}

	// Fetch results
	updatedMaster, _ := s.incidentRepo.FindByIDWithRelations(ctx, masterID)
	mergedIncidents, _ := s.mergeRepo.GetMergedIncidents(ctx, masterID)

	masterResp := models.ToIncidentResponse(updatedMaster)
	mergedResps := make([]models.IncidentResponse, len(mergedIncidents))
	for i, inc := range mergedIncidents {
		mergedResps[i] = models.ToIncidentResponse(&inc)
	}

	response := &models.IncidentMergeResponse{
		MasterIncident:  &masterResp,
		MergedIncidents: mergedResps,
		MergedCount:     len(mergedIncidents),
		Message:         fmt.Sprintf("Successfully merged %d incident(s) into master %s", len(mergedIncidents), updatedMaster.IncidentNumber),
	}

	if s.wsHub != nil {
		s.wsHub.BroadcastToAll("incidents_merged", map[string]interface{}{
			"master_incident":  masterResp,
			"merged_incidents": mergedResps,
			"merged_count":     len(mergedIncidents),
		})
	}

	return response, nil
}

// UnmergeIncident detaches an incident from its master
func (s *incidentMergeService) UnmergeIncident(ctx context.Context, req *models.IncidentUnmergeRequest, userID uuid.UUID, userRoleIDs []uuid.UUID) (*models.IncidentUnmergeResponse, error) {
	incidentID, err := uuid.Parse(req.IncidentID)
	if err != nil {
		return nil, fmt.Errorf("invalid incident ID: %s", req.IncidentID)
	}

	// Get incident to determine workflow
	incident, err := s.incidentRepo.FindByID(ctx, incidentID)
	if err != nil {
		return nil, fmt.Errorf("error fetching incident: %v", err)
	}

	// Check user permission for this workflow
	canMerge, err := s.CanUserMerge(ctx, incident.WorkflowID, userRoleIDs)
	if err != nil {
		return nil, err
	}
	if !canMerge {
		return nil, errors.New("you do not have permission to unmerge incidents")
	}

	isMerged, err := s.mergeRepo.IsIncidentMerged(ctx, incidentID)
	if err != nil {
		return nil, fmt.Errorf("error checking merge status: %v", err)
	}
	if !isMerged {
		return nil, errors.New("incident is not merged into another incident")
	}

	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	txMergeRepo := s.mergeRepo.WithTx(tx)
	txIncidentRepo := s.incidentRepo.WithTx(tx)

	// Get master incident number for the revision (safe nil check)
	// Use existing 'incident' variable fetched earlier
	var masterIncidentNumber string
	if incident != nil && incident.MasterIncidentID != nil {
		masterInc, mErr := txIncidentRepo.FindByID(ctx, *incident.MasterIncidentID)
		if mErr == nil && masterInc != nil {
			masterIncidentNumber = masterInc.IncidentNumber
		}
	}

	if err := txMergeRepo.UnmergeIncident(ctx, incidentID, userID, req.Comment); err != nil {
		tx.Rollback()
		return nil, fmt.Errorf("failed to unmerge incident: %v", err)
	}

	// Create revision
	changes := []models.IncidentFieldChange{
		{
			FieldName:  "master_incident_id",
			FieldLabel: "Master Incident",
			OldValue:   strPtr(masterIncidentNumber),
			NewValue:   nil,
		},
	}

	revision := &models.IncidentRevision{
		IncidentID:        incidentID,
		RevisionNumber:    0,
		ActionType:        models.RevisionActionStatusChanged,
		ActionDescription: "Incident unmerged from master",
		Changes:           marshalJSON(changes),
		PerformedByID:     userID,
	}

	nextRevNum, _ := txIncidentRepo.GetNextRevisionNumber(ctx, incidentID)
	revision.RevisionNumber = nextRevNum
	txIncidentRepo.CreateRevision(ctx, revision)

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit unmerge: %v", err)
	}

	updatedIncident, _ := s.incidentRepo.FindByIDWithRelations(ctx, incidentID)
	incidentResp := models.ToIncidentResponse(updatedIncident)

	response := &models.IncidentUnmergeResponse{
		UnmergedIncident: &incidentResp,
		Message:          fmt.Sprintf("Successfully unmerged incident %s", updatedIncident.IncidentNumber),
	}

	if s.wsHub != nil {
		s.wsHub.BroadcastToAll("incident_unmerged", map[string]interface{}{
			"incident": incidentResp,
		})
	}

	return response, nil
}

// BulkUnmergeIncidents detaches multiple incidents from their masters in a single transaction
func (s *incidentMergeService) BulkUnmergeIncidents(ctx context.Context, req *models.IncidentBulkUnmergeRequest, userID uuid.UUID, userRoleIDs []uuid.UUID) (*models.IncidentBulkUnmergeResponse, error) {
	// Parse and validate all IDs upfront
	incidentIDs := make([]uuid.UUID, 0, len(req.IncidentIDs))
	for _, idStr := range req.IncidentIDs {
		id, err := uuid.Parse(idStr)
		if err != nil {
			return nil, fmt.Errorf("invalid incident ID: %s", idStr)
		}
		incidentIDs = append(incidentIDs, id)
	}

	// Fetch all incidents to check workflow permissions
	incidents := make([]*models.Incident, 0, len(incidentIDs))
	for _, id := range incidentIDs {
		incident, err := s.incidentRepo.FindByID(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("error fetching incident %s: %v", id, err)
		}
		incidents = append(incidents, incident)
	}

	// Check permission for each incident's workflow
	for _, incident := range incidents {
		canMerge, err := s.CanUserMerge(ctx, incident.WorkflowID, userRoleIDs)
		if err != nil {
			return nil, err
		}
		if !canMerge {
			return nil, fmt.Errorf("you do not have permission to unmerge incident %s", incident.IncidentNumber)
		}
	}

	tx := s.db.Begin()
	defer func() {
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()

	txMergeRepo := s.mergeRepo.WithTx(tx)
	txIncidentRepo := s.incidentRepo.WithTx(tx)

	unmergedCount := 0
	var failures []string
	var unmergedResponses []models.IncidentResponse

	for _, incidentID := range incidentIDs {
		// Check if actually merged
		isMerged, err := txMergeRepo.IsIncidentMerged(ctx, incidentID)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: error checking merge status", incidentID))
			continue
		}
		if !isMerged {
			failures = append(failures, fmt.Sprintf("%s: not merged", incidentID))
			continue
		}

		// Get master incident number for the revision
		incident, _ := txIncidentRepo.FindByID(ctx, incidentID)
		var masterIncidentNumber string
		if incident != nil && incident.MasterIncidentID != nil {
			masterInc, mErr := txIncidentRepo.FindByID(ctx, *incident.MasterIncidentID)
			if mErr == nil && masterInc != nil {
				masterIncidentNumber = masterInc.IncidentNumber
			}
		}

		if err := txMergeRepo.UnmergeIncident(ctx, incidentID, userID, req.Comment); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", incidentID, err))
			continue
		}

		// Create revision
		changes := []models.IncidentFieldChange{
			{
				FieldName:  "master_incident_id",
				FieldLabel: "Master Incident",
				OldValue:   strPtr(masterIncidentNumber),
				NewValue:   nil,
			},
		}

		revision := &models.IncidentRevision{
			IncidentID:        incidentID,
			RevisionNumber:    0,
			ActionType:        models.RevisionActionStatusChanged,
			ActionDescription: "Incident unmerged from master (bulk)",
			Changes:           marshalJSON(changes),
			PerformedByID:     userID,
		}

		nextRevNum, _ := txIncidentRepo.GetNextRevisionNumber(ctx, incidentID)
		revision.RevisionNumber = nextRevNum
		txIncidentRepo.CreateRevision(ctx, revision)

		unmergedCount++
	}

	if err := tx.Commit().Error; err != nil {
		return nil, fmt.Errorf("failed to commit bulk unmerge: %v", err)
	}

	// Fetch updated incidents for WS broadcast
	for _, incidentID := range incidentIDs {
		updatedIncident, err := s.incidentRepo.FindByIDWithRelations(ctx, incidentID)
		if err == nil {
			resp := models.ToIncidentResponse(updatedIncident)
			unmergedResponses = append(unmergedResponses, resp)
		}
	}

	if s.wsHub != nil && len(unmergedResponses) > 0 {
		s.wsHub.BroadcastToAll("incidents_bulk_unmerged", map[string]interface{}{
			"incidents":      unmergedResponses,
			"unmerged_count": unmergedCount,
		})
	}

	response := &models.IncidentBulkUnmergeResponse{
		UnmergedCount: unmergedCount,
		Failures:      failures,
		Message:       fmt.Sprintf("Successfully unmerged %d incident(s)", unmergedCount),
	}

	return response, nil
}

// HandleStatusChange handles status changes for merged incidents
func (s *incidentMergeService) HandleStatusChange(ctx context.Context, incidentID uuid.UUID, newStateID uuid.UUID, isTerminalState bool) error {
	hasMerged, err := s.mergeRepo.HasMergedIncidents(ctx, incidentID)
	if err != nil {
		return err
	}

	if hasMerged {
		if err := s.mergeRepo.SyncStatusToMergedIncidents(ctx, incidentID, newStateID); err != nil {
			return fmt.Errorf("failed to sync status to merged incidents: %v", err)
		}
	}

	return nil
}

// HandleReopen handles reopening of incidents
func (s *incidentMergeService) HandleReopen(ctx context.Context, incidentID uuid.UUID) error {
	return s.mergeRepo.AutoUnmergeOnReopen(ctx, incidentID)
}

// HandleClose handles closing of master incidents
func (s *incidentMergeService) HandleClose(ctx context.Context, incidentID uuid.UUID) error {
	hasMerged, err := s.mergeRepo.HasMergedIncidents(ctx, incidentID)
	if err != nil {
		return err
	}

	if hasMerged {
		if err := s.mergeRepo.AutoUnmergeOnClose(ctx, incidentID); err != nil {
			return fmt.Errorf("failed to auto-unmerge incidents: %v", err)
		}
	}

	return nil
}

// GetMergedIncidents returns all incidents merged into a master
func (s *incidentMergeService) GetMergedIncidents(ctx context.Context, masterIncidentIDStr string) ([]models.IncidentResponse, error) {
	masterID, err := uuid.Parse(masterIncidentIDStr)
	if err != nil {
		return nil, fmt.Errorf("invalid master incident ID: %s", masterIncidentIDStr)
	}

	incidents, err := s.mergeRepo.GetMergedIncidents(ctx, masterID)
	if err != nil {
		return nil, err
	}

	responses := make([]models.IncidentResponse, len(incidents))
	for i, inc := range incidents {
		responses[i] = models.ToIncidentResponse(&inc)
	}

	return responses, nil
}

func strPtr(s string) *string {
	return &s
}

func marshalJSON(v interface{}) string {
	data, _ := json.Marshal(v)
	return string(data)
}
