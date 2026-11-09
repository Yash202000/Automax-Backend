package handlers

import (
	"errors"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

const (
	kpiAccessScopeKey   = "kpi_access_scope"
	kpiAccessServiceKey = "kpi_access_service"
)

// KpiAccessHandler enforces per-KPI access: only the Super Admin and users
// with an active collaborator assignment on a KPI may reach it, and only
// for the actions their collaborator type allows.
type KpiAccessHandler struct {
	svc *services.KpiAccessService
}

func NewKpiAccessHandler(svc *services.KpiAccessService) *KpiAccessHandler {
	return &KpiAccessHandler{svc: svc}
}

// KpiResolver finds the KPI a request is about.
type KpiResolver func(c *fiber.Ctx, svc *services.KpiAccessService) (services.KpiRef, error)

// LoadScope stores the user's KPI access scope for list handlers (see
// kpiScope). Mounted on the /kpi group after authentication.
func (h *KpiAccessHandler) LoadScope() fiber.Handler {
	return func(c *fiber.Ctx) error {
		userID, ok := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
		if !ok {
			return utils.ErrorResponse(c, fiber.StatusUnauthorized, i18n.T(c.UserContext(), "user_not_authenticated"))
		}
		scope, err := h.svc.Scope(c.UserContext(), userID)
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "internal_server_error"))
		}
		c.Locals(kpiAccessScopeKey, scope)
		c.Locals(kpiAccessServiceKey, h.svc)
		return c.Next()
	}
}

// kpiScope returns the scope LoadScope stored. Without one it sees nothing.
func kpiScope(c *fiber.Ctx) services.KpiAccessScope {
	if scope, ok := c.Locals(kpiAccessScopeKey).(services.KpiAccessScope); ok {
		return scope
	}
	userID, _ := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	return services.KpiAccessScope{UserID: userID}
}

// Require allows the request only when the user may perform action on the
// KPI resolve finds. A record that does not exist is passed through so the
// handler answers with its usual not-found error.
func (h *KpiAccessHandler) Require(action services.KpiAction, resolve KpiResolver) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ref, err := resolve(c, h.svc)
		if errors.Is(err, services.ErrKpiNotFound) {
			return c.Next()
		}
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
		}
		if denied, err := kpiAccessDenied(c, ref.KpiType, ref.KpiID, action); denied {
			return err
		}
		return c.Next()
	}
}

// RequireTransition is Require for workflow transitions, whose action
// depends on the transition chosen in the body: transition_id (entries, KPI
// lifecycle) or action (targets).
func (h *KpiAccessHandler) RequireTransition(resolve KpiResolver) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ref, err := resolve(c, h.svc)
		if errors.Is(err, services.ErrKpiNotFound) {
			return c.Next()
		}
		if err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_id"))
		}
		var body struct {
			TransitionID string `json:"transition_id"`
			Action       string `json:"action"`
		}
		if err := c.BodyParser(&body); err != nil {
			return utils.ErrorResponse(c, fiber.StatusBadRequest, i18n.T(c.UserContext(), "invalid_request_body"))
		}
		code := body.Action
		if transitionID, err := uuid.Parse(body.TransitionID); err == nil {
			if code, err = h.svc.TransitionCode(c.UserContext(), transitionID); err != nil {
				return utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "internal_server_error"))
			}
		}
		if denied, err := kpiAccessDenied(c, ref.KpiType, ref.KpiID, services.KpiTransitionAction(code)); denied {
			return err
		}
		return c.Next()
	}
}

// kpiAccessDenied writes a 403 response unless the user may perform action
// on the KPI. When denied is true the caller must return err (the result of
// writing that response) without going further.
func kpiAccessDenied(c *fiber.Ctx, kpiType string, kpiID uuid.UUID, action services.KpiAction) (denied bool, err error) {
	svc, ok := c.Locals(kpiAccessServiceKey).(*services.KpiAccessService)
	if !ok {
		return true, utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "internal_server_error"))
	}
	userID, _ := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	allowed, err := svc.Can(c.UserContext(), userID, kpiType, kpiID, action)
	if err != nil {
		return true, utils.ErrorResponse(c, fiber.StatusInternalServerError, i18n.T(c.UserContext(), "internal_server_error"))
	}
	if allowed {
		return false, nil
	}
	key := "kpi_access_denied"
	if action != services.KpiActionView {
		key = "kpi_action_denied"
	}
	return true, utils.ErrorResponse(c, fiber.StatusForbidden, i18n.T(c.UserContext(), key))
}

// allowedTransitions drops the transitions the user's collaborator type on
// the KPI does not allow, so the client only offers what will succeed.
func allowedTransitions(c *fiber.Ctx, kpiType string, kpiID uuid.UUID, transitions []models.WorkflowTransition) ([]models.WorkflowTransition, error) {
	svc, ok := c.Locals(kpiAccessServiceKey).(*services.KpiAccessService)
	if !ok {
		return transitions, nil
	}
	userID, _ := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	allows, err := svc.Allowed(c.UserContext(), userID, kpiType, kpiID)
	if err != nil {
		return nil, err
	}
	kept := make([]models.WorkflowTransition, 0, len(transitions))
	for _, t := range transitions {
		if allows(services.KpiTransitionAction(t.Code)) {
			kept = append(kept, t)
		}
	}
	return kept, nil
}

