package repository

import (
	"context"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ════════════════════════════════════════════════════
// ReviewRepository Interface
// ════════════════════════════════════════════════════

type ReviewRepository interface {
	// Cycle CRUD
	CreateCycle(ctx context.Context, cycle *models.ReviewCycle) error
	FindCycleByID(ctx context.Context, id uuid.UUID) (*models.ReviewCycle, error)
	FindCycleByIDWithRelations(ctx context.Context, id uuid.UUID) (*models.ReviewCycle, error)
	ListCycles(ctx context.Context, status string, departmentID *uuid.UUID, page, limit int) ([]models.ReviewCycle, int64, error)
	UpdateCycle(ctx context.Context, cycle *models.ReviewCycle) error
	DeleteCycle(ctx context.Context, id uuid.UUID) error

	// Assignment CRUD
	CreateAssignment(ctx context.Context, assignment *models.ReviewAssignment) error
	CreateAssignmentsBulk(ctx context.Context, assignments []models.ReviewAssignment) error
	FindAssignmentByID(ctx context.Context, id uuid.UUID) (*models.ReviewAssignment, error)
	FindAssignmentByIDWithRelations(ctx context.Context, id uuid.UUID) (*models.ReviewAssignment, error)
	ListAssignmentsByCycle(ctx context.Context, cycleID uuid.UUID) ([]models.ReviewAssignment, error)
	ListAssignmentsByEmployee(ctx context.Context, employeeID uuid.UUID, page, limit int) ([]models.ReviewAssignment, int64, error)
	ListAssignmentsByReviewer(ctx context.Context, reviewerID uuid.UUID, page, limit int) ([]models.ReviewAssignment, int64, error)
	UpdateAssignment(ctx context.Context, assignment *models.ReviewAssignment) error
	DeleteAssignment(ctx context.Context, id uuid.UUID) error

	// Goal Scores
	UpsertGoalScores(ctx context.Context, assignmentID uuid.UUID, scores []models.GoalScore) error
	ListGoalScoresByAssignment(ctx context.Context, assignmentID uuid.UUID) ([]models.GoalScore, error)

	// Transaction
	BeginTx(ctx context.Context) *gorm.DB
}

// ════════════════════════════════════════════════════
// Implementation
// ════════════════════════════════════════════════════

type reviewRepository struct {
	db *gorm.DB
}

func NewReviewRepository(db *gorm.DB) ReviewRepository {
	return &reviewRepository{db: db}
}

// ──────────────────────────────────────────────────
// Cycle CRUD
// ──────────────────────────────────────────────────

func (r *reviewRepository) CreateCycle(ctx context.Context, cycle *models.ReviewCycle) error {
	return r.db.WithContext(ctx).Create(cycle).Error
}

func (r *reviewRepository) FindCycleByID(ctx context.Context, id uuid.UUID) (*models.ReviewCycle, error) {
	var cycle models.ReviewCycle
	err := r.db.WithContext(ctx).First(&cycle, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &cycle, nil
}

func (r *reviewRepository) FindCycleByIDWithRelations(ctx context.Context, id uuid.UUID) (*models.ReviewCycle, error) {
	var cycle models.ReviewCycle
	err := r.db.WithContext(ctx).
		Preload("Department").
		Preload("CreatedBy").
		Preload("Assignments").
		Preload("Assignments.Employee").
		Preload("Assignments.Reviewer").
		First(&cycle, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &cycle, nil
}

func (r *reviewRepository) ListCycles(ctx context.Context, status string, departmentID *uuid.UUID, page, limit int) ([]models.ReviewCycle, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.ReviewCycle{})

	if status != "" {
		query = query.Where("status = ?", status)
	}
	if departmentID != nil {
		query = query.Where("department_id = ?", *departmentID)
	}

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var cycles []models.ReviewCycle
	err := query.
		Preload("Department").
		Preload("CreatedBy").
		Preload("Assignments").
		Order("created_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&cycles).Error
	if err != nil {
		return nil, 0, err
	}
	return cycles, total, nil
}

func (r *reviewRepository) UpdateCycle(ctx context.Context, cycle *models.ReviewCycle) error {
	return r.db.WithContext(ctx).Save(cycle).Error
}

func (r *reviewRepository) DeleteCycle(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.ReviewCycle{}, "id = ?", id).Error
}

// ──────────────────────────────────────────────────
// Assignment CRUD
// ──────────────────────────────────────────────────

func (r *reviewRepository) CreateAssignment(ctx context.Context, assignment *models.ReviewAssignment) error {
	return r.db.WithContext(ctx).Create(assignment).Error
}

func (r *reviewRepository) CreateAssignmentsBulk(ctx context.Context, assignments []models.ReviewAssignment) error {
	return r.db.WithContext(ctx).Create(&assignments).Error
}

func (r *reviewRepository) FindAssignmentByID(ctx context.Context, id uuid.UUID) (*models.ReviewAssignment, error) {
	var assignment models.ReviewAssignment
	err := r.db.WithContext(ctx).First(&assignment, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &assignment, nil
}

func (r *reviewRepository) FindAssignmentByIDWithRelations(ctx context.Context, id uuid.UUID) (*models.ReviewAssignment, error) {
	var assignment models.ReviewAssignment
	err := r.db.WithContext(ctx).
		Preload("Cycle").
		Preload("Employee").
		Preload("Reviewer").
		Preload("GoalScores").
		Preload("GoalScores.Goal").
		First(&assignment, "id = ?", id).Error
	if err != nil {
		return nil, err
	}
	return &assignment, nil
}

func (r *reviewRepository) ListAssignmentsByCycle(ctx context.Context, cycleID uuid.UUID) ([]models.ReviewAssignment, error) {
	var assignments []models.ReviewAssignment
	err := r.db.WithContext(ctx).
		Preload("Employee").
		Preload("Reviewer").
		Where("cycle_id = ?", cycleID).
		Order("created_at ASC").
		Find(&assignments).Error
	if err != nil {
		return nil, err
	}
	return assignments, nil
}

func (r *reviewRepository) ListAssignmentsByEmployee(ctx context.Context, employeeID uuid.UUID, page, limit int) ([]models.ReviewAssignment, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.ReviewAssignment{}).Where("employee_id = ?", employeeID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var assignments []models.ReviewAssignment
	err := query.
		Preload("Cycle").
		Preload("Reviewer").
		Preload("GoalScores").
		Preload("GoalScores.Goal").
		Order("created_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&assignments).Error
	if err != nil {
		return nil, 0, err
	}
	return assignments, total, nil
}

func (r *reviewRepository) ListAssignmentsByReviewer(ctx context.Context, reviewerID uuid.UUID, page, limit int) ([]models.ReviewAssignment, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.ReviewAssignment{}).Where("reviewer_id = ?", reviewerID)

	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var assignments []models.ReviewAssignment
	err := query.
		Preload("Cycle").
		Preload("Employee").
		Preload("GoalScores").
		Preload("GoalScores.Goal").
		Order("created_at DESC").
		Offset((page - 1) * limit).
		Limit(limit).
		Find(&assignments).Error
	if err != nil {
		return nil, 0, err
	}
	return assignments, total, nil
}

func (r *reviewRepository) UpdateAssignment(ctx context.Context, assignment *models.ReviewAssignment) error {
	return r.db.WithContext(ctx).Save(assignment).Error
}

func (r *reviewRepository) DeleteAssignment(ctx context.Context, id uuid.UUID) error {
	return r.db.WithContext(ctx).Delete(&models.ReviewAssignment{}, "id = ?", id).Error
}

// ──────────────────────────────────────────────────
// Goal Scores
// ──────────────────────────────────────────────────

func (r *reviewRepository) UpsertGoalScores(ctx context.Context, assignmentID uuid.UUID, scores []models.GoalScore) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		for i := range scores {
			scores[i].AssignmentID = assignmentID

			var existing models.GoalScore
			err := tx.Where("assignment_id = ? AND goal_id = ?", assignmentID, scores[i].GoalID).First(&existing).Error
			if err == nil {
				// Update existing
				existing.Weight = scores[i].Weight
				existing.Rating = scores[i].Rating
				existing.Comments = scores[i].Comments
				existing.AchievementPct = scores[i].AchievementPct
				if err := tx.Save(&existing).Error; err != nil {
					return err
				}
				scores[i].ID = existing.ID
			} else {
				// Create new
				if err := tx.Create(&scores[i]).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func (r *reviewRepository) ListGoalScoresByAssignment(ctx context.Context, assignmentID uuid.UUID) ([]models.GoalScore, error) {
	var scores []models.GoalScore
	err := r.db.WithContext(ctx).
		Preload("Goal").
		Where("assignment_id = ?", assignmentID).
		Find(&scores).Error
	if err != nil {
		return nil, err
	}
	return scores, nil
}

func (r *reviewRepository) BeginTx(ctx context.Context) *gorm.DB {
	return r.db.WithContext(ctx).Begin()
}
