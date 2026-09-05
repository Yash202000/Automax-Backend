package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	KPITypeStrategic   = "strategic"
	KPITypeOperational = "operational"
	KPITypeAward       = "award"
)

const (
	KPIPolarityAscending  = "ascending"
	KPIPolarityDescending = "descending"
)

// KPI Workflow states — the KPI dictionary's activation_status column now
// mirrors the KPI Workflow's current state (see kpi_dictionary_workflow_service.go),
// widened from the old 3-value draft/active/inactive domain to these 5 codes.
// KPIStatusInactive is retained only for reading pre-existing rows; nothing
// new writes it.
const (
	KPIStatusDraft    = "draft"
	KPIStatusReviewed = "reviewed"
	KPIStatusApproved = "approved"
	KPIStatusActive   = "active"
	KPIStatusClosed   = "closed"
	KPIStatusInactive = "inactive"
)

const (
	KPIFrequencyMonthly    = "monthly"
	KPIFrequencyQuarterly  = "quarterly"
	KPIFrequencySemiAnnual = "semi_annual"
	KPIFrequencyAnnually   = "annually"
	KPIFrequencyCustom     = "custom"
)

const (
	KPIOwnerTypeInternal = "internal"
	KPIOwnerTypeExternal = "external"
)

// ──────────────────────────────────────────────────────────
// StrategicKPI
// Code format: KPI-P{pillar_no}-{goal_no}-{seq}  e.g. KPI-P1-01-01
// ──────────────────────────────────────────────────────────

type StrategicKPI struct {
	ID                 uuid.UUID        `gorm:"type:uuid;primary_key" json:"id"`
	Code               string           `gorm:"size:50;uniqueIndex;not null" json:"code"`
	NameEn             string           `gorm:"size:255;not null" json:"name_en"`
	NameAr             string           `gorm:"size:255;not null;default:''" json:"name_ar"`
	PillarID           *uuid.UUID       `gorm:"type:uuid;index" json:"pillar_id"`
	Pillar             *Pillar          `gorm:"foreignKey:PillarID" json:"pillar,omitempty"`
	DomainID           *uuid.UUID       `gorm:"type:uuid;index" json:"domain_id"`
	Domain             *Domain          `gorm:"foreignKey:DomainID" json:"domain,omitempty"`
	OwnerType          string           `gorm:"size:20;not null;default:'internal'" json:"owner_type"`
	OwnerDeptID        *uuid.UUID       `gorm:"type:uuid;index" json:"owner_dept_id"`
	OwnerDept          *Department      `gorm:"foreignKey:OwnerDeptID" json:"owner_dept,omitempty"`
	OwnerOrgID         *uuid.UUID       `gorm:"type:uuid;index" json:"owner_org_id"`
	OwnerOrg           *KpiOrganization `gorm:"foreignKey:OwnerOrgID" json:"owner_org,omitempty"`
	OwningAgencyID     *uuid.UUID       `gorm:"type:uuid;index" json:"owning_agency_id"`
	OwningAgency       *Department      `gorm:"foreignKey:OwningAgencyID" json:"owning_agency,omitempty"`
	GoalID             *uuid.UUID       `gorm:"type:uuid;index" json:"goal_id"`
	Goal               *Goal            `gorm:"foreignKey:GoalID" json:"goal,omitempty"`
	ProcessID          *uuid.UUID       `gorm:"type:uuid;index" json:"process_id"`
	Process            *Process         `gorm:"foreignKey:ProcessID" json:"process,omitempty"`
	Polarity           string           `gorm:"size:20;not null;default:'ascending'" json:"polarity"`
	ActivationStatus   string           `gorm:"size:20;not null;default:'draft'" json:"activation_status"`
	DescriptionEn      string           `gorm:"type:text" json:"description_en"`
	DescriptionAr      string           `gorm:"type:text" json:"description_ar"`
	Formula            string           `gorm:"type:text" json:"formula"`
	Baseline           float64          `gorm:"default:0" json:"baseline"`
	UnitOfMeasure      string           `gorm:"size:50" json:"unit_of_measure"`
	ReportingFrequency string           `gorm:"size:50" json:"reporting_frequency"`
	Lifecycle          string           `gorm:"size:100" json:"lifecycle"`
	DataSource         string           `gorm:"size:255" json:"data_source"`
	SegmentationAxes   string           `gorm:"type:text" json:"segmentation_axes"`
	RelatedUnits       string           `gorm:"type:text" json:"related_units"`
	Notes              string           `gorm:"type:text" json:"notes"`
	DocumentaFolderID  string           `gorm:"size:255" json:"documenta_folder_id"`
	WorkflowInstanceID *uuid.UUID       `gorm:"type:uuid;index" json:"workflow_instance_id"`
	CreatedAt          time.Time        `json:"created_at"`
	UpdatedAt          time.Time        `json:"updated_at"`
	DeletedAt          gorm.DeletedAt   `gorm:"index" json:"-"`
}

