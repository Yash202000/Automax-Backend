package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/i18n"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ════════════════════════════════════════════════════
// ReviewService Interface
// ════════════════════════════════════════════════════

type ReviewService interface {
	// Cycle CRUD
	CreateCycle(ctx context.Context, req *models.ReviewCycleCreateRequest, userID uuid.UUID) (*models.ReviewCycleResponse, error)
	GetCycle(ctx context.Context, id uuid.UUID) (*models.ReviewCycleResponse, error)
	ListCycles(ctx context.Context, status string, departmentID *uuid.UUID, page, limit int) ([]models.ReviewCycleResponse, int64, error)
	UpdateCycle(ctx context.Context, id uuid.UUID, req *models.ReviewCycleUpdateRequest) (*models.ReviewCycleResponse, error)
	DeleteCycle(ctx context.Context, id uuid.UUID) error

	// Cycle lifecycle
	ActivateCycle(ctx context.Context, id uuid.UUID) (*models.ReviewCycleResponse, error)
	CompleteCycle(ctx context.Context, id uuid.UUID) (*models.ReviewCycleResponse, error)

	// Assignments
	AssignReviewees(ctx context.Context, cycleID uuid.UUID, req *models.BulkAssignRequest) ([]models.ReviewAssignmentResponse, error)
	ListCycleAssignments(ctx context.Context, cycleID uuid.UUID) ([]models.ReviewAssignmentResponse, error)
	GetAssignment(ctx context.Context, id uuid.UUID) (*models.ReviewAssignmentResponse, error)
	RemoveAssignment(ctx context.Context, id uuid.UUID) error

	// My reviews
	ListMyReviews(ctx context.Context, userID uuid.UUID, page, limit int) ([]models.ReviewAssignmentResponse, int64, error)
	ListMyReviewTasks(ctx context.Context, userID uuid.UUID, page, limit int) ([]models.ReviewAssignmentResponse, int64, error)

	// Scoring
	ScoreGoals(ctx context.Context, assignmentID uuid.UUID, scores []models.GoalScoreUpdateRequest, userID uuid.UUID) (*models.ReviewAssignmentResponse, error)
	SubmitReview(ctx context.Context, assignmentID uuid.UUID, req *models.ReviewSubmitRequest, userID uuid.UUID) (*models.ReviewAssignmentResponse, error)
}

// ════════════════════════════════════════════════════
// Implementation
// ════════════════════════════════════════════════════

type reviewService struct {
	reviewRepo repository.ReviewRepository
	goalRepo   repository.GoalRepository
}

func NewReviewService(reviewRepo repository.ReviewRepository, goalRepo repository.GoalRepository) ReviewService {
	return &reviewService{
		reviewRepo: reviewRepo,
		goalRepo:   goalRepo,
	}
}

// ──────────────────────────────────────────────────
// Cycle CRUD
// ──────────────────────────────────────────────────

func (s *reviewService) CreateCycle(ctx context.Context, req *models.ReviewCycleCreateRequest, userID uuid.UUID) (*models.ReviewCycleResponse, error) {
	cycle := &models.ReviewCycle{
		Title:        req.Title,
		Description:  req.Description,
		PeriodStart:  req.PeriodStart,
		PeriodEnd:    req.PeriodEnd,
		DepartmentID: req.DepartmentID,
		Status:       models.ReviewCycleStatusDraft,
		CreatedByID:  userID,
	}

	if err := s.reviewRepo.CreateCycle(ctx, cycle); err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(ctx, "failed_to_create_review_cycle"), err)
	}

	// Reload with relations
	cycle, err := s.reviewRepo.FindCycleByIDWithRelations(ctx, cycle.ID)
	if err != nil {
		return nil, err
	}
	resp := cycle.ToResponse()
	return &resp, nil
}

func (s *reviewService) GetCycle(ctx context.Context, id uuid.UUID) (*models.ReviewCycleResponse, error) {
	cycle, err := s.reviewRepo.FindCycleByIDWithRelations(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%s", i18n.T(ctx, "review_cycle_not_found"))
		}
		return nil, err
	}
	resp := cycle.ToResponse()
	return &resp, nil
}

