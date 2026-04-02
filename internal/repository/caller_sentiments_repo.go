package repository

import (
	"context"

	"github.com/automax/backend/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type CallerSentimentRepository interface {
	Create(ctx context.Context, s *models.CallerSentiment) error
	GetSummaryByCaller(ctx context.Context, callerID string) ([]models.SentimentAgg, error)
	GetSummaryByCallerAndCallee(ctx context.Context, callerID, calleeID string) ([]models.SentimentAgg, error)
	GetAllCallerIDs(ctx context.Context) ([]string, error)
	GetCallHistoryByCaller(ctx context.Context, callerID string) ([]models.CallHistory, error)
}

type callerSentimentRepo struct {
	db *gorm.DB
}

func NewCallerSentimentRepo(db *gorm.DB) CallerSentimentRepository {
	return &callerSentimentRepo{db: db}
}

func (r *callerSentimentRepo) Create(ctx context.Context, s *models.CallerSentiment) error {
	return r.db.WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "call_uuid"}},
			DoNothing: true,
		}).
		Create(s).Error
}
func (r *callerSentimentRepo) GetSummaryByCaller(ctx context.Context, callerID string) ([]models.SentimentAgg, error) {
	var result []models.SentimentAgg

	err := r.db.WithContext(ctx).
		Table("caller_sentiments").
		Select("sentiment,callee_id, COUNT(*) as count").
		Where("caller_id = ?", callerID).
		Group("sentiment,callee_id").
		Scan(&result).Error

	return result, err
}
func (r *callerSentimentRepo) GetSummaryByCallerAndCallee(ctx context.Context, callerID, calleeID string) ([]models.SentimentAgg, error) {
	var result []models.SentimentAgg

	err := r.db.WithContext(ctx).
		Table("caller_sentiments").
		Select("sentiment,callee_id, COUNT(*) as count").
		Where("caller_id = ? AND callee_id = ?", callerID, calleeID).
		Group("sentiment,callee_id").
		Scan(&result).Error

	return result, err
}

func (r *callerSentimentRepo) GetAllCallerIDs(ctx context.Context) ([]string, error) {
	var callers []string

	err := r.db.WithContext(ctx).
		Table("caller_sentiments").
		Distinct("caller_id").
		Pluck("caller_id", &callers).Error

	return callers, err
}

func (r *callerSentimentRepo) GetCallHistoryByCaller(ctx context.Context, callerID string) ([]models.CallHistory, error) {
	var result []models.CallHistory

	err := r.db.WithContext(ctx).
		Table("caller_sentiments").
		Select("call_uuid, sentiment, callee_id, feedback, created_at").
		Where("caller_id = ?", callerID).
		Order("created_at DESC").
		Scan(&result).Error

	return result, err
}
