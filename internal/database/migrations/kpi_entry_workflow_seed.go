package migrations

import (
	"log"

	"github.com/automax/backend/internal/models"
	"gorm.io/gorm"
)

// SeedKpiEntryWorkflow creates a default draft -> submitted -> approved/rejected
// workflow (record_type = "kpi_entry") the first time this runs, so KpiEntry
// submissions have somewhere to go without requiring an admin to hand-build
// one first via the generic Workflow designer. Idempotent: no-ops if a
// kpi_entry workflow already exists (e.g. an admin customized it since).
func SeedKpiEntryWorkflow(db *gorm.DB) error {
	var count int64
	if err := db.Model(&models.Workflow{}).Where("record_type = ?", "kpi_entry").Count(&count).Error; err != nil {
		return nil
	}
	if count > 0 {
		return nil
	}

	wf := &models.Workflow{
		Name:       "KPI Entry Approval",
		Code:       "kpi_entry_approval",
		RecordType: "kpi_entry",
		IsActive:   true,
		IsDefault:  true,
		Description: "Default submit/review/approve workflow for KPI performance entries.",
	}
	if err := db.Create(wf).Error; err != nil {
		log.Printf("kpi_entry workflow seed: failed to create workflow: %v", err)
		return nil
	}

	draft := &models.WorkflowState{WorkflowID: wf.ID, Name: "Draft", Code: "draft", StateType: "initial"}
	submitted := &models.WorkflowState{WorkflowID: wf.ID, Name: "Submitted", Code: "submitted", StateType: "normal"}
	approved := &models.WorkflowState{WorkflowID: wf.ID, Name: "Approved", Code: "approved", StateType: "terminal"}
	rejected := &models.WorkflowState{WorkflowID: wf.ID, Name: "Rejected", Code: "rejected", StateType: "terminal"}
	for _, st := range []*models.WorkflowState{draft, submitted, approved, rejected} {
		if err := db.Create(st).Error; err != nil {
			log.Printf("kpi_entry workflow seed: failed to create state %s: %v", st.Code, err)
			return nil
		}
	}

	transitions := []*models.WorkflowTransition{
		{WorkflowID: wf.ID, Name: "Submit", Code: "submit", FromStateID: draft.ID, ToStateID: submitted.ID},
		{WorkflowID: wf.ID, Name: "Approve", Code: "approve", FromStateID: submitted.ID, ToStateID: approved.ID},
		{WorkflowID: wf.ID, Name: "Reject", Code: "reject", FromStateID: submitted.ID, ToStateID: rejected.ID},
		{WorkflowID: wf.ID, Name: "Request Changes", Code: "request_changes", FromStateID: submitted.ID, ToStateID: draft.ID},
	}
	for _, t := range transitions {
		if err := db.Create(t).Error; err != nil {
			log.Printf("kpi_entry workflow seed: failed to create transition %s: %v", t.Code, err)
			return nil
		}
	}

	log.Printf("kpi_entry workflow seed: created default KPI Entry Approval workflow")
	return nil
}
