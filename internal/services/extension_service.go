package services

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	urlpkg "net/url"
	"strings"
	"time"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/database"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/constants"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const (
	extensionPoolCacheKey = "PBX_EXTENSIONS_POOL"
	extensionPoolCacheTTL = 5 * time.Minute
	extensionModule       = "extensions"
)

var (
	ErrExtensionNotInPool   = errors.New("extension does not exist in the PBX pool")
	ErrExtensionNotAssigned = errors.New("extension is not currently assigned to anyone")
)

// ExtensionService manages PBX extension assignment. The current assignment and its
// full history live in dedicated tables (extension_assignments / _history) — fully
// decoupled from the users table. Every change is action-logged and broadcast to
// agents/admins.
type ExtensionService interface {
	ListExtensions(ctx context.Context, statusFilter string) ([]models.ExtensionStatus, error)
	MyExtension(ctx context.Context, userID uuid.UUID) (*models.ExtensionStatus, error)
	AssignExtension(ctx context.Context, req models.ExtensionAssignRequest, actorID uuid.UUID) (*models.ExtensionAssignmentResponse, error)
	ReleaseExtension(ctx context.Context, extension string, actorID uuid.UUID) error
	CreateExtension(ctx context.Context, req models.ExtensionCreateRequest, actorID uuid.UUID) error
	GetHistory(ctx context.Context, extension string, limit int) ([]models.ExtensionAssignmentResponse, error)
}

type extensionService struct {
	db           *gorm.DB
	extRepo      repository.ExtensionAssignmentRepository
	userRepo     repository.UserRepository
	roleRepo     repository.RoleRepository
	actionLog    ActionLogService
	notifService *NotificationService
	wsHub        *WSHub
	sessionStore *database.SessionStore
	cfg          *config.Config
}

func NewExtensionService(
	db *gorm.DB,
	extRepo repository.ExtensionAssignmentRepository,
	userRepo repository.UserRepository,
	roleRepo repository.RoleRepository,
	actionLog ActionLogService,
	notifService *NotificationService,
	wsHub *WSHub,
	sessionStore *database.SessionStore,
	cfg *config.Config,
) ExtensionService {
	return &extensionService{
		db:           db,
		extRepo:      extRepo,
		userRepo:     userRepo,
		roleRepo:     roleRepo,
		actionLog:    actionLog,
		notifService: notifService,
		wsHub:        wsHub,
		sessionStore: sessionStore,
		cfg:          cfg,
	}
}

// pbxExtension is one entry from the PBX list endpoint. The "password" field the
// PBX returns is intentionally NOT mapped, so it is never cached or exposed.
type pbxExtension struct {
	Extension  string `json:"extension"`
	CallerName string `json:"caller_name"`
	CallGroup  string `json:"callgroup"`
}

// --- Reads ---

func (s *extensionService) ListExtensions(ctx context.Context, statusFilter string) ([]models.ExtensionStatus, error) {
	pool, err := s.fetchPool(ctx)
	if err != nil {
		return nil, err
	}

	active, err := s.extRepo.ListActive(ctx)
	if err != nil {
		return nil, err
	}
	byExt := make(map[string]*models.User, len(active))
	for i := range active {
		byExt[active[i].Extension] = active[i].User
	}

	statusFilter = strings.ToLower(strings.TrimSpace(statusFilter))
	result := make([]models.ExtensionStatus, 0, len(pool))
	for _, p := range pool {
		entry := models.ExtensionStatus{
			Extension:  p.Extension,
			CallerName: p.CallerName,
			CallGroup:  p.CallGroup,
			Status:     "available",
		}
		if holder, ok := byExt[p.Extension]; ok {
			entry.Status = "assigned"
			entry.AssignedTo = models.NewExtensionUserSummary(holder)
		}
		if statusFilter != "" && statusFilter != entry.Status {
			continue
		}
		result = append(result, entry)
	}
	return result, nil
}

func (s *extensionService) MyExtension(ctx context.Context, userID uuid.UUID) (*models.ExtensionStatus, error) {
	cur, err := s.extRepo.GetByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if cur == nil {
		return nil, nil
	}
	return &models.ExtensionStatus{
		Extension:  cur.Extension,
		Status:     "assigned",
		AssignedTo: models.NewExtensionUserSummary(cur.User),
	}, nil
}

func (s *extensionService) GetHistory(ctx context.Context, extension string, limit int) ([]models.ExtensionAssignmentResponse, error) {
	rows, err := s.extRepo.HistoryByExtension(ctx, strings.TrimSpace(extension), limit)
	if err != nil {
		return nil, err
	}
	out := make([]models.ExtensionAssignmentResponse, 0, len(rows))
	for i := range rows {
		out = append(out, models.ToExtensionAssignmentResponse(&rows[i]))
	}
	return out, nil
}

