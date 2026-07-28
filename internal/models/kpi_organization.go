package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ──────────────────────────────────────────────────────────
// KpiOrganization — external-entity master data, used when a KPI's
// OwnerType is "external" (owner_org_id) instead of an internal Department.
// ──────────────────────────────────────────────────────────

type KpiOrganization struct {
	ID          uuid.UUID      `gorm:"type:uuid;primary_key" json:"id"`
	NameEn      string         `gorm:"size:255;not null" json:"name_en"`
	NameAr      string         `gorm:"size:255;not null;default:''" json:"name_ar"`
	ContactInfo string         `gorm:"size:255" json:"contact_info"`
	IsActive    bool           `gorm:"default:true" json:"is_active"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

func (o *KpiOrganization) BeforeCreate(tx *gorm.DB) error {
	if o.ID == uuid.Nil {
		o.ID = uuid.New()
	}
	return nil
}

type KpiOrganizationRequest struct {
	NameEn      string `json:"name_en" validate:"required,max=255"`
	NameAr      string `json:"name_ar" validate:"max=255"`
	ContactInfo string `json:"contact_info" validate:"max=255"`
}

type KpiOrganizationResponse struct {
	ID          uuid.UUID `json:"id"`
	NameEn      string    `json:"name_en"`
	NameAr      string    `json:"name_ar"`
	ContactInfo string    `json:"contact_info"`
	IsActive    bool      `json:"is_active"`
}

func (o *KpiOrganization) ToResponse() KpiOrganizationResponse {
	return KpiOrganizationResponse{
		ID:          o.ID,
		NameEn:      o.NameEn,
		NameAr:      o.NameAr,
		ContactInfo: o.ContactInfo,
		IsActive:    o.IsActive,
	}
}

// ──────────────────────────────────────────────────────────
// KpiSegmentationAxis — links a KPI dictionary row (any of the three types,
// scoped by KpiID+KpiType like KpiEvidence/KpiMetric) to one governed
// KpiSegmentationDimension. Replaces the free-text segmentation_axes column
// with a structured, KPI-specific, per-type list.
// ──────────────────────────────────────────────────────────

type KpiSegmentationAxis struct {
	ID          uuid.UUID                 `gorm:"type:uuid;primary_key" json:"id"`
	KpiID       uuid.UUID                 `gorm:"type:uuid;index:idx_kpi_seg_axis_unique,unique;not null" json:"kpi_id"`
	KpiType     string                    `gorm:"size:20;index:idx_kpi_seg_axis_unique,unique;not null" json:"kpi_type"`
	DimensionID uuid.UUID                 `gorm:"type:uuid;index:idx_kpi_seg_axis_unique,unique;not null" json:"dimension_id"`
	Dimension   *KpiSegmentationDimension `gorm:"foreignKey:DimensionID" json:"dimension,omitempty"`
	CreatedAt   time.Time                 `json:"created_at"`
}

func (a *KpiSegmentationAxis) BeforeCreate(tx *gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}

type KpiSegmentationAxisRequest struct {
	DimensionID uuid.UUID `json:"dimension_id" validate:"required"`
}

type KpiSegmentationAxisResponse struct {
	ID          uuid.UUID                         `json:"id"`
	KpiID       uuid.UUID                         `json:"kpi_id"`
	KpiType     string                            `json:"kpi_type"`
	DimensionID uuid.UUID                         `json:"dimension_id"`
	Dimension   *KpiSegmentationDimensionResponse `json:"dimension,omitempty"`
}

func (a *KpiSegmentationAxis) ToResponse() KpiSegmentationAxisResponse {
	resp := KpiSegmentationAxisResponse{
		ID:          a.ID,
		KpiID:       a.KpiID,
		KpiType:     a.KpiType,
		DimensionID: a.DimensionID,
	}
	if a.Dimension != nil {
		r := a.Dimension.ToResponse()
		resp.Dimension = &r
	}
	return resp
}

// ──────────────────────────────────────────────────────────
// KpiAdministrativeUnit — links a KPI dictionary row to one or more
// Departments acting as "related administrative units". Replaces the
// free-text related_units column with a structured, multi-value list.
// ──────────────────────────────────────────────────────────

type KpiAdministrativeUnit struct {
	ID           uuid.UUID   `gorm:"type:uuid;primary_key" json:"id"`
	KpiID        uuid.UUID   `gorm:"type:uuid;index:idx_kpi_admin_unit_unique,unique;not null" json:"kpi_id"`
	KpiType      string      `gorm:"size:20;index:idx_kpi_admin_unit_unique,unique;not null" json:"kpi_type"`
	DepartmentID uuid.UUID   `gorm:"type:uuid;index:idx_kpi_admin_unit_unique,unique;not null" json:"department_id"`
	Department   *Department `gorm:"foreignKey:DepartmentID" json:"department,omitempty"`
	CreatedAt    time.Time   `json:"created_at"`
}

func (u *KpiAdministrativeUnit) BeforeCreate(tx *gorm.DB) error {
	if u.ID == uuid.Nil {
		u.ID = uuid.New()
	}
	return nil
}

type KpiAdministrativeUnitRequest struct {
	DepartmentID uuid.UUID `json:"department_id" validate:"required"`
}

type KpiAdministrativeUnitResponse struct {
	ID           uuid.UUID                `json:"id"`
	KpiID        uuid.UUID                `json:"kpi_id"`
	KpiType      string                   `json:"kpi_type"`
	DepartmentID uuid.UUID                `json:"department_id"`
	Department   *DepartmentBriefResponse `json:"department,omitempty"`
}

func (u *KpiAdministrativeUnit) ToResponse() KpiAdministrativeUnitResponse {
	return KpiAdministrativeUnitResponse{
		ID:           u.ID,
		KpiID:        u.KpiID,
		KpiType:      u.KpiType,
		DepartmentID: u.DepartmentID,
		Department:   ToDepartmentBriefResponse(u.Department),
	}
}
