package services

import (
	"context"
	"fmt"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
)

type CallerSentimentService interface {
	Create(ctx context.Context, req *models.CallerSentiment) error
	GetCallerSentiments(ctx context.Context, callerID string) (*models.CallerSentimentSummaryResponse, error)
	GetCallerSentimentsByCallerAndCallee(ctx context.Context, callerID, calleeID string) (*models.CallerSentimentSummaryResponse, error)
	GetAllCallerSentiments(ctx context.Context) ([]models.CallerSentimentSummaryResponse, error)
}

type callerSentimentService struct {
	repo repository.CallerSentimentRepository
}

func NewCallerSentimentService(repo repository.CallerSentimentRepository) CallerSentimentService {
	return &callerSentimentService{
		repo: repo,
	}
}

func (s *callerSentimentService) Create(ctx context.Context, req *models.CallerSentiment) error {
	if req.Sentiment < 1 || req.Sentiment > 5 {
		return fmt.Errorf("invalid sentiment")
	}
	return s.repo.Create(ctx, req)
}

func (s *callerSentimentService) GetCallerSentiments(ctx context.Context, callerID string) (*models.CallerSentimentSummaryResponse, error) {

	data, err := s.repo.GetSummaryByCaller(ctx, callerID)
	if err != nil {
		return nil, err
	}

	// Call History
	history, err := s.repo.GetCallHistoryByCaller(ctx, callerID)
	if err != nil {
		return nil, err
	}
	countMap := map[int]int{}
	total := 0

	for _, d := range data {
		countMap[d.Sentiment] += d.Count
		total += d.Count
	}

	var dominant int
	max := 0
	var summary []models.SentimentCount

	for i := 1; i <= 5; i++ {
		count := countMap[i]
		percent := 0

		if total > 0 {
			percent = (count * 100) / total
		}

		if count > max {
			max = count
			dominant = i
		}

		summary = append(summary, models.SentimentCount{
			Sentiment: i,
			Count:     count,
			Percent:   percent,
		})
	}

	return &models.CallerSentimentSummaryResponse{
		CallerID: callerID,
		Summary:  summary,
		Dominant: dominant,
		Calls:    history, // attach history
	}, nil
}
func (s *callerSentimentService) GetCallerSentimentsByCallerAndCallee(ctx context.Context, callerID, calleeID string) (*models.CallerSentimentSummaryResponse, error) {

	data, err := s.repo.GetSummaryByCallerAndCallee(ctx, callerID, calleeID)
	if err != nil {
		return nil, err
	}

	// Call History
	history, err := s.repo.GetCallHistoryByCaller(ctx, callerID)
	if err != nil {
		return nil, err
	}
	countMap := map[int]int{}
	total := 0

	for _, d := range data {
		countMap[d.Sentiment] += d.Count
		total += d.Count
	}

	var dominant int
	max := 0
	var summary []models.SentimentCount

	for i := 1; i <= 5; i++ {
		count := countMap[i]
		percent := 0

		if total > 0 {
			percent = (count * 100) / total
		}

		if count > max {
			max = count
			dominant = i
		}

		summary = append(summary, models.SentimentCount{
			Sentiment: i,
			Count:     count,
			Percent:   percent,
		})
	}

	return &models.CallerSentimentSummaryResponse{
		CallerID: callerID,
		Summary:  summary,
		Dominant: dominant,
		Calls:    history, // attach history
	}, nil
}

func (s *callerSentimentService) GetAllCallerSentiments(ctx context.Context) ([]models.CallerSentimentSummaryResponse, error) {

	callers, err := s.repo.GetAllCallerIDs(ctx)
	if err != nil {
		return nil, err
	}

	var result []models.CallerSentimentSummaryResponse

	for _, callerID := range callers {
		res, err := s.GetCallerSentiments(ctx, callerID)
		if err != nil {
			continue
		}
		result = append(result, *res)
	}

	return result, nil
}