func (s *reviewService) ListCycles(ctx context.Context, status string, departmentID *uuid.UUID, page, limit int) ([]models.ReviewCycleResponse, int64, error) {
	cycles, total, err := s.reviewRepo.ListCycles(ctx, status, departmentID, page, limit)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]models.ReviewCycleResponse, len(cycles))
	for i, c := range cycles {
		responses[i] = c.ToResponse()
	}
	return responses, total, nil
}

func (s *reviewService) UpdateCycle(ctx context.Context, id uuid.UUID, req *models.ReviewCycleUpdateRequest) (*models.ReviewCycleResponse, error) {
	cycle, err := s.reviewRepo.FindCycleByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%s", i18n.T(ctx, "review_cycle_not_found"))
		}
		return nil, err
	}

	if cycle.Status != models.ReviewCycleStatusDraft {
		return nil, fmt.Errorf("%s", i18n.T(ctx, "only_update_draft_cycle"))
	}

	if req.Title != nil {
		cycle.Title = *req.Title
	}
	if req.Description != nil {
		cycle.Description = *req.Description
	}
	if req.PeriodStart != nil {
		cycle.PeriodStart = *req.PeriodStart
	}
	if req.PeriodEnd != nil {
		cycle.PeriodEnd = *req.PeriodEnd
	}
	if req.DepartmentID != nil {
		cycle.DepartmentID = req.DepartmentID
	}

	if err := s.reviewRepo.UpdateCycle(ctx, cycle); err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(ctx, "failed_to_update_review_cycle"), err)
	}

	cycle, err = s.reviewRepo.FindCycleByIDWithRelations(ctx, cycle.ID)
	if err != nil {
		return nil, err
	}
	resp := cycle.ToResponse()
	return &resp, nil
}

func (s *reviewService) DeleteCycle(ctx context.Context, id uuid.UUID) error {
	cycle, err := s.reviewRepo.FindCycleByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%s", i18n.T(ctx, "review_cycle_not_found"))
		}
		return err
	}

	if cycle.Status != models.ReviewCycleStatusDraft {
		return fmt.Errorf("%s", i18n.T(ctx, "only_delete_draft_cycle"))
	}

	return s.reviewRepo.DeleteCycle(ctx, id)
}

// ──────────────────────────────────────────────────
// Cycle Lifecycle
// ──────────────────────────────────────────────────

func (s *reviewService) ActivateCycle(ctx context.Context, id uuid.UUID) (*models.ReviewCycleResponse, error) {
	cycle, err := s.reviewRepo.FindCycleByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%s", i18n.T(ctx, "review_cycle_not_found"))
		}
		return nil, err
	}

	if cycle.Status != models.ReviewCycleStatusDraft {
		return nil, fmt.Errorf("%s", i18n.T(ctx, "can_only_activate_draft_cycles"))
	}

	cycle.Status = models.ReviewCycleStatusActive
	if err := s.reviewRepo.UpdateCycle(ctx, cycle); err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(ctx, "failed_to_activate_cycle"), err)
	}

	cycle, err = s.reviewRepo.FindCycleByIDWithRelations(ctx, cycle.ID)
	if err != nil {
		return nil, err
	}
	resp := cycle.ToResponse()
	return &resp, nil
}

func (s *reviewService) CompleteCycle(ctx context.Context, id uuid.UUID) (*models.ReviewCycleResponse, error) {
	cycle, err := s.reviewRepo.FindCycleByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%s", i18n.T(ctx, "review_cycle_not_found"))
		}
		return nil, err
	}

	if cycle.Status != models.ReviewCycleStatusActive {
		return nil, fmt.Errorf("%s", i18n.T(ctx, "can_only_complete_active_cycles"))
	}

	cycle.Status = models.ReviewCycleStatusCompleted
	if err := s.reviewRepo.UpdateCycle(ctx, cycle); err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(ctx, "failed_to_complete_cycle"), err)
	}

	cycle, err = s.reviewRepo.FindCycleByIDWithRelations(ctx, cycle.ID)
	if err != nil {
		return nil, err
	}
	resp := cycle.ToResponse()
	return &resp, nil
}

// ──────────────────────────────────────────────────
// Assignments
// ──────────────────────────────────────────────────