func (k *StrategicKPI) BeforeCreate(tx *gorm.DB) error {
	if k.ID == uuid.Nil {
		k.ID = uuid.New()
	}
	return nil
}

type StrategicKPIRequest struct {
	Code               string     `json:"code" validate:"required,max=50"`
	NameEn             string     `json:"name_en" validate:"required,max=255"`
	NameAr             string     `json:"name_ar" validate:"max=255"`
	PillarID           *uuid.UUID `json:"pillar_id"`
	DomainID           *uuid.UUID `json:"domain_id"`
	OwnerType          string     `json:"owner_type" validate:"omitempty,oneof=internal external"`
	OwnerDeptID        *uuid.UUID `json:"owner_dept_id"`
	OwnerOrgID         *uuid.UUID `json:"owner_org_id"`
	OwningAgencyID     *uuid.UUID `json:"owning_agency_id"`
	GoalID             uuid.UUID  `json:"goal_id" validate:"required"`
	ProcessID          uuid.UUID  `json:"process_id" validate:"required"`
	Polarity           string     `json:"polarity" validate:"omitempty,oneof=ascending descending"`
	DescriptionEn      string     `json:"description_en"`
	DescriptionAr      string     `json:"description_ar"`
	Formula            string     `json:"formula"`
	Baseline           float64    `json:"baseline"`
	UnitOfMeasure      string     `json:"unit_of_measure" validate:"max=50"`
	ReportingFrequency string     `json:"reporting_frequency" validate:"omitempty,oneof=monthly quarterly semi_annual annually custom"`
	Lifecycle          string     `json:"lifecycle" validate:"max=100"`
	DataSource         string     `json:"data_source" validate:"max=255"`
	SegmentationAxes   string     `json:"segmentation_axes"`
	RelatedUnits       string     `json:"related_units"`
	Notes              string     `json:"notes"`
	DocumentaFolderID  string     `json:"documenta_folder_id"`
}

type StrategicKPIResponse struct {
	ID                 uuid.UUID                `json:"id"`
	Code               string                   `json:"code"`
	NameEn             string                   `json:"name_en"`
	NameAr             string                   `json:"name_ar"`
	PillarID           *uuid.UUID               `json:"pillar_id"`
	Pillar             *PillarResponse          `json:"pillar,omitempty"`
	DomainID           *uuid.UUID               `json:"domain_id"`
	Domain             *DomainResponse          `json:"domain,omitempty"`
	OwnerType          string                   `json:"owner_type"`
	OwnerDeptID        *uuid.UUID               `json:"owner_dept_id"`
	OwnerDept          *DepartmentBriefResponse `json:"owner_dept,omitempty"`
	OwnerOrgID         *uuid.UUID               `json:"owner_org_id"`
	OwnerOrg           *KpiOrganizationResponse `json:"owner_org,omitempty"`
	OwningAgencyID     *uuid.UUID               `json:"owning_agency_id"`
	OwningAgency       *DepartmentBriefResponse `json:"owning_agency,omitempty"`
	GoalID             *uuid.UUID               `json:"goal_id"`
	Goal               *GoalBriefResponse       `json:"goal,omitempty"`
	ProcessID          *uuid.UUID               `json:"process_id"`
	Process            *ProcessResponse         `json:"process,omitempty"`
	Polarity           string                   `json:"polarity"`
	ActivationStatus   string                   `json:"activation_status"`
	DescriptionEn      string                   `json:"description_en"`
	DescriptionAr      string                   `json:"description_ar"`
	Formula            string                   `json:"formula"`
	Baseline           float64                  `json:"baseline"`
	UnitOfMeasure      string                   `json:"unit_of_measure"`
	ReportingFrequency string                   `json:"reporting_frequency"`
	Lifecycle          string                   `json:"lifecycle"`
	DataSource         string                   `json:"data_source"`
	SegmentationAxes   string                   `json:"segmentation_axes"`
	RelatedUnits       string                   `json:"related_units"`
	Notes              string                   `json:"notes"`
	DocumentaFolderID  string                   `json:"documenta_folder_id"`
	WorkflowInstanceID *uuid.UUID               `json:"workflow_instance_id"`
	CreatedAt          time.Time                `json:"created_at"`
	UpdatedAt          time.Time                `json:"updated_at"`
}

