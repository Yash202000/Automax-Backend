package repository

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type RoleRepository interface {
	Create(ctx context.Context, role *models.Role) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.Role, error)
	FindByIDs(ctx context.Context, ids []uuid.UUID) ([]models.Role, error)
	FindByCode(ctx context.Context, code string) (*models.Role, error)
	FindByName(ctx context.Context, name string) (*models.Role, error)
	Update(ctx context.Context, role *models.Role) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context) ([]models.Role, error)
	ListByDepartment(ctx context.Context, departmentID uuid.UUID) ([]models.Role, error)
	AssignPermissions(ctx context.Context, roleID uuid.UUID, permissionIDs []uuid.UUID) error
	GetPermissions(ctx context.Context, roleID uuid.UUID) ([]models.Permission, error)
}

// roleCodePrefix is the fixed prefix for the auto-generated Role Code (e.g. role-000001).
const roleCodePrefix = "role-"

// Role Code allocation. Mirrors department's orgCodeSeq: an in-process counter,
// seeded once from the current DB maximum and incremented under a mutex on every create.
var (
	roleCodeMu     sync.Mutex
	roleCodeSeq    int64
	roleCodeLoaded bool
)

type roleRepository struct {
	db *gorm.DB
}

func NewRoleRepository(db *gorm.DB) RoleRepository {
	return &roleRepository{db: db}
}

func (r *roleRepository) Create(ctx context.Context, role *models.Role) error {
	role.Code = strings.ToLower(strings.TrimSpace(role.Code))
	if role.Code == "" {
		code, err := r.nextRoleCode(ctx)
		if err != nil {
			return err
		}
		role.Code = code
	}
	return r.db.WithContext(ctx).Create(role).Error
}

// nextRoleCode returns the next unique Role Code (e.g. role-000001). See
// departmentRepository.nextOrgCode for the seeding/locking rationale.
func (r *roleRepository) nextRoleCode(ctx context.Context) (string, error) {
	roleCodeMu.Lock()
	defer roleCodeMu.Unlock()

	if !roleCodeLoaded {
		var maxCode *string
		if err := r.db.WithContext(ctx).Unscoped().Model(&models.Role{}).
			Select("MAX(code)").
			Where("code LIKE ?", roleCodePrefix+"%").
			Scan(&maxCode).Error; err != nil {
			return "", err
		}
		if maxCode != nil && *maxCode != "" {
			var n int64
			if _, err := fmt.Sscanf(*maxCode, roleCodePrefix+"%d", &n); err == nil {
				roleCodeSeq = n
			}
		}
		roleCodeLoaded = true
	}

	roleCodeSeq++
	return fmt.Sprintf("%s%06d", roleCodePrefix, roleCodeSeq), nil
}

