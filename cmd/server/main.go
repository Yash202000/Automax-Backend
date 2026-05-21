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
	"github.com/automax/backend/internal/licensing"
	"github.com/automax/backend/internal/licensing/devseed"
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
	godotenv.Load() // must be before config.Load() so .env values are in the environment
	cfg := config.Load()

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
	ssoJWTManager := utils.NewSSOJWTManager(cfg.SSOPrivateKey, cfg.SSOIssuerURL)
	sessionStore := database.NewSessionStore(redisClient)

	// Initialize repositories
	userRepo := repository.NewUserRepository(db)
	categoryRepo := repository.NewCategoryRepository(db)
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
	rejectionLogRepo := repository.NewRejectionLogRepository(db)
	lookupRepo := repository.NewLookupRepository(db)
	callLogRepo := repository.NewCallLogRepository(db)
	applicationLinkRepo := repository.NewApplicationLinkRepository(db)
	settingsRepo := repository.NewSettingsRepository(db)
	escalationRepo := repository.NewEscalationRepository(db)
	escalationGroupRepo := repository.NewEscalationGroupRepository(db)
	escalationPolicyRepo := repository.NewEscalationPolicyRepository(db)
	callerSentimentRepo := repository.NewCallerSentimentRepo(db)
	goalRepo := repository.NewGoalRepository(db)
	aiQualityFeedbackRepo := repository.NewAIQualityFeedbackRepository(db)
	feedbackTemplateRepo := repository.NewFeedbackTemplateRepository(db)
	commentTemplateRepo := repository.NewCommentTemplateRepository(db)

	// Initialize WebSocket hub and start it
	wsHub := services.NewWSHub()
	go wsHub.Run()
	log.Println("WebSocket hub started")
	notificationTemplateRepo := repository.NewNotificationTemplateRepository(db)
	notificationLogRepo := repository.NewNotificationLogRepository(db)

	// Initialize services
	actionLogService := services.NewActionLogService(actionLogRepo)
	notificationService := services.NewNotificationService(notificationTemplateRepo, notificationLogRepo, userRepo, minioStorage)
	userService := services.NewUserService(userRepo, departmentRepo, jwtManager, sessionStore, minioStorage, cfg, actionLogService, nil, redisClient)
	otpService := services.NewOTPService(redisClient, notificationService, notificationLogRepo, userRepo, userService)

	// Initialize LDAP service
	ldapService, err := services.NewLDAPService(cfg)
	if err != nil {
		log.Fatalf("Failed to initialize LDAP service: %v", err)
	}
	defer ldapService.Close()

	callLogService := services.NewCallLogService(callLogRepo, userRepo, minioStorage)
	workflowService := services.NewWorkflowService(workflowRepo, roleRepo, departmentRepo, classificationRepo, userRepo, db)
	incidentService := services.NewIncidentService(incidentRepo, incidentMergeRepo, workflowRepo, userRepo, departmentRepo, classificationRepo, rejectionLogRepo, roleRepo, minioStorage, db, wsHub)
	incidentMergeService := services.NewIncidentMergeService(incidentMergeRepo, incidentRepo, workflowRepo, roleRepo, locationRepo, classificationRepo, db, wsHub)
	reportService := services.NewReportService(reportRepo, rejectionLogRepo, locationRepo, classificationRepo, workflowRepo)
	reportTemplateService := services.NewReportTemplateService(reportTemplateRepo, reportRepo)
	applicationLinkService := services.NewApplicationLinkService(applicationLinkRepo)
	settingsService := services.NewSettingsService(settingsRepo)
	presenceService := services.NewPresenceService(redisClient)
	escalationPolicyService := services.NewEscalationPolicyService(escalationPolicyRepo, userRepo)
	escalationService := services.NewEscalationService(escalationRepo, escalationPolicyRepo, incidentRepo, workflowRepo, userRepo, notificationService)
	escalationService.SetPolicyService(escalationPolicyService)
	escalationService.SetFrontendURL(cfg.FrontendURL)
	escalationGroupService := services.NewEscalationGroupService(escalationGroupRepo, incidentRepo, userRepo, notificationService, cfg.Escalation)
	escalationGroupService.SetFrontendURL(cfg.FrontendURL)
	escalationGroupService.SetPolicyService(escalationPolicyService)
	fcmService := services.NewFCMService(repository.NewDeviceTokenRepository(db), notificationLogRepo)
	callerSentimentService := services.NewCallerSentimentService(callerSentimentRepo)
	feedbackTemplateService := services.NewFeedbackTemplateService(feedbackTemplateRepo)
	commentTemplateService := services.NewCommentTemplateService(commentTemplateRepo)

	// Goal management services
	var documentaClient storage.DocumentaClient
	if cfg.Documenta.Enabled {
		documentaClient = storage.NewHTTPDocumentaClient(cfg.Documenta, redisClient)
		log.Println("Documenta HTTP client enabled")
	} else {
		documentaClient = storage.NewStubDocumentaClient()
		log.Println("Documenta stub client (DOCUMENTA_ENABLED=false)")
	}
	goalService := services.NewGoalService(goalRepo, workflowRepo, userRepo, departmentRepo, documentaClient, notificationService, actionLogService, cfg, wsHub)
	documentService := services.NewDocumentService(documentaClient, cfg.Documenta)
	documentAuthzService := services.NewDocumentAuthzService(db, documentaClient, cfg.Documenta)

	// Ready-to-Close service
	readyToCloseRepo := repository.NewReadyToCloseRepository(db)
	readyToCloseService := services.NewReadyToCloseService(readyToCloseRepo, incidentRepo, workflowRepo, notificationService, cfg.ReadyToClose, db)

	// Wire ReadyToCloseService into IncidentService (post-construction to avoid circular deps)
	incidentService.SetReadyToCloseService(readyToCloseService)
	incidentService.SetNotificationService(notificationService)
	incidentService.SetUserService(userService)
	incidentService.SetFCMService(fcmService)
	incidentService.SetActionExecutor(services.NewActionExecutor(incidentRepo, userRepo, notificationService))

	// External Integration
	integrationRepo := repository.NewIntegrationRepository(db)
	integrationService := services.NewIntegrationService(integrationRepo, cfg.Integration.SecretsKey)
	integrationExecutor := services.NewIntegrationExecutor(integrationRepo, integrationService)
	incidentService.SetIntegrationExecutor(integrationExecutor)

	// Initialize and start SLA Monitor (checks every 5 minutes)
	slaMonitor := services.NewSLAMonitor(incidentRepo, escalationService, escalationGroupService, readyToCloseService, 5*time.Minute)
	ctx := context.Background()
	slaMonitor.Start(ctx)
	defer slaMonitor.Stop()

	// Initialize and start AI Quality Monitor
	aiQualityMonitor := services.NewAIQualityMonitor(aiQualityFeedbackRepo, incidentRepo, minioStorage, cfg.AIQuality)
	aiQualityMonitor.Start(ctx)
	defer aiQualityMonitor.Stop()
	// Initialize validator
	validate := validator.New()

	// Initialize handlers
	userHandler := handlers.NewUserHandler(userService, minioStorage, redisClient, cfg)
	healthHandler := handlers.NewHealthHandler()

	// Initialize LDAP handler
	ldapHandler := handlers.NewLDAPHandler(ldapService, userService, userRepo, jwtManager, sessionStore, cfg)

	categoryService := services.NewCategoryService(categoryRepo)
	categoryHandler := handlers.NewCategoryHandler(categoryService)
	classificationHandler := handlers.NewClassificationHandler(classificationRepo)
	locationHandler := handlers.NewLocationHandler(locationRepo)
	departmentHandler := handlers.NewDepartmentHandler(departmentRepo)
	roleHandler := handlers.NewRoleHandler(roleRepo, permissionRepo)
	actionLogHandler := handlers.NewActionLogHandler(actionLogService, validate)
	callLogHandler := handlers.NewCallLogHandler(callLogService, validate, userService, minioStorage)
	workflowHandler := handlers.NewWorkflowHandler(workflowService, actionLogService)
	incidentHandler := handlers.NewIncidentHandler(incidentService, userService, userRepo, incidentRepo, locationRepo, classificationRepo, minioStorage, presenceService)
	incidentHandler.SetReadyToCloseService(readyToCloseService)
	incidentMergeHandler := handlers.NewIncidentMergeHandler(incidentMergeService, userRepo)
	websocketHandler := handlers.NewWebSocketHandler(wsHub)
	reportHandler := handlers.NewReportHandler(reportService)
	reportTemplateHandler := handlers.NewReportTemplateHandler(reportTemplateService)
	lookupHandler := handlers.NewLookupHandler(lookupRepo)
	applicationLinkHandler := handlers.NewApplicationLinkHandler(applicationLinkService, minioStorage)
	settingsHandler := handlers.NewSettingsHandler(settingsService)
	jwksHandler := handlers.NewJWKSHandler(ssoJWTManager)
	ssoHandler := handlers.NewSSOHandler(ssoJWTManager, jwtManager, sessionStore, userRepo, applicationLinkRepo, userService, otpService, cfg.SSOFrontendURL, cfg.NafathAPIBaseURL)
	notificationTemplateService := services.NewNotificationTemplateService(notificationTemplateRepo, db)
	notificationHandler := handlers.NewNotificationHandler(notificationService, minioStorage)
	templateHandler := handlers.NewNotificationTemplateHandler(notificationTemplateService)
	attachmentHandler := handlers.NewAttachmentHandler(incidentService, notificationService, minioStorage)
	otpHandler := handlers.NewOTPHandler(otpService, userService)
	escalationHandler := handlers.NewEscalationHandler(escalationService)
	escalationGroupHandler := handlers.NewEscalationGroupHandler(escalationGroupService)
	escalationPolicyHandler := handlers.NewEscalationPolicyHandler(escalationPolicyService, userRepo, departmentRepo)
	feedbackTemplateHandler := handlers.NewFeedbackTemplateHandler(feedbackTemplateService)
	commentTemplateHandler := handlers.NewCommentTemplateHandler(commentTemplateService)
	rejectionLogHandler := handlers.NewRejectionLogHandler(rejectionLogRepo)
	incidentFeedbackHandler := handlers.NewIncidentFeedbackHandler(incidentRepo)
	aiQualityFeedbackHandler := handlers.NewAIQualityFeedbackHandler(aiQualityFeedbackRepo, incidentService, userRepo)
	fcmHandler := handlers.NewFCMHandler(fcmService)
	sentimentHandler := handlers.NewCallerSentimentHandler(callerSentimentService)
	goalHandler := handlers.NewGoalHandler(goalService, actionLogService)
	goalAnalyticsService := services.NewGoalAnalyticsService(db)
	goalAnalyticsHandler := handlers.NewGoalAnalyticsHandler(goalAnalyticsService)
	reviewRepo := repository.NewReviewRepository(db)
	reviewService := services.NewReviewService(reviewRepo, goalRepo)
	reviewHandler := handlers.NewReviewHandler(reviewService)
	documentHandler := handlers.NewDocumentHandler(documentService, documentAuthzService, cfg.Documenta.WorkspaceName)

	// Goal Templates
	goalTemplateRepo := repository.NewGoalTemplateRepository(db)
	goalTemplateService := services.NewGoalTemplateService(goalTemplateRepo)
	goalTemplateHandler := handlers.NewGoalTemplateHandler(goalTemplateService)

	// Integration handler
	integrationHandler := handlers.NewIntegrationHandler(integrationService, integrationExecutor, integrationRepo, incidentRepo)
	webhookHandler := handlers.NewWebhookHandler(integrationService, incidentService)

	// License management
	licenseRepo := repository.NewLicenseRepository(db)
	licenseService := services.NewLicenseService(licenseRepo, cfg.License)
	if err := licenseService.LoadFromDB(ctx); err != nil {
		log.Printf("Warning: Failed to load license from DB: %v", err)
	}
	// Dev-mode auto-seeder: installs a full-scope license when APP_ENV=development
	// and no license is active. No-op in production.
	if err := devseed.SeedDevLicenseIfNeeded(ctx, cfg, licenseService); err != nil {
		log.Printf("Warning: dev license seeder failed: %v", err)
	}
	licenseHandler := handlers.NewLicenseHandler(licenseService)

	// Wire license service into user service for user limit enforcement
	userService.SetLicenseService(licenseService, licenseRepo)

	// Initialize EPM handler
	epmHandler := handlers.NewEPMHandler(userRepo, jwtManager, sessionStore)
	epmIncidentHandler := handlers.NewEPMIncidentHandler(userRepo, locationRepo, classificationRepo, incidentRepo, workflowRepo, lookupRepo, jwtManager, sessionStore, minioStorage, db)

	// Initialize middleware
	authMiddleware := middleware.NewAuthMiddleware(jwtManager, sessionStore, userRepo)
	licenseMiddleware := middleware.NewLicenseMiddleware(licenseService)
	loginRateLimitMiddleware := middleware.LoginRateLimitMiddleware(cfg, redisClient, userService)

	app := fiber.New(fiber.Config{
		AppName:      "Automax Backend",
		ErrorHandler: customErrorHandler,
		BodyLimit:    100 * 1024 * 1024, // 100MB max body size
	})

	app.Use(recover.New())
	app.Use(logger.New(logger.Config{
		Format: "[${time}] ${status} - ${latency} ${method} ${path}\n",
	}))
	// When AllowOrigins is "*", credentials cannot be sent (browser spec).
	// In that case we allow all origins without the credentials flag.
	corsAllowCredentials := cfg.Server.AllowOrigins != "*"
	app.Use(cors.New(cors.Config{
		AllowOrigins:     cfg.Server.AllowOrigins,
		AllowMethods:     "GET,POST,PUT,DELETE,PATCH,OPTIONS",
		AllowHeaders:     "Origin,Content-Type,Accept,Authorization",
		AllowCredentials: corsAllowCredentials,
	}))

	app.Use(middleware.RequestContext())
	app.Use(middleware.ValidationMiddleware())

	// Public SSO routes (no auth required — browser redirect landing points)
	app.Get("/.well-known/jwks.json", jwksHandler.GetJWKS)
	app.Get("/sso/callback", ssoHandler.Callback)

	// EPM external API routes
	app.Post("/Momra/API/EPM/Login", epmHandler.Login)
	app.Post("/Momra/API/EPM/InsertIncidents", epmIncidentHandler.InsertIncidents)
	app.Get("/Momra/API/EPM/GetMomraIncidentStatusDetails", epmIncidentHandler.GetMomraIncidentStatusDetails)

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

	// Goal WebSocket route for real-time goal updates
	// Query params: goal_id, user_id, token (for auth)
	v1.Get("/ws/goal", websocketHandler.GoalWebSocketUpgrader())

	// Goal broadcast WebSocket route for goal list updates
	// Query params: token (for auth)
	v1.Get("/ws/goal/broadcast", websocketHandler.GoalBroadcastWebSocketUpgrader())

	// WebSocket connection statistics endpoint
	v1.Get("/ws/stats", authMiddleware.RequirePermission("incidents:view"), websocketHandler.GetConnectionStats)

	// Webhook routes (no authentication required - external services)
	webhooks := v1.Group("/webhooks")
	webhooks.Post("/sendgrid/inbound", notificationHandler.SendGridInboundWebhook)
	webhooks.Post("/automax-callback", webhookHandler.HandleAutomaxCallback)

	ivr := v1.Group("/ivr/incident")
	// Public: validates signed URL + last 6 digits, returns incident + session token
	ivr.Get("/sms-link/:id", incidentHandler.FindByIDWithLast6DigitValidation)
	// Protected: require valid IVR session token issued by the GET route above
	// ivr.Put("/sms-link/update/:id", authMiddleware.ValidateIvrSmsToken(), incidentHandler.UpdateIncidentViaIvrSms)
	// ivr.Post("/sms-link/attachment/:id", authMiddleware.ValidateIvrSmsToken(), incidentHandler.UploadAttachmentIvrSms)

	// Auth routes
	auth := v1.Group("/auth")
	auth.Post("/register", userHandler.Register)
	auth.Post("/sso/register", ssoHandler.SSORegister)
	auth.Post("/login", loginRateLimitMiddleware, userHandler.Login)
	auth.Post("/sso/login", ssoHandler.SSOLogin)
	auth.Get("/sso/validate", ssoHandler.SSOValidate)
	auth.Post("/refresh", userHandler.RefreshToken)
	auth.Post("/logout", authMiddleware.Authenticate(), userHandler.Logout)
	auth.Post("/unblock-user", authMiddleware.Authenticate(), authMiddleware.RequirePermission("users:unblock"), userHandler.UnblockUser)
	auth.Post("/forgot-password", userHandler.ForgotPassword)
	auth.Post("/verify-reset-otp", userHandler.VerifyOTPForReset)
	auth.Post("/reset-password", userHandler.ResetPassword)

	// LDAP routes (public - for LDAP authentication)
	ldap := v1.Group("/ldap")
	ldap.Post("/login", ldapHandler.Login)
	ldap.Post("/test", authMiddleware.Authenticate(), authMiddleware.RequirePermission("admin:ldap"), ldapHandler.TestConnection)
	ldap.Post("/search", authMiddleware.Authenticate(), authMiddleware.RequirePermission("users:view"), ldapHandler.SearchUser)
	ldap.Post("/sync", authMiddleware.Authenticate(), authMiddleware.RequirePermission("users:update"), ldapHandler.SyncUser)
	ldap.Get("/status", authMiddleware.Authenticate(), authMiddleware.RequirePermission("admin:ldap"), ldapHandler.GetLDAPStatus)

	// SSO routes
	sso := v1.Group("/sso", authMiddleware.Authenticate(), licenseMiddleware.RequireLicensedFeature(string(licensing.FeatureSSO)))
	sso.Post("/launch", ssoHandler.Launch)

	// User routes
	users := v1.Group("/users")
	users.Get("/me", authMiddleware.Authenticate(), userHandler.GetProfile)
	users.Put("/me", authMiddleware.Authenticate(), userHandler.UpdateProfile)
	users.Post("/me/avatar", authMiddleware.Authenticate(), userHandler.UploadAvatar)
	users.Put("/me/password", authMiddleware.Authenticate(), userHandler.ChangePassword)
	users.Delete("/me", authMiddleware.Authenticate(), userHandler.DeleteAccount)
	users.Put("/:userExtID/status", userHandler.UpdateUserCallStatus)
	users.Put("/:userID/password", authMiddleware.Authenticate(), authMiddleware.RequirePermission("users:update"), userHandler.AdminResetPassword)

	// Incident routes (authenticated users)
	incidents := v1.Group("/incidents", authMiddleware.Authenticate(), licenseMiddleware.RequireLicensedFeature(string(licensing.FeatureIncidents)))
	incidents.Post("/", authMiddleware.RequirePermission("incidents:create"), incidentHandler.CreateIncident)
	incidents.Get("/", authMiddleware.RequirePermission("incidents:view"), incidentHandler.ListIncidents)
	incidents.Get("/stats", authMiddleware.RequirePermission("incidents:view"), incidentHandler.GetStats)
	incidents.Get("/stats/v2", authMiddleware.RequirePermission("incidents:view"), incidentHandler.GetStatsV2)
	incidents.Get("/priority-counts", authMiddleware.RequirePermission("incidents:view"), incidentHandler.GetPriorityCounts)
	incidents.Get("/my-assigned", authMiddleware.RequirePermission("incidents:view"), incidentHandler.GetMyAssigned)
	incidents.Get("/my-reported", authMiddleware.RequirePermission("incidents:view"), incidentHandler.GetMyReported)
	incidents.Get("/sla-breached", authMiddleware.RequirePermission("incidents:view"), incidentHandler.GetSLABreached)
	incidents.Get("/ready-to-close/duration-options", authMiddleware.RequirePermission("incidents:view"), incidentHandler.GetReadyToCloseDurationOptions)
	incidents.Post("/bulk/convert-to-request", authMiddleware.RequirePermission("incidents:update"), incidentHandler.BulkConvertToRequest)
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
	incidents.Get("/:id/feedbacks", authMiddleware.RequirePermission("incidents:view"), incidentHandler.ListFeedbacks)
	incidents.Put("/:id/comments/:comment_id", authMiddleware.RequirePermission("incidents:comment"), incidentHandler.UpdateComment)
	incidents.Delete("/:id/comments/:comment_id", authMiddleware.RequirePermission("incidents:comment"), incidentHandler.DeleteComment)
	incidents.Post("/:id/attachments", authMiddleware.RequirePermission("incidents:update"), incidentHandler.UploadAttachment)
	incidents.Get("/:id/attachments", authMiddleware.RequirePermission("incidents:view"), incidentHandler.ListAttachments)
	incidents.Delete("/:id/attachments/:attachment_id", authMiddleware.RequirePermission("incidents:update"), incidentHandler.DeleteAttachment)
	incidents.Put("/:id/assign", authMiddleware.RequirePermission("incidents:assign"), incidentHandler.AssignIncident)
	incidents.Get("/:id/revisions", authMiddleware.RequirePermission("incidents:view"), incidentHandler.ListRevisions)
	incidents.Get("/:id/rejection-logs", authMiddleware.RequirePermission("incidents:view"), rejectionLogHandler.GetByIncident)
	incidents.Get("/:id/ai-quality", authMiddleware.RequirePermission("incidents:view"), aiQualityFeedbackHandler.GetByIncident)
	incidents.Post("/:id/reopen", authMiddleware.RequirePermission("incidents:transition"), aiQualityFeedbackHandler.ReopenIncident)
	incidents.Post("/:id/request-info", authMiddleware.RequirePermission("incidents:request-info"), incidentHandler.RequestCitizenInfo)

	aiQuality := v1.Group("/ai-quality", authMiddleware.Authenticate(), licenseMiddleware.RequireLicensedFeature(string(licensing.FeatureAIQuality)))
	aiQuality.Get("/", authMiddleware.RequirePermission("incidents:view"), aiQualityFeedbackHandler.GetAll)

	feedback := v1.Group("/feedback", authMiddleware.Authenticate())
	feedback.Get("/", authMiddleware.RequirePermission("incidents:view"), incidentFeedbackHandler.ListAllFeedback)
	feedback.Post("/:id", authMiddleware.RequirePermission("incidents:update"), incidentFeedbackHandler.CreateFeedback)
	feedback.Get("/:id", authMiddleware.RequirePermission("incidents:view"), incidentFeedbackHandler.ListFeedback)

	// Closed incident editing (requires special permission)
	incidents.Patch("/:id/closed-summary", authMiddleware.RequirePermission("incidents:edit-closed"), incidentHandler.UpdateClosedIncidentSummary)

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
	complaints := v1.Group("/complaints", authMiddleware.Authenticate(), licenseMiddleware.RequireLicensedFeature(string(licensing.FeatureComplaints)))
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
	complaints.Get("/:id/feedbacks", authMiddleware.RequirePermission("complaints:view"), incidentHandler.ListFeedbacks)
	complaints.Put("/:id/comments/:comment_id", authMiddleware.RequirePermission("complaints:comment"), incidentHandler.UpdateComment)
	complaints.Delete("/:id/comments/:comment_id", authMiddleware.RequirePermission("complaints:comment"), incidentHandler.DeleteComment)
	complaints.Post("/:id/attachments", authMiddleware.RequirePermission("complaints:update"), incidentHandler.UploadAttachment)
	complaints.Get("/:id/attachments", authMiddleware.RequirePermission("complaints:view"), incidentHandler.ListAttachments)
	complaints.Delete("/:id/attachments/:attachment_id", authMiddleware.RequirePermission("complaints:update"), incidentHandler.DeleteAttachment)
	complaints.Post("/:id/evaluate", authMiddleware.RequirePermission("complaints:update"), incidentHandler.IncrementEvaluation)
	complaints.Get("/:id/revisions", authMiddleware.RequirePermission("complaints:view"), incidentHandler.ListRevisions)

	// Query routes (authenticated users)
	queries := v1.Group("/queries", authMiddleware.Authenticate(), licenseMiddleware.RequireLicensedFeature(string(licensing.FeatureQueries)))
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
	admin := v1.Group("/admin", authMiddleware.Authenticate(), licenseMiddleware.LicenseGate())

	// User management (action logging handled by service layer for detailed old/new values)
	usersGroup := admin.Group("/users")
	usersGroup.Get("/", authMiddleware.RequirePermission("users:view"), userHandler.ListUsers)
	usersGroup.Post("/", authMiddleware.RequirePermission("users:create"), userHandler.AdminCreateUser)
	usersGroup.Post("/match", authMiddleware.RequirePermission("users:view"), userHandler.MatchUsers)
	usersGroup.Get("/export", authMiddleware.RequirePermission("users:view"), userHandler.Export)
	usersGroup.Post("/import", authMiddleware.RequirePermission("users:create"), userHandler.Import)
	usersGroup.Get("/:id", authMiddleware.RequirePermission("users:view"), userHandler.GetUser)
	usersGroup.Put("/:id", authMiddleware.RequirePermission("users:update"), userHandler.AdminUpdateUser)
	usersGroup.Delete("/:id", authMiddleware.RequirePermission("users:delete"), userHandler.AdminDeleteUser)

	// Category routes (foundational admin taxonomy used by goals — no license gate)
	categories := admin.Group("/categories", middleware.ActionLogger(middleware.ActionLoggerConfig{
		Enabled:     true,
		LogService:  actionLogService,
		SkipMethods: []string{"GET"},
	}))
	categories.Post("/", authMiddleware.RequirePermission("categories:create"), categoryHandler.Create)
	categories.Get("/", authMiddleware.RequirePermission("categories:view"), categoryHandler.List)
	categories.Get("/tree", authMiddleware.RequirePermission("categories:view"), categoryHandler.GetTree)
	categories.Get("/:id", authMiddleware.RequirePermission("categories:view"), categoryHandler.GetByID)
	categories.Put("/:id", authMiddleware.RequirePermission("categories:update"), categoryHandler.Update)
	categories.Delete("/:id", authMiddleware.RequirePermission("categories:delete"), categoryHandler.Delete)

	// Classification routes (with action logging)
	classifications := admin.Group("/classifications", middleware.ActionLogger(middleware.ActionLoggerConfig{
		Enabled:     true,
		LogService:  actionLogService,
		SkipMethods: []string{"GET"},
	}))
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
	// Classification Criticality routes
	classifications.Get("/:classification_id/criticalities", authMiddleware.RequirePermission("classifications:view"), classificationHandler.GetCriticalities)
	classifications.Post("/:classification_id/criticalities", authMiddleware.RequirePermission("classifications:create"), classificationHandler.CreateCriticality)
	classifications.Get("/criticalities/:id", authMiddleware.RequirePermission("classifications:view"), classificationHandler.GetCriticalityByID)
	classifications.Put("/criticalities/:id", authMiddleware.RequirePermission("classifications:update"), classificationHandler.UpdateCriticality)
	classifications.Delete("/criticalities/:id", authMiddleware.RequirePermission("classifications:delete"), classificationHandler.DeleteCriticality)

	// Location routes (with action logging)
	locations := admin.Group("/locations", middleware.ActionLogger(middleware.ActionLoggerConfig{
		Enabled:     true,
		LogService:  actionLogService,
		SkipMethods: []string{"GET"},
	}))
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

	// Department routes (with action logging)
	departments := admin.Group("/departments", middleware.ActionLogger(middleware.ActionLoggerConfig{
		Enabled:     true,
		LogService:  actionLogService,
		SkipMethods: []string{"GET"},
	}))
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

	// Role routes (with action logging)
	roles := admin.Group("/roles", middleware.ActionLogger(middleware.ActionLoggerConfig{
		Enabled:     true,
		LogService:  actionLogService,
		SkipMethods: []string{"GET"},
	}))
	roles.Post("/", authMiddleware.RequirePermission("roles:create"), roleHandler.CreateRole)
	roles.Get("/", authMiddleware.RequirePermission("roles:view"), roleHandler.ListRoles)
	roles.Get("/export", authMiddleware.RequirePermission("roles:view"), roleHandler.Export)
	roles.Post("/import", authMiddleware.RequirePermission("roles:create"), roleHandler.Import)
	roles.Get("/:id", authMiddleware.RequirePermission("roles:view"), roleHandler.GetRole)
	roles.Put("/:id", authMiddleware.RequirePermission("roles:update"), roleHandler.UpdateRole)
	roles.Delete("/:id", authMiddleware.RequirePermission("roles:delete"), roleHandler.DeleteRole)
	roles.Post("/:id/permissions", authMiddleware.RequirePermission("roles:update"), roleHandler.AssignPermissions)

	// Permission routes (with action logging)
	permissions := admin.Group("/permissions", middleware.ActionLogger(middleware.ActionLoggerConfig{
		Enabled:     true,
		LogService:  actionLogService,
		SkipMethods: []string{"GET"},
	}))
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
	callLogs := admin.Group("/call-logs", licenseMiddleware.RequireLicensedFeature(string(licensing.FeatureCallCentre)))
	callLogs.Post("/", authMiddleware.RequirePermission("call-logs:create"), callLogHandler.CreateCallLog)
	callLogs.Get("/", authMiddleware.RequirePermission("call-logs:view"), callLogHandler.ListCallLogs)
	callLogs.Get("/stats", authMiddleware.RequirePermission("call-logs:view"), callLogHandler.GetStats)
	callLogs.Get("/:id", authMiddleware.RequirePermission("call-logs:view"), callLogHandler.GetCallLog)
	callLogs.Put("/:id", authMiddleware.RequirePermission("call-logs:update"), callLogHandler.UpdateCallLog)
	callLogs.Delete("/:id", authMiddleware.RequirePermission("call-logs:delete"), callLogHandler.DeleteCallLog)

	// Workflow routes (with action logging)
	workflows := admin.Group("/workflows", licenseMiddleware.RequireLicensedFeature(string(licensing.FeatureWorkflows)), middleware.ActionLogger(middleware.ActionLoggerConfig{
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
	workflows.Get("/:id/initial-state/matching-users", authMiddleware.RequirePermission("workflows:view"), workflowHandler.GetInitialStateMatchingUsers)
	workflows.Get("/:id/export", authMiddleware.RequirePermission("workflows:view"), workflowHandler.ExportWorkflow)
	workflows.Post("/import", authMiddleware.RequirePermission("workflows:create"), workflowHandler.ImportWorkflow)

	// Workflow state routes (with action logging)
	workflows.Post("/:id/states", authMiddleware.RequirePermission("workflows:update"), workflowHandler.CreateState)
	workflows.Get("/:id/states", authMiddleware.RequirePermission("workflows:view"), workflowHandler.ListStates)
	workflows.Put("/:id/states/:state_id", authMiddleware.RequirePermission("workflows:update"), workflowHandler.UpdateState)
	workflows.Delete("/:id/states/:state_id", authMiddleware.RequirePermission("workflows:update"), workflowHandler.DeleteState)
	workflows.Get("/states/:state_id/transitions", authMiddleware.RequirePermission("workflows:view"), workflowHandler.GetTransitionsToState)

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

	// Comment Template routes (with action logging)
	commentTemplates := admin.Group("/comment-templates", middleware.ActionLogger(middleware.ActionLoggerConfig{
		Enabled:     true,
		LogService:  actionLogService,
		SkipMethods: []string{"GET"},
	}))
	commentTemplates.Post("/", authMiddleware.RequirePermission("workflows:update"), commentTemplateHandler.Create)
	commentTemplates.Get("/", authMiddleware.RequirePermission("workflows:view"), commentTemplateHandler.List)
	commentTemplates.Get("/workflow-transition/:workflowTransitionId", authMiddleware.RequirePermission("workflows:view"), commentTemplateHandler.GetByWorkflowTransitionID)
	commentTemplates.Get("/:id", authMiddleware.RequirePermission("workflows:view"), commentTemplateHandler.GetByID)
	commentTemplates.Put("/:id", authMiddleware.RequirePermission("workflows:update"), commentTemplateHandler.Update)
	commentTemplates.Delete("/:id", authMiddleware.RequirePermission("workflows:update"), commentTemplateHandler.Delete)

	feedbackTemplates := admin.Group("/feedback-templates", middleware.ActionLogger(middleware.ActionLoggerConfig{
		Enabled:     true,
		LogService:  actionLogService,
		SkipMethods: []string{"GET"},
	}))
	feedbackTemplates.Post("/", authMiddleware.RequirePermission("workflows:update"), feedbackTemplateHandler.Create)
	feedbackTemplates.Get("/", authMiddleware.RequirePermission("workflows:view"), feedbackTemplateHandler.List)
	feedbackTemplates.Get("/workflow-transition/:workflowTransitionId", authMiddleware.RequirePermission("workflows:view"), feedbackTemplateHandler.GetByWorkflowTransitionID)
	feedbackTemplates.Get("/:id", authMiddleware.RequirePermission("workflows:view"), feedbackTemplateHandler.GetByID)
	feedbackTemplates.Put("/:id", authMiddleware.RequirePermission("workflows:update"), feedbackTemplateHandler.Update)
	feedbackTemplates.Delete("/:id", authMiddleware.RequirePermission("workflows:update"), feedbackTemplateHandler.Delete)

	// Notification Template routes (admin managed: email/SMS templates with bilingual support)
	notifTemplates := admin.Group("/notification-templates", middleware.ActionLogger(middleware.ActionLoggerConfig{
		Enabled:     true,
		LogService:  actionLogService,
		SkipMethods: []string{"GET"},
	}))
	notifTemplates.Post("/", authMiddleware.RequirePermission("templates:create"), templateHandler.Create)
	notifTemplates.Get("/by-code/:code", authMiddleware.RequirePermission("templates:read"), templateHandler.GetByCode)
	notifTemplates.Get("/by-transition/:transitionId", authMiddleware.RequirePermission("templates:read"), templateHandler.GetByTransition)
	notifTemplates.Get("/available-variables", authMiddleware.RequirePermission("templates:read"), templateHandler.GetAvailableVariables)
	notifTemplates.Get("/", authMiddleware.RequirePermission("templates:read"), templateHandler.List)
	notifTemplates.Get("/:id", authMiddleware.RequirePermission("templates:read"), templateHandler.GetByID)
	notifTemplates.Put("/:id", authMiddleware.RequirePermission("templates:update"), templateHandler.Update)
	notifTemplates.Patch("/:id/toggle", authMiddleware.RequirePermission("templates:update"), templateHandler.Toggle)
	notifTemplates.Delete("/:id", authMiddleware.RequirePermission("templates:delete"), templateHandler.Delete)

	// External Integration routes
	integrationVars := admin.Group("/integration-variables")
	integrationVars.Post("/", authMiddleware.RequirePermission("admin:integration"), integrationHandler.CreateVariable)
	integrationVars.Get("/", authMiddleware.RequirePermission("admin:integration"), integrationHandler.ListVariables)
	integrationVars.Delete("/:id", authMiddleware.RequirePermission("admin:integration"), integrationHandler.DeleteVariable)

	integrationScripts := admin.Group("/integration-scripts")
	integrationScripts.Post("/", authMiddleware.RequirePermission("admin:integration"), integrationHandler.CreateScript)
	integrationScripts.Get("/", authMiddleware.RequirePermission("admin:integration"), integrationHandler.ListScripts)
	integrationScripts.Get("/:id", authMiddleware.RequirePermission("admin:integration"), integrationHandler.GetScript)
	integrationScripts.Put("/:id", authMiddleware.RequirePermission("admin:integration"), integrationHandler.UpdateScript)
	integrationScripts.Delete("/:id", authMiddleware.RequirePermission("admin:integration"), integrationHandler.DeleteScript)
	integrationScripts.Post("/:id/test", authMiddleware.RequirePermission("admin:integration"), integrationHandler.TestScript)
	integrationScripts.Get("/:id/logs", authMiddleware.RequirePermission("admin:integration"), integrationHandler.ListLogsByScript)

	stateIntegrations := admin.Group("/workflow-states")
	stateIntegrations.Post("/:stateId/triggers", authMiddleware.RequirePermission("admin:integration"), integrationHandler.CreateStateTrigger)
	stateIntegrations.Get("/:stateId/triggers", authMiddleware.RequirePermission("admin:integration"), integrationHandler.ListStateTriggers)
	stateIntegrations.Put("/:stateId/triggers/:id", authMiddleware.RequirePermission("admin:integration"), integrationHandler.UpdateStateTrigger)
	stateIntegrations.Delete("/:stateId/triggers/:id", authMiddleware.RequirePermission("admin:integration"), integrationHandler.DeleteStateTrigger)

	transitionIntegrations := admin.Group("/workflow-transitions")
	transitionIntegrations.Post("/:transitionId/triggers", authMiddleware.RequirePermission("admin:integration"), integrationHandler.CreateTransitionTrigger)
	transitionIntegrations.Get("/:transitionId/triggers", authMiddleware.RequirePermission("admin:integration"), integrationHandler.ListTransitionTriggers)
	transitionIntegrations.Put("/:transitionId/triggers/:id", authMiddleware.RequirePermission("admin:integration"), integrationHandler.UpdateTransitionTrigger)
	transitionIntegrations.Delete("/:transitionId/triggers/:id", authMiddleware.RequirePermission("admin:integration"), integrationHandler.DeleteTransitionTrigger)

	// Per-incident integration log + bridge viewer (accessible to incident handlers)
	v1.Get("/incidents/:incidentId/integration-logs", authMiddleware.Authenticate(), authMiddleware.RequirePermission("incidents:view"), integrationHandler.ListLogsByIncident)
	v1.Get("/incidents/:incidentId/bridges", authMiddleware.Authenticate(), authMiddleware.RequirePermission("incidents:view"), integrationHandler.ListBridgesByIncident)

	// Webhook callback config management
	webhookConfigs := admin.Group("/webhook-configs")
	webhookConfigs.Post("/", authMiddleware.RequirePermission("admin:integration"), integrationHandler.CreateWebhookConfig)
	webhookConfigs.Get("/", authMiddleware.RequirePermission("admin:integration"), integrationHandler.ListWebhookConfigs)
	webhookConfigs.Put("/:id", authMiddleware.RequirePermission("admin:integration"), integrationHandler.UpdateWebhookConfig)
	webhookConfigs.Delete("/:id", authMiddleware.RequirePermission("admin:integration"), integrationHandler.DeleteWebhookConfig)

	// Rejection Log routes (admin-level reporting)
	rejectionLogs := admin.Group("/rejection-logs")
	rejectionLogs.Get("/", authMiddleware.RequirePermission("reports:view"), rejectionLogHandler.List)

	// Report routes
	reports := admin.Group("/reports", licenseMiddleware.RequireLicensedFeature(string(licensing.FeatureReports)))
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
	reportTemplates := admin.Group("/report-templates", licenseMiddleware.RequireLicensedFeature(string(licensing.FeatureReports)))
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
	appLinks.Delete("/:id/image", authMiddleware.RequirePermission("application-links:update"), applicationLinkHandler.RemoveImage)

	// Dashboard application links - requires explicit permission
	v1.Get("/application-links", authMiddleware.Authenticate(), authMiddleware.RequirePermission("application-links:dashboard"), applicationLinkHandler.ListActiveLinks)

	// Settings routes
	// Public settings endpoint (accessible to everyone for branding)
	v1.Get("/settings", settingsHandler.GetSettings)
	// Admin settings endpoint (requires settings:update permission)
	admin.Put("/settings", authMiddleware.RequirePermission("settings:update"), settingsHandler.UpdateSettings)

	// License management routes
	licenseGroup := admin.Group("/license")
	licenseGroup.Post("/activate", authMiddleware.RequirePermission("license:manage"), licenseHandler.Activate)
	licenseGroup.Get("/status", authMiddleware.RequirePermission("license:view"), licenseHandler.GetStatus)
	licenseGroup.Delete("/", authMiddleware.RequirePermission("license:manage"), licenseHandler.Deactivate)
	// Public license info (all authenticated users)
	v1.Get("/license/info", authMiddleware.Authenticate(), licenseHandler.GetPublicInfo)
	// Public feature catalog — no auth; describes product shape, not tenant state.
	v1.Get("/license/catalog", licenseHandler.GetCatalog)

	// Call routes (authenticated users)
	calls := v1.Group("/calls", authMiddleware.Authenticate())
	calls.Post("/start", callLogHandler.StartCall)
	calls.Post("/:call_uuid/end", callLogHandler.EndCall)
	calls.Post("/:call_uuid/join", callLogHandler.JoinCall)
	calls.Post("/:call_uuid/attachments", callLogHandler.UploadAttachment)
	calls.Get("/:attachment_id/preview", callLogHandler.PreviewAttachment)

	// Call logs routes
	callLogsPublic := v1.Group("/call-logs", authMiddleware.Authenticate())
	callLogsPublic.Get("/sip-info", callLogHandler.GetSipInfo)
	callLogsPublic.Get("/extension/:extension", callLogHandler.GetCallLogsByExtension)

	// ---- TEMPLATE ROUTES (legacy path, no feature-license gate) ----
	templates := v1.Group("/templates", authMiddleware.Authenticate())
	templates.Post("/", authMiddleware.RequirePermission("templates:create"), templateHandler.Create)
	templates.Get("/", authMiddleware.RequirePermission("templates:read"), templateHandler.List)
	templates.Get("/:id", authMiddleware.RequirePermission("templates:read"), templateHandler.GetByID)
	templates.Put("/:id", authMiddleware.RequirePermission("templates:update"), templateHandler.Update)
	templates.Delete("/:id", authMiddleware.RequirePermission("templates:delete"), templateHandler.Delete)

	// ---- NOTIFICATION ROUTES ----
	notifications := v1.Group("/notifications", authMiddleware.Authenticate(), licenseMiddleware.RequireLicensedFeature(string(licensing.FeatureCommunication)))

	// Stats
	notifications.Get("/stats", authMiddleware.RequirePermission("notifications:read"), notificationHandler.GetStats)

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
	notifications.Patch("/:id/read", authMiddleware.RequirePermission("notifications:read"), notificationHandler.MarkAsRead)
	notifications.Patch("/:id/star", authMiddleware.RequirePermission("notifications:read"), notificationHandler.ToggleStar)
	notifications.Patch("/:id/move", authMiddleware.RequirePermission("notifications:read"), notificationHandler.MoveToCategory)

	// Bulk actions
	notifications.Post("/bulk/move", authMiddleware.RequirePermission("notifications:read"), notificationHandler.BulkMoveToCategory)
	notifications.Post("/bulk/delete", authMiddleware.RequirePermission("notifications:delete"), notificationHandler.BulkDelete)

	// Attachment operations
	notifications.Get("/:id/attachments/:filename", authMiddleware.RequirePermission("notifications:read"), notificationHandler.DownloadAttachment)
	notifications.Get("/:id/attachments/:filename/url", authMiddleware.RequirePermission("notifications:read"), notificationHandler.GetAttachmentURL)

	// Delete (move to trash)
	notifications.Delete("/:id", authMiddleware.RequirePermission("notifications:delete"), notificationHandler.Delete)
	notifications.Delete("/:id/permanent", authMiddleware.RequirePermission("notifications:delete"), notificationHandler.PermanentDelete)

	// OTP Routes
	// Public OTP routes for mobile login (no auth required)
	otpPublic := v1.Group("/otp")
	otpPublic.Post("/send", otpHandler.SendOTP)
	otpPublic.Post("/verify", otpHandler.VerifyOTP)

	// Authenticated OTP routes (for admin users to send OTP to others)
	otp := v1.Group("/otp", authMiddleware.Authenticate())
	otp.Post("/send", authMiddleware.RequirePermission("otp:send"), otpHandler.SendOTP)
	otp.Post("/verify", authMiddleware.RequirePermission("otp:verify"), otpHandler.VerifyOTP)

	escalation := v1.Group("/escalation", authMiddleware.Authenticate())

	// GET /api/v1/escalation— list all SLA breach notification records
	escalation.Get("/", escalationHandler.List)
	escalation.Get("/incident/:incident_id", escalationHandler.ListByIncident)

	// Custom Escalation Groups (admin)
	escalationGroups := admin.Group("/escalation-groups", authMiddleware.Authenticate(), authMiddleware.RequirePermission("escalation-groups:manage_rules"), licenseMiddleware.RequireLicensedFeature(string(licensing.FeatureEscalation)))
	escalationGroups.Post("/", escalationGroupHandler.Create)
	escalationGroups.Get("/", escalationGroupHandler.List)
	escalationGroups.Get("/:id", escalationGroupHandler.GetByID)
	escalationGroups.Put("/:id", escalationGroupHandler.Update)
	escalationGroups.Delete("/:id", escalationGroupHandler.Delete)

	// Escalation Policies (admin)
	escalationPolicies := admin.Group("/escalation-policies", authMiddleware.Authenticate(), authMiddleware.RequirePermission("escalation-groups:manage_rules"), licenseMiddleware.RequireLicensedFeature(string(licensing.FeatureEscalation)))
	escalationPolicies.Post("/resolve-users", escalationPolicyHandler.ResolveTargetUsers)
	escalationPolicies.Get("/", escalationPolicyHandler.List)
	escalationPolicies.Post("/", escalationPolicyHandler.Create)
	escalationPolicies.Get("/:id", escalationPolicyHandler.GetByID)
	escalationPolicies.Put("/:id", escalationPolicyHandler.Update)
	escalationPolicies.Delete("/:id", escalationPolicyHandler.Delete)

	//FCM
	fcm := v1.Group("/fcm", authMiddleware.Authenticate())
	fcm.Post("/register", fcmHandler.RegisterToken)
	fcm.Post("/push", fcmHandler.PushNotification)
	fcm.Get("/device-tokens", fcmHandler.GetUserDeviceTokens)
	fcm.Delete("/remove-device", fcmHandler.RemoveDevice)

	//Caller sentiments
	callerSentiments := v1.Group("/caller-sentiments", authMiddleware.Authenticate(), licenseMiddleware.RequireLicensedFeature(string(licensing.FeatureCallCentre)))
	callerSentiments.Post("/", authMiddleware.RequirePermission("caller-sentiment:create"), sentimentHandler.Create)
	callerSentiments.Get("/", authMiddleware.RequirePermission("caller-sentiment:view"), sentimentHandler.GetAllCallerSentiments)
	callerSentiments.Get("/:caller_id", authMiddleware.RequirePermission("caller-sentiment:view"), sentimentHandler.GetCallerSentiments)
	callerSentiments.Get("/:caller_id/:callee_id", sentimentHandler.GetCallerSentimentsByCallerAndCallee)

	// ---- GOAL TEMPLATE ROUTES ----
	goalTemplates := v1.Group("/goal-templates", authMiddleware.Authenticate(), licenseMiddleware.RequireLicensedFeature(string(licensing.FeatureGoals)))
	goalTemplates.Post("/", authMiddleware.RequirePermission("goals:create"), goalTemplateHandler.Create)
	goalTemplates.Get("/", authMiddleware.RequirePermission("goals:view"), goalTemplateHandler.List)
	goalTemplates.Get("/active", authMiddleware.RequirePermission("goals:view"), goalTemplateHandler.ListActive)
	goalTemplates.Get("/:id", authMiddleware.RequirePermission("goals:view"), goalTemplateHandler.GetByID)
	goalTemplates.Put("/:id", authMiddleware.RequirePermission("goals:update"), goalTemplateHandler.Update)
	goalTemplates.Delete("/:id", authMiddleware.RequirePermission("goals:delete"), goalTemplateHandler.Delete)

	// ---- GOAL MANAGEMENT ROUTES ----
	goals := v1.Group("/goals", authMiddleware.Authenticate(), licenseMiddleware.RequireLicensedFeature(string(licensing.FeatureGoals)))
	goals.Get("/export", authMiddleware.RequirePermission("goals:view"), goalHandler.ExportGoals)
	goals.Post("/import", authMiddleware.RequirePermission("goals:create"), goalHandler.ImportGoals)
	goals.Get("/metrics/export-template", authMiddleware.RequirePermission("goals:view"), goalHandler.ExportMetricsTemplate)
	goals.Post("/metrics/import", authMiddleware.RequirePermission("goals:update"), goalHandler.ImportMetrics)
	goals.Get("/metric-batches", authMiddleware.RequirePermission("goals:view"), goalHandler.ListMetricImportBatches)
	goals.Get("/metric-batches/:id", authMiddleware.RequirePermission("goals:view"), goalHandler.GetMetricImportBatch)
	goals.Delete("/metric-batches/:id", authMiddleware.RequirePermission("goals:update"), goalHandler.DeleteMetricImportBatch)
	goals.Get("/metric-batches/:id/available-transitions", authMiddleware.RequirePermission("goals:view"), goalHandler.GetAvailableMetricBatchTransitions)
	goals.Post("/metric-batches/:id/transition", authMiddleware.RequirePermission("goals:update"), goalHandler.ExecuteMetricBatchTransition)
	goals.Get("/metric-batches/:id/transition-history", authMiddleware.RequirePermission("goals:view"), goalHandler.GetMetricBatchTransitionHistory)
	goals.Post("/bulk", authMiddleware.RequirePermission("goals:update"), goalHandler.BulkAction)
	// Analytics routes (must be before /:id catch-all)
	goals.Get("/analytics/stats", authMiddleware.RequirePermission("goals:view"), goalAnalyticsHandler.GetGoalStats)
	goals.Get("/analytics/distributions", authMiddleware.RequirePermission("goals:view"), goalAnalyticsHandler.GetDistributions)
	goals.Get("/analytics/progress", authMiddleware.RequirePermission("goals:view"), goalAnalyticsHandler.GetProgressSummary)
	goals.Get("/analytics/at-risk", authMiddleware.RequirePermission("goals:view"), goalAnalyticsHandler.GetAtRiskGoals)
	goals.Get("/analytics/trends", authMiddleware.RequirePermission("goals:view"), goalAnalyticsHandler.GetTrendData)
	goals.Get("/analytics/okr-tree", authMiddleware.RequirePermission("goals:view"), goalAnalyticsHandler.GetOKRTree)
	goals.Post("/", authMiddleware.RequirePermission("goals:create"), goalHandler.CreateGoal)
	goals.Get("/", authMiddleware.RequirePermission("goals:view"), goalHandler.ListGoals)
	goals.Get("/:id/activity", authMiddleware.RequirePermission("goals:view"), goalHandler.GetGoalActivity)
	goals.Post("/:id/comments", authMiddleware.RequirePermission("goals:update"), goalHandler.AddGoalComment)
	goals.Get("/:id/comments", authMiddleware.RequirePermission("goals:view"), goalHandler.ListGoalComments)
	goals.Delete("/comments/:cid", authMiddleware.RequirePermission("goals:update"), goalHandler.DeleteGoalComment)
	goals.Get("/:id", authMiddleware.RequirePermission("goals:view"), goalHandler.GetGoal)
	goals.Put("/:id", authMiddleware.RequirePermission("goals:update"), goalHandler.UpdateGoal)
	goals.Delete("/:id", authMiddleware.RequirePermission("goals:delete"), goalHandler.DeleteGoal)
	goals.Post("/:id/transition", authMiddleware.RequirePermission("goals:update"), goalHandler.TransitionStatus)
	goals.Post("/:id/clone", authMiddleware.RequirePermission("goals:create"), goalHandler.CloneGoal)
	goals.Get("/:id/children", authMiddleware.RequirePermission("goals:view"), goalHandler.ListChildGoals)
	goals.Get("/:id/tree", authMiddleware.RequirePermission("goals:view"), goalHandler.GetGoalTree)
	goals.Post("/:id/check-ins", authMiddleware.RequirePermission("goals:update"), goalHandler.CreateCheckIn)
	goals.Get("/:id/check-ins", authMiddleware.RequirePermission("goals:view"), goalHandler.ListCheckIns)
	goals.Delete("/check-ins/:id", authMiddleware.RequirePermission("goals:update"), goalHandler.DeleteCheckIn)
	goals.Post("/:id/collaborators", authMiddleware.RequirePermission("goals:assign"), goalHandler.AddCollaborator)
	goals.Delete("/:id/collaborators/:user_id", authMiddleware.RequirePermission("goals:assign"), goalHandler.RemoveCollaborator)
	goals.Post("/:gid/metrics", authMiddleware.RequirePermission("goals:update"), goalHandler.CreateMetric)
	goals.Put("/metrics/:id", authMiddleware.RequirePermission("goals:update"), goalHandler.UpdateMetric)
	goals.Delete("/metrics/:id", authMiddleware.RequirePermission("goals:update"), goalHandler.DeleteMetric)
	goals.Put("/metrics/:id/value", authMiddleware.RequirePermission("goals:update"), goalHandler.UpdateMetricValue)
	goals.Get("/metrics/:id/history", authMiddleware.RequirePermission("goals:view"), goalHandler.GetMetricHistory)
	goals.Post("/:gid/evidences", authMiddleware.RequirePermission("goals:update"), goalHandler.UploadEvidence)
	goals.Get("/:gid/evidences", authMiddleware.RequirePermission("goals:view"), goalHandler.ListEvidences)
	goals.Get("/evidences/:id", authMiddleware.RequirePermission("goals:view"), goalHandler.GetEvidence)
	goals.Get("/evidences/:id/preview", authMiddleware.RequirePermission("goals:view"), goalHandler.GetEvidencePreview)
	goals.Get("/evidences/:id/download", authMiddleware.RequirePermission("goals:view"), goalHandler.DownloadEvidence)
	goals.Delete("/evidences/:id", authMiddleware.RequirePermission("goals:update"), goalHandler.DeleteEvidence)
	goals.Put("/evidences/:id/file", authMiddleware.RequirePermission("goals:update"), goalHandler.ReplaceEvidenceFile)
	goals.Get("/evidences/:id/available-transitions", authMiddleware.RequirePermission("goals:view"), goalHandler.GetAvailableEvidenceTransitions)
	goals.Post("/evidences/:id/transition", authMiddleware.RequirePermission("goals:update"), goalHandler.ExecuteEvidenceTransition)
	goals.Get("/evidences/:id/transition-history", authMiddleware.RequirePermission("goals:view"), goalHandler.GetEvidenceTransitionHistory)

	// ---- METRIC APPROVAL WORKFLOW ROUTES ----
	goalMetrics := v1.Group("/goal-metrics", authMiddleware.Authenticate(), licenseMiddleware.RequireLicensedFeature(string(licensing.FeatureGoals)))
	goalMetrics.Get("/:id/available-transitions", authMiddleware.RequirePermission("goals:view"), goalHandler.GetAvailableMetricTransitions)
	goalMetrics.Post("/:id/transition", authMiddleware.RequirePermission("goals:approve"), goalHandler.TransitionMetric)

	goalMetricValueChanges := v1.Group("/goal-metric-value-changes", authMiddleware.Authenticate(), licenseMiddleware.RequireLicensedFeature(string(licensing.FeatureGoals)))
	goalMetricValueChanges.Get("/:id/available-transitions", authMiddleware.RequirePermission("goals:view"), goalHandler.GetAvailableMetricValueChangeTransitions)
	goalMetricValueChanges.Post("/:id/transition", authMiddleware.RequirePermission("goals:approve"), goalHandler.TransitionMetricValueChange)

	// Per-metric value-change history (any caller with access to the parent goal)
	goals.Get("/:id/metrics/:metric_id/value-changes", authMiddleware.RequirePermission("goals:view"), goalHandler.ListMetricValueChanges)

	// ---- APPROVAL ROUTES ----
	approvals := v1.Group("/approvals", authMiddleware.Authenticate(), licenseMiddleware.RequireLicensedFeature(string(licensing.FeatureGoals)))
	approvals.Get("/pending", authMiddleware.RequirePermission("goals:approve"), goalHandler.ListPendingApprovals)
	approvals.Get("/completed", authMiddleware.RequirePermission("goals:approve"), goalHandler.ListCompletedApprovals)
	approvals.Get("/pending-metrics", authMiddleware.RequirePermission("goals:approve"), goalHandler.ListPendingMetricApprovals)
	approvals.Get("/pending-metric-value-changes", authMiddleware.RequirePermission("goals:approve"), goalHandler.ListPendingMetricValueChangeApprovals)

	// ---- PERFORMANCE REVIEW ROUTES ----
	reviews := v1.Group("/reviews", authMiddleware.Authenticate(), licenseMiddleware.RequireLicensedFeature(string(licensing.FeatureGoals)))
	reviews.Post("/cycles", authMiddleware.RequirePermission("goals:update"), reviewHandler.CreateCycle)
	reviews.Get("/cycles", authMiddleware.RequirePermission("goals:view"), reviewHandler.ListCycles)
	reviews.Get("/cycles/:id", authMiddleware.RequirePermission("goals:view"), reviewHandler.GetCycle)
	reviews.Put("/cycles/:id", authMiddleware.RequirePermission("goals:update"), reviewHandler.UpdateCycle)
	reviews.Delete("/cycles/:id", authMiddleware.RequirePermission("goals:delete"), reviewHandler.DeleteCycle)
	reviews.Post("/cycles/:id/activate", authMiddleware.RequirePermission("goals:update"), reviewHandler.ActivateCycle)
	reviews.Post("/cycles/:id/complete", authMiddleware.RequirePermission("goals:update"), reviewHandler.CompleteCycle)
	reviews.Post("/cycles/:id/assignments", authMiddleware.RequirePermission("goals:assign"), reviewHandler.AssignReviewees)
	reviews.Get("/cycles/:id/assignments", authMiddleware.RequirePermission("goals:view"), reviewHandler.ListCycleAssignments)
	reviews.Delete("/assignments/:id", authMiddleware.RequirePermission("goals:assign"), reviewHandler.RemoveAssignment)
	reviews.Get("/assignments/:id", authMiddleware.RequirePermission("goals:view"), reviewHandler.GetAssignment)
	reviews.Post("/assignments/:id/score", authMiddleware.RequirePermission("goals:update"), reviewHandler.ScoreGoals)
	reviews.Post("/assignments/:id/submit", authMiddleware.RequirePermission("goals:update"), reviewHandler.SubmitReview)
	reviews.Get("/my-reviews", authMiddleware.RequirePermission("goals:view"), reviewHandler.ListMyReviews)
	reviews.Get("/my-review-tasks", authMiddleware.RequirePermission("goals:view"), reviewHandler.ListMyReviewTasks)

	// ---- DOCUMENT MANAGEMENT ROUTES ----
	docs := v1.Group("/documents", authMiddleware.Authenticate(), licenseMiddleware.RequireLicensedFeature(string(licensing.FeatureDocuments)))
	docs.Get("/files", authMiddleware.RequirePermission("goals:view"), documentHandler.ListFiles)
	docs.Post("/search", authMiddleware.RequirePermission("goals:view"), documentHandler.SearchFiles)
	docs.Get("/files/:id/info", authMiddleware.RequirePermission("goals:view"), documentHandler.GetFileInfo)
	docs.Get("/files/:id/breadcrumb", authMiddleware.RequirePermission("goals:view"), documentHandler.GetFileBreadcrumb)
	docs.Get("/files/:id/preview", authMiddleware.RequirePermission("goals:view"), documentHandler.GetPreviewURL)
	docs.Get("/files/:id/download", authMiddleware.RequirePermission("goals:view"), documentHandler.DownloadFile)
	docs.Get("/files/:id/comments", authMiddleware.RequirePermission("goals:view"), documentHandler.GetComments)
	docs.Post("/files/:id/comments", authMiddleware.RequirePermission("goals:update"), documentHandler.AddComment)
	docs.Get("/files/:id/tags", authMiddleware.RequirePermission("goals:view"), documentHandler.GetTags)
	docs.Put("/files/:id/tags", authMiddleware.RequirePermission("goals:update"), documentHandler.SetTags)
	docs.Get("/files/:id/versions", authMiddleware.RequirePermission("goals:view"), documentHandler.ListVersions)
	docs.Post("/files/:id/versions", authMiddleware.RequirePermission("goals:update"), documentHandler.UploadVersion)
	docs.Get("/versions/:vid/download", authMiddleware.RequirePermission("goals:view"), documentHandler.DownloadVersion)
	docs.Post("/files/:id/versions/rollback", authMiddleware.RequirePermission("goals:update"), documentHandler.RollbackVersion)

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
