package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Department represents a hierarchical department structure
type Department struct {
	ID            uuid.UUID    `gorm:"type:uuid;primary_key" json:"id"`
	Name          string       `gorm:"not null;size:100" json:"name"`
	NameAr        string       `gorm:"size:100" json:"name_ar"`
	Code          string       `gorm:"size:50;uniqueIndex" json:"code"`
	Description   string       `gorm:"size:500" json:"description"`
	DescriptionAr string       `gorm:"size:500" json:"description_ar"`
	Type          string       `gorm:"size:20;default:internal" json:"type"` // internal | external
	ParentID      *uuid.UUID   `gorm:"type:uuid;index" json:"parent_id"`
	Parent        *Department  `gorm:"foreignKey:ParentID" json:"parent,omitempty"`
	Children      []Department `gorm:"foreignKey:ParentID" json:"children,omitempty"`
	Level         int          `gorm:"default:0" json:"level"`
	Path          string       `gorm:"size:1000" json:"path"`
	ManagerID     *uuid.UUID   `gorm:"type:uuid" json:"manager_id"`
	SupervisorID  *uuid.UUID   `gorm:"type:uuid" json:"supervisor_id"`
	Supervisor    *User        `gorm:"foreignKey:SupervisorID" json:"supervisor,omitempty"`
	IsActive      bool         `gorm:"default:true" json:"is_active"`
	SortOrder     int          `gorm:"default:0" json:"sort_order"`

	// Many-to-many relationships
	Locations       []Location       `gorm:"many2many:department_locations;" json:"locations,omitempty"`
	Classifications []Classification `gorm:"many2many:department_classifications;" json:"classifications,omitempty"`
	Roles           []Role           `gorm:"many2many:department_roles;" json:"roles,omitempty"`

	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func (d *Department) BeforeCreate(tx *gorm.DB) error {
	if d.ID == uuid.Nil {
		d.ID = uuid.New()
	}
	return nil
}

// DepartmentCreateRequest for creating a new department
type DepartmentCreateRequest struct {
	Name              string      `json:"name" validate:"required,notblank,name,min=1,max=100"`
	NameAr            string      `json:"name_ar" validate:"omitempty,notblank,name,max=100"`
	Code              string      `json:"code" validate:"omitempty,min=1,max=50"`
	Description       string      `json:"description" validate:"max=500"`
	DescriptionAr     string      `json:"description_ar" validate:"max=500"`
	Type              string      `json:"type" validate:"omitempty,oneof=internal external"`
	ParentID          *uuid.UUID  `json:"parent_id"`
	ManagerID         *uuid.UUID  `json:"manager_id"`
	SupervisorID      *uuid.UUID  `json:"supervisor_id"`
	LocationIDs       []uuid.UUID `json:"location_ids"`
	ClassificationIDs []uuid.UUID `json:"classification_ids"`
	RoleIDs           []uuid.UUID `json:"role_ids"`
	SortOrder         int         `json:"sort_order"`
}

// DepartmentUpdateRequest for updating a department
type DepartmentUpdateRequest struct {
	Name              string      `json:"name" validate:"omitempty,notblank,name,min=1,max=100"`
	NameAr            string      `json:"name_ar" validate:"omitempty,notblank,name,max=100"`
	Code              string      `json:"code" validate:"omitempty,min=1,max=50"`
	Description       string      `json:"description" validate:"max=500"`
	DescriptionAr     string      `json:"description_ar" validate:"max=500"`
	Type              string      `json:"type" validate:"omitempty,oneof=internal external"`
	ManagerID         *uuid.UUID  `json:"manager_id"`
	SupervisorID      *uuid.UUID  `json:"supervisor_id"`
	LocationIDs       []uuid.UUID `json:"location_ids"`
	ClassificationIDs []uuid.UUID `json:"classification_ids"`
	RoleIDs           []uuid.UUID `json:"role_ids"`
	IsActive          *bool       `json:"is_active"`
	SortOrder         *int        `json:"sort_order"`
}

// DepartmentListFilter holds query filters for the paginated department listing.
type DepartmentListFilter struct {
	Search         string     `query:"search"         json:"search"         validate:"omitempty,max=100"`
	Code           string     `query:"code"           json:"code"           validate:"omitempty,max=50"`
	Location       *uuid.UUID `query:"location"       json:"location"       validate:"omitempty,uuid4"`
	Classification *uuid.UUID `query:"classification" json:"classification" validate:"omitempty,uuid4"`
	Page           int        `query:"page"              json:"page"              validate:"omitempty,gte=1"`
	Limit          int        `query:"limit"             json:"limit"             validate:"omitempty,gte=1,lte=100"`
}

// DepartmentResponse for API responses
type DepartmentResponse struct {
	ID              uuid.UUID                `json:"id"`
	Name            string                   `json:"name"`
	NameAr          string                   `json:"name_ar"`
	Code            string                   `json:"code"`
	Description     string                   `json:"description"`
	DescriptionAr   string                   `json:"description_ar"`
	Type            string                   `json:"type"`
	ParentID        *uuid.UUID               `json:"parent_id"`
	Level           int                      `json:"level"`
	Path            string                   `json:"path"`
	ManagerID       *uuid.UUID               `json:"manager_id"`
	SupervisorID    *uuid.UUID               `json:"supervisor_id"`
	IsActive        bool                     `json:"is_active"`
	SortOrder       int                      `json:"sort_order"`
	Children        []DepartmentResponse     `json:"children,omitempty"`
	Locations       []LocationResponse       `json:"locations,omitempty"`
	Classifications []ClassificationResponse `json:"classifications,omitempty"`
	Roles           []RoleResponse           `json:"roles,omitempty"`
	CreatedAt       time.Time                `json:"created_at"`
}

// DepartmentMatchRequest for finding departments that match given criteria
type DepartmentMatchRequest struct {
	ClassificationID *string `json:"classification_id" validate:"omitempty,uuid"`
	LocationID       *string `json:"location_id" validate:"omitempty,uuid"`
	DepartmentType   *string `json:"department_type" validate:"omitempty,oneof=internal external"`
	// IncidentID is optional. When provided and the incident is MOMRA-sourced
	// (Source == "MOMRA"), external-type matches are resolved from that incident's own
	// AvailableEEList (the exact entities MOMRA declared eligible for it) rather than
	// purely from classification/location linkage — see MatchDepartment's doc comment.
	IncidentID *string `json:"incident_id" validate:"omitempty,uuid"`
}

// DepartmentMatchResponse for returning matched departments
type DepartmentMatchResponse struct {
	Departments         []DepartmentResponse `json:"departments"`
	SingleMatch         bool                 `json:"single_match"`
	MatchedDepartmentID *string              `json:"matched_department_id,omitempty"`
}

func ToDepartmentResponse(d *Department) DepartmentResponse {
	dType := d.Type
	if dType == "" {
		dType = "internal"
	}
	resp := DepartmentResponse{
		ID:            d.ID,
		Name:          d.Name,
		NameAr:        d.NameAr,
		Code:          d.Code,
		Description:   d.Description,
		DescriptionAr: d.DescriptionAr,
		Type:          dType,
		ParentID:      d.ParentID,
		Level:         d.Level,
		Path:          d.Path,
		ManagerID:     d.ManagerID,
		SupervisorID:  d.SupervisorID,
		IsActive:      d.IsActive,
		SortOrder:     d.SortOrder,
		CreatedAt:     d.CreatedAt,
	}

	if len(d.Children) > 0 {
		resp.Children = make([]DepartmentResponse, len(d.Children))
		for i, child := range d.Children {
			resp.Children[i] = ToDepartmentResponse(&child)
		}
	}

	if len(d.Locations) > 0 {
		resp.Locations = make([]LocationResponse, len(d.Locations))
		for i, loc := range d.Locations {
			resp.Locations[i] = ToLocationResponse(&loc)
		}
	}

	if len(d.Classifications) > 0 {
		resp.Classifications = make([]ClassificationResponse, len(d.Classifications))
		for i, cls := range d.Classifications {
			resp.Classifications[i] = ToClassificationResponse(&cls)
		}
	}

	if len(d.Roles) > 0 {
		resp.Roles = make([]RoleResponse, len(d.Roles))
		for i, role := range d.Roles {
			resp.Roles[i] = ToRoleResponse(&role)
		}
	}

	return resp
}
