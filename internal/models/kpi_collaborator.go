package models

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	CollaboratorTypeOwner           = "KPI Owner"
	CollaboratorTypeDataContributor = "Data Contributor"
	CollaboratorTypeDataSubmitter   = "Data Submitter"
	CollaboratorTypeReviewer        = "Reviewer"
	CollaboratorTypeApprover        = "Approver"
	CollaboratorTypeViewer          = "Viewer"
)

var ValidCollaboratorTypes = []string{
	CollaboratorTypeOwner,
	CollaboratorTypeDataContributor,
	CollaboratorTypeDataSubmitter,
	CollaboratorTypeReviewer,
	CollaboratorTypeApprover,
	CollaboratorTypeViewer,
}

const (
	UserCategoryInternalEmployee      = "Internal Employee"
	UserCategoryExternalConsultant    = "External Consultant"
	UserCategoryContractor            = "Contractor"
	UserCategoryServiceProvider       = "Service Provider"
	UserCategorySystemIntegrationAcct = "System / Integration Account"
)

var ValidUserCategories = []string{
	UserCategoryInternalEmployee,
	UserCategoryExternalConsultant,
	UserCategoryContractor,
	UserCategoryServiceProvider,
	UserCategorySystemIntegrationAcct,
}

const (
	PeriodScopeAllPeriods     = "All Periods"
	PeriodScopeCurrentPeriod  = "Current Period"
	PeriodScopeSpecificYear   = "Specific Year"
	PeriodScopeSpecificPeriod = "Specific Periods"
)

var ValidPeriodScopes = []string{
	PeriodScopeAllPeriods,
	PeriodScopeCurrentPeriod,
	PeriodScopeSpecificYear,
	PeriodScopeSpecificPeriod,
}

const (
	NotificationAssignment   = "Assignment"
	NotificationPeriodOpen   = "Period Open"
	NotificationReminder     = "Reminder"
	NotificationSubmitted    = "Submitted"
	NotificationReturned     = "Returned"
	NotificationApproved     = "Approved"
	NotificationRejected     = "Rejected"
	NotificationLocked       = "Locked"
)

var ValidNotificationPreferences = []string{
	NotificationAssignment,
	NotificationPeriodOpen,
	NotificationReminder,
	NotificationSubmitted,
	NotificationReturned,
	NotificationApproved,
	NotificationRejected,
	NotificationLocked,
}