// --- Writes ---

func (s *extensionService) AssignExtension(ctx context.Context, req models.ExtensionAssignRequest, actorID uuid.UUID) (*models.ExtensionAssignmentResponse, error) {
	ext := strings.TrimSpace(req.Extension)
	if ext == "" {
		return nil, ErrExtensionNotInPool
	}

	inPool, err := s.extensionInPool(ctx, ext)
	if err != nil {
		return nil, err
	}
	if !inPool {
		return nil, ErrExtensionNotInPool
	}

	targetID := actorID
	if req.UserID != nil && *req.UserID != uuid.Nil {
		targetID = *req.UserID
	}
	target, err := s.userRepo.FindByID(ctx, targetID)
	if err != nil {
		return nil, fmt.Errorf("target user not found: %w", err)
	}

	prevAssign, err := s.extRepo.GetByExtension(ctx, ext)
	if err != nil {
		return nil, err
	}
	var prevHolder *models.User
	if prevAssign != nil {
		prevHolder = prevAssign.User
	}

	// Already assigned to the target — no-op.
	if prevAssign != nil && prevAssign.UserID == targetID {
		return nil, nil
	}

	targetCur, err := s.extRepo.GetByUserID(ctx, targetID)
	if err != nil {
		return nil, err
	}

	action := models.ExtensionActionAssign
	if prevAssign != nil {
		action = models.ExtensionActionTakeover
	}

	var historyRow models.ExtensionAssignmentHistory

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Takeover: remove the previous holder's current row for this extension.
		if prevAssign != nil {
			if err := s.extRepo.DeleteByExtensionTx(tx, ext); err != nil {
				return err
			}
		}

		// One-per-user: release the target's existing (different) extension.
		if targetCur != nil && targetCur.Extension != ext {
			if err := s.extRepo.DeleteByUserTx(tx, targetID); err != nil {
				return err
			}
			releaseHist := models.ExtensionAssignmentHistory{
				Extension:  targetCur.Extension,
				UserID:     &targetID,
				AssignedBy: actorID,
				Action:     models.ExtensionActionRelease,
				Note:       "auto-released on new assignment",
			}
			if err := s.extRepo.CreateHistoryTx(tx, &releaseHist); err != nil {
				return err
			}
		}

		// Create the new current assignment.
		cur := models.ExtensionAssignment{
			Extension:  ext,
			UserID:     targetID,
			AssignedBy: actorID,
			Note:       strings.TrimSpace(req.Note),
		}
		if err := s.extRepo.AssignTx(tx, &cur); err != nil {
			return err
		}

		historyRow = models.ExtensionAssignmentHistory{
			Extension:  ext,
			UserID:     &targetID,
			AssignedBy: actorID,
			Action:     action,
			Note:       strings.TrimSpace(req.Note),
		}
		if prevAssign != nil {
			pid := prevAssign.UserID
			historyRow.PreviousUserID = &pid
		}
		return s.extRepo.CreateHistoryTx(tx, &historyRow)
	})
	if err != nil {
		return nil, err
	}

	s.bustPoolCache(ctx)

	oldVal := map[string]interface{}{"previous_holder": models.NewExtensionUserSummary(prevHolder)}
	newVal := map[string]interface{}{"extension": ext, "assigned_to": models.NewExtensionUserSummary(target)}
	s.logAction(ctx, actorID, action, ext,
		fmt.Sprintf("%s extension %s to %s", action, ext, displayName(target)), oldVal, newVal)

	s.notify(ctx, action, ext, target, prevHolder, actorID)

	historyRow.User = target
	historyRow.PreviousUser = prevHolder
	resp := models.ToExtensionAssignmentResponse(&historyRow)
	return &resp, nil
}

func (s *extensionService) ReleaseExtension(ctx context.Context, extension string, actorID uuid.UUID) error {
	ext := strings.TrimSpace(extension)
	cur, err := s.extRepo.GetByExtension(ctx, ext)
	if err != nil {
		return err
	}
	if cur == nil {
		return ErrExtensionNotAssigned
	}
	holder := cur.User
	holderID := cur.UserID

	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := s.extRepo.DeleteByExtensionTx(tx, ext); err != nil {
			return err
		}
		hist := models.ExtensionAssignmentHistory{
			Extension:  ext,
			UserID:     &holderID,
			AssignedBy: actorID,
			Action:     models.ExtensionActionRelease,
		}
		return s.extRepo.CreateHistoryTx(tx, &hist)
	})
	if err != nil {
		return err
	}

	s.bustPoolCache(ctx)
	s.logAction(ctx, actorID, models.ExtensionActionRelease, ext,
		fmt.Sprintf("released extension %s from %s", ext, displayName(holder)),
		map[string]interface{}{"extension": ext, "was_assigned_to": models.NewExtensionUserSummary(holder)}, nil)
	s.notify(ctx, models.ExtensionActionRelease, ext, holder, nil, actorID)
	return nil
}

