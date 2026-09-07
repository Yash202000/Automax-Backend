package database

import (
	"log"

	"github.com/automax/backend/internal/models"
	"gorm.io/gorm"
)

// seedKpiDictionaryWorkflow seeds the "KPI Workflow" — the
// Draft->Reviewed->Approved->Active->Closed lifecycle auto-assigned to every
// KPI dictionary row (Strategic/Operational/Award) at creation time (see
// services.KpiWorkflowService.InitiateKpiDictionaryWorkflow). Mirrors
// seedKpiPerformanceWorkflow's shape (plain slug codes, db.Create directly,
// idempotent). Must run after role seeding (kpi_owner/l1_reviewer roles),
// since it attaches those roles as ViewableRoles/EditableRoles/AllowedRoles.
func seedKpiDictionaryWorkflow(db *gorm.DB) {
	var wf models.Workflow
	exists := db.Where("code = ?", "kpi_dictionary_workflow").First(&wf).Error == nil

	if !exists {
		log.Println("Seeding default KPI Workflow...")

		wf = models.Workflow{
			Name:        "KPI Workflow",
			Code:        "kpi_dictionary_workflow",
			Description: "Default lifecycle workflow for KPI dictionary records (Strategic/Operational/Award): Draft -> Reviewed -> Approved -> Active -> Closed.",
			RecordType:  "kpi_dictionary",
			IsActive:    true,
			IsDefault:   true,
		}
		if err := db.Create(&wf).Error; err != nil {
			log.Printf("Failed to create KPI Workflow: %v", err)
			return
		}

		var adminRole, kpiOwnerRole, l1ReviewerRole models.Role
		db.Where("code = ?", "admin").First(&adminRole)
		db.Where("code = ?", "kpi_owner").First(&kpiOwnerRole)
		db.Where("code = ?", "l1_reviewer").First(&l1ReviewerRole)

		var allRoles []models.Role
		db.Find(&allRoles)

		type stateSpec struct {
			Name          string
			Code          string
			StateType     string
			Color         string
			SortOrder     int
			PosX          int
			PosY          int
			ViewableRoles []models.Role
			EditableRoles []models.Role
		}

		stateSpecs := []stateSpec{
			{"Draft", "draft", "initial", "#94a3b8", 1, 100, 200, []models.Role{adminRole, kpiOwnerRole}, allRoles},
			{"Reviewed", "reviewed", "normal", "#3b82f6", 2, 300, 200, []models.Role{adminRole, l1ReviewerRole}, allRoles},
			{"Approved", "approved", "normal", "#f59e0b", 3, 500, 200, []models.Role{adminRole, kpiOwnerRole}, allRoles},
			{"Active", "active", "normal", "#22c55e", 4, 700, 200, []models.Role{adminRole, kpiOwnerRole}, []models.Role{adminRole}},
			{"Closed", "closed", "terminal", "#64748b", 5, 900, 200, []models.Role{adminRole, kpiOwnerRole}, allRoles},
		}

		for _, sp := range stateSpecs {
			state := models.WorkflowState{
				WorkflowID: wf.ID,
				Name:       sp.Name,
				Code:       sp.Code,
				StateType:  sp.StateType,
				Color:      sp.Color,
				SortOrder:  sp.SortOrder,
				PositionX:  sp.PosX,
				PositionY:  sp.PosY,
				IsActive:   true,
			}
			if err := db.Create(&state).Error; err != nil {
				log.Printf("Failed to create KPI Workflow state %s: %v", sp.Code, err)
				return
			}
			if err := db.Model(&state).Association("ViewableRoles").Replace(sp.ViewableRoles); err != nil {
				log.Printf("Failed to set viewable roles for state %s: %v", sp.Code, err)
			}
			if err := db.Model(&state).Association("EditableRoles").Replace(sp.EditableRoles); err != nil {
				log.Printf("Failed to set editable roles for state %s: %v", sp.Code, err)
			}
		}
	}

	// Ensure transitions exist (even if the workflow was already seeded).
	{
		var adminRole, kpiOwnerRole, l1ReviewerRole models.Role
		db.Where("code = ?", "admin").First(&adminRole)
		db.Where("code = ?", "kpi_owner").First(&kpiOwnerRole)
		db.Where("code = ?", "l1_reviewer").First(&l1ReviewerRole)

		stateMap := make(map[string]models.WorkflowState)
		var states []models.WorkflowState
		db.Where("workflow_id = ?", wf.ID).Find(&states)
		for _, s := range states {
			stateMap[s.Code] = s
		}

		type transSpec struct {
			Name         string
			Code         string
			From         string
			To           string
			SortOrder    int
			AllowedRoles []models.Role
		}

		transSpecs := []transSpec{
			{"Review Done", "review_done", "draft", "reviewed", 1, []models.Role{adminRole, l1ReviewerRole}},
			{"Reviewed", "reviewed", "reviewed", "approved", 2, []models.Role{adminRole, l1ReviewerRole}},
			{"Activate", "activate", "approved", "active", 3, []models.Role{adminRole, kpiOwnerRole}},
			{"Close", "close", "active", "closed", 4, []models.Role{adminRole, kpiOwnerRole}},
		}

		for _, sp := range transSpecs {
			fromState, fromOk := stateMap[sp.From]
			toState, toOk := stateMap[sp.To]
			if !fromOk || !toOk {
				log.Printf("Skipping KPI Workflow transition %s: missing state %s or %s", sp.Code, sp.From, sp.To)
				continue
			}

			var count int64
			db.Model(&models.WorkflowTransition{}).Where("workflow_id = ? AND code = ? AND from_state_id = ?", wf.ID, sp.Code, fromState.ID).Count(&count)
			if count > 0 {
				continue
			}

			transition := models.WorkflowTransition{
				WorkflowID:  wf.ID,
				Name:        sp.Name,
				Code:        sp.Code,
				FromStateID: fromState.ID,
				ToStateID:   toState.ID,
				IsActive:    true,
				SortOrder:   sp.SortOrder,
			}
			if err := db.Create(&transition).Error; err != nil {
				log.Printf("Failed to create KPI Workflow transition %s: %v", sp.Code, err)
				continue
			}
			if err := db.Model(&transition).Association("AllowedRoles").Replace(sp.AllowedRoles); err != nil {
				log.Printf("Failed to set allowed roles for transition %s: %v", sp.Code, err)
			}
		}
	}

	if !exists {
		log.Println("KPI Workflow seeded successfully")
	}
}
