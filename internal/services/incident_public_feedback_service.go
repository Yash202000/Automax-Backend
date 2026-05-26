package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/constants"
	"github.com/automax/backend/pkg/utils"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const feedbackTokenDuration = 72 * time.Hour

type IncidentPublicFeedbackService interface {
	Create(ctx context.Context, incidentID uuid.UUID, req *models.IncidentPublicFeedbackCreateRequest) (*models.IncidentPublicFeedbackResponse, error)
	Submit(ctx context.Context, incidentID uuid.UUID, feedbackID uuid.UUID, req *models.IncidentPublicFeedbackSubmitRequest) (*models.IncidentPublicFeedbackResponse, error)
	List(ctx context.Context) ([]models.IncidentPublicFeedbackResponse, error)
	ListByIncident(ctx context.Context, incidentID uuid.UUID) ([]models.IncidentPublicFeedbackResponse, error)
}

type incidentPublicFeedbackService struct {
	repo               repository.IncidentPublicFeedbackRepository
	notification       *NotificationService
	incidentService    IncidentService
	workflowRepo       repository.WorkflowRepository
	classificationRepo repository.ClassificationRepository
}

func NewIncidentPublicFeedbackService(
	repo repository.IncidentPublicFeedbackRepository,
	notification *NotificationService,
	incidentService IncidentService,
	workflowRepo repository.WorkflowRepository,
	classificationRepo repository.ClassificationRepository,
) IncidentPublicFeedbackService {
	return &incidentPublicFeedbackService{
		repo:               repo,
		notification:       notification,
		incidentService:    incidentService,
		workflowRepo:       workflowRepo,
		classificationRepo: classificationRepo,
	}
}

func (s *incidentPublicFeedbackService) Create(ctx context.Context, incidentID uuid.UUID, req *models.IncidentPublicFeedbackCreateRequest) (*models.IncidentPublicFeedbackResponse, error) {
	createdByID, ok := ctx.Value(constants.ContextKeys.UserID).(uuid.UUID)
	if !ok {
		return nil, errors.New("user information not found in context")
	}

	f := &models.IncidentPublicFeedback{
		IncidentID: incidentID,
		MobileNo:   req.MobileNo,
		Source:     req.Source,
		Meta:       req.Meta,
		ReporterID: req.ReporterID,
		CreatedBy:  createdByID,
	}

	if err := s.repo.Create(ctx, f); err != nil {
		return nil, fmt.Errorf("failed to create public feedback: %w", err)
	}

	token := utils.GenerateFeedbackToken(f.ID.String(), incidentID.String(), feedbackTokenDuration)

	// Send SMS asynchronously — a delivery failure must not block the API response.
	go func() {
		smsBody := req.Message
		if smsBody == "" {
			link := utils.BuildFeedbackLink(ctx, incidentID.String(), f.ID.String(), feedbackTokenDuration)
			smsBody = fmt.Sprintf("Please rate your experience. Submit your feedback here: %s", link)
		}
		if _, err := s.notification.SendNotification(
			context.Background(), "sms", nil, "en",
			[]string{f.MobileNo}, nil, nil,
			"", smsBody, nil, nil, &createdByID, nil,
		); err != nil {
			log.Printf("[IncidentPublicFeedback] SMS send failed for feedback %s: %v", f.ID, err)
		}
	}()

	resp := models.ToIncidentPublicFeedbackResponse(f)
	resp.SignedToken = token
	return &resp, nil
}

func (s *incidentPublicFeedbackService) Submit(ctx context.Context, incidentID uuid.UUID, feedbackID uuid.UUID, req *models.IncidentPublicFeedbackSubmitRequest) (*models.IncidentPublicFeedbackResponse, error) {
	f, err := s.repo.FindByID(ctx, feedbackID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errors.New("feedback record not found")
		}
		return nil, fmt.Errorf("failed to find feedback: %w", err)
	}

	if f.IncidentID != incidentID {
		return nil, errors.New("feedback does not belong to this incident")
	}

	if f.SubmittedAt != nil {
		return nil, errors.New("feedback has already been submitted")
	}

	now := time.Now()
	f.Satisfied = &req.Satisfied
	if req.Comment != "" {
		f.Comment = &req.Comment
	}
	f.SubmittedAt = &now
	f.UpdatedAt = now

	if err := s.repo.Update(ctx, f); err != nil {
		return nil, fmt.Errorf("failed to submit feedback: %w", err)
	}

	// Not satisfied → auto-create a complaint in the background.
	if !req.Satisfied {
		go s.createComplaintAsync(f)
	}

	resp := models.ToIncidentPublicFeedbackResponse(f)
	return &resp, nil
}

// createComplaintAsync resolves the first active complaint workflow and classification
// from the database and creates a complaint record, logging any failure without surfacing it.
func (s *incidentPublicFeedbackService) createComplaintAsync(f *models.IncidentPublicFeedback) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Resolve the first active workflow configured for complaints.
	workflows, err := s.workflowRepo.ListByRecordType(ctx, "complaint", true)
	if err != nil || len(workflows) == 0 {
		log.Printf("[IncidentPublicFeedback] no active complaint workflow found, skipping auto-complaint for feedback %s: %v", f.ID, err)
		return
	}
	workflow := workflows[0]

	// Resolve the first complaint classification.
	classifications, err := s.classificationRepo.ListByType(ctx, []string{"complaint"})
	if err != nil || len(classifications) == 0 {
		log.Printf("[IncidentPublicFeedback] no complaint classification found, skipping auto-complaint for feedback %s: %v", f.ID, err)
		return
	}
	classification := classifications[0]

	comment := ""
	if f.Comment != nil {
		comment = *f.Comment
	}
	reporterID := f.CreatedBy.String()
	sourceIncidentID := f.IncidentID.String()

	complaintReq := &models.CreateComplaintRequest{
		Title:            fmt.Sprintf("Unsatisfied feedback for incident %s", f.IncidentID),
		Description:      comment,
		ClassificationID: classification.ID.String(),
		WorkflowID:       workflow.ID.String(),
		SourceIncidentID: &sourceIncidentID,
		ReporterID:       &reporterID,
		Source:           f.Source,
		Channel:          f.Source,
	}

	if _, err := s.incidentService.CreateComplaint(ctx, complaintReq, f.CreatedBy); err != nil {
		log.Printf("[IncidentPublicFeedback] auto-complaint creation failed for feedback %s: %v", f.ID, err)
	}
}

func (s *incidentPublicFeedbackService) List(ctx context.Context) ([]models.IncidentPublicFeedbackResponse, error) {
	items, err := s.repo.List(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list public feedback: %w", err)
	}
	return models.ToIncidentPublicFeedbackResponseList(items), nil
}

func (s *incidentPublicFeedbackService) ListByIncident(ctx context.Context, incidentID uuid.UUID) ([]models.IncidentPublicFeedbackResponse, error) {
	items, err := s.repo.FindByIncidentID(ctx, incidentID)
	if err != nil {
		return nil, fmt.Errorf("failed to list public feedback for incident: %w", err)
	}
	return models.ToIncidentPublicFeedbackResponseList(items), nil
}