func (k *StrategicKPI) ToResponse() StrategicKPIResponse {
	resp := StrategicKPIResponse{
		ID:                 k.ID,
		Code:               k.Code,
		NameEn:             k.NameEn,
		NameAr:             k.NameAr,
		PillarID:           k.PillarID,
		DomainID:           k.DomainID,
		OwnerType:          k.OwnerType,
		OwnerDeptID:        k.OwnerDeptID,
		OwnerOrgID:         k.OwnerOrgID,
		OwningAgencyID:     k.OwningAgencyID,
		GoalID:             k.GoalID,
		ProcessID:          k.ProcessID,
		Polarity:           k.Polarity,
		ActivationStatus:   k.ActivationStatus,
		DescriptionEn:      k.DescriptionEn,
		DescriptionAr:      k.DescriptionAr,
		Formula:            k.Formula,
		Baseline:           k.Baseline,
		UnitOfMeasure:      k.UnitOfMeasure,
		ReportingFrequency: k.ReportingFrequency,
		Lifecycle:          k.Lifecycle,
		DataSource:         k.DataSource,
		SegmentationAxes:   k.SegmentationAxes,
		RelatedUnits:       k.RelatedUnits,
		Notes:              k.Notes,
		DocumentaFolderID:  k.DocumentaFolderID,
		WorkflowInstanceID: k.WorkflowInstanceID,
		CreatedAt:          k.CreatedAt,
		UpdatedAt:          k.UpdatedAt,
	}
	if k.Pillar != nil {
		r := k.Pillar.ToResponse()
		resp.Pillar = &r
	}
	if k.Domain != nil {
		r := k.Domain.ToResponse()
		resp.Domain = &r
	}
	if k.OwnerDept != nil {
		resp.OwnerDept = ToDepartmentBriefResponse(k.OwnerDept)
	}
	if k.OwnerOrg != nil {
		r := k.OwnerOrg.ToResponse()
		resp.OwnerOrg = &r
	}
	if k.OwningAgency != nil {
		resp.OwningAgency = ToDepartmentBriefResponse(k.OwningAgency)
	}
	if k.Goal != nil {
		r := ToGoalBriefResponse(k.Goal)
		resp.Goal = r
	}
	if k.Process != nil {
		r := k.Process.ToResponse()
		resp.Process = &r
	}
	return resp
}

// ──────────────────────────────────────────────────────────
// OperationalKPI
// Code format: OP-P{pillar_no}-{goal_no}-{seq}  e.g. OP-P1-01-01
// ──────────────────────────────────────────────────────────