func (s *extensionService) CreateExtension(ctx context.Context, req models.ExtensionCreateRequest, actorID uuid.UUID) error {
	ext := strings.TrimSpace(req.Extension)
	if ext == "" || strings.TrimSpace(req.Password) == "" {
		return fmt.Errorf("extension and password are required")
	}

	if err := s.createPBXExtension(ext, req.Password); err != nil {
		return err
	}

	// New extension should appear in the pool listing immediately.
	s.bustPoolCache(ctx)

	hist := models.ExtensionAssignmentHistory{
		Extension:  ext,
		AssignedBy: actorID,
		Action:     models.ExtensionActionCreate,
		Note:       strings.TrimSpace(req.Note),
	}
	if err := s.extRepo.CreateHistory(ctx, &hist); err != nil {
		log.Printf("[ExtensionService] failed to record create history for %s: %v", ext, err)
	}

	s.logAction(ctx, actorID, models.ExtensionActionCreate, ext,
		fmt.Sprintf("created PBX extension %s", ext), nil,
		map[string]interface{}{"extension": ext})
	s.notify(ctx, models.ExtensionActionCreate, ext, nil, nil, actorID)
	return nil
}

// --- Helpers ---

func (s *extensionService) extensionInPool(ctx context.Context, ext string) (bool, error) {
	pool, err := s.fetchPool(ctx)
	if err != nil {
		return false, err
	}
	for _, p := range pool {
		if p.Extension == ext {
			return true, nil
		}
	}
	return false, nil
}

// fetchPool returns the PBX extension pool, cached in Redis for a few minutes.
func (s *extensionService) fetchPool(ctx context.Context) ([]pbxExtension, error) {
	var cached []pbxExtension
	if err := s.sessionStore.Get(ctx, extensionPoolCacheKey, &cached); err == nil && len(cached) > 0 {
		return cached, nil
	}

	url := s.cfg.PBX.BaseURL + "?action=list"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build PBX request: %w", err)
	}
	req.Header.Set("User-Agent", "Go-http-client/1.1")

	resp, err := pbxHTTPClient(s.cfg.PBX).Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch extensions from PBX: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read PBX response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("PBX returned status %d: %s", resp.StatusCode, string(body))
	}

	var pool []pbxExtension
	if err := json.Unmarshal(body, &pool); err != nil {
		return nil, fmt.Errorf("failed to parse PBX extension list: %w", err)
	}

	if err := s.sessionStore.Set(ctx, extensionPoolCacheKey, pool, extensionPoolCacheTTL); err != nil {
		log.Printf("[ExtensionService] failed to cache extension pool: %v", err)
	}
	return pool, nil
}

func (s *extensionService) bustPoolCache(ctx context.Context) {
	if err := s.sessionStore.Delete(ctx, extensionPoolCacheKey); err != nil {
		log.Printf("[ExtensionService] failed to bust extension pool cache: %v", err)
	}
}

// createPBXExtension creates an extension on the external PBX. Adapted from the
// existing sendCreateUserRequest routine: forced TLS 1.2, HTTP/1.1, form-encoded.
func (s *extensionService) createPBXExtension(extension, password string) error {
	data := urlpkg.Values{}
	data.Set("username", extension)
	data.Set("password", password)
	data.Set("action", "create")

	req, err := http.NewRequest(http.MethodPost, s.cfg.PBX.BaseURL, strings.NewReader(data.Encode()))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Go-http-client/1.1")

	resp, err := pbxHTTPClient(s.cfg.PBX).Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response: %w", err)
	}
	body := string(bodyBytes)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned status %d, body: %s", resp.StatusCode, body)
	}

	switch {
	case strings.Contains(body, "user exists"), strings.Contains(body, "already"):
		return fmt.Errorf("server returned an error: %s", body)
	case strings.Contains(body, "created_success"):
		return nil
	case strings.Contains(body, "invalid"):
		return fmt.Errorf("invalid username or password format")
	case strings.Contains(body, "error"):
		return fmt.Errorf("server returned an error: %s", body)
	}

	log.Println("[ExtensionService] extension created:", body)
	return nil
}

