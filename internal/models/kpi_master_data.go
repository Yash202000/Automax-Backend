package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	InitiativeStatusDraft     = "draft"
	InitiativeStatusActive    = "active"
	InitiativeStatusOnHold    = "on_hold"
	InitiativeStatusComplete  = "complete"
	InitiativeStatusCancelled = "cancelled"
)

const (
	DomainTypeStrategy         = "strategy"
	DomainTypeAward            = "award"
	DomainTypeStrategyAndAward = "strategy_and_award"
)

// ──────────────────────────────────────────────────────────
// Pillar
// ──────────────────────────────────────────────────────────

type Pillar struct {
	ID          uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	NameEn      string         `gorm:"size:255;not null" json:"name_en"`
	NameAr      string         `gorm:"size:255;not null;default:''" json:"name_ar"`
	OwnerID     *uuid.UUID     `gorm:"type:uuid;index" json:"owner_id"`
	Owner       *User          `gorm:"foreignKey:OwnerID" json:"owner,omitempty"`
	Initiatives []Initiative   `gorm:"foreignKey:PillarID" json:"initiatives,omitempty"`
	IsActive    bool           `gorm:"default:true" json:"is_active"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (p *Pillar) BeforeCreate(tx *gorm.DB) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	return nil
}

type PillarRequest struct {
	NameEn  string     `json:"name_en" validate:"required,max=255"`
	NameAr  string     `json:"name_ar" validate:"max=255"`
	OwnerID *uuid.UUID `json:"owner_id"`
}

type PillarResponse struct {
	ID        uuid.UUID          `json:"id"`
	NameEn    string             `json:"name_en"`
	NameAr    string             `json:"name_ar"`
	OwnerID   *uuid.UUID         `json:"owner_id"`
	Owner     *UserBriefResponse `json:"owner,omitempty"`
	IsActive  bool               `json:"is_active"`
	CreatedAt time.Time          `json:"created_at"`
}

func (p *Pillar) ToResponse() PillarResponse {
	return PillarResponse{
		ID:        p.ID,
		NameEn:    p.NameEn,
		NameAr:    p.NameAr,
		OwnerID:   p.OwnerID,
		Owner:     ToUserBriefResponse(p.Owner),
		IsActive:  p.IsActive,
		CreatedAt: p.CreatedAt,
	}
}

// ──────────────────────────────────────────────────────────
// Enabler
// ──────────────────────────────────────────────────────────

type Enabler struct {
	ID          uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	NameEn      string         `gorm:"size:255;not null" json:"name_en"`
	NameAr      string         `gorm:"size:255;not null;default:''" json:"name_ar"`
	OwnerID     *uuid.UUID     `gorm:"type:uuid;index" json:"owner_id"`
	Owner       *User          `gorm:"foreignKey:OwnerID" json:"owner,omitempty"`
	Initiatives []Initiative   `gorm:"foreignKey:EnablerID" json:"initiatives,omitempty"`
	IsActive    bool           `gorm:"default:true" json:"is_active"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (e *Enabler) BeforeCreate(tx *gorm.DB) error {
	if e.ID == uuid.Nil {
		e.ID = uuid.New()
	}
	return nil
}

type EnablerRequest struct {
	NameEn  string     `json:"name_en" validate:"required,max=255"`
	NameAr  string     `json:"name_ar" validate:"max=255"`
	OwnerID *uuid.UUID `json:"owner_id"`
}

type EnablerResponse struct {
	ID        uuid.UUID          `json:"id"`
	NameEn    string             `json:"name_en"`
	NameAr    string             `json:"name_ar"`
	OwnerID   *uuid.UUID         `json:"owner_id"`
	Owner     *UserBriefResponse `json:"owner,omitempty"`
	IsActive  bool               `json:"is_active"`
	CreatedAt time.Time          `json:"created_at"`
}

func (e *Enabler) ToResponse() EnablerResponse {
	return EnablerResponse{
		ID:        e.ID,
		NameEn:    e.NameEn,
		NameAr:    e.NameAr,
		OwnerID:   e.OwnerID,
		Owner:     ToUserBriefResponse(e.Owner),
		IsActive:  e.IsActive,
		CreatedAt: e.CreatedAt,
	}
}

// ──────────────────────────────────────────────────────────
// OperationalObjective
// ──────────────────────────────────────────────────────────
//
// OperationalObjective, Process, and Initiative classify against the Goal
// Management `Goal` entity directly (GoalID) rather than a separate
// "StrategicGoal" master-data table — Goal already is the strategic goal.

type OperationalObjective struct {
	ID        uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	NameEn    string         `gorm:"size:255;not null" json:"name_en"`
	NameAr    string         `gorm:"size:255;not null;default:''" json:"name_ar"`
	GoalID    *uuid.UUID     `gorm:"type:uuid;index" json:"goal_id"`
	Goal      *Goal          `gorm:"foreignKey:GoalID" json:"goal,omitempty"`
	PillarID  *uuid.UUID     `gorm:"type:uuid;index" json:"pillar_id"`
	Pillar    *Pillar        `gorm:"foreignKey:PillarID" json:"pillar,omitempty"`
	EnablerID *uuid.UUID     `gorm:"type:uuid;index" json:"enabler_id"`
	Enabler   *Enabler       `gorm:"foreignKey:EnablerID" json:"enabler,omitempty"`
	Processes []Process      `gorm:"foreignKey:OperationalObjectiveID" json:"processes,omitempty"`
	IsActive  bool           `gorm:"default:true" json:"is_active"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (o *OperationalObjective) BeforeCreate(tx *gorm.DB) error {
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	return nil
}

type OperationalObjectiveRequest struct {
	NameEn    string     `json:"name_en" validate:"required,max=255"`
	NameAr    string     `json:"name_ar" validate:"max=255"`
	GoalID    uuid.UUID  `json:"goal_id" validate:"required"`
	PillarID  *uuid.UUID `json:"pillar_id"`
	EnablerID *uuid.UUID `json:"enabler_id"`
}

type OperationalObjectiveResponse struct {
	ID        uuid.UUID          `json:"id"`
	NameEn    string             `json:"name_en"`
	NameAr    string             `json:"name_ar"`
	GoalID    *uuid.UUID         `json:"goal_id"`
	Goal      *GoalBriefResponse `json:"goal,omitempty"`
	PillarID  *uuid.UUID         `json:"pillar_id"`
	Pillar    *PillarResponse    `json:"pillar,omitempty"`
	EnablerID *uuid.UUID         `json:"enabler_id"`
	Enabler   *EnablerResponse   `json:"enabler,omitempty"`
	IsActive  bool               `json:"is_active"`
	CreatedAt time.Time          `json:"created_at"`
}

func (o *OperationalObjective) ToResponse() OperationalObjectiveResponse {
	resp := OperationalObjectiveResponse{
		ID:        o.ID,
		NameEn:    o.NameEn,
		NameAr:    o.NameAr,
		GoalID:    o.GoalID,
		PillarID:  o.PillarID,
		EnablerID: o.EnablerID,
		IsActive:  o.IsActive,
		CreatedAt: o.CreatedAt,
	}
	if o.Goal != nil {
		resp.Goal = ToGoalBriefResponse(o.Goal)
	}
	if o.Pillar != nil {
		r := o.Pillar.ToResponse()
		resp.Pillar = &r
	}
	if o.Enabler != nil {
		r := o.Enabler.ToResponse()
		resp.Enabler = &r
	}
	return resp
}

// ──────────────────────────────────────────────────────────
// Process
// ──────────────────────────────────────────────────────────

type Process struct {
	ID                     uuid.UUID             `gorm:"type:uuid;primary_key" json:"id"`
	NameEn                 string                `gorm:"size:255;not null" json:"name_en"`
	NameAr                 string                `gorm:"size:255;not null;default:''" json:"name_ar"`
	OperationalObjectiveID uuid.UUID             `gorm:"type:uuid;not null;index" json:"operational_objective_id"`
	OperationalObjective   *OperationalObjective `gorm:"foreignKey:OperationalObjectiveID" json:"operational_objective,omitempty"`
	GoalID                 *uuid.UUID            `gorm:"type:uuid;index" json:"goal_id"`
	Goal                   *Goal                 `gorm:"foreignKey:GoalID" json:"goal,omitempty"`
	PillarID               *uuid.UUID            `gorm:"type:uuid;index" json:"pillar_id"`
	Pillar                 *Pillar               `gorm:"foreignKey:PillarID" json:"pillar,omitempty"`
	EnablerID              *uuid.UUID            `gorm:"type:uuid;index" json:"enabler_id"`
	Enabler                *Enabler              `gorm:"foreignKey:EnablerID" json:"enabler,omitempty"`
	DepartmentID           *uuid.UUID            `gorm:"type:uuid;index" json:"department_id"`
	Department             *Department           `gorm:"foreignKey:DepartmentID" json:"department,omitempty"`
	Unit                   string                `gorm:"size:255" json:"unit"`
	IsActive               bool                  `gorm:"default:true" json:"is_active"`
	CreatedAt              time.Time             `json:"created_at"`
	UpdatedAt              time.Time             `json:"updated_at"`
	DeletedAt              gorm.DeletedAt        `gorm:"index" json:"-"`
}

func (p *Process) BeforeCreate(tx *gorm.DB) error {
	if p.ID == uuid.Nil {
		p.ID = uuid.New()
	}
	return nil
}

type ProcessRequest struct {
	NameEn                 string     `json:"name_en" validate:"required,max=255"`
	NameAr                 string     `json:"name_ar" validate:"max=255"`
	OperationalObjectiveID uuid.UUID  `json:"operational_objective_id" validate:"required"`
	GoalID                 uuid.UUID  `json:"goal_id" validate:"required"`
	PillarID               *uuid.UUID `json:"pillar_id"`
	EnablerID              *uuid.UUID `json:"enabler_id"`
	DepartmentID           *uuid.UUID `json:"department_id"`
	Unit                   string     `json:"unit" validate:"max=255"`
}

type ProcessResponse struct {
	ID                     uuid.UUID                     `json:"id"`
	NameEn                 string                        `json:"name_en"`
	NameAr                 string                        `json:"name_ar"`
	OperationalObjectiveID uuid.UUID                     `json:"operational_objective_id"`
	OperationalObjective   *OperationalObjectiveResponse `json:"operational_objective,omitempty"`
	GoalID                 *uuid.UUID                    `json:"goal_id"`
	Goal                   *GoalBriefResponse            `json:"goal,omitempty"`
	PillarID               *uuid.UUID                    `json:"pillar_id"`
	Pillar                 *PillarResponse               `json:"pillar,omitempty"`
	EnablerID              *uuid.UUID                    `json:"enabler_id"`
	Enabler                *EnablerResponse              `json:"enabler,omitempty"`
	DepartmentID           *uuid.UUID                    `json:"department_id"`
	Department             *DepartmentBriefResponse      `json:"department,omitempty"`
	Unit                   string                        `json:"unit"`
	IsActive               bool                          `json:"is_active"`
	CreatedAt              time.Time                     `json:"created_at"`
}

func (p *Process) ToResponse() ProcessResponse {
	resp := ProcessResponse{
		ID:                     p.ID,
		NameEn:                 p.NameEn,
		NameAr:                 p.NameAr,
		OperationalObjectiveID: p.OperationalObjectiveID,
		GoalID:                 p.GoalID,
		PillarID:               p.PillarID,
		EnablerID:              p.EnablerID,
		DepartmentID:           p.DepartmentID,
		Unit:                   p.Unit,
		IsActive:               p.IsActive,
		CreatedAt:              p.CreatedAt,
	}
	if p.OperationalObjective != nil {
		r := p.OperationalObjective.ToResponse()
		resp.OperationalObjective = &r
	}
	if p.Goal != nil {
		resp.Goal = ToGoalBriefResponse(p.Goal)
	}
	if p.Pillar != nil {
		r := p.Pillar.ToResponse()
		resp.Pillar = &r
	}
	if p.Enabler != nil {
		r := p.Enabler.ToResponse()
		resp.Enabler = &r
	}
	if p.Department != nil {
		resp.Department = ToDepartmentBriefResponse(p.Department)
	}
	return resp
}

// ──────────────────────────────────────────────────────────
// Initiative
// ──────────────────────────────────────────────────────────

type Initiative struct {
	ID          uuid.UUID             `gorm:"type:uuid;primary_key" json:"id"`
	NameEn      string                `gorm:"size:255;not null" json:"name_en"`
	NameAr      string                `gorm:"size:255;not null;default:''" json:"name_ar"`
	GoalID      *uuid.UUID            `gorm:"type:uuid;index" json:"goal_id"`
	Goal        *Goal                 `gorm:"foreignKey:GoalID" json:"goal,omitempty"`
	ObjectiveID *uuid.UUID            `gorm:"type:uuid;index" json:"objective_id"`
	Objective   *OperationalObjective `gorm:"foreignKey:ObjectiveID" json:"objective,omitempty"`
	PillarID    *uuid.UUID            `gorm:"type:uuid;index" json:"pillar_id"`
	Pillar      *Pillar               `gorm:"foreignKey:PillarID" json:"pillar,omitempty"`
	EnablerID   *uuid.UUID            `gorm:"type:uuid;index" json:"enabler_id"`
	Enabler     *Enabler              `gorm:"foreignKey:EnablerID" json:"enabler,omitempty"`
	OwnerID     *uuid.UUID            `gorm:"type:uuid;index" json:"owner_id"`
	Owner       *User                 `gorm:"foreignKey:OwnerID" json:"owner,omitempty"`
	Status      string                `gorm:"size:50;not null;default:'draft'" json:"status"`
	CreatedAt   time.Time             `json:"created_at"`
	UpdatedAt   time.Time             `json:"updated_at"`
	DeletedAt   gorm.DeletedAt        `gorm:"index" json:"-"`
}

func (i *Initiative) BeforeCreate(tx *gorm.DB) error {
	if i.ID == uuid.Nil {
		i.ID = uuid.New()
	}
	return nil
}

type InitiativeRequest struct {
	NameEn      string     `json:"name_en" validate:"required,max=255"`
	NameAr      string     `json:"name_ar" validate:"max=255"`
	GoalID      uuid.UUID  `json:"goal_id" validate:"required"`
	ObjectiveID *uuid.UUID `json:"objective_id"`
	PillarID    *uuid.UUID `json:"pillar_id"`
	EnablerID   *uuid.UUID `json:"enabler_id"`
	OwnerID     *uuid.UUID `json:"owner_id"`
	Status      string     `json:"status" validate:"omitempty,oneof=draft active on_hold complete cancelled"`
}

type InitiativeResponse struct {
	ID          uuid.UUID                     `json:"id"`
	NameEn      string                        `json:"name_en"`
	NameAr      string                        `json:"name_ar"`
	GoalID      *uuid.UUID                    `json:"goal_id"`
	Goal        *GoalBriefResponse            `json:"goal,omitempty"`
	ObjectiveID *uuid.UUID                    `json:"objective_id"`
	Objective   *OperationalObjectiveResponse `json:"objective,omitempty"`
	PillarID    *uuid.UUID                    `json:"pillar_id"`
	EnablerID   *uuid.UUID                    `json:"enabler_id"`
	OwnerID     *uuid.UUID                    `json:"owner_id"`
	Owner       *UserBriefResponse            `json:"owner,omitempty"`
	Status      string                        `json:"status"`
	CreatedAt   time.Time                     `json:"created_at"`
}

func (i *Initiative) ToResponse() InitiativeResponse {
	resp := InitiativeResponse{
		ID:          i.ID,
		NameEn:      i.NameEn,
		NameAr:      i.NameAr,
		GoalID:      i.GoalID,
		ObjectiveID: i.ObjectiveID,
		PillarID:    i.PillarID,
		EnablerID:   i.EnablerID,
		OwnerID:     i.OwnerID,
		Owner:       ToUserBriefResponse(i.Owner),
		Status:      i.Status,
		CreatedAt:   i.CreatedAt,
	}
	if i.Goal != nil {
		resp.Goal = ToGoalBriefResponse(i.Goal)
	}
	if i.Objective != nil {
		r := i.Objective.ToResponse()
		resp.Objective = &r
	}
	return resp
}

// ──────────────────────────────────────────────────────────
// Domain
// ──────────────────────────────────────────────────────────

type Domain struct {
	ID        uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	NameEn    string         `gorm:"size:255;not null" json:"name_en"`
	NameAr    string         `gorm:"size:255;not null;default:''" json:"name_ar"`
	Type      string         `gorm:"size:50;not null" json:"type"`
	IsActive  bool           `gorm:"default:true" json:"is_active"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (d *Domain) BeforeCreate(tx *gorm.DB) error {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	return nil
}

type DomainRequest struct {
	NameEn string `json:"name_en" validate:"required,max=255"`
	NameAr string `json:"name_ar" validate:"max=255"`
	Type   string `json:"type" validate:"required,oneof=strategy award strategy_and_award"`
}

type DomainResponse struct {
	ID       uuid.UUID `json:"id"`
	NameEn   string    `json:"name_en"`
	NameAr   string    `json:"name_ar"`
	Type     string    `json:"type"`
	IsActive bool      `json:"is_active"`
}

func (d *Domain) ToResponse() DomainResponse {
	return DomainResponse{
		ID:       d.ID,
		NameEn:   d.NameEn,
		NameAr:   d.NameAr,
		Type:     d.Type,
		IsActive: d.IsActive,
	}
}

// ──────────────────────────────────────────────────────────
// AwardCriterion
// ──────────────────────────────────────────────────────────

type AwardCriterion struct {
	ID          uuid.UUID           `gorm:"type:uuid;primary_key" json:"id"`
	CriterionNo int                 `gorm:"uniqueIndex;not null" json:"criterion_no"`
	NameEn      string              `gorm:"size:255;not null" json:"name_en"`
	NameAr      string              `gorm:"size:255;not null;default:''" json:"name_ar"`
	SubCriteria []AwardSubCriterion `gorm:"foreignKey:AwardCriterionID" json:"sub_criteria,omitempty"`
	IsActive    bool                `gorm:"default:true" json:"is_active"`
	CreatedAt   time.Time           `json:"created_at"`
	UpdatedAt   time.Time           `json:"updated_at"`
	DeletedAt   gorm.DeletedAt      `gorm:"index" json:"-"`
}

func (a *AwardCriterion) BeforeCreate(tx *gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}

type AwardCriterionRequest struct {
	CriterionNo int    `json:"criterion_no" validate:"required"`
	NameEn      string `json:"name_en" validate:"required,max=255"`
	NameAr      string `json:"name_ar" validate:"max=255"`
}

type AwardCriterionResponse struct {
	ID          uuid.UUID `json:"id"`
	CriterionNo int       `json:"criterion_no"`
	NameEn      string    `json:"name_en"`
	NameAr      string    `json:"name_ar"`
	IsActive    bool      `json:"is_active"`
}

func (a *AwardCriterion) ToResponse() AwardCriterionResponse {
	return AwardCriterionResponse{
		ID:          a.ID,
		CriterionNo: a.CriterionNo,
		NameEn:      a.NameEn,
		NameAr:      a.NameAr,
		IsActive:    a.IsActive,
	}
}

// ──────────────────────────────────────────────────────────
// AwardSubCriterion
// ──────────────────────────────────────────────────────────

type AwardSubCriterion struct {
	ID               uuid.UUID       `gorm:"type:uuid;primary_key" json:"id"`
	AwardCriterionID uuid.UUID       `gorm:"type:uuid;not null;index" json:"award_criterion_id"`
	AwardCriterion   *AwardCriterion `gorm:"foreignKey:AwardCriterionID" json:"award_criterion,omitempty"`
	SubNo            string          `gorm:"size:20;not null" json:"sub_no"`
	NameEn           string          `gorm:"size:255;not null" json:"name_en"`
	NameAr           string          `gorm:"size:255;not null;default:''" json:"name_ar"`
	IsActive         bool            `gorm:"default:true" json:"is_active"`
	CreatedAt        time.Time       `json:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"`
	DeletedAt        gorm.DeletedAt  `gorm:"index" json:"-"`
}

func (a *AwardSubCriterion) BeforeCreate(tx *gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}

type AwardSubCriterionRequest struct {
	AwardCriterionID uuid.UUID `json:"award_criterion_id" validate:"required"`
	SubNo            string    `json:"sub_no" validate:"required,max=20"`
	NameEn           string    `json:"name_en" validate:"required,max=255"`
	NameAr           string    `json:"name_ar" validate:"max=255"`
}

type AwardSubCriterionResponse struct {
	ID               uuid.UUID               `json:"id"`
	AwardCriterionID uuid.UUID               `json:"award_criterion_id"`
	AwardCriterion   *AwardCriterionResponse `json:"award_criterion,omitempty"`
	SubNo            string                  `json:"sub_no"`
	NameEn           string                  `json:"name_en"`
	NameAr           string                  `json:"name_ar"`
	IsActive         bool                    `json:"is_active"`
}

func (a *AwardSubCriterion) ToResponse() AwardSubCriterionResponse {
	resp := AwardSubCriterionResponse{
		ID:               a.ID,
		AwardCriterionID: a.AwardCriterionID,
		SubNo:            a.SubNo,
		NameEn:           a.NameEn,
		NameAr:           a.NameAr,
		IsActive:         a.IsActive,
	}
	if a.AwardCriterion != nil {
		r := a.AwardCriterion.ToResponse()
		resp.AwardCriterion = &r
	}
	return resp
}

// ──────────────────────────────────────────────────────────
// KpiDataSource — governed list of KPI data source names, replacing the
// free-text data_source field on KPI dictionary records.
// ──────────────────────────────────────────────────────────

type KpiDataSource struct {
	ID        uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	NameEn    string         `gorm:"size:255;not null;uniqueIndex" json:"name_en"`
	NameAr    string         `gorm:"size:255;not null;default:''" json:"name_ar"`
	IsActive  bool           `gorm:"default:true" json:"is_active"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (d *KpiDataSource) BeforeCreate(tx *gorm.DB) error {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	return nil
}

type KpiDataSourceRequest struct {
	NameEn string `json:"name_en" validate:"required,max=255"`
	NameAr string `json:"name_ar" validate:"max=255"`
}

type KpiDataSourceResponse struct {
	ID       uuid.UUID `json:"id"`
	NameEn   string    `json:"name_en"`
	NameAr   string    `json:"name_ar"`
	IsActive bool      `json:"is_active"`
}

func (d *KpiDataSource) ToResponse() KpiDataSourceResponse {
	return KpiDataSourceResponse{
		ID:       d.ID,
		NameEn:   d.NameEn,
		NameAr:   d.NameAr,
		IsActive: d.IsActive,
	}
}

// ──────────────────────────────────────────────────────────
// KpiSegmentationDimension — governed list of segmentation dimension names
// (e.g. Municipality, District, Zone), replacing free-text dimension_name
// entries on KpiSegmentation records.
// ──────────────────────────────────────────────────────────

type KpiSegmentationDimension struct {
	ID        uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	NameEn    string         `gorm:"size:255;not null;uniqueIndex" json:"name_en"`
	NameAr    string         `gorm:"size:255;not null;default:''" json:"name_ar"`
	IsActive  bool           `gorm:"default:true" json:"is_active"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (d *KpiSegmentationDimension) BeforeCreate(tx *gorm.DB) error {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	return nil
}

type KpiSegmentationDimensionRequest struct {
	NameEn string `json:"name_en" validate:"required,max=255"`
	NameAr string `json:"name_ar" validate:"max=255"`
}

type KpiSegmentationDimensionResponse struct {
	ID       uuid.UUID `json:"id"`
	NameEn   string    `json:"name_en"`
	NameAr   string    `json:"name_ar"`
	IsActive bool      `json:"is_active"`
}

func (d *KpiSegmentationDimension) ToResponse() KpiSegmentationDimensionResponse {
	return KpiSegmentationDimensionResponse{
		ID:       d.ID,
		NameEn:   d.NameEn,
		NameAr:   d.NameAr,
		IsActive: d.IsActive,
	}
}