type OperationalKPI struct {
	ID                     uuid.UUID             `gorm:"type:uuid;primary_key" json:"id"`
	Code                   string                `gorm:"size:50;uniqueIndex;not null" json:"code"`
	NameEn                 string                `gorm:"size:255;not null" json:"name_en"`
	NameAr                 string                `gorm:"size:255;not null;default:''" json:"name_ar"`
	GoalID                 *uuid.UUID            `gorm:"type:uuid;index" json:"goal_id"`
	Goal                   *Goal                 `gorm:"foreignKey:GoalID" json:"goal,omitempty"`
	OperationalObjectiveID uuid.UUID             `gorm:"type:uuid;not null;index" json:"operational_objective_id"`
	OperationalObjective   *OperationalObjective `gorm:"foreignKey:OperationalObjectiveID" json:"operational_objective,omitempty"`
	ProcessID              uuid.UUID             `gorm:"type:uuid;not null;index" json:"process_id"`
	Process                *Process              `gorm:"foreignKey:ProcessID" json:"process,omitempty"`
	DomainID               *uuid.UUID            `gorm:"type:uuid;index" json:"domain_id"`
	Domain                 *Domain               `gorm:"foreignKey:DomainID" json:"domain,omitempty"`
	OwnerType              string                `gorm:"size:20;not null;default:'internal'" json:"owner_type"`
	OwnerDeptID            *uuid.UUID            `gorm:"type:uuid;index" json:"owner_dept_id"`
	OwnerDept              *Department           `gorm:"foreignKey:OwnerDeptID" json:"owner_dept,omitempty"`
	OwnerOrgID             *uuid.UUID            `gorm:"type:uuid;index" json:"owner_org_id"`
	OwnerOrg               *KpiOrganization      `gorm:"foreignKey:OwnerOrgID" json:"owner_org,omitempty"`
	OwningAgencyID         *uuid.UUID            `gorm:"type:uuid;index" json:"owning_agency_id"`
	OwningAgency           *Department           `gorm:"foreignKey:OwningAgencyID" json:"owning_agency,omitempty"`
	Polarity               string                `gorm:"size:20;not null;default:'ascending'" json:"polarity"`
	ActivationStatus       string                `gorm:"size:20;not null;default:'draft'" json:"activation_status"`
	DescriptionEn          string                `gorm:"type:text" json:"description_en"`
	DescriptionAr          string                `gorm:"type:text" json:"description_ar"`
	Formula                string                `gorm:"type:text" json:"formula"`
	Baseline               float64               `gorm:"default:0" json:"baseline"`
	UnitOfMeasure          string                `gorm:"size:50" json:"unit_of_measure"`
	ReportingFrequency     string                `gorm:"size:50" json:"reporting_frequency"`
	Lifecycle              string                `gorm:"size:100" json:"lifecycle"`
	DataSource             string                `gorm:"size:255" json:"data_source"`
	Notes                  string                `gorm:"type:text" json:"notes"`
	DocumentaFolderID      string                `gorm:"size:255" json:"documenta_folder_id"`
	WorkflowInstanceID     *uuid.UUID            `gorm:"type:uuid;index" json:"workflow_instance_id"`
	CreatedAt              time.Time             `json:"created_at"`
	UpdatedAt              time.Time             `json:"updated_at"`
	DeletedAt              gorm.DeletedAt        `gorm:"index" json:"-"`
}

func (k *OperationalKPI) BeforeCreate(tx *gorm.DB) error {
	if k.ID == uuid.Nil {
		k.ID = uuid.New()
	}
	return nil
}

type OperationalKPIRequest struct {
	Code                   string     `json:"code" validate:"required,max=50"`
	NameEn                 string     `json:"name_en" validate:"required,max=255"`
	NameAr                 string     `json:"name_ar" validate:"max=255"`
	GoalID                 uuid.UUID  `json:"goal_id" validate:"required"`
	OperationalObjectiveID uuid.UUID  `json:"operational_objective_id" validate:"required"`
	ProcessID              uuid.UUID  `json:"process_id" validate:"required"`
	DomainID               *uuid.UUID `json:"domain_id"`
	OwnerType              string     `json:"owner_type" validate:"omitempty,oneof=internal external"`
	OwnerDeptID            *uuid.UUID `json:"owner_dept_id"`
	OwnerOrgID             *uuid.UUID `json:"owner_org_id"`
	OwningAgencyID         *uuid.UUID `json:"owning_agency_id"`
	Polarity               string     `json:"polarity" validate:"omitempty,oneof=ascending descending"`
	DescriptionEn          string     `json:"description_en"`
	DescriptionAr          string     `json:"description_ar"`
	Formula                string     `json:"formula"`
	Baseline               float64    `json:"baseline"`
	UnitOfMeasure          string     `json:"unit_of_measure" validate:"max=50"`
	ReportingFrequency     string     `json:"reporting_frequency" validate:"omitempty,oneof=monthly quarterly semi_annual annually custom"`
	Lifecycle              string     `json:"lifecycle" validate:"max=100"`
	DataSource             string     `json:"data_source" validate:"max=255"`
	Notes                  string     `json:"notes"`
	DocumentaFolderID      string     `json:"documenta_folder_id"`
}