func (s *reviewService) AssignReviewees(ctx context.Context, cycleID uuid.UUID, req *models.BulkAssignRequest) ([]models.ReviewAssignmentResponse, error) {
	cycle, err := s.reviewRepo.FindCycleByID(ctx, cycleID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%s", i18n.T(ctx, "review_cycle_not_found"))
		}
		return nil, err
	}

	if cycle.Status != models.ReviewCycleStatusDraft && cycle.Status != models.ReviewCycleStatusActive {
		return nil, fmt.Errorf("%s", i18n.T(ctx, "can_only_assign_draft_or_active"))
	}

	assignments := make([]models.ReviewAssignment, len(req.Assignments))
	for i, a := range req.Assignments {
		assignments[i] = models.ReviewAssignment{
			CycleID:    cycleID,
			EmployeeID: a.EmployeeID,
			ReviewerID: a.ReviewerID,
			Status:     models.ReviewAssignmentStatusPending,
		}
	}

	if err := s.reviewRepo.CreateAssignmentsBulk(ctx, assignments); err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(ctx, "failed_to_create_assignments"), err)
	}

	// Reload assignments for this cycle
	all, err := s.reviewRepo.ListAssignmentsByCycle(ctx, cycleID)
	if err != nil {
		return nil, err
	}

	responses := make([]models.ReviewAssignmentResponse, len(all))
	for i, a := range all {
		responses[i] = a.ToResponse()
	}
	return responses, nil
}

func (s *reviewService) ListCycleAssignments(ctx context.Context, cycleID uuid.UUID) ([]models.ReviewAssignmentResponse, error) {
	assignments, err := s.reviewRepo.ListAssignmentsByCycle(ctx, cycleID)
	if err != nil {
		return nil, err
	}

	responses := make([]models.ReviewAssignmentResponse, len(assignments))
	for i, a := range assignments {
		responses[i] = a.ToResponse()
	}
	return responses, nil
}

func (s *reviewService) GetAssignment(ctx context.Context, id uuid.UUID) (*models.ReviewAssignmentResponse, error) {
	assignment, err := s.reviewRepo.FindAssignmentByIDWithRelations(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%s", i18n.T(ctx, "review_assignment_not_found"))
		}
		return nil, err
	}
	resp := assignment.ToResponse()
	return &resp, nil
}

func (s *reviewService) RemoveAssignment(ctx context.Context, id uuid.UUID) error {
	assignment, err := s.reviewRepo.FindAssignmentByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%s", i18n.T(ctx, "review_assignment_not_found"))
		}
		return err
	}

	if assignment.Status == models.ReviewAssignmentStatusCompleted {
		return fmt.Errorf("%s", i18n.T(ctx, "cannot_remove_completed_assignment"))
	}

	return s.reviewRepo.DeleteAssignment(ctx, id)
}

// ──────────────────────────────────────────────────
// My Reviews
// ──────────────────────────────────────────────────

func (s *reviewService) ListMyReviews(ctx context.Context, userID uuid.UUID, page, limit int) ([]models.ReviewAssignmentResponse, int64, error) {
	assignments, total, err := s.reviewRepo.ListAssignmentsByEmployee(ctx, userID, page, limit)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]models.ReviewAssignmentResponse, len(assignments))
	for i, a := range assignments {
		responses[i] = a.ToResponse()
	}
	return responses, total, nil
}

func (s *reviewService) ListMyReviewTasks(ctx context.Context, userID uuid.UUID, page, limit int) ([]models.ReviewAssignmentResponse, int64, error) {
	assignments, total, err := s.reviewRepo.ListAssignmentsByReviewer(ctx, userID, page, limit)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]models.ReviewAssignmentResponse, len(assignments))
	for i, a := range assignments {
		responses[i] = a.ToResponse()
	}
	return responses, total, nil
}

// ──────────────────────────────────────────────────
// Scoring
// ──────────────────────────────────────────────────