func (r *roleRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.Role, error) {
	var role models.Role
	err := r.db.WithContext(ctx).Preload("Permissions").First(&role, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &role, nil
}

func (r *roleRepository) FindByCode(ctx context.Context, code string) (*models.Role, error) {
	var role models.Role
	err := r.db.WithContext(ctx).Preload("Permissions").First(&role, "LOWER(code) = LOWER(?)", code).Error
	if err != nil {
		return nil, err
	}
	return &role, nil
}

func (r *roleRepository) FindByIDs(ctx context.Context, ids []uuid.UUID) ([]models.Role, error) {
	var roles []models.Role
	err := r.db.WithContext(ctx).Preload("Permissions").Where("id IN ?", ids).Find(&roles).Error
	return roles, err
}

func (r *roleRepository) FindByName(ctx context.Context, name string) (*models.Role, error) {
	var role models.Role
	err := r.db.WithContext(ctx).First(&role, "LOWER(name) = LOWER(?)", name).Error
	if err != nil {
		return nil, err
	}
	return &role, nil
}

func (r *roleRepository) Update(ctx context.Context, role *models.Role) error {
	role.Code = strings.ToLower(strings.TrimSpace(role.Code))
	return r.db.WithContext(ctx).Save(role).Error
}

func (r *roleRepository) Delete(ctx context.Context, id uuid.UUID) error {
	var role models.Role
	if err := r.db.WithContext(ctx).First(&role, "id = ?", id).Error; err != nil {
		return err
	}
	if role.IsSystem {
		return gorm.ErrRecordNotFound // Cannot delete system roles
	}
	return r.db.WithContext(ctx).Delete(&models.Role{}, "id = ?", id).Error
}

func (r *roleRepository) List(ctx context.Context) ([]models.Role, error) {
	var roles []models.Role
	err := r.db.WithContext(ctx).Preload("Permissions").Order("name").Find(&roles).Error
	return roles, err
}

// ListByDepartment returns the roles assignable to users in a department. Roles are not
// owned by a department, so this returns the full role list rather than filtering by which
// roles are already held by existing users there — the previous "roles already in use in
// this department" filter meant a department with no (or newly added) users produced an
// empty list, breaking the role dropdown on the create/edit-user form.
func (r *roleRepository) ListByDepartment(ctx context.Context, departmentID uuid.UUID) ([]models.Role, error) {
	return r.List(ctx)
}

func (r *roleRepository) AssignPermissions(ctx context.Context, roleID uuid.UUID, permissionIDs []uuid.UUID) error {
	var role models.Role
	if err := r.db.WithContext(ctx).First(&role, "id = ?", roleID).Error; err != nil {
		return err
	}

	var permissions []models.Permission
	if len(permissionIDs) > 0 {
		if err := r.db.WithContext(ctx).Where("id IN ?", permissionIDs).Find(&permissions).Error; err != nil {
			return err
		}
	}

	return r.db.WithContext(ctx).Model(&role).Association("Permissions").Replace(permissions)
}

func (r *roleRepository) GetPermissions(ctx context.Context, roleID uuid.UUID) ([]models.Permission, error) {
	var role models.Role
	if err := r.db.WithContext(ctx).Preload("Permissions").First(&role, "id = ?", roleID).Error; err != nil {
		return nil, err
	}
	return role.Permissions, nil
}

type PermissionRepository interface {
	Create(ctx context.Context, permission *models.Permission) error
	FindByID(ctx context.Context, id uuid.UUID) (*models.Permission, error)
	FindByCode(ctx context.Context, code string) (*models.Permission, error)
	Update(ctx context.Context, permission *models.Permission) error
	Delete(ctx context.Context, id uuid.UUID) error
	List(ctx context.Context) ([]models.Permission, error)
	ListByModule(ctx context.Context, module string) ([]models.Permission, error)
	GetModules(ctx context.Context, arabic bool) ([]string, error)
}

type permissionRepository struct {
	db *gorm.DB
}

func NewPermissionRepository(db *gorm.DB) PermissionRepository {
	return &permissionRepository{db: db}
}

func (r *permissionRepository) Create(ctx context.Context, permission *models.Permission) error {
	return r.db.WithContext(ctx).Create(permission).Error
}

func (r *permissionRepository) FindByID(ctx context.Context, id uuid.UUID) (*models.Permission, error) {
	var permission models.Permission
	err := r.db.WithContext(ctx).First(&permission, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &permission, nil
}

func (r *permissionRepository) FindByCode(ctx context.Context, code string) (*models.Permission, error) {
	var permission models.Permission
	err := r.db.WithContext(ctx).First(&permission, "LOWER(code) = LOWER(?)", code).Error
	if err != nil {
		return nil, err
	}
	return &permission, nil
}

func (r *permissionRepository) Update(ctx context.Context, permission *models.Permission) error {
	return r.db.WithContext(ctx).Save(permission).Error
}

func (r *permissionRepository) Delete(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.Permission{}, "id = ?", id).Error
}

func (r *permissionRepository) List(ctx context.Context) ([]models.Permission, error) {
	var permissions []models.Permission
	err := r.db.WithContext(ctx).Order("module, name").Find(&permissions).Error
	return permissions, err
}

func (r *permissionRepository) ListByModule(ctx context.Context, module string) ([]models.Permission, error) {
	var permissions []models.Permission
	err := r.db.WithContext(ctx).Where("module = ? OR module_ar = ?", module, module).Order("name").Find(&permissions).Error
	return permissions, err
}

func (r *permissionRepository) GetModules(ctx context.Context, isArabic bool) ([]string, error) {
	var modules []string
	if isArabic {
		err := r.db.WithContext(ctx).Model(&models.Permission{}).Distinct("module_ar").Pluck("module_ar", &modules).Error
		return modules, err
	}
	err := r.db.WithContext(ctx).Model(&models.Permission{}).Distinct("module").Pluck("module", &modules).Error
	return modules, err
}