type OperationalKPIResponse struct {
	ID                     uuid.UUID                     `json:"id"`
	Code                   string                        `json:"code"`
	NameEn                 string                        `json:"name_en"`
	NameAr                 string                        `json:"name_ar"`
	GoalID                 *uuid.UUID                    `json:"goal_id"`
	Goal                   *GoalBriefResponse            `json:"goal,omitempty"`
	OperationalObjectiveID uuid.UUID                     `json:"operational_objective_id"`
	OperationalObjective   *OperationalObjectiveResponse `json:"operational_objective,omitempty"`
	ProcessID              uuid.UUID                     `json:"process_id"`
	Process                *ProcessResponse              `json:"process,omitempty"`
	DomainID               *uuid.UUID                    `json:"domain_id"`
	Domain                 *DomainResponse               `json:"domain,omitempty"`
	OwnerType              string                        `json:"owner_type"`
	OwnerDeptID            *uuid.UUID                    `json:"owner_dept_id"`
	OwnerDept              *DepartmentBriefResponse      `json:"owner_dept,omitempty"`
	OwnerOrgID             *uuid.UUID                    `json:"owner_org_id"`
	OwnerOrg               *KpiOrganizationResponse      `json:"owner_org,omitempty"`
	OwningAgencyID         *uuid.UUID                    `json:"owning_agency_id"`
	OwningAgency           *DepartmentBriefResponse      `json:"owning_agency,omitempty"`
	Polarity               string                        `json:"polarity"`
	ActivationStatus       string                        `json:"activation_status"`
	DescriptionEn          string                        `json:"description_en"`
	DescriptionAr          string                        `json:"description_ar"`
	Formula                string                        `json:"formula"`
	Baseline               float64                       `json:"baseline"`
	UnitOfMeasure          string                        `json:"unit_of_measure"`
	ReportingFrequency     string                        `json:"reporting_frequency"`
	Lifecycle              string                        `json:"lifecycle"`
	DataSource             string                        `json:"data_source"`
	Notes                  string                        `json:"notes"`
	DocumentaFolderID      string                        `json:"documenta_folder_id"`
	WorkflowInstanceID     *uuid.UUID                    `json:"workflow_instance_id"`
	CreatedAt              time.Time                     `json:"created_at"`
	UpdatedAt              time.Time                     `json:"updated_at"`
}

func (k *OperationalKPI) ToResponse() OperationalKPIResponse {
	resp := OperationalKPIResponse{
		ID:                     k.ID,
		Code:                   k.Code,
		NameEn:                 k.NameEn,
		NameAr:                 k.NameAr,
		GoalID:                 k.GoalID,
		OperationalObjectiveID: k.OperationalObjectiveID,
		ProcessID:              k.ProcessID,
		DomainID:               k.DomainID,
		OwnerType:              k.OwnerType,
		OwnerDeptID:            k.OwnerDeptID,
		OwnerOrgID:             k.OwnerOrgID,
		OwningAgencyID:         k.OwningAgencyID,
		Polarity:               k.Polarity,
		ActivationStatus:       k.ActivationStatus,
		DescriptionEn:          k.DescriptionEn,
		DescriptionAr:          k.DescriptionAr,
		Formula:                k.Formula,
		Baseline:               k.Baseline,
		UnitOfMeasure:          k.UnitOfMeasure,
		ReportingFrequency:     k.ReportingFrequency,
		Lifecycle:              k.Lifecycle,
		DataSource:             k.DataSource,
		Notes:                  k.Notes,
		DocumentaFolderID:      k.DocumentaFolderID,
		WorkflowInstanceID:     k.WorkflowInstanceID,
		CreatedAt:              k.CreatedAt,
		UpdatedAt:              k.UpdatedAt,
	}
	if k.OperationalObjective != nil {
		r := k.OperationalObjective.ToResponse()
		resp.OperationalObjective = &r
	}
	if k.Process != nil {
		r := k.Process.ToResponse()
		resp.Process = &r
	}
	if k.Domain != nil {
		r := k.Domain.ToResponse()
		resp.Domain = &r
	}
	if k.Goal != nil {
		resp.Goal = ToGoalBriefResponse(k.Goal)
	}
	if k.OwnerDept != nil {
		resp.OwnerDept = ToDepartmentBriefResponse(k.OwnerDept)
	}
	if k.OwnerOrg != nil {
		r := k.OwnerOrg.ToResponse()
		resp.OwnerOrg = &r
	}
	if k.OwningAgency != nil {
		resp.OwningAgency = ToDepartmentBriefResponse(k.OwningAgency)
	}
	return resp
}

// ──────────────────────────────────────────────────────────
// AwardKPI
// Code format: KPI-AW-{criterion_no}-{seq}  e.g. KPI-AW-05-01
// ──────────────────────────────────────────────────────────

