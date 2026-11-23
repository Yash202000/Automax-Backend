package services

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// KpiAccessChecker answers "may this user perform action on that KPI" for
// many KPIs in a row (bulk imports), loading the user's Super Admin flag once
// and each KPI's collaborator type at most once.
type KpiAccessChecker struct {
	ctx        context.Context
	db         *gorm.DB
	userID     uuid.UUID
	superAdmin bool
	collab     map[string]string // type|id -> collaborator type ("" = none)
}

// NewKpiAccessChecker builds a checker for userID. The Super Admin may do
// everything; everyone else needs an active collaborator assignment whose
// type grants the action (see kpiActionsByCollaboratorType).
func NewKpiAccessChecker(ctx context.Context, db *gorm.DB, userID uuid.UUID) (*KpiAccessChecker, error) {
	svc := &KpiAccessService{db: db}
	superAdmin, err := svc.isSuperAdmin(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &KpiAccessChecker{ctx: ctx, db: db, userID: userID, superAdmin: superAdmin, collab: map[string]string{}}, nil
}

// IsSuperAdmin reports whether the checker's user bypasses per-KPI access.
func (c *KpiAccessChecker) IsSuperAdmin() bool { return c.superAdmin }

// Can reports whether the user may perform action on the KPI (kpiType, kpiID).
func (c *KpiAccessChecker) Can(kpiType string, kpiID uuid.UUID, action KpiAction) (bool, error) {
	if c.superAdmin {
		return true, nil
	}
	key := kpiType + "|" + kpiID.String()
	collaboratorType, ok := c.collab[key]
	if !ok {
		var err error
		collaboratorType, err = (&KpiAccessService{db: c.db}).CollaboratorType(c.ctx, c.userID, kpiType, kpiID)
		if err != nil {
			return false, err
		}
		c.collab[key] = collaboratorType
	}
	return collaboratorTypeAllows(collaboratorType, action), nil
}

// CanByCode is Can for a KPI referenced by type + code (matched
// case-insensitively, as the imports do). It returns ErrKpiNotFound when no
// live KPI of that type has the code.
func (c *KpiAccessChecker) CanByCode(kpiType, code string, action KpiAction) (bool, error) {
	ref, err := kpiByCodeFold(c.db.WithContext(c.ctx), kpiType, code)
	if err != nil {
		return false, err
	}
	return c.Can(ref.KpiType, ref.KpiID, action)
}

// CanOnKpiByCode is the one-off form of KpiAccessChecker.CanByCode: may
// userID perform action on the KPI of kpiType whose code is code
// (case-insensitive)? Super Admin always may. Returns ErrKpiNotFound when the
// KPI does not exist. Use NewKpiAccessChecker when checking many rows.
func CanOnKpiByCode(ctx context.Context, db *gorm.DB, userID uuid.UUID, kpiType, code string, action KpiAction) (bool, error) {
	checker, err := NewKpiAccessChecker(ctx, db, userID)
	if err != nil {
		return false, err
	}
	return checker.CanByCode(kpiType, code, action)
}

func kpiByCodeFold(db *gorm.DB, kpiType, code string) (KpiRef, error) {
	if !isKpiType(kpiType) || strings.TrimSpace(code) == "" {
		return KpiRef{}, ErrKpiNotFound
	}
	var row struct{ ID uuid.UUID }
	if err := db.Table(kpiCodeUnionSQL+" k").Select("k.id").
		Where("k.kpi_type = ? AND upper(trim(k.code)) = upper(trim(?))", kpiType, code).
		Limit(1).Scan(&row).Error; err != nil {
		return KpiRef{}, err
	}
	if row.ID == uuid.Nil {
		return KpiRef{}, ErrKpiNotFound
	}
	return KpiRef{KpiType: kpiType, KpiID: row.ID}, nil
}

// IsKpiNotFound reports whether err means the KPI does not exist.
func IsKpiNotFound(err error) bool { return errors.Is(err, ErrKpiNotFound) }