// pbxHTTPClient builds an HTTP client that forces TLS 1.2 and HTTP/1.1, matching
// the PBX server's requirements. Certificate validation is enabled against the
// system trust store; it is only skipped when cfg.InsecureSkipVerify is set
// (dev/staging escape hatch, defaults to false).
func pbxHTTPClient(cfg config.PBXConfig) *http.Client {
	if cfg.InsecureSkipVerify {
		log.Println("[ExtensionService] WARNING: PBX TLS certificate verification is disabled (PBX_INSECURE_SKIP_VERIFY=true)")
	}
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: cfg.InsecureSkipVerify,
				MinVersion:         tls.VersionTLS12,
				MaxVersion:         tls.VersionTLS12,
			},
			ForceAttemptHTTP2: false,
		},
	}
}

// logAction writes a clean, explicit audit entry to the action log.
func (s *extensionService) logAction(ctx context.Context, actorID uuid.UUID, action, resourceID, description string, oldVal, newVal interface{}) {
	if s.actionLog == nil {
		return
	}
	if err := s.actionLog.LogAction(ctx, &LogActionParams{
		UserID:      actorID,
		Action:      action,
		Module:      extensionModule,
		ResourceID:  resourceID,
		Description: description,
		OldValue:    oldVal,
		NewValue:    newVal,
	}); err != nil {
		log.Printf("[ExtensionService] failed to write action log: %v", err)
	}
}

// notify sends an in-app notification and websocket broadcast to all agents and
// admins (except the actor), plus the directly-affected users.
func (s *extensionService) notify(ctx context.Context, action, extension string, target, prev *models.User, actorID uuid.UUID) {
	recipients := s.recipientEmails(ctx, actorID, target, prev)

	subject, body := extensionNotificationContent(action, extension, target, prev)

	if len(recipients) > 0 {
		sentBy := actorID
		if _, err := s.notifService.SendNotification(
			ctx, "notification", nil, "en",
			recipients, nil, nil,
			subject, body, nil, nil,
			&sentBy, nil,
		); err != nil {
			log.Printf("[ExtensionService] in-app notification failed for %s: %v", extension, err)
		}
	}

	if s.wsHub != nil {
		s.wsHub.BroadcastToAll("extension_assignment", map[string]interface{}{
			"action":        action,
			"extension":     extension,
			"user":          models.NewExtensionUserSummary(target),
			"previous_user": models.NewExtensionUserSummary(prev),
			"by":            actorID,
		})
	}
}

// recipientEmails collects active agent/admin emails (excluding the actor), plus
// the target and previous holder when they differ from the actor.
func (s *extensionService) recipientEmails(ctx context.Context, actorID uuid.UUID, target, prev *models.User) []string {
	var roleIDs []uuid.UUID
	for _, code := range []string{constants.ROLES.AGENT, constants.ROLES.ADMIN} {
		if role, err := s.roleRepo.FindByCode(ctx, code); err == nil && role != nil {
			roleIDs = append(roleIDs, role.ID)
		}
	}

	seen := make(map[string]bool)
	emails := make([]string, 0)
	add := func(u *models.User) {
		if u == nil || u.ID == actorID || strings.TrimSpace(u.Email) == "" {
			return
		}
		if !seen[u.Email] {
			seen[u.Email] = true
			emails = append(emails, u.Email)
		}
	}

	if len(roleIDs) > 0 {
		users, err := s.userRepo.FindByRoleAndContext(ctx, roleIDs, nil, nil, nil)
		if err != nil {
			log.Printf("[ExtensionService] failed to load agent/admin recipients: %v", err)
		}
		for i := range users {
			add(&users[i])
		}
	}
	add(target)
	add(prev)
	return emails
}

func extensionNotificationContent(action, extension string, target, prev *models.User) (string, string) {
	switch action {
	case models.ExtensionActionCreate:
		return "New PBX extension created",
			fmt.Sprintf("Extension %s has been created and is now available for assignment.", extension)
	case models.ExtensionActionRelease:
		return "PBX extension released",
			fmt.Sprintf("Extension %s has been released from %s and is now available.", extension, displayName(target))
	case models.ExtensionActionTakeover:
		return "PBX extension reassigned",
			fmt.Sprintf("Extension %s has been reassigned to %s (previously held by %s).", extension, displayName(target), displayName(prev))
	default: // assign
		return "PBX extension assigned",
			fmt.Sprintf("Extension %s has been assigned to %s.", extension, displayName(target))
	}
}

func displayName(u *models.User) string {
	if u == nil {
		return "someone"
	}
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if name == "" {
		name = u.Username
	}
	if name == "" {
		name = u.Email
	}
	return name
}