type AwardKPI struct {
	ID                  uuid.UUID          `gorm:"type:uuid;primary_key" json:"id"`
	Code                string             `gorm:"size:50;uniqueIndex;not null" json:"code"`
	NameEn              string             `gorm:"size:255;not null" json:"name_en"`
	NameAr              string             `gorm:"size:255;not null;default:''" json:"name_ar"`
	AwardSubCriterionID uuid.UUID          `gorm:"type:uuid;not null;index" json:"award_sub_criterion_id"`
	AwardSubCriterion   *AwardSubCriterion `gorm:"foreignKey:AwardSubCriterionID" json:"award_sub_criterion,omitempty"`
	DomainID            *uuid.UUID         `gorm:"type:uuid;index" json:"domain_id"`
	Domain              *Domain            `gorm:"foreignKey:DomainID" json:"domain,omitempty"`
	OwnerType           string             `gorm:"size:20;not null;default:'internal'" json:"owner_type"`
	OwnerDeptID         *uuid.UUID         `gorm:"type:uuid;index" json:"owner_dept_id"`
	OwnerDept           *Department        `gorm:"foreignKey:OwnerDeptID" json:"owner_dept,omitempty"`
	OwnerOrgID          *uuid.UUID         `gorm:"type:uuid;index" json:"owner_org_id"`
	OwnerOrg            *KpiOrganization   `gorm:"foreignKey:OwnerOrgID" json:"owner_org,omitempty"`
	OwningAgencyID      *uuid.UUID         `gorm:"type:uuid;index" json:"owning_agency_id"`
	OwningAgency        *Department        `gorm:"foreignKey:OwningAgencyID" json:"owning_agency,omitempty"`
	Polarity            string             `gorm:"size:20;not null;default:'ascending'" json:"polarity"`
	ActivationStatus    string             `gorm:"size:20;not null;default:'draft'" json:"activation_status"`
	DescriptionEn       string             `gorm:"type:text" json:"description_en"`
	DescriptionAr       string             `gorm:"type:text" json:"description_ar"`
	Formula             string             `gorm:"type:text" json:"formula"`
	Baseline            float64            `gorm:"default:0" json:"baseline"`
	UnitOfMeasure       string             `gorm:"size:50" json:"unit_of_measure"`
	ReportingFrequency  string             `gorm:"size:50" json:"reporting_frequency"`
	Lifecycle           string             `gorm:"size:100" json:"lifecycle"`
	DataSource          string             `gorm:"size:255" json:"data_source"`
	Notes               string             `gorm:"type:text" json:"notes"`
	DocumentaFolderID   string             `gorm:"size:255" json:"documenta_folder_id"`
	WorkflowInstanceID  *uuid.UUID         `gorm:"type:uuid;index" json:"workflow_instance_id"`
	CreatedAt           time.Time          `json:"created_at"`
	UpdatedAt           time.Time          `json:"updated_at"`
	DeletedAt           gorm.DeletedAt     `gorm:"index" json:"-"`
}

func (k *AwardKPI) BeforeCreate(tx *gorm.DB) error {
	if k.ID == uuid.Nil {
		k.ID = uuid.New()
	}
	return nil
}

type AwardKPIRequest struct {
	Code                string     `json:"code" validate:"required,max=50"`
	NameEn              string     `json:"name_en" validate:"required,max=255"`
	NameAr              string     `json:"name_ar" validate:"max=255"`
	AwardSubCriterionID uuid.UUID  `json:"award_sub_criterion_id" validate:"required"`
	DomainID            *uuid.UUID `json:"domain_id"`
	OwnerType           string     `json:"owner_type" validate:"omitempty,oneof=internal external"`
	OwnerDeptID         *uuid.UUID `json:"owner_dept_id"`
	OwnerOrgID          *uuid.UUID `json:"owner_org_id"`
	OwningAgencyID      *uuid.UUID `json:"owning_agency_id"`
	Polarity            string     `json:"polarity" validate:"omitempty,oneof=ascending descending"`
	DescriptionEn       string     `json:"description_en"`
	DescriptionAr       string     `json:"description_ar"`
	Formula             string     `json:"formula"`
	Baseline            float64    `json:"baseline"`
	UnitOfMeasure       string     `json:"unit_of_measure" validate:"max=50"`
	ReportingFrequency  string     `json:"reporting_frequency" validate:"omitempty,oneof=monthly quarterly semi_annual annually custom"`
	Lifecycle           string     `json:"lifecycle" validate:"max=100"`
	DataSource          string     `json:"data_source" validate:"max=255"`
	Notes               string     `json:"notes"`
	DocumentaFolderID   string     `json:"documenta_folder_id"`
}

