package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/database"
	"github.com/automax/backend/internal/handlers"
	"github.com/automax/backend/internal/middleware"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/services"
	"github.com/automax/backend/internal/storage"
	"github.com/automax/backend/pkg/utils"
	"github.com/automax/backend/pkg/validation"
	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/joho/godotenv"
)

func main() {
	cfg := config.Load()
	godotenv.Load()

	validation.InitValidatorRegistry()
	db, err := database.Connect(&cfg.Database)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer database.Close(db)

	if err := database.Migrate(db); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Seed default data
	if err := database.Seed(db); err != nil {
		log.Printf("Warning: Failed to seed database: %v", err)
	}

	redisClient, err := database.ConnectRedis(&cfg.Redis)
	if err != nil {
		log.Fatalf("Failed to connect to Redis: %v", err)
	}
	defer database.CloseRedis(redisClient)

	minioStorage, err := storage.NewMinIOStorage(&cfg.MinIO)
	if err != nil {
		log.Fatalf("Failed to connect to MinIO: %v", err)
	}

	jwtManager := utils.NewJWTManager(cfg.JWT.Secret, cfg.JWT.ExpireHour)
	sessionStore := database.NewSessionStore(redisClient)

	// Initialize repositories
	userRepo := repository.NewUserRepository(db)
	classificationRepo := repository.NewClassificationRepository(db)
	locationRepo := repository.NewLocationRepository(db)
	departmentRepo := repository.NewDepartmentRepository(db)
	roleRepo := repository.NewRoleRepository(db)
	permissionRepo := repository.NewPermissionRepository(db)
	actionLogRepo := repository.NewActionLogRepository(db)
	workflowRepo := repository.NewWorkflowRepository(db)
	incidentRepo := repository.NewIncidentRepository(db)
	incidentMergeRepo := repository.NewIncidentMergeRepository(db)
	reportRepo := repository.NewReportRepository(db)
	reportTemplateRepo := repository.NewReportTemplateRepository(db)
	lookupRepo := repository.NewLookupRepository(db)
	callLogRepo := repository.NewCallLogRepository(db)
	applicationLinkRepo := repository.NewApplicationLinkRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)

	// Initialize WebSocket hub and start it
	wsHub := services.NewWSHub()
	go wsHub.Run()
	log.Println("WebSocket hub started")
	notificationTemplateRepo := repository.NewNotificationTemplateRepository(db)
	notificationLogRepo := repository.NewNotificationLogRepository(db)

	// Initialize services
	actionLogService := services.NewActionLogService(actionLogRepo)
	userService := services.NewUserService(userRepo, departmentRepo, jwtManager, sessionStore, minioStorage, cfg, actionLogService)
	callLogService := services.NewCallLogService(callLogRepo, userRepo)
	workflowService := services.NewWorkflowService(workflowRepo, roleRepo, departmentRepo, classificationRepo, db)
	incidentService := services.NewIncidentService(incidentRepo, incidentMergeRepo, workflowRepo, userRepo, minioStorage, db, wsHub)
	incidentMergeService := services.NewIncidentMergeService(incidentMergeRepo, incidentRepo, workflowRepo, roleRepo, locationRepo, classificationRepo, db, wsHub)
	reportService := services.NewReportService(reportRepo)
	reportTemplateService := services.NewReportTemplateService(reportTemplateRepo, reportRepo)
	applicationLinkService := services.NewApplicationLinkService(applicationLinkRepo)
	settingsService := services.NewSettingsService(settingsRepo)
	presenceService := services.NewPresenceService(redisClient)
	notificationService := services.NewNotificationService(notificationTemplateRepo, notificationLogRepo, userRepo)
	otpService := services.NewOTPService(redisClient, notificationService, notificationLogRepo)

	// Initialize and start SLA Monitor (checks every 5 minutes)
	slaMonitor := services.NewSLAMonitor(incidentRepo, 5*time.Minute)
	ctx := context.Background()
	slaMonitor.Start(ctx)
	defer slaMonitor.Stop()

	// Initialize validator
	validate := validator.New()

	// Initialize handlers
	userHandler := handlers.NewUserHandler(userService, minioStorage)
	healthHandler := handlers.NewHealthHandler()
	classificationHandler := handlers.NewClassificationHandler(classificationRepo)
	locationHandler := handlers.NewLocationHandler(locationRepo)
	departmentHandler := handlers.NewDepartmentHandler(departmentRepo)
	roleHandler := handlers.NewRoleHandler(roleRepo, permissionRepo)
	actionLogHandler := handlers.NewActionLogHandler(actionLogService, validate)
	callLogHandler := handlers.NewCallLogHandler(callLogService, validate, userService)
	workflowHandler := handlers.NewWorkflowHandler(workflowService, actionLogService)
	incidentHandler := handlers.NewIncidentHandler(incidentService, userRepo, incidentRepo, minioStorage, presenceService)
	incidentMergeHandler := handlers.NewIncidentMergeHandler(incidentMergeService, userRepo)
	websocketHandler := handlers.NewWebSocketHandler(wsHub)
	reportHandler := handlers.NewReportHandler(reportService)
	reportTemplateHandler := handlers.NewReportTemplateHandler(reportTemplateService)
	lookupHandler := handlers.NewLookupHandler(lookupRepo)
	applicationLinkHandler := handlers.NewApplicationLinkHandler(applicationLinkService, minioStorage)
	settingsHandler := handlers.NewSettingsHandler(settingsService)
	notificationHandler := handlers.NewNotificationHandler(notificationService, minioStorage)
	templateHandler := handlers.NewNotificationTemplateHandler(notificationTemplateRepo)
	attachmentHandler := handlers.NewAttachmentHandler(incidentService, notificationService, minioStorage)
	otpHandler := handlers.NewOTPHandler(otpService)

	// Initialize middleware
	authMiddleware := middleware.NewAuthMiddleware(jwtManager, sessionStore, userRepo)

	app := fiber.New(fiber.Config{
		AppName:      "Automax Backend",
		ErrorHandler: customErrorHandler,
		BodyLimit:    100 * 1024 * 1024, // 100MB max body size
	})

	app.Use(recover.New())
	app.Use(logger.New(logger.Config{
		Format: "[${time}] ${status} - ${latency} ${method} ${path}\n",
	}))
	app.Use(cors.New(cors.Config{
		AllowOrigins:     "http://localhost:3000,http://localhost:5173,http://localhost:5174",
		AllowMethods:     "GET,POST,PUT,DELETE,PATCH,OPTIONS",
		AllowHeaders:     "Origin,Content-Type,Accept,Authorization",
		AllowCredentials: true,
	}))

	app.Use(middleware.ValidationMiddleware())
	api := app.Group("/api")
	v1 := api.Group("/v1")

	// Health routes
	v1.Get("/health", healthHandler.Health)
	v1.Get("/ready", healthHandler.Ready)

	// WebSocket route for real-time updates
	// Query params: incident_id, user_id, token (for auth)
	v1.Get("/ws", websocketHandler.WebSocketUpgrader())

	// Broadcast WebSocket route for incident list viewer count updates
	// Query params: token (for auth)
	v1.Get("/ws/broadcast", websocketHandler.BroadcastWebSocketUpgrader())

	// WebSocket connection statistics endpoint
	v1.Get("/ws/stats", authMiddleware.RequirePermission("incidents:view"), websocketHandler.GetConnectionStats)

	// Webhook routes (no authentication required - external services)
	webhooks := v1.Group("/webhooks")
	webhooks.Post("/sendgrid/inbound", notificationHandler.SendGridInboundWebhook)

	// Auth routes
	auth := v1.Group("/auth")
	auth.Post("/register", userHandler.Register)
	auth.Post("/login", userHandler.Login)
	auth.Post("/refresh", userHandler.RefreshToken)
	auth.Post("/logout", authMiddleware.Authenticate(), userHandler.Logout)

	// User routes
	users := v1.Group("/users")
	users.Get("/me", authMiddleware.Authenticate(), userHandler.GetProfile)
	users.Put("/me", authMiddleware.Authenticate(), userHandler.UpdateProfile)
	users.Post("/me/avatar", authMiddleware.Authenticate(), userHandler.UploadAvatar)
	users.Put("/me/password", authMiddleware.Authenticate(), userHandler.ChangePassword)
	users.Delete("/me", authMiddleware.Authenticate(), userHandler.DeleteAccount)
	users.Put("/:userExtID/status", userHandler.UpdateUserCallStatus)

	// Incident routes (authenticated users)
	incidents := v1.Group("/incidents", authMiddleware.Authenticate())
	incidents.Post("/", authMiddleware.RequirePermission("incidents:create"), incidentHandler.CreateIncident)
	incidents.Get("/", authMiddleware.RequirePermission("incidents:view"), incidentHandler.ListIncidents)
	incidents.Get("/stats", authMiddleware.RequirePermission("incidents:view"), incidentHandler.GetStats)
	incidents.Get("/priority-counts", authMiddleware.RequirePermission("incidents:view"), incidentHandler.GetPriorityCounts)
	incidents.Get("/my-assigned", authMiddleware.RequirePermission("incidents:view"), incidentHandler.GetMyAssigned)
	incidents.Get("/my-reported", authMiddleware.RequirePermission("incidents:view"), incidentHandler.GetMyReported)
	incidents.Get("/sla-breached", authMiddleware.RequirePermission("incidents:view"), incidentHandler.GetSLABreached)
	incidents.Get("/:id", authMiddleware.RequirePermission("incidents:view"), incidentHandler.GetIncident)
	incidents.Get("/:id/report", authMiddleware.RequirePermission("reports:view"), incidentHandler.GenerateReport)
	incidents.Put("/:id", authMiddleware.RequirePermission("incidents:update"), incidentHandler.UpdateIncident)
	incidents.Delete("/:id", authMiddleware.RequirePermission("incidents:delete"), incidentHandler.DeleteIncident)
	incidents.Post("/:id/transition", authMiddleware.RequirePermission("incidents:transition"), incidentHandler.ExecuteTransition)
	incidents.Post("/:id/convert-to-request", authMiddleware.RequirePermission("incidents:update"), incidentHandler.ConvertToRequest)
	incidents.Get("/:id/can-convert", authMiddleware.RequirePermission("incidents:view"), incidentHandler.CanConvertToRequest)
	incidents.Get("/:id/available-transitions", authMiddleware.RequirePermission("incidents:view"), incidentHandler.GetAvailableTransitions)
	incidents.Get("/:id/history", authMiddleware.RequirePermission("incidents:view"), incidentHandler.GetTransitionHistory)
	incidents.Post("/:id/comments", authMiddleware.RequirePermission("incidents:comment"), incidentHandler.AddComment)
	incidents.Get("/:id/comments", authMiddleware.RequirePermission("incidents:view"), incidentHandler.ListComments)
	incidents.Put("/:id/comments/:comment_id", authMiddleware.RequirePermission("incidents:comment"), incidentHandler.UpdateComment)
	incidents.Delete("/:id/comments/:comment_id", authMiddleware.RequirePermission("incidents:comment"), incidentHandler.DeleteComment)
	incidents.Post("/:id/attachments", authMiddleware.RequirePermission("incidents:update"), incidentHandler.UploadAttachment)
	incidents.Get("/:id/attachments", authMiddleware.RequirePermission("incidents:view"), incidentHandler.ListAttachments)
	incidents.Delete("/:id/attachments/:attachment_id", authMiddleware.RequirePermission("incidents:update"), incidentHandler.DeleteAttachment)
	incidents.Put("/:id/assign", authMiddleware.RequirePermission("incidents:assign"), incidentHandler.AssignIncident)
	incidents.Get("/:id/revisions", authMiddleware.RequirePermission("incidents:view"), incidentHandler.ListRevisions)

	// Presence tracking routes
	incidents.Post("/:id/presence", authMiddleware.RequirePermission("incidents:view"), incidentHandler.MarkPresence)
	incidents.Get("/:id/presence", authMiddleware.RequirePermission("incidents:view"), incidentHandler.GetPresence)
	incidents.Delete("/:id/presence", authMiddleware.RequirePermission("incidents:view"), incidentHandler.RemovePresence)

	// Incident Merge routes
	incidents.Post("/merge/validate", authMiddleware.RequirePermission("incidents:merge"), incidentMergeHandler.ValidateMerge)
	incidents.Post("/merge", authMiddleware.RequirePermission("incidents:merge"), incidentMergeHandler.MergeIncidents)
	incidents.Post("/merge/unmerge", authMiddleware.RequirePermission("incidents:merge"), incidentMergeHandler.UnmergeIncident)
	incidents.Post("/merge/bulk-unmerge", authMiddleware.RequirePermission("incidents:merge"), incidentMergeHandler.BulkUnmergeIncidents)
	incidents.Get("/merge/:id/merged", authMiddleware.RequirePermission("incidents:view"), incidentMergeHandler.GetMergedIncidents)
	incidents.Get("/merge/can-merge", authMiddleware.RequirePermission("incidents:view"), incidentMergeHandler.CanMerge)

	// Attachment download route (unified for incidents and notifications)
	attachments := v1.Group("/attachments", authMiddleware.Authenticate())
	attachments.Get("/:attachment_id", attachmentHandler.DownloadAttachment)
	attachments.Get("/:attachment_id/preview", attachmentHandler.PreviewAttachment)

	// Complaint routes (authenticated users)
	complaints := v1.Group("/complaints", authMiddleware.Authenticate())
	complaints.Post("/", authMiddleware.RequirePermission("complaints:create"), incidentHandler.CreateComplaint)
	complaints.Get("/", authMiddleware.RequirePermission("complaints:view"), incidentHandler.ListComplaints)
	complaints.Get("/:id", authMiddleware.RequirePermission("complaints:view"), incidentHandler.GetComplaint)
	complaints.Get("/:id/report", authMiddleware.RequirePermission("reports:view"), incidentHandler.GenerateReport)
	complaints.Put("/:id", authMiddleware.RequirePermission("complaints:update"), incidentHandler.UpdateIncident)
	complaints.Post("/:id/transition", authMiddleware.RequirePermission("complaints:transition"), incidentHandler.ExecuteTransition)
	complaints.Get("/:id/available-transitions", authMiddleware.RequirePermission("complaints:view"), incidentHandler.GetAvailableTransitions)
	complaints.Get("/:id/history", authMiddleware.RequirePermission("complaints:view"), incidentHandler.GetTransitionHistory)
	complaints.Post("/:id/comments", authMiddleware.RequirePermission("complaints:comment"), incidentHandler.AddComment)
	complaints.Get("/:id/comments", authMiddleware.RequirePermission("complaints:view"), incidentHandler.ListComments)
	complaints.Put("/:id/comments/:comment_id", authMiddleware.RequirePermission("complaints:comment"), incidentHandler.UpdateComment)
	complaints.Delete("/:id/comments/:comment_id", authMiddleware.RequirePermission("complaints:comment"), incidentHandler.DeleteComment)
	complaints.Post("/:id/attachments", authMiddleware.RequirePermission("complaints:update"), incidentHandler.UploadAttachment)
	complaints.Get("/:id/attachments", authMiddleware.RequirePermission("complaints:view"), incidentHandler.ListAttachments)
	complaints.Delete("/:id/attachments/:attachment_id", authMiddleware.RequirePermission("complaints:update"), incidentHandler.DeleteAttachment)
	complaints.Post("/:id/evaluate", authMiddleware.RequirePermission("complaints:update"), incidentHandler.IncrementEvaluation)
	complaints.Get("/:id/revisions", authMiddleware.RequirePermission("complaints:view"), incidentHandler.ListRevisions)

	// Query routes (authenticated users)
	queries := v1.Group("/queries", authMiddleware.Authenticate())
	queries.Post("/", authMiddleware.RequirePermission("queries:create"), incidentHandler.CreateQuery)
	queries.Get("/", authMiddleware.RequirePermission("queries:view"), incidentHandler.ListQueries)
	queries.Get("/:id", authMiddleware.RequirePermission("queries:view"), incidentHandler.GetQuery)
	queries.Get("/:id/report", authMiddleware.RequirePermission("reports:view"), incidentHandler.GenerateReport)
	queries.Put("/:id", authMiddleware.RequirePermission("queries:update"), incidentHandler.UpdateIncident)
	queries.Post("/:id/transition", authMiddleware.RequirePermission("queries:transition"), incidentHandler.ExecuteTransition)
	queries.Get("/:id/available-transitions", authMiddleware.RequirePermission("queries:view"), incidentHandler.GetAvailableTransitions)
	queries.Get("/:id/history", authMiddleware.RequirePermission("queries:view"), incidentHandler.GetTransitionHistory)
	queries.Post("/:id/comments", authMiddleware.RequirePermission("queries:comment"), incidentHandler.AddComment)
	queries.Get("/:id/comments", authMiddleware.RequirePermission("queries:view"), incidentHandler.ListComments)
	queries.Put("/:id/comments/:comment_id", authMiddleware.RequirePermission("queries:comment"), incidentHandler.UpdateComment)
	queries.Delete("/:id/comments/:comment_id", authMiddleware.RequirePermission("queries:comment"), incidentHandler.DeleteComment)
	queries.Post("/:id/attachments", authMiddleware.RequirePermission("queries:update"), incidentHandler.UploadAttachment)
	queries.Get("/:id/attachments", authMiddleware.RequirePermission("queries:view"), incidentHandler.ListAttachments)
	queries.Delete("/:id/attachments/:attachment_id", authMiddleware.RequirePermission("queries:update"), incidentHandler.DeleteAttachment)
	queries.Get("/:id/revisions", authMiddleware.RequirePermission("queries:view"), incidentHandler.ListRevisions)

	// Admin routes
	admin := v1.Group("/admin", authMiddleware.Authenticate())

	// User management
	admin.Get("/users", authMiddleware.RequirePermission("users:view"), userHandler.ListUsers)
	admin.Post("/users", authMiddleware.RequirePermission("users:create"), userHandler.AdminCreateUser)
	admin.Post("/users/match", authMiddleware.RequirePermission("users:view"), userHandler.MatchUsers)
	admin.Get("/users/export", authMiddleware.RequirePermission("users:view"), userHandler.Export)
	admin.Post("/users/import", authMiddleware.RequirePermission("users:create"), userHandler.Import)
	admin.Get("/users/:id", authMiddleware.RequirePermission("users:view"), userHandler.GetUser)
	admin.Put("/users/:id", authMiddleware.RequirePermission("users:update"), userHandler.AdminUpdateUser)

	// Classification routes
	classifications := admin.Group("/classifications")
	classifications.Post("/", authMiddleware.RequirePermission("classifications:create"), classificationHandler.Create)
	classifications.Get("/", authMiddleware.RequirePermission("classifications:view"), classificationHandler.List)
	classifications.Get("/tree", authMiddleware.RequirePermission("classifications:view"), classificationHandler.GetTree)
	classifications.Get("/tree/stats", authMiddleware.RequirePermission("classifications:view"), classificationHandler.GetTreeWithStats)
	classifications.Get("/children", authMiddleware.RequirePermission("classifications:view"), classificationHandler.GetChildren)
	classifications.Get("/export", authMiddleware.RequirePermission("classifications:view"), classificationHandler.Export)
	classifications.Post("/import", authMiddleware.RequirePermission("classifications:create"), classificationHandler.Import)
	classifications.Get("/:id", authMiddleware.RequirePermission("classifications:view"), classificationHandler.GetByID)
	classifications.Put("/:id", authMiddleware.RequirePermission("classifications:update"), classificationHandler.Update)
	classifications.Delete("/:id", authMiddleware.RequirePermission("classifications:delete"), classificationHandler.Delete)

	// Location routes
	locations := admin.Group("/locations")
	locations.Post("/", authMiddleware.RequirePermission("locations:create"), locationHandler.Create)
	locations.Get("/", authMiddleware.RequirePermission("locations:view"), locationHandler.List)
	locations.Get("/tree", authMiddleware.RequirePermission("locations:view"), locationHandler.GetTree)
	locations.Get("/tree/stats", authMiddleware.RequirePermission("locations:view"), locationHandler.GetTreeWithStats)
	locations.Get("/children", authMiddleware.RequirePermission("locations:view"), locationHandler.GetChildren)
	locations.Get("/by-type", authMiddleware.RequirePermission("locations:view"), locationHandler.GetByType)
	locations.Get("/export", authMiddleware.RequirePermission("locations:view"), locationHandler.Export)
	locations.Post("/import", authMiddleware.RequirePermission("locations:create"), locationHandler.Import)
	locations.Get("/:id", authMiddleware.RequirePermission("locations:view"), locationHandler.GetByID)
	locations.Put("/:id", authMiddleware.RequirePermission("locations:update"), locationHandler.Update)
	locations.Delete("/:id", authMiddleware.RequirePermission("locations:delete"), locationHandler.Delete)

	// Department routes
	departments := admin.Group("/departments")
	departments.Post("/", authMiddleware.RequirePermission("departments:create"), departmentHandler.Create)
	departments.Get("/", authMiddleware.RequirePermission("departments:view"), departmentHandler.List)
	departments.Get("/tree", authMiddleware.RequirePermission("departments:view"), departmentHandler.GetTree)
	departments.Get("/children", authMiddleware.RequirePermission("departments:view"), departmentHandler.GetChildren)
	departments.Post("/match", authMiddleware.RequirePermission("departments:view"), departmentHandler.MatchDepartment)
	departments.Get("/export", authMiddleware.RequirePermission("departments:view"), departmentHandler.Export)
	departments.Post("/import", authMiddleware.RequirePermission("departments:create"), departmentHandler.Import)
	departments.Get("/:id", authMiddleware.RequirePermission("departments:view"), departmentHandler.GetByID)
	departments.Put("/:id", authMiddleware.RequirePermission("departments:update"), departmentHandler.Update)
	departments.Delete("/:id", authMiddleware.RequirePermission("departments:delete"), departmentHandler.Delete)

	// Role routes
	roles := admin.Group("/roles")
	roles.Post("/", authMiddleware.RequirePermission("roles:create"), roleHandler.CreateRole)
	roles.Get("/", authMiddleware.RequirePermission("roles:view"), roleHandler.ListRoles)
	roles.Get("/export", authMiddleware.RequirePermission("roles:view"), roleHandler.Export)
	roles.Post("/import", authMiddleware.RequirePermission("roles:create"), roleHandler.Import)
	roles.Get("/:id", authMiddleware.RequirePermission("roles:view"), roleHandler.GetRole)
	roles.Put("/:id", authMiddleware.RequirePermission("roles:update"), roleHandler.UpdateRole)
	roles.Delete("/:id", authMiddleware.RequirePermission("roles:delete"), roleHandler.DeleteRole)
	roles.Post("/:id/permissions", authMiddleware.RequirePermission("roles:update"), roleHandler.AssignPermissions)

	// Permission routes
	permissions := admin.Group("/permissions")
	permissions.Post("/", authMiddleware.RequirePermission("permissions:create"), roleHandler.CreatePermission)
	permissions.Get("/", authMiddleware.RequirePermission("permissions:view"), roleHandler.ListPermissions)
	permissions.Get("/modules", authMiddleware.RequirePermission("permissions:view"), roleHandler.GetModules)
	permissions.Get("/:id", authMiddleware.RequirePermission("permissions:view"), roleHandler.GetPermission)
	permissions.Put("/:id", authMiddleware.RequirePermission("permissions:update"), roleHandler.UpdatePermission)
	permissions.Delete("/:id", authMiddleware.RequirePermission("permissions:delete"), roleHandler.DeletePermission)

	// Action Log routes
	actionLogs := admin.Group("/action-logs")
	actionLogs.Get("/", authMiddleware.RequirePermission("action-logs:view"), actionLogHandler.ListActionLogs)
	actionLogs.Get("/stats", authMiddleware.RequirePermission("action-logs:view"), actionLogHandler.GetStats)
	actionLogs.Get("/filter-options", authMiddleware.RequirePermission("action-logs:view"), actionLogHandler.GetFilterOptions)
	actionLogs.Get("/user/:id", authMiddleware.RequirePermission("action-logs:view"), actionLogHandler.GetUserActions)
	actionLogs.Get("/:id", authMiddleware.RequirePermission("action-logs:view"), actionLogHandler.GetActionLog)
	actionLogs.Get("/export", authMiddleware.RequirePermission("action-logs:export"), actionLogHandler.ExportActionLogs)
	actionLogs.Delete("/cleanup", authMiddleware.RequirePermission("action-logs:delete"), actionLogHandler.CleanupOldLogs)

	// Call Log routes
	callLogs := admin.Group("/call-logs")
	callLogs.Post("/", authMiddleware.RequirePermission("call-logs:create"), callLogHandler.CreateCallLog)
	callLogs.Get("/", authMiddleware.RequirePermission("call-logs:view"), callLogHandler.ListCallLogs)
	callLogs.Get("/stats", authMiddleware.RequirePermission("call-logs:view"), callLogHandler.GetStats)
	callLogs.Get("/:id", authMiddleware.RequirePermission("call-logs:view"), callLogHandler.GetCallLog)
	callLogs.Put("/:id", authMiddleware.RequirePermission("call-logs:update"), callLogHandler.UpdateCallLog)
	callLogs.Delete("/:id", authMiddleware.RequirePermission("call-logs:delete"), callLogHandler.DeleteCallLog)

	// Workflow routes (with action logging)
	workflows := admin.Group("/workflows", middleware.ActionLogger(middleware.ActionLoggerConfig{
		Enabled:     true,
		LogService:  actionLogService,
		SkipMethods: []string{"GET"}, // Skip read-only operations from logging
	}))
	workflows.Post("/", authMiddleware.RequirePermission("workflows:create"), workflowHandler.CreateWorkflow)
	workflows.Get("/", authMiddleware.RequirePermission("workflows:view"), workflowHandler.ListWorkflows)
	workflows.Get("/deleted", authMiddleware.RequirePermission("workflows:view"), workflowHandler.ListDeletedWorkflows) // List soft-deleted workflows
	workflows.Post("/match", authMiddleware.RequirePermission("workflows:view"), workflowHandler.MatchWorkflow)         // For mobile apps - matches workflow based on criteria
	workflows.Get("/by-classification/:classification_id", authMiddleware.RequirePermission("workflows:view"), workflowHandler.GetWorkflowByClassification)
	workflows.Get("/:id", authMiddleware.RequirePermission("workflows:view"), workflowHandler.GetWorkflow)
	workflows.Put("/:id", authMiddleware.RequirePermission("workflows:update"), workflowHandler.UpdateWorkflow)
	workflows.Delete("/:id", authMiddleware.RequirePermission("workflows:delete"), workflowHandler.DeleteWorkflow)
	workflows.Delete("/:id/permanent", authMiddleware.RequirePermission("workflows:delete"), workflowHandler.PermanentDeleteWorkflow) // Hard delete
	workflows.Post("/:id/restore", authMiddleware.RequirePermission("workflows:update"), workflowHandler.RestoreWorkflow)             // Restore soft-deleted workflow
	workflows.Post("/:id/duplicate", authMiddleware.RequirePermission("workflows:create"), workflowHandler.DuplicateWorkflow)
	workflows.Post("/:id/classifications", authMiddleware.RequirePermission("workflows:update"), workflowHandler.AssignClassifications)
	workflows.Get("/:id/initial-state", authMiddleware.RequirePermission("workflows:view"), workflowHandler.GetInitialState)
	workflows.Get("/:id/export", authMiddleware.RequirePermission("workflows:view"), workflowHandler.ExportWorkflow)
	workflows.Post("/import", authMiddleware.RequirePermission("workflows:create"), workflowHandler.ImportWorkflow)

	// Workflow state routes (with action logging)
	workflows.Post("/:id/states", authMiddleware.RequirePermission("workflows:update"), workflowHandler.CreateState)
	workflows.Get("/:id/states", authMiddleware.RequirePermission("workflows:view"), workflowHandler.ListStates)
	workflows.Put("/:id/states/:state_id", authMiddleware.RequirePermission("workflows:update"), workflowHandler.UpdateState)
	workflows.Delete("/:id/states/:state_id", authMiddleware.RequirePermission("workflows:update"), workflowHandler.DeleteState)
	workflows.Get("/states/:state_id/transitions", authMiddleware.RequirePermission("workflows:view"), workflowHandler.GetTransitionsFromState)

	// Workflow transition routes (with action logging)
	workflows.Post("/:id/transitions", authMiddleware.RequirePermission("workflows:update"), workflowHandler.CreateTransition)
	workflows.Get("/:id/transitions", authMiddleware.RequirePermission("workflows:view"), workflowHandler.ListTransitions)
	workflows.Put("/:id/transitions/:transition_id", authMiddleware.RequirePermission("workflows:update"), workflowHandler.UpdateTransition)
	workflows.Delete("/:id/transitions/:transition_id", authMiddleware.RequirePermission("workflows:update"), workflowHandler.DeleteTransition)

	// Transition configuration routes (with action logging)
	transitions := admin.Group("/transitions", middleware.ActionLogger(middleware.ActionLoggerConfig{
		Enabled:     true,
		LogService:  actionLogService,
		SkipMethods: []string{"GET"}, // Skip read-only operations from logging
	}))
	transitions.Put("/:id/roles", authMiddleware.RequirePermission("workflows:update"), workflowHandler.SetTransitionRoles)
	transitions.Put("/:id/requirements", authMiddleware.RequirePermission("workflows:update"), workflowHandler.SetTransitionRequirements)
	transitions.Put("/:id/actions", authMiddleware.RequirePermission("workflows:update"), workflowHandler.SetTransitionActions)
	transitions.Put("/:id/field-changes", authMiddleware.RequirePermission("workflows:update"), workflowHandler.SetTransitionFieldChanges)

	// Report routes
	reports := admin.Group("/reports")
	reports.Post("/", authMiddleware.RequirePermission("reports:create"), reportHandler.CreateReport)
	reports.Get("/", authMiddleware.RequirePermission("reports:view"), reportHandler.ListReports)
	reports.Get("/data-sources", authMiddleware.RequirePermission("reports:view"), reportHandler.GetDataSources)
	reports.Post("/preview", authMiddleware.RequirePermission("reports:view"), reportHandler.PreviewReport)
	reports.Post("/query", authMiddleware.RequirePermission("reports:view"), reportHandler.QueryReport)
	reports.Post("/export", authMiddleware.RequirePermission("reports:view"), reportHandler.ExportReport)
	reports.Get("/:id", authMiddleware.RequirePermission("reports:view"), reportHandler.GetReport)
	reports.Put("/:id", authMiddleware.RequirePermission("reports:update"), reportHandler.UpdateReport)
	reports.Delete("/:id", authMiddleware.RequirePermission("reports:delete"), reportHandler.DeleteReport)
	reports.Post("/:id/duplicate", authMiddleware.RequirePermission("reports:create"), reportHandler.DuplicateReport)
	reports.Post("/:id/execute", authMiddleware.RequirePermission("reports:view"), reportHandler.ExecuteReport)
	reports.Get("/:id/executions", authMiddleware.RequirePermission("reports:view"), reportHandler.GetExecutionHistory)

	// Report Template routes
	reportTemplates := admin.Group("/report-templates")
	reportTemplates.Post("/", authMiddleware.RequirePermission("reports:create"), reportTemplateHandler.CreateTemplate)
	reportTemplates.Get("/", authMiddleware.RequirePermission("reports:view"), reportTemplateHandler.ListTemplates)
	reportTemplates.Get("/default", authMiddleware.RequirePermission("reports:view"), reportTemplateHandler.GetDefaultTemplate)
	reportTemplates.Post("/preview", authMiddleware.RequirePermission("reports:view"), reportTemplateHandler.PreviewTemplate)
	reportTemplates.Post("/generate", authMiddleware.RequirePermission("reports:view"), reportTemplateHandler.GenerateReport)
	reportTemplates.Get("/:id", authMiddleware.RequirePermission("reports:view"), reportTemplateHandler.GetTemplate)
	reportTemplates.Put("/:id", authMiddleware.RequirePermission("reports:update"), reportTemplateHandler.UpdateTemplate)
	reportTemplates.Delete("/:id", authMiddleware.RequirePermission("reports:delete"), reportTemplateHandler.DeleteTemplate)
	reportTemplates.Post("/:id/duplicate", authMiddleware.RequirePermission("reports:create"), reportTemplateHandler.DuplicateTemplate)
	reportTemplates.Post("/:id/set-default", authMiddleware.RequirePermission("reports:update"), reportTemplateHandler.SetDefaultTemplate)

	// Lookup routes (admin)
	lookups := admin.Group("/lookups")
	lookups.Post("/categories", authMiddleware.RequirePermission("lookups:create"), lookupHandler.CreateCategory)
	lookups.Get("/categories", authMiddleware.RequirePermission("lookups:view"), lookupHandler.ListCategories)
	lookups.Get("/categories/:id", authMiddleware.RequirePermission("lookups:view"), lookupHandler.GetCategoryByID)
	lookups.Put("/categories/:id", authMiddleware.RequirePermission("lookups:update"), lookupHandler.UpdateCategory)
	lookups.Delete("/categories/:id", authMiddleware.RequirePermission("lookups:delete"), lookupHandler.DeleteCategory)
	lookups.Post("/categories/:id/values", authMiddleware.RequirePermission("lookups:create"), lookupHandler.CreateValue)
	lookups.Get("/categories/:id/values", authMiddleware.RequirePermission("lookups:view"), lookupHandler.ListValuesByCategory)
	lookups.Get("/values/:id", authMiddleware.RequirePermission("lookups:view"), lookupHandler.GetValueByID)
	lookups.Put("/values/:id", authMiddleware.RequirePermission("lookups:update"), lookupHandler.UpdateValue)
	lookups.Delete("/values/:id", authMiddleware.RequirePermission("lookups:delete"), lookupHandler.DeleteValue)

	// Public lookup endpoint (by category code) - accessible to authenticated users
	v1.Get("/lookups/:code", authMiddleware.Authenticate(), lookupHandler.GetValuesByCategoryCode)

	// Application Links routes (admin)
	appLinks := admin.Group("/application-links")
	appLinks.Post("/", authMiddleware.RequirePermission("application-links:create"), applicationLinkHandler.CreateLink)
	appLinks.Get("/", authMiddleware.RequirePermission("application-links:view"), applicationLinkHandler.ListLinks)
	appLinks.Get("/:id", authMiddleware.RequirePermission("application-links:view"), applicationLinkHandler.GetLink)
	appLinks.Put("/:id", authMiddleware.RequirePermission("application-links:update"), applicationLinkHandler.UpdateLink)
	appLinks.Delete("/:id", authMiddleware.RequirePermission("application-links:delete"), applicationLinkHandler.DeleteLink)
	appLinks.Post("/:id/upload-image", authMiddleware.RequirePermission("application-links:update"), applicationLinkHandler.UploadImage)

	// Public application links endpoint (active links only) - accessible to authenticated users
	v1.Get("/application-links", authMiddleware.Authenticate(), applicationLinkHandler.ListActiveLinks)

	// Settings routes
	// Public settings endpoint (accessible to everyone for branding)
	v1.Get("/settings", settingsHandler.GetSettings)
	// Admin settings endpoint (requires settings:update permission)
	admin.Put("/settings", authMiddleware.RequirePermission("settings:update"), settingsHandler.UpdateSettings)

	// Call routes (authenticated users)
	calls := v1.Group("/calls", authMiddleware.Authenticate())
	calls.Post("/start", callLogHandler.StartCall)
	calls.Post("/:call_uuid/end", callLogHandler.EndCall)
	calls.Post("/:call_uuid/join", callLogHandler.JoinCall)

	// Call logs routes
	callLogsPublic := v1.Group("/call-logs", authMiddleware.Authenticate())
	callLogsPublic.Get("/sip-info", callLogHandler.GetSipInfo)
	callLogsPublic.Get("/extension/:extension", callLogHandler.GetCallLogsByExtension)

	// ---- TEMPLATE ROUTES ----
	templates := v1.Group("/templates", authMiddleware.Authenticate())
	templates.Post("/", authMiddleware.RequirePermission("templates:create"), templateHandler.Create)
	templates.Get("/", authMiddleware.RequirePermission("templates:read"), templateHandler.List)
	templates.Get("/:id", authMiddleware.RequirePermission("templates:read"), templateHandler.GetByID)
	templates.Put("/:id", authMiddleware.RequirePermission("templates:update"), templateHandler.Update)
	templates.Delete("/:id", authMiddleware.RequirePermission("templates:delete"), templateHandler.Delete)

	// ---- NOTIFICATION ROUTES ----
	notifications := v1.Group("/notifications", authMiddleware.Authenticate())

	// Send notifications
	notifications.Post("/send", authMiddleware.RequirePermission("notifications:send"), notificationHandler.Send)

	// List and view notifications (inbox-like functionality)
	notifications.Get("/", authMiddleware.RequirePermission("notifications:read"), notificationHandler.List)
	notifications.Get("/:id", authMiddleware.RequirePermission("notifications:read"), notificationHandler.Get)

	// Thread operations
	notifications.Post("/:id/reply", authMiddleware.RequirePermission("notifications:send"), notificationHandler.Reply)
	notifications.Get("/threads/:thread_id", authMiddleware.RequirePermission("notifications:read"), notificationHandler.GetThread)

	// Draft management
	notifications.Post("/drafts", authMiddleware.RequirePermission("notifications:create"), notificationHandler.CreateDraft)
	notifications.Put("/drafts/:id", authMiddleware.RequirePermission("notifications:update"), notificationHandler.UpdateDraft)
	notifications.Post("/drafts/:id/send", authMiddleware.RequirePermission("notifications:send"), notificationHandler.SendDraft)

	// Email actions (mark as read, star, move to folder)
	notifications.Patch("/:id/read", authMiddleware.RequirePermission("notifications:update"), notificationHandler.MarkAsRead)
	notifications.Patch("/:id/star", authMiddleware.RequirePermission("notifications:update"), notificationHandler.ToggleStar)
	notifications.Patch("/:id/move", authMiddleware.RequirePermission("notifications:update"), notificationHandler.MoveToCategory)

	// Bulk actions
	notifications.Post("/bulk/move", authMiddleware.RequirePermission("notifications:update"), notificationHandler.BulkMoveToCategory)
	notifications.Post("/bulk/delete", authMiddleware.RequirePermission("notifications:delete"), notificationHandler.BulkDelete)

	// Attachment operations
	notifications.Get("/:id/attachments/:filename", authMiddleware.RequirePermission("notifications:read"), notificationHandler.DownloadAttachment)
	notifications.Get("/:id/attachments/:filename/url", authMiddleware.RequirePermission("notifications:read"), notificationHandler.GetAttachmentURL)

	// Delete (move to trash)
	notifications.Delete("/:id", authMiddleware.RequirePermission("notifications:delete"), notificationHandler.Delete)
	notifications.Delete("/:id/permanent", authMiddleware.RequirePermission("notifications:delete"), notificationHandler.PermanentDelete)

	// OTP Routes

	otp := v1.Group("/otp", authMiddleware.Authenticate())
	otp.Post("/send", authMiddleware.RequirePermission("otp:send"), otpHandler.SendOTP)
	otp.Post("/verify", authMiddleware.RequirePermission("otp:verify"), otpHandler.VerifyOTP)

	go func() {
		addr := fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port)
		log.Printf("Server starting on %s", addr)
		if err := app.Listen(addr); err != nil {
			log.Fatalf("Failed to start server: %v", err)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Println("Shutting down server...")
	if err := app.Shutdown(); err != nil {
		log.Printf("Error during shutdown: %v", err)
	}
	log.Println("Server stopped")
}

func customErrorHandler(c *fiber.Ctx, err error) error {
	code := fiber.StatusInternalServerError
	message := "Internal Server Error"

	if e, ok := err.(*fiber.Error); ok {
		code = e.Code
		message = e.Message
	}

	return c.Status(code).JSON(fiber.Map{
		"success": false,
		"error":   message,
	})
}
