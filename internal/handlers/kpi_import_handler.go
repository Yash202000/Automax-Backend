package handlers

import (
	"context"
	"fmt"
	"io"

	"github.com/automax/backend/internal/middleware"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/utils"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// KpiImportHandler serves the KPI Dictionary bulk import: Validate returns
// the validation report + import preview without writing anything; Commit
// re-validates the same file and applies it with the chosen options.
type KpiImportHandler struct {
	db           *gorm.DB
	actionLogSvc services.ActionLogService
	workflowSvc  *services.KpiWorkflowService
}

func NewKpiImportHandler(db *gorm.DB, actionLogSvc services.ActionLogService, workflowSvc *services.KpiWorkflowService) *KpiImportHandler {
	return &KpiImportHandler{db: db, actionLogSvc: actionLogSvc, workflowSvc: workflowSvc}
}

func (h *KpiImportHandler) readRequest(c *fiber.Ctx) ([]byte, string, services.KpiImportOptions, error) {
	opts := services.KpiImportOptions{
		ExistingMode: c.FormValue("existing_mode", services.KpiImportExistingSkip),
		ErrorMode:    c.FormValue("error_mode", services.KpiImportErrorsSkipInvalid),
	}
	file, err := c.FormFile("file")
	if err != nil {
		return nil, "", opts, fmt.Errorf("an Excel file is required")
	}
	f, err := file.Open()
	if err != nil {
		return nil, "", opts, fmt.Errorf("failed to open the uploaded file")
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 25<<20))
	if err != nil {
		return nil, "", opts, fmt.Errorf("failed to read the uploaded file")
	}
	return data, file.Filename, opts, nil
}

func (h *KpiImportHandler) Validate(c *fiber.Ctx) error {
	data, name, opts, err := h.readRequest(c)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)
	res, err := services.ValidateKpiDictionaryImport(c.UserContext(), h.db, data, name, userID, opts)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.SuccessResponse(c, fiber.StatusOK, "", res)
}

func (h *KpiImportHandler) Commit(c *fiber.Ctx) error {
	data, name, opts, err := h.readRequest(c)
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusBadRequest, err.Error())
	}
	// Overwriting existing KPIs needs update rights on top of create.
	if opts.ExistingMode == services.KpiImportExistingUpdate && !hasTargetPermission(c, "kpi:update") {
		return utils.ErrorResponse(c, fiber.StatusForbidden, "Updating existing KPIs requires the kpi:update permission")
	}
	userID := c.Locals(constants.ContextKeys.UserID).(uuid.UUID)

	res, err := services.CommitKpiDictionaryImport(c.UserContext(), h.db, data, name, userID, opts,
		func(ctx context.Context, kpiType string, kpiID uuid.UUID) error {
			return h.workflowSvc.InitiateKpiDictionaryWorkflow(ctx, kpiType, kpiID, userID)
		})
	if err != nil {
		return utils.ErrorResponse(c, fiber.StatusInternalServerError, err.Error())
	}

	status := "success"
	if !res.Committed {
		status = "failed"
	}
	middleware.LogAction(c, h.actionLogSvc, &services.LogActionParams{
		Action: "import",
		Module: "kpi",
		Description: fmt.Sprintf("KPI Dictionary import %q: %d created, %d updated, %d skipped, %d rejected, %d rolled back",
			name, res.Totals.Created, res.Totals.Updated, res.Totals.Skipped, res.Totals.Rejected, res.Totals.RolledBack),
		Status: status,
	})
	return utils.SuccessResponse(c, fiber.StatusOK, "", res)
}