func (s *reviewService) ScoreGoals(ctx context.Context, assignmentID uuid.UUID, scoreReqs []models.GoalScoreUpdateRequest, userID uuid.UUID) (*models.ReviewAssignmentResponse, error) {
	assignment, err := s.reviewRepo.FindAssignmentByID(ctx, assignmentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%s", i18n.T(ctx, "review_assignment_not_found"))
		}
		return nil, err
	}

	if assignment.ReviewerID != userID {
		return nil, fmt.Errorf("%s", i18n.T(ctx, "only_assigned_reviewer_score"))
	}

	if assignment.Status == models.ReviewAssignmentStatusCompleted {
		return nil, fmt.Errorf("%s", i18n.T(ctx, "cannot_modify_completed_review"))
	}

	// Set status to in_progress if pending
	if assignment.Status == models.ReviewAssignmentStatusPending {
		assignment.Status = models.ReviewAssignmentStatusInProgress
		if err := s.reviewRepo.UpdateAssignment(ctx, assignment); err != nil {
			return nil, fmt.Errorf("%s: %w", i18n.T(ctx, "failed_to_update_assignment_status"), err)
		}
	}

	// Build GoalScore models, auto-populating AchievementPct from goal progress
	scores := make([]models.GoalScore, len(scoreReqs))
	for i, sr := range scoreReqs {
		var achievementPct float64
		goal, err := s.goalRepo.FindByID(ctx, sr.GoalID)
		if err == nil {
			achievementPct = goal.Progress
		}

		scores[i] = models.GoalScore{
			GoalID:         sr.GoalID,
			Weight:         sr.Weight,
			Rating:         sr.Rating,
			Comments:       sr.Comments,
			AchievementPct: achievementPct,
		}
	}

	if err := s.reviewRepo.UpsertGoalScores(ctx, assignmentID, scores); err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(ctx, "failed_to_save_goal_scores"), err)
	}

	// Reload with relations
	assignment, err = s.reviewRepo.FindAssignmentByIDWithRelations(ctx, assignmentID)
	if err != nil {
		return nil, err
	}
	resp := assignment.ToResponse()
	return &resp, nil
}

func (s *reviewService) SubmitReview(ctx context.Context, assignmentID uuid.UUID, req *models.ReviewSubmitRequest, userID uuid.UUID) (*models.ReviewAssignmentResponse, error) {
	assignment, err := s.reviewRepo.FindAssignmentByID(ctx, assignmentID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%s", i18n.T(ctx, "review_assignment_not_found"))
		}
		return nil, err
	}

	if assignment.ReviewerID != userID {
		return nil, fmt.Errorf("%s", i18n.T(ctx, "only_assigned_reviewer_submit"))
	}

	if assignment.Status == models.ReviewAssignmentStatusCompleted {
		return nil, fmt.Errorf("%s", i18n.T(ctx, "review_already_submitted"))
	}

	// Save goal scores if provided
	if len(req.GoalScores) > 0 {
		scores := make([]models.GoalScore, len(req.GoalScores))
		for i, sr := range req.GoalScores {
			var achievementPct float64
			goal, err := s.goalRepo.FindByID(ctx, sr.GoalID)
			if err == nil {
				achievementPct = goal.Progress
			}

			scores[i] = models.GoalScore{
				GoalID:         sr.GoalID,
				Weight:         sr.Weight,
				Rating:         sr.Rating,
				Comments:       sr.Comments,
				AchievementPct: achievementPct,
			}
		}

		if err := s.reviewRepo.UpsertGoalScores(ctx, assignmentID, scores); err != nil {
			return nil, fmt.Errorf("%s: %w", i18n.T(ctx, "failed_to_save_goal_scores"), err)
		}
	}

	// Set overall rating and complete
	now := time.Now()
	overallRating := req.OverallRating
	assignment.OverallRating = &overallRating
	assignment.Comments = req.Comments
	assignment.Status = models.ReviewAssignmentStatusCompleted
	assignment.CompletedAt = &now

	if err := s.reviewRepo.UpdateAssignment(ctx, assignment); err != nil {
		return nil, fmt.Errorf("%s: %w", i18n.T(ctx, "failed_to_submit_review"), err)
	}

	// Reload with relations
	assignment, err = s.reviewRepo.FindAssignmentByIDWithRelations(ctx, assignmentID)
	if err != nil {
		return nil, err
	}
	resp := assignment.ToResponse()
	return &resp, nil
}