type AwardKPIResponse struct {
	ID                  uuid.UUID                  `json:"id"`
	Code                string                     `json:"code"`
	NameEn              string                     `json:"name_en"`
	NameAr              string                     `json:"name_ar"`
	AwardSubCriterionID uuid.UUID                  `json:"award_sub_criterion_id"`
	AwardSubCriterion   *AwardSubCriterionResponse `json:"award_sub_criterion,omitempty"`
	DomainID            *uuid.UUID                 `json:"domain_id"`
	Domain              *DomainResponse            `json:"domain,omitempty"`
	OwnerType           string                     `json:"owner_type"`
	OwnerDeptID         *uuid.UUID                 `json:"owner_dept_id"`
	OwnerDept           *DepartmentBriefResponse   `json:"owner_dept,omitempty"`
	OwnerOrgID          *uuid.UUID                 `json:"owner_org_id"`
	OwnerOrg            *KpiOrganizationResponse   `json:"owner_org,omitempty"`
	OwningAgencyID      *uuid.UUID                 `json:"owning_agency_id"`
	OwningAgency        *DepartmentBriefResponse   `json:"owning_agency,omitempty"`
	Polarity            string                     `json:"polarity"`
	ActivationStatus    string                     `json:"activation_status"`
	DescriptionEn       string                     `json:"description_en"`
	DescriptionAr       string                     `json:"description_ar"`
	Formula             string                     `json:"formula"`
	Baseline            float64                    `json:"baseline"`
	UnitOfMeasure       string                     `json:"unit_of_measure"`
	ReportingFrequency  string                     `json:"reporting_frequency"`
	Lifecycle           string                     `json:"lifecycle"`
	DataSource          string                     `json:"data_source"`
	Notes               string                     `json:"notes"`
	DocumentaFolderID   string                     `json:"documenta_folder_id"`
	WorkflowInstanceID  *uuid.UUID                 `json:"workflow_instance_id"`
	CreatedAt           time.Time                  `json:"created_at"`
	UpdatedAt           time.Time                  `json:"updated_at"`
}

func (k *AwardKPI) ToResponse() AwardKPIResponse {
	resp := AwardKPIResponse{
		ID:                  k.ID,
		Code:                k.Code,
		NameEn:              k.NameEn,
		NameAr:              k.NameAr,
		AwardSubCriterionID: k.AwardSubCriterionID,
		DomainID:            k.DomainID,
		OwnerType:           k.OwnerType,
		OwnerDeptID:         k.OwnerDeptID,
		OwnerOrgID:          k.OwnerOrgID,
		OwningAgencyID:      k.OwningAgencyID,
		Polarity:            k.Polarity,
		ActivationStatus:    k.ActivationStatus,
		DescriptionEn:       k.DescriptionEn,
		DescriptionAr:       k.DescriptionAr,
		Formula:             k.Formula,
		Baseline:            k.Baseline,
		UnitOfMeasure:       k.UnitOfMeasure,
		ReportingFrequency:  k.ReportingFrequency,
		Lifecycle:           k.Lifecycle,
		DataSource:          k.DataSource,
		Notes:               k.Notes,
		DocumentaFolderID:   k.DocumentaFolderID,
		WorkflowInstanceID:  k.WorkflowInstanceID,
		CreatedAt:           k.CreatedAt,
		UpdatedAt:           k.UpdatedAt,
	}
	if k.AwardSubCriterion != nil {
		r := k.AwardSubCriterion.ToResponse()
		resp.AwardSubCriterion = &r
	}
	if k.Domain != nil {
		r := k.Domain.ToResponse()
		resp.Domain = &r
	}
	if k.OwnerDept != nil {
		resp.OwnerDept = ToDepartmentBriefResponse(k.OwnerDept)
	}
	if k.OwnerOrg != nil {
		r := k.OwnerOrg.ToResponse()
		resp.OwnerOrg = &r
	}
	if k.OwningAgency != nil {
		resp.OwningAgency = ToDepartmentBriefResponse(k.OwningAgency)
	}
	return resp
}