type KpiCollaboratorAssignment struct {
	ID                   uuid.UUID  `gorm:"type:uuid;primary_key" json:"id"`
	KpiID                uuid.UUID  `gorm:"type:uuid;index;not null;uniqueIndex:idx_kpi_collab_assign_user" json:"kpi_id"`
	KpiType              string     `gorm:"size:20;index;not null" json:"kpi_type"`
	UserID               uuid.UUID  `gorm:"type:uuid;index;not null;uniqueIndex:idx_kpi_collab_assign_user" json:"user_id"`
	User                 *User      `gorm:"foreignKey:UserID" json:"user,omitempty"`
	UserCategory         string     `gorm:"size:50;not null" json:"user_category"`
	CollaboratorType     string     `gorm:"size:50;not null" json:"collaborator_type"`
	OrganizationScope    []string   `gorm:"type:json;serializer:json" json:"organization_scope"`
	MetricScope          string     `gorm:"size:20;not null;default:'All Metrics'" json:"metric_scope"`
	MetricScopeIDs       []string   `gorm:"type:json;serializer:json" json:"metric_scope_ids"`
	PeriodScope          string     `gorm:"size:30;not null;default:'All Periods'" json:"period_scope"`
	PeriodScopeYear      int        `gorm:"default:0" json:"period_scope_year"`
	PeriodScopePeriods   []string   `gorm:"type:json;serializer:json" json:"period_scope_periods"`
	EffectiveFrom        time.Time  `gorm:"not null" json:"effective_from"`
	EffectiveTo          *time.Time `json:"effective_to"`
	IsActive             bool       `gorm:"not null;default:true" json:"is_active"`
	DelegateForUserID    *uuid.UUID `gorm:"type:uuid;index" json:"delegate_for_user_id"`
	DelegateForUser      *User      `gorm:"foreignKey:DelegateForUserID" json:"delegate_for_user,omitempty"`
	DelegationReason     string     `gorm:"type:text" json:"delegation_reason"`
	NotificationPrefs    []string   `gorm:"type:json;serializer:json" json:"notification_prefs"`
	CreatedByID          uuid.UUID  `gorm:"type:uuid;not null" json:"created_by_id"`
	CreatedBy            *User      `gorm:"foreignKey:CreatedByID" json:"created_by,omitempty"`
	UpdatedByID          uuid.UUID  `gorm:"type:uuid;not null" json:"updated_by_id"`
	UpdatedBy            *User      `gorm:"foreignKey:UpdatedByID" json:"updated_by,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
	DeletedAt            gorm.DeletedAt `gorm:"index" json:"-"`
}

func (a *KpiCollaboratorAssignment) BeforeCreate(tx *gorm.DB) error {
	if a.ID == uuid.Nil {
		a.ID = uuid.New()
	}
	return nil
}

type KpiCollaboratorAssignmentRequest struct {
	UserID             uuid.UUID  `json:"user_id" validate:"required"`
	UserCategory       string     `json:"user_category" validate:"required,oneof='Internal Employee' 'External Consultant' Contractor 'Service Provider' 'System / Integration Account'"`
	CollaboratorType   string     `json:"collaborator_type" validate:"required,oneof='KPI Owner' 'Data Contributor' 'Data Submitter' Reviewer Approver Viewer"`
	OrganizationScope  []string   `json:"organization_scope"`
	MetricScope        string     `json:"metric_scope"`
	MetricScopeIDs     []string   `json:"metric_scope_ids"`
	PeriodScope        string     `json:"period_scope"`
	PeriodScopeYear    int        `json:"period_scope_year"`
	PeriodScopePeriods []string   `json:"period_scope_periods"`
	EffectiveFrom      string     `json:"effective_from" validate:"required"`
	EffectiveTo        string     `json:"effective_to"`
	IsActive           *bool      `json:"is_active"`
	DelegateForUserID  *uuid.UUID `json:"delegate_for_user_id"`
	DelegationReason   string     `json:"delegation_reason"`
	NotificationPrefs  []string   `json:"notification_prefs"`
}

type KpiCollaboratorAssignmentResponse struct {
	ID                 uuid.UUID  `json:"id"`
	KpiID              uuid.UUID  `json:"kpi_id"`
	KpiType            string     `json:"kpi_type"`
	UserID             uuid.UUID  `json:"user_id"`
	User               *UserBrief `json:"user,omitempty"`
	UserCategory       string     `json:"user_category"`
	CollaboratorType   string     `json:"collaborator_type"`
	OrganizationScope  []string   `json:"organization_scope"`
	MetricScope        string     `json:"metric_scope"`
	MetricScopeIDs     []string   `json:"metric_scope_ids"`
	PeriodScope        string     `json:"period_scope"`
	PeriodScopeYear    int        `json:"period_scope_year"`
	PeriodScopePeriods []string   `json:"period_scope_periods"`
	EffectiveFrom      *time.Time `json:"effective_from"`
	EffectiveTo        *time.Time `json:"effective_to,omitempty"`
	IsActive           bool       `json:"is_active"`
	DelegateForUserID  *uuid.UUID `json:"delegate_for_user_id,omitempty"`
	DelegateForUser    *UserBrief `json:"delegate_for_user,omitempty"`
	DelegationReason   string     `json:"delegation_reason,omitempty"`
	NotificationPrefs  []string   `json:"notification_prefs"`
	CreatedByID        uuid.UUID  `json:"created_by_id"`
	CreatedBy          *UserBrief `json:"created_by,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type UserBrief struct {
	ID        uuid.UUID `json:"id"`
	FirstName string    `json:"first_name"`
	LastName  string    `json:"last_name"`
	Email     string    `json:"email"`
	IsActive  bool      `json:"is_active"`
}

type CollaboratorPermissionMatrix struct {
	CollaboratorType string `json:"collaborator_type"`
	ViewKPI          bool   `json:"view_kpi"`
	ViewEntries      bool   `json:"view_entries"`
	CreateDraft      bool   `json:"create_draft"`
	EditOwnDraft     bool   `json:"edit_own_draft"`
	EditOthersDraft  string `json:"edit_others_draft"`
	SubmitEntry      bool   `json:"submit_entry"`
	Review           string `json:"review"`
	Return           bool   `json:"return"`
	ApproveReject    string `json:"approve_reject"`
	ManageTargets    string `json:"manage_targets"`
	ManageCollabs    bool   `json:"manage_collaborators"`
	ScopeRule        string `json:"scope_rule"`
	CriticalConstraint string `json:"critical_constraint"`
}