// assignKpiCreator makes the user who created a KPI its KPI Owner, so they
// keep access to it.
func assignKpiCreator(c *fiber.Ctx, kpiType string, kpiID uuid.UUID) error {
	svc, ok := c.Locals(kpiAccessServiceKey).(*services.KpiAccessService)
	if !ok {
		return errors.New("kpi access service not loaded")
	}
	userID, _ := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	return svc.AssignCreatorAsOwner(c.UserContext(), userID, kpiType, kpiID)
}

// GetMyAccess returns the KPIs the current user is assigned to and what
// they may do on each. GET /kpi/my-access
func (h *KpiAccessHandler) GetMyAccess(c *fiber.Ctx) error {
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	access, err := h.svc.MyAccess(c.UserContext(), userID)
	if err != nil {
		return utils.InternalErrorResponse(c, err, i18n.T(c.UserContext(), "internal_server_error"))
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", access)
}

// ─── Resolvers ───────────────────────────────────────────────────────────

// KpiFromPath reads the KPI from /:type/:id.
func KpiFromPath(c *fiber.Ctx, _ *services.KpiAccessService) (services.KpiRef, error) {
	return kpiRefFromParams(c.Params("type"), c.Params("id"))
}

// KpiOfType reads the KPI ID from :id on a route fixed to one KPI type
// (/strategic/:id, ...).
func KpiOfType(kpiType string) KpiResolver {
	return func(c *fiber.Ctx, _ *services.KpiAccessService) (services.KpiRef, error) {
		return kpiRefFromParams(kpiType, c.Params("id"))
	}
}

func kpiRefFromParams(kpiType, rawID string) (services.KpiRef, error) {
	id, err := uuid.Parse(rawID)
	if err != nil || !isValidKpiType(kpiType) {
		return services.KpiRef{}, errors.New("invalid kpi")
	}
	return services.KpiRef{KpiType: kpiType, KpiID: id}, nil
}

// KpiOfRecord resolves the KPI of the row of table (which carries kpi_type +
// kpi_id) whose ID is in the param route parameter.
func KpiOfRecord(table, param string) KpiResolver {
	return func(c *fiber.Ctx, svc *services.KpiAccessService) (services.KpiRef, error) {
		id, err := uuid.Parse(c.Params(param))
		if err != nil {
			return services.KpiRef{}, err
		}
		return svc.KpiOfRecord(c.UserContext(), table, id)
	}
}

// KpiOfCodedRecord is KpiOfRecord for tables that reference their KPI by
// kpi_type + kpi_code.
func KpiOfCodedRecord(table, param string) KpiResolver {
	return func(c *fiber.Ctx, svc *services.KpiAccessService) (services.KpiRef, error) {
		id, err := uuid.Parse(c.Params(param))
		if err != nil {
			return services.KpiRef{}, err
		}
		return svc.KpiOfCodedRecord(c.UserContext(), table, id)
	}
}

// KpiFromBodyCode reads kpi_type + kpi_code from the JSON body of a create
// request (targets, legacy performance, benchmarks, segmentation).
func KpiFromBodyCode(c *fiber.Ctx, svc *services.KpiAccessService) (services.KpiRef, error) {
	var body struct {
		KpiType string `json:"kpi_type"`
		KpiCode string `json:"kpi_code"`
	}
	if err := c.BodyParser(&body); err != nil {
		return services.KpiRef{}, err
	}
	if body.KpiCode == "" {
		// Let the handler's own validation report the missing field.
		return services.KpiRef{}, services.ErrKpiNotFound
	}
	return svc.KpiByCode(c.UserContext(), body.KpiType, body.KpiCode)
}

// KpiOfPerformanceQuery resolves the KPI of the legacy performance record
// named by the performance_id query parameter (corrective action list).
func KpiOfPerformanceQuery(c *fiber.Ctx, svc *services.KpiAccessService) (services.KpiRef, error) {
	id, err := uuid.Parse(c.Query("performance_id"))
	if err != nil {
		return services.KpiRef{}, err
	}
	return svc.KpiOfCodedRecord(c.UserContext(), "kpi_performances", id)
}

// KpiOfPerformanceBody resolves the KPI of the legacy performance record
// named by kpi_performance_id in the JSON body (corrective action create).
func KpiOfPerformanceBody(c *fiber.Ctx, svc *services.KpiAccessService) (services.KpiRef, error) {
	var body struct {
		KpiPerformanceID string `json:"kpi_performance_id"`
	}
	if err := c.BodyParser(&body); err != nil {
		return services.KpiRef{}, err
	}
	id, err := uuid.Parse(body.KpiPerformanceID)
	if err != nil {
		// Let the handler's own validation report the field.
		return services.KpiRef{}, services.ErrKpiNotFound
	}
	return svc.KpiOfCodedRecord(c.UserContext(), "kpi_performances", id)
}

// KpiOfCorrectiveAction resolves the KPI of the corrective action in :id.
func KpiOfCorrectiveAction(c *fiber.Ctx, svc *services.KpiAccessService) (services.KpiRef, error) {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return services.KpiRef{}, err
	}
	perfID, err := svc.CorrectiveActionPerformance(c.UserContext(), id)
	if err != nil {
		return services.KpiRef{}, err
	}
	return svc.KpiOfCodedRecord(c.UserContext(), "kpi_performances", perfID)
}
