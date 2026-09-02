package repository

import (
	"context"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type IntegrationRepository interface {
	// Variables
	CreateVariable(ctx context.Context, v *models.IntegrationVariable) error
	FindVariableByID(ctx context.Context, id uuid.UUID) (*models.IntegrationVariable, error)
	FindVariableByName(ctx context.Context, name string) (*models.IntegrationVariable, error)
	ListVariables(ctx context.Context) ([]models.IntegrationVariable, error)
	DeleteVariable(ctx context.Context, id uuid.UUID) error

	// Scripts
	CreateScript(ctx context.Context, s *models.IntegrationScript) error
	FindScriptByID(ctx context.Context, id uuid.UUID) (*models.IntegrationScript, error)
	ListScripts(ctx context.Context, activeOnly bool) ([]models.IntegrationScript, error)
	UpdateScript(ctx context.Context, s *models.IntegrationScript) error
	DeleteScript(ctx context.Context, id uuid.UUID) error

	// State triggers
	CreateStateTrigger(ctx context.Context, t *models.WorkflowStateTrigger) error
	FindStateTriggerByID(ctx context.Context, id uuid.UUID) (*models.WorkflowStateTrigger, error)
	ListStateTriggers(ctx context.Context, stateID uuid.UUID) ([]models.WorkflowStateTrigger, error)
	// ListActiveStateTriggersForEvent returns all active triggers for the state filtered by trigger_on.
	ListActiveStateTriggersForEvent(ctx context.Context, stateID uuid.UUID, event string) ([]models.WorkflowStateTrigger, error)
	UpdateStateTrigger(ctx context.Context, t *models.WorkflowStateTrigger) error
	DeleteStateTrigger(ctx context.Context, id uuid.UUID) error
	ReplaceStateTriggerClassifications(ctx context.Context, triggerID uuid.UUID, classIDs []uuid.UUID) error

	// Transition triggers
	CreateTransitionTrigger(ctx context.Context, t *models.WorkflowTransitionTrigger) error
	FindTransitionTriggerByID(ctx context.Context, id uuid.UUID) (*models.WorkflowTransitionTrigger, error)
	ListTransitionTriggers(ctx context.Context, transitionID uuid.UUID) ([]models.WorkflowTransitionTrigger, error)
	ListActiveTransitionTriggers(ctx context.Context, transitionID uuid.UUID) ([]models.WorkflowTransitionTrigger, error)
	UpdateTransitionTrigger(ctx context.Context, t *models.WorkflowTransitionTrigger) error
	DeleteTransitionTrigger(ctx context.Context, id uuid.UUID) error
	ReplaceTransitionTriggerClassifications(ctx context.Context, triggerID uuid.UUID, classIDs []uuid.UUID) error

	// Execution logs
	CreateExecutionLog(ctx context.Context, l *models.IntegrationExecutionLog) error
	UpdateExecutionLog(ctx context.Context, l *models.IntegrationExecutionLog) error
	FindExecutionLogByID(ctx context.Context, id uuid.UUID) (*models.IntegrationExecutionLog, error)
	ListExecutionLogsByScript(ctx context.Context, scriptID uuid.UUID, limit, offset int) ([]models.IntegrationExecutionLog, int64, error)
	ListExecutionLogsByIncident(ctx context.Context, incidentID uuid.UUID, limit, offset int) ([]models.IntegrationExecutionLog, int64, error)

	// Incident bridges
	CreateBridge(ctx context.Context, b *models.IncidentBridge) error
	ListBridgesByIncident(ctx context.Context, incidentID uuid.UUID) ([]models.IncidentBridge, error)
	UpdateBridgeStatus(ctx context.Context, bridgeID uuid.UUID, status string, closedAt *time.Time) error
	CloseOpenBridgesForIncident(ctx context.Context, incidentID uuid.UUID) error

	// Webhook callback configs
	CreateWebhookConfig(ctx context.Context, cfg *models.WebhookCallbackConfig) error
	FindWebhookConfigByID(ctx context.Context, id uuid.UUID) (*models.WebhookCallbackConfig, error)
	ListWebhookConfigs(ctx context.Context) ([]models.WebhookCallbackConfig, error)
	ListActiveWebhookConfigs(ctx context.Context) ([]models.WebhookCallbackConfig, error)
	UpdateWebhookConfig(ctx context.Context, cfg *models.WebhookCallbackConfig) error
	DeleteWebhookConfig(ctx context.Context, id uuid.UUID) error
}

type integrationRepository struct {
	db *gorm.DB
}

func NewIntegrationRepository(db *gorm.DB) IntegrationRepository {
	return &integrationRepository{db: db}
}

// ---- Variables ----

func (r *integrationRepository) CreateVariable(ctx context.Context, v *models.IntegrationVariable) error {
	return r.db.WithContext(ctx).Create(v).Error
}

func (r *integrationRepository) FindVariableByID(ctx context.Context, id uuid.UUID) (*models.IntegrationVariable, error) {
	var v models.IntegrationVariable
	if err := r.db.WithContext(ctx).First(&v, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *integrationRepository) FindVariableByName(ctx context.Context, name string) (*models.IntegrationVariable, error) {
	var v models.IntegrationVariable
	if err := r.db.WithContext(ctx).First(&v, "name = ?", name).Error; err != nil {
		return nil, err
	}
	return &v, nil
}

func (r *integrationRepository) ListVariables(ctx context.Context) ([]models.IntegrationVariable, error) {
	var list []models.IntegrationVariable
	if err := r.db.WithContext(ctx).Order("name asc").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (r *integrationRepository) DeleteVariable(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.IntegrationVariable{}, "id = ?", id).Error
}

// ---- Scripts ----

func (r *integrationRepository) CreateScript(ctx context.Context, s *models.IntegrationScript) error {
	return r.db.WithContext(ctx).Create(s).Error
}

func (r *integrationRepository) FindScriptByID(ctx context.Context, id uuid.UUID) (*models.IntegrationScript, error) {
	var s models.IntegrationScript
	if err := r.db.WithContext(ctx).First(&s, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &s, nil
}

func (r *integrationRepository) ListScripts(ctx context.Context, activeOnly bool) ([]models.IntegrationScript, error) {
	var list []models.IntegrationScript
	q := r.db.WithContext(ctx).Order("name asc")
	if activeOnly {
		q = q.Where("is_active = true")
	}
	return list, q.Find(&list).Error
}

func (r *integrationRepository) UpdateScript(ctx context.Context, s *models.IntegrationScript) error {
	return r.db.WithContext(ctx).Save(s).Error
}

func (r *integrationRepository) DeleteScript(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.IntegrationScript{}, "id = ?", id).Error
}

// ---- State triggers ----

func (r *integrationRepository) CreateStateTrigger(ctx context.Context, t *models.WorkflowStateTrigger) error {
	return r.db.WithContext(ctx).Omit("Classifications.*").Create(t).Error
}

func (r *integrationRepository) FindStateTriggerByID(ctx context.Context, id uuid.UUID) (*models.WorkflowStateTrigger, error) {
	var t models.WorkflowStateTrigger
	if err := r.db.WithContext(ctx).
		Preload("IntegrationScript").
		Preload("Classifications").
		First(&t, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *integrationRepository) ListStateTriggers(ctx context.Context, stateID uuid.UUID) ([]models.WorkflowStateTrigger, error) {
	var list []models.WorkflowStateTrigger
	err := r.db.WithContext(ctx).
		Preload("IntegrationScript").
		Preload("Classifications").
		Where("workflow_state_id = ?", stateID).
		Order("execution_order asc, created_at asc").
		Find(&list).Error
	return list, err
}

func (r *integrationRepository) ListActiveStateTriggersForEvent(ctx context.Context, stateID uuid.UUID, event string) ([]models.WorkflowStateTrigger, error) {
	var list []models.WorkflowStateTrigger
	err := r.db.WithContext(ctx).
		Preload("IntegrationScript").
		Preload("Classifications").
		Where("workflow_state_id = ? AND is_active = true AND (trigger_on = ? OR trigger_on = 'both')", stateID, event).
		Order("execution_order asc, created_at asc").
		Find(&list).Error
	return list, err
}

func (r *integrationRepository) UpdateStateTrigger(ctx context.Context, t *models.WorkflowStateTrigger) error {
	return r.db.WithContext(ctx).Omit("Classifications").Save(t).Error
}

func (r *integrationRepository) DeleteStateTrigger(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.WorkflowStateTrigger{}, "id = ?", id).Error
}

func (r *integrationRepository) ReplaceStateTriggerClassifications(ctx context.Context, triggerID uuid.UUID, classIDs []uuid.UUID) error {
	trigger := &models.WorkflowStateTrigger{ID: triggerID}
	if err := r.db.WithContext(ctx).Model(trigger).Association("Classifications").Unscoped().Clear(); err != nil {
		return err
	}
	if len(classIDs) == 0 {
		return nil
	}
	classes := make([]models.Classification, len(classIDs))
	for i, id := range classIDs {
		classes[i] = models.Classification{ID: id}
	}
	return r.db.WithContext(ctx).Model(trigger).Association("Classifications").Replace(classes)
}

// ---- Transition triggers ----

func (r *integrationRepository) CreateTransitionTrigger(ctx context.Context, t *models.WorkflowTransitionTrigger) error {
	return r.db.WithContext(ctx).Omit("Classifications.*").Create(t).Error
}

func (r *integrationRepository) FindTransitionTriggerByID(ctx context.Context, id uuid.UUID) (*models.WorkflowTransitionTrigger, error) {
	var t models.WorkflowTransitionTrigger
	if err := r.db.WithContext(ctx).
		Preload("IntegrationScript").
		Preload("Classifications").
		First(&t, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *integrationRepository) ListTransitionTriggers(ctx context.Context, transitionID uuid.UUID) ([]models.WorkflowTransitionTrigger, error) {
	var list []models.WorkflowTransitionTrigger
	err := r.db.WithContext(ctx).
		Preload("IntegrationScript").
		Preload("Classifications").
		Where("workflow_transition_id = ?", transitionID).
		Order("execution_order asc, created_at asc").
		Find(&list).Error
	return list, err
}

func (r *integrationRepository) ListActiveTransitionTriggers(ctx context.Context, transitionID uuid.UUID) ([]models.WorkflowTransitionTrigger, error) {
	var list []models.WorkflowTransitionTrigger
	err := r.db.WithContext(ctx).
		Preload("IntegrationScript").
		Preload("Classifications").
		Where("workflow_transition_id = ? AND is_active = true", transitionID).
		Order("execution_order asc, created_at asc").
		Find(&list).Error
	return list, err
}

func (r *integrationRepository) UpdateTransitionTrigger(ctx context.Context, t *models.WorkflowTransitionTrigger) error {
	return r.db.WithContext(ctx).Omit("Classifications").Save(t).Error
}

func (r *integrationRepository) DeleteTransitionTrigger(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.WorkflowTransitionTrigger{}, "id = ?", id).Error
}

func (r *integrationRepository) ReplaceTransitionTriggerClassifications(ctx context.Context, triggerID uuid.UUID, classIDs []uuid.UUID) error {
	trigger := &models.WorkflowTransitionTrigger{ID: triggerID}
	if err := r.db.WithContext(ctx).Model(trigger).Association("Classifications").Unscoped().Clear(); err != nil {
		return err
	}
	if len(classIDs) == 0 {
		return nil
	}
	classes := make([]models.Classification, len(classIDs))
	for i, id := range classIDs {
		classes[i] = models.Classification{ID: id}
	}
	return r.db.WithContext(ctx).Model(trigger).Association("Classifications").Replace(classes)
}

// ---- Execution logs ----

func (r *integrationRepository) CreateExecutionLog(ctx context.Context, l *models.IntegrationExecutionLog) error {
	return r.db.WithContext(ctx).Create(l).Error
}

func (r *integrationRepository) UpdateExecutionLog(ctx context.Context, l *models.IntegrationExecutionLog) error {
	return r.db.WithContext(ctx).Save(l).Error
}

func (r *integrationRepository) FindExecutionLogByID(ctx context.Context, id uuid.UUID) (*models.IntegrationExecutionLog, error) {
	var l models.IntegrationExecutionLog
	if err := r.db.WithContext(ctx).First(&l, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &l, nil
}

func (r *integrationRepository) ListExecutionLogsByScript(ctx context.Context, scriptID uuid.UUID, limit, offset int) ([]models.IntegrationExecutionLog, int64, error) {
	var list []models.IntegrationExecutionLog
	var total int64
	q := r.db.WithContext(ctx).Model(&models.IntegrationExecutionLog{}).Where("integration_script_id = ?", scriptID)
	q.Count(&total)
	err := q.Order("executed_at desc").Limit(limit).Offset(offset).Find(&list).Error
	return list, total, err
}

func (r *integrationRepository) ListExecutionLogsByIncident(ctx context.Context, incidentID uuid.UUID, limit, offset int) ([]models.IntegrationExecutionLog, int64, error) {
	var list []models.IntegrationExecutionLog
	var total int64
	q := r.db.WithContext(ctx).Model(&models.IntegrationExecutionLog{}).Where("incident_id = ?", incidentID)
	q.Count(&total)
	err := q.Order("executed_at desc").Limit(limit).Offset(offset).Find(&list).Error
	return list, total, err
}

// ---- Incident bridges ----

func (r *integrationRepository) CreateBridge(ctx context.Context, b *models.IncidentBridge) error {
	return r.db.WithContext(ctx).Create(b).Error
}

func (r *integrationRepository) ListBridgesByIncident(ctx context.Context, incidentID uuid.UUID) ([]models.IncidentBridge, error) {
	var list []models.IncidentBridge
	err := r.db.WithContext(ctx).
		Where("local_incident_id = ?", incidentID).
		Order("created_at asc").
		Find(&list).Error
	return list, err
}

func (r *integrationRepository) UpdateBridgeStatus(ctx context.Context, bridgeID uuid.UUID, status string, closedAt *time.Time) error {
	updates := map[string]interface{}{"status": status}
	if closedAt != nil {
		updates["closed_at"] = closedAt
	}
	return r.db.WithContext(ctx).Model(&models.IncidentBridge{}).Where("id = ?", bridgeID).Updates(updates).Error
}

func (r *integrationRepository) CloseOpenBridgesForIncident(ctx context.Context, incidentID uuid.UUID) error {
	now := time.Now()
	return r.db.WithContext(ctx).Model(&models.IncidentBridge{}).
		Where("local_incident_id = ? AND status = 'open'", incidentID).
		Updates(map[string]interface{}{"status": "closed", "closed_at": now}).Error
}

// ---- Webhook callback configs ----

func (r *integrationRepository) CreateWebhookConfig(ctx context.Context, cfg *models.WebhookCallbackConfig) error {
	return r.db.WithContext(ctx).Create(cfg).Error
}

func (r *integrationRepository) FindWebhookConfigByID(ctx context.Context, id uuid.UUID) (*models.WebhookCallbackConfig, error) {
	var cfg models.WebhookCallbackConfig
	if err := r.db.WithContext(ctx).First(&cfg, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (r *integrationRepository) ListWebhookConfigs(ctx context.Context) ([]models.WebhookCallbackConfig, error) {
	var list []models.WebhookCallbackConfig
	err := r.db.WithContext(ctx).Order("name asc").Find(&list).Error
	return list, err
}

func (r *integrationRepository) ListActiveWebhookConfigs(ctx context.Context) ([]models.WebhookCallbackConfig, error) {
	var list []models.WebhookCallbackConfig
	err := r.db.WithContext(ctx).Where("is_active = true").Order("name asc").Find(&list).Error
	return list, err
}

func (r *integrationRepository) UpdateWebhookConfig(ctx context.Context, cfg *models.WebhookCallbackConfig) error {
	return r.db.WithContext(ctx).Save(cfg).Error
}

func (r *integrationRepository) DeleteWebhookConfig(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.WebhookCallbackConfig{}, "id = ?", id).Error
}
