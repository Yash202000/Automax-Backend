package services

import (
	"context"
	"fmt"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ════════════════════════════════════════════════════
// DocumentScope
// ════════════════════════════════════════════════════

// DocumentScope represents the caller's access level for document operations.
type DocumentScope struct {
	// IsUnrestricted is true for super admin OR anyone holding goals:delete / goals:approve.
	// Unrestricted callers see the whole workspace without filtering.
	IsUnrestricted bool

	// UserID of the current caller.
	UserID uuid.UUID

	// AccessibleGoalFolderIDs maps a goals.documenta_folder_id → goal.id for every goal
	// the user may access (owned or collaborator). Only populated for scoped callers.
	AccessibleGoalFolderIDs map[string]uuid.UUID

	// GoalManagementFolderID is the DMS UUID of the "Goal Management" master folder
	// at the workspace root. Resolved lazily when ResolveDocumentScope is called with
	// a scoped caller.
	GoalManagementFolderID string
}

// ════════════════════════════════════════════════════
// DocumentAuthzService
// ════════════════════════════════════════════════════

// DocumentAuthzService authorizes fine-grained access to Documenta DMS nodes based on
// which goals a user owns or collaborates on. Permission-level gating (goals:view,
// goals:update, …) continues to be enforced by the Fiber middleware — this layer
// adds per-goal scoping on top of those coarse-grained checks.
type DocumentAuthzService interface {
	ResolveDocumentScope(ctx context.Context, user *models.User) (*DocumentScope, error)
	CheckFolderAccess(ctx context.Context, scope *DocumentScope, dmsNodeID string, userEmail string) (bool, error)
	FindGoalForDMSNode(ctx context.Context, dmsNodeID string, userEmail string) (*models.Goal, error)
	GoalManagementFolderID(ctx context.Context, userEmail string) (string, error)
}

type documentAuthzService struct {
	db              *gorm.DB
	documentaClient storage.DocumentaClient
	workspaceName   string

	// Cached Goal Management folder UUID (lazily resolved, process-lifetime).
	goalMgmtFolderID string
}

// NewDocumentAuthzService constructs a DocumentAuthzService.
func NewDocumentAuthzService(db *gorm.DB, documentaClient storage.DocumentaClient, cfg config.DocumentaConfig) DocumentAuthzService {
	return &documentAuthzService{
		db:              db,
		documentaClient: documentaClient,
		workspaceName:   cfg.WorkspaceName,
	}
}

// ResolveDocumentScope builds the caller's DocumentScope from the authenticated user.
// For unrestricted callers (super admin, goals:delete, goals:approve), the goal-folder
// map is left empty and IsUnrestricted is true.
// For scoped callers, preloads the DMS folder UUIDs of every goal they own or
// collaborate on.
func (s *documentAuthzService) ResolveDocumentScope(ctx context.Context, user *models.User) (*DocumentScope, error) {
	if user == nil {
		return nil, fmt.Errorf("documentauthz: nil user")
	}

	scope := &DocumentScope{
		UserID: user.ID,
	}

	if user.IsSuperAdmin || user.HasPermission("goals:delete") || user.HasPermission("goals:approve") {
		scope.IsUnrestricted = true
		return scope, nil
	}

	// Scoped caller — fetch every goal the user owns or is a collaborator on,
	// keyed by DMS folder UUID for quick lookup.
	type row struct {
		ID                uuid.UUID
		DocumentaFolderID string
	}
	var rows []row
	err := s.db.WithContext(ctx).
		Model(&models.Goal{}).
		Select("id, documenta_folder_id").
		Where(
			"documenta_folder_id <> '' AND (owner_id = ? OR id IN (SELECT goal_id FROM goal_collaborators WHERE user_id = ?))",
			user.ID, user.ID,
		).
		Find(&rows).Error
	if err != nil {
		return nil, fmt.Errorf("documentauthz: load accessible goals: %w", err)
	}

	scope.AccessibleGoalFolderIDs = make(map[string]uuid.UUID, len(rows))
	for _, r := range rows {
		scope.AccessibleGoalFolderIDs[r.DocumentaFolderID] = r.ID
	}

	// Resolve the Goal Management master folder UUID once per request. Needed so
	// handlers can detect requests for that folder (for synthetic listing) and so
	// CheckFolderAccess can short-circuit access to the master folder itself.
	gmID, err := s.GoalManagementFolderID(ctx, emailFromUser(user))
	if err == nil {
		scope.GoalManagementFolderID = gmID
	}

	return scope, nil
}

// GoalManagementFolderID returns the DMS UUID of the "Goal Management" root folder,
// caching the result in memory for the lifetime of the process (matches the cache
// behaviour of DocumentaClient.EnsureFolder).
func (s *documentAuthzService) GoalManagementFolderID(ctx context.Context, userEmail string) (string, error) {
	if s.goalMgmtFolderID != "" {
		return s.goalMgmtFolderID, nil
	}
	id, err := s.documentaClient.EnsureFolder(storage.ContextWithOnBehalf(ctx, userEmail), s.workspaceName, "", "Goal Management")
	if err != nil {
		return "", fmt.Errorf("documentauthz: resolve Goal Management folder: %w", err)
	}
	s.goalMgmtFolderID = id
	return id, nil
}

// FindGoalForDMSNode walks the ancestor chain of a DMS folder/file UUID until it
// finds a node whose UUID matches a goals.documenta_folder_id. Returns the owning
// goal or (nil, nil) if the node is outside any goal's tree.
//
// Uses DocumentaClient.GetFileBreadcrumb — one network call per resolution.
func (s *documentAuthzService) FindGoalForDMSNode(ctx context.Context, dmsNodeID string, userEmail string) (*models.Goal, error) {
	if dmsNodeID == "" {
		return nil, nil
	}

	// First, see if the node itself is a goal folder (avoids the breadcrumb roundtrip
	// when the caller hands us a goal folder UUID directly).
	var goal models.Goal
	err := s.db.WithContext(ctx).
		Where("documenta_folder_id = ?", dmsNodeID).
		First(&goal).Error
	if err == nil {
		return &goal, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, fmt.Errorf("documentauthz: lookup goal by folder: %w", err)
	}

	// Fall back to walking the ancestor chain via MyDocs. Each entry is an ancestor
	// ordered root → parent; any of them might be a goal folder.
	ctxWithUser := storage.ContextWithOnBehalf(ctx, userEmail)
	chain, err := s.documentaClient.GetFileBreadcrumb(ctxWithUser, dmsNodeID)
	if err != nil {
		return nil, fmt.Errorf("documentauthz: breadcrumb for %s: %w", dmsNodeID, err)
	}
	if len(chain) == 0 {
		return nil, nil
	}

	// Collect ancestor UUIDs and look up in one DB roundtrip.
	ancestorIDs := make([]string, 0, len(chain))
	for _, entry := range chain {
		ancestorIDs = append(ancestorIDs, entry.UUID)
	}
	var found models.Goal
	err = s.db.WithContext(ctx).
		Where("documenta_folder_id IN ?", ancestorIDs).
		First(&found).Error
	if err == nil {
		return &found, nil
	}
	if err == gorm.ErrRecordNotFound {
		return nil, nil
	}
	return nil, fmt.Errorf("documentauthz: lookup goal by ancestor chain: %w", err)
}

// CheckFolderAccess authorizes a specific DMS node id for a scope. Returns
// (allowed, error).
//
// Rules:
//  1. Unrestricted scope → true.
//  2. Empty ID or the Goal Management master folder UUID → true (viewer will see
//     a filtered child list anyway).
//  3. Direct goal folder hit → check AccessibleGoalFolderIDs.
//  4. Otherwise walk the ancestor chain to find the owning goal; same check.
//  5. No owning goal found → deny.
func (s *documentAuthzService) CheckFolderAccess(ctx context.Context, scope *DocumentScope, dmsNodeID string, userEmail string) (bool, error) {
	if scope == nil {
		return false, fmt.Errorf("documentauthz: nil scope")
	}
	if scope.IsUnrestricted {
		return true, nil
	}
	if dmsNodeID == "" {
		return true, nil
	}
	if scope.GoalManagementFolderID != "" && dmsNodeID == scope.GoalManagementFolderID {
		return true, nil
	}

	// Direct hit — node IS a goal folder?
	if _, ok := scope.AccessibleGoalFolderIDs[dmsNodeID]; ok {
		return true, nil
	}

	goal, err := s.FindGoalForDMSNode(ctx, dmsNodeID, userEmail)
	if err != nil {
		return false, err
	}
	if goal == nil {
		return false, nil
	}
	_, ok := scope.AccessibleGoalFolderIDs[goal.DocumentaFolderID]
	return ok, nil
}

// emailFromUser extracts the best email to pass as X-On-Behalf-Of. Falls back to
// username if the email is somehow blank (rare — Email is a NOT NULL uniqueIndex).
func emailFromUser(u *models.User) string {
	if u == nil {
		return ""
	}
	if u.Email != "" {
		return u.Email
	}
	return u.Username
}
