package services

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/storage"
	pkgUtils "github.com/automax/backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CallLogService interface {
	CreateCallLog(ctx context.Context, req *models.CallLogCreateRequest, participants []models.ParticipantData) (*models.CallLogResponse, error)
	GetCallLog(ctx context.Context, id uuid.UUID) (*models.CallLogResponse, error)
	UpdateCallLog(ctx context.Context, id uuid.UUID, req *models.CallLogUpdateRequest) (*models.CallLogResponse, error)
	DeleteCallLog(ctx context.Context, id uuid.UUID) error
	ListCallLogs(ctx context.Context, filter *models.CallLogFilter) ([]models.CallLogResponse, int64, error)
	ListCallLogsSummary(ctx context.Context, filter *models.CallLogFilter, currentUserID uuid.UUID) ([]models.CallLogListItem, int64, error)
	GetStats(ctx context.Context) (*models.CallLogStats, error)
	StartCall(ctx context.Context, callUUID, callType string, initiator models.ParticipantData, recipients []models.ParticipantData) (*models.CallLogResponse, error)
	EndCall(ctx context.Context, callUUID string, endAt *time.Time, status string) (*models.CallLogResponse, error)
	JoinCall(ctx context.Context, callUUID string, userID *uuid.UUID, guestPhone string) error
	GetCallLogsByUserID(ctx context.Context, userID uuid.UUID, page, limit int) ([]models.CallLogResponse, int64, error)
	GetSipInfo(ctx context.Context) (map[string]interface{}, error)

	// Attachments
	AddAttachment(ctx context.Context, callUUID string, attachment *models.CallLogAttachment) error
	GetAttachment(ctx context.Context, attachmentID uuid.UUID) (*models.CallLogAttachment, error)
}

type callLogService struct {
	repo     repository.CallLogRepository
	userRepo repository.UserRepository
	storage  *storage.MinIOStorage
}

func NewCallLogService(repo repository.CallLogRepository, userRepo repository.UserRepository, storage *storage.MinIOStorage) CallLogService {
	return &callLogService{
		repo:     repo,
		userRepo: userRepo,
		storage:  storage,
	}
}

func (s *callLogService) CreateCallLog(ctx context.Context, req *models.CallLogCreateRequest, participants []models.ParticipantData) (*models.CallLogResponse, error) {
	callLog := &models.CallLog{
		CallUuid:     req.CallUuid,
		CallType:     req.CallType,
		StartAt:      req.StartAt,
		EndAt:        req.EndAt,
		Status:       req.Status,
		RecordingUrl: req.RecordingUrl,
		Meta:         req.Meta,
		CreatedAt:    time.Now(),
	}

	callParticipants := make([]*models.CallParticipant, 0, len(req.Participants))
	for i, pi := range req.Participants {
		joinStatus := pi.JoinStatus
		if joinStatus == "" {
			joinStatus = "invited"
		}
		cp := &models.CallParticipant{
			Role:       pi.Role,
			JoinStatus: joinStatus,
		}
		if i < len(participants) {
			cp.UserID = participants[i].UserID
			if participants[i].Phone != nil {
				cp.PhoneNumber = participants[i].Phone
			}
		}
		callParticipants = append(callParticipants, cp)
	}

	if err := s.repo.CreateWithParticipants(ctx, callLog, callParticipants); err != nil {
		return nil, err
	}

	return s.getCallLogResponse(ctx, callLog.ID)
}

func (s *callLogService) GetCallLog(ctx context.Context, id uuid.UUID) (*models.CallLogResponse, error) {
	return s.getCallLogResponse(ctx, id)
}

func (s *callLogService) UpdateCallLog(ctx context.Context, id uuid.UUID, req *models.CallLogUpdateRequest) (*models.CallLogResponse, error) {
	now := time.Now()
	updates := map[string]interface{}{"updated_at": now}

	if req.StartAt != nil {
		updates["start_at"] = req.StartAt
	}
	if req.EndAt != nil {
		updates["end_at"] = req.EndAt
	}
	if req.Status != "" {
		updates["status"] = req.Status
	}
	if req.RecordingUrl != "" {
		updates["recording_url"] = req.RecordingUrl
	}
	if req.Meta != nil {
		updates["meta"] = req.Meta
	}

	if err := s.repo.UpdateByField(ctx, id, updates); err != nil {
		return nil, err
	}

	return s.getCallLogResponse(ctx, id)
}

func (s *callLogService) DeleteCallLog(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id)
}

func (s *callLogService) ListCallLogs(ctx context.Context, filter *models.CallLogFilter) ([]models.CallLogResponse, int64, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 10
	}

	callLogs, total, err := s.repo.List(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]models.CallLogResponse, len(callLogs))
	for i, callLog := range callLogs {
		resp, err := s.toCallLogResponse(ctx, &callLog)
		if err != nil {
			return nil, 0, err
		}
		responses[i] = resp
	}

	return responses, total, nil
}

// ListCallLogsSummary returns the slim list view.
// Direction is "outgoing" if the current user is the initiator participant, "incoming" otherwise.
func (s *callLogService) ListCallLogsSummary(ctx context.Context, filter *models.CallLogFilter, currentUserID uuid.UUID) ([]models.CallLogListItem, int64, error) {
	if filter.Page <= 0 {
		filter.Page = 1
	}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 10
	}

	callLogs, total, err := s.repo.ListSummary(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	// Bulk-fetch all participant user IDs across all calls.
	userIDSet := make(map[uuid.UUID]struct{})
	for _, cl := range callLogs {
		for _, p := range cl.Participants {
			if p.UserID != nil {
				userIDSet[*p.UserID] = struct{}{}
			}
		}
	}

	userMap := make(map[uuid.UUID]*models.User)
	if len(userIDSet) > 0 {
		ids := make([]uuid.UUID, 0, len(userIDSet))
		for id := range userIDSet {
			ids = append(ids, id)
		}
		if users, err := s.userRepo.FindByIDs(ctx, ids); err == nil {
			for i := range users {
				userMap[users[i].ID] = &users[i]
			}
		}
	}

	items := make([]models.CallLogListItem, len(callLogs))
	for i, cl := range callLogs {
		duration := 0
		if cl.StartAt != nil && cl.EndAt != nil {
			if secs := int(cl.EndAt.Sub(*cl.StartAt).Seconds()); secs > 0 {
				duration = secs
			}
		}

		item := models.CallLogListItem{
			ID:           cl.ID,
			CallUuid:     cl.CallUuid,
			CallType:     cl.CallType,
			Status:       cl.Status,
			Duration:     duration,
			RecordingUrl: cl.RecordingUrl,
			CreatedAt:    cl.CreatedAt,
		}

		// Identify initiator and first non-initiator from preloaded participants.
		var initiatorParticipant *models.CallParticipant
		var otherParticipant *models.CallParticipant
		for j := range cl.Participants {
			p := &cl.Participants[j]
			if p.Role == "initiator" {
				initiatorParticipant = p
			} else if otherParticipant == nil {
				otherParticipant = p
			}
		}

		if initiatorParticipant != nil && initiatorParticipant.UserID != nil && *initiatorParticipant.UserID == currentUserID {
			item.Direction = "outgoing"
		} else {
			item.Direction = "incoming"
		}

		if item.Direction == "outgoing" {
			if otherParticipant != nil {
				if otherParticipant.UserID != nil {
					if u, ok := userMap[*otherParticipant.UserID]; ok {
						item.OtherPartyName = strings.TrimSpace(u.FirstName + " " + u.LastName)
						item.OtherPartyExtension = u.Extension
					}
				} else if otherParticipant.PhoneNumber != nil {
					item.OtherPartyName = *otherParticipant.PhoneNumber
					item.OtherPartyPhone = *otherParticipant.PhoneNumber
				}
			}
		} else {
			// Incoming: the initiator is the other party.
			if initiatorParticipant != nil {
				if initiatorParticipant.UserID != nil {
					if u, ok := userMap[*initiatorParticipant.UserID]; ok {
						item.OtherPartyName = strings.TrimSpace(u.FirstName + " " + u.LastName)
						item.OtherPartyExtension = u.Extension
					}
				} else if initiatorParticipant.PhoneNumber != nil {
					item.OtherPartyName = *initiatorParticipant.PhoneNumber
					item.OtherPartyPhone = *initiatorParticipant.PhoneNumber
				}
			}
		}

		items[i] = item
	}

	return items, total, nil
}

func (s *callLogService) GetStats(ctx context.Context) (*models.CallLogStats, error) {
	return s.repo.GetStats(ctx)
}

func (s *callLogService) StartCall(ctx context.Context, callUUID, callType string, initiator models.ParticipantData, recipients []models.ParticipantData) (*models.CallLogResponse, error) {
	existing, err := s.repo.FindByCallUUID(ctx, callUUID)
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("call already exists")
	}

	now := time.Now()
	callLog := &models.CallLog{
		CallUuid:  callUUID,
		CallType:  callType,
		Status:    "initiated",
		CreatedAt: now,
	}

	participants := make([]*models.CallParticipant, 0, 1+len(recipients))
	participants = append(participants, &models.CallParticipant{
		UserID:      initiator.UserID,
		PhoneNumber: initiator.Phone,
		Role:        "initiator",
		JoinStatus:  "joined",
		JoinedAt:    &now,
	})

	recipientRole := "recipient"
	if callType == "group" {
		recipientRole = "participant"
	}
	for _, r := range recipients {
		p := r // copy
		participants = append(participants, &models.CallParticipant{
			UserID:      p.UserID,
			PhoneNumber: p.Phone,
			Role:        recipientRole,
			JoinStatus:  "invited",
		})
	}

	if err := s.repo.CreateWithParticipants(ctx, callLog, participants); err != nil {
		return nil, err
	}

	return s.getCallLogResponse(ctx, callLog.ID)
}

func (s *callLogService) EndCall(ctx context.Context, callUUID string, endAt *time.Time, status string) (*models.CallLogResponse, error) {
	callLog, err := s.repo.FindByCallUUID(ctx, callUUID)
	if err != nil {
		return nil, err
	}

	if endAt == nil {
		now := time.Now()
		endAt = &now
	}

	now := time.Now()
	updates := map[string]interface{}{
		"end_at":     endAt,
		"status":     status,
		"updated_at": now,
	}
	if err := s.repo.UpdateByField(ctx, callLog.ID, updates); err != nil {
		return nil, err
	}

	if err := s.repo.UpdateParticipantsLeftAt(ctx, callLog.ID, *endAt); err != nil {
		return nil, err
	}

	return s.getCallLogResponse(ctx, callLog.ID)
}

func (s *callLogService) JoinCall(ctx context.Context, callUUID string, userID *uuid.UUID, guestPhone string) error {
	callLog, err := s.repo.FindByCallUUID(ctx, callUUID)
	if err != nil {
		return err
	}

	now := time.Now()

	var phone *string
	if guestPhone != "" {
		phone = &guestPhone
	}

	participant, err := s.repo.FindParticipant(ctx, callLog.ID, userID, phone)
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}

	joinedRole := "participant"
	if participant != nil {
		joinedRole = participant.Role
		participant.JoinStatus = "joined"
		participant.JoinedAt = &now
		if err := s.repo.UpdateParticipant(ctx, participant); err != nil {
			return err
		}
	} else {
		if err := s.repo.CreateParticipant(ctx, &models.CallParticipant{
			CallLogID:   callLog.ID,
			UserID:      userID,
			PhoneNumber: phone,
			Role:        "participant",
			JoinStatus:  "joined",
			JoinedAt:    &now,
		}); err != nil {
			return err
		}
	}

	// StartAt is set only when a non-initiator joins — that marks the real call start.
	if callLog.Status == "initiated" && joinedRole != "initiator" {
		updates := map[string]interface{}{
			"status":     "ongoing",
			"start_at":   now,
			"updated_at": now,
		}
		return s.repo.UpdateByField(ctx, callLog.ID, updates)
	}

	return nil
}

func (s *callLogService) GetCallLogsByUserID(ctx context.Context, userID uuid.UUID, page, limit int) ([]models.CallLogResponse, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}

	callLogs, total, err := s.repo.FindByUserID(ctx, userID, page, limit)
	if err != nil {
		return nil, 0, err
	}

	responses := make([]models.CallLogResponse, len(callLogs))
	for i, callLog := range callLogs {
		resp, err := s.toCallLogResponse(ctx, &callLog)
		if err != nil {
			return nil, 0, err
		}
		responses[i] = resp
	}

	return responses, total, nil
}

func (s *callLogService) GetSipInfo(ctx context.Context) (map[string]interface{}, error) {
	domain := os.Getenv("SIP_DOMAIN")
	socketURL := os.Getenv("SIP_SOCKET_URL")
	if os.Getenv("SIP_INTEGRATION_ENABLED") != "true" || domain == "" || socketURL == "" {
		return map[string]interface{}{
			"enabled":   false,
			"socketURL": "",
			"domain":    "",
		}, nil
	}

	return map[string]interface{}{
		"enabled":   true,
		"socketURL": socketURL,
		"domain":    domain,
	}, nil
}

func (s *callLogService) getCallLogResponse(ctx context.Context, id uuid.UUID) (*models.CallLogResponse, error) {
	callLog, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}
	response, err := s.toCallLogResponse(ctx, callLog)
	if err != nil {
		return nil, err
	}
	return &response, nil
}

// toCallLogResponse converts a CallLog (with preloaded Participants) to the API response,
// bulk-fetching user details for registered participants.
func (s *callLogService) toCallLogResponse(ctx context.Context, callLog *models.CallLog) (models.CallLogResponse, error) {
	resp := models.CallLogResponse{
		ID:           callLog.ID,
		CallUuid:     callLog.CallUuid,
		CallType:     callLog.CallType,
		Status:       callLog.Status,
		StartAt:      callLog.StartAt,
		EndAt:        callLog.EndAt,
		RecordingUrl: callLog.RecordingUrl,
		Meta:         callLog.Meta,
		CreatedAt:    callLog.CreatedAt,
		UpdatedAt:    callLog.UpdatedAt,
		Participants: []models.CallParticipantResponse{},
	}

	if len(callLog.Participants) == 0 {
		return resp, nil
	}

	// Collect unique user IDs for a single bulk fetch.
	var userIDs []uuid.UUID
	for _, p := range callLog.Participants {
		if p.UserID != nil {
			userIDs = append(userIDs, *p.UserID)
		}
	}

	userMap := make(map[uuid.UUID]*models.User)
	if len(userIDs) > 0 {
		if users, err := s.userRepo.FindByIDs(ctx, userIDs); err == nil {
			for i := range users {
				userMap[users[i].ID] = &users[i]
			}
		}
	}

	participantResponses := make([]models.CallParticipantResponse, len(callLog.Participants))
	for i, p := range callLog.Participants {
		pr := models.CallParticipantResponse{
			ID:          p.ID,
			UserID:      p.UserID,
			PhoneNumber: p.PhoneNumber,
			Role:        p.Role,
			JoinStatus:  p.JoinStatus,
			JoinedAt:    p.JoinedAt,
			LeftAt:      p.LeftAt,
		}
		if p.UserID != nil {
			if u, ok := userMap[*p.UserID]; ok {
				pr.User = &models.UserMinimalResponse{
					ID:        u.ID,
					Extension: u.Extension,
				}
			}
		}
		participantResponses[i] = pr
	}
	resp.Participants = participantResponses

	return resp, nil
}

// Attachments

func (s *callLogService) AddAttachment(ctx context.Context, callUUID string, attachment *models.CallLogAttachment) error {
	// Resolve the SIP call UUID to the actual call log record so we have the primary key.
	callLog, err := s.repo.FindByCallUUID(ctx, callUUID)
	if err != nil {
		return err
	}

	attachment.CallLogID = callLog.ID

	if err := s.repo.CreateAttachment(ctx, attachment); err != nil {
		return err
	}

	if attachment.ID != uuid.Nil {
		recordingURL := pkgUtils.GenerateAttachmentAppURL(ctx, attachment.ID)
		updates := map[string]interface{}{"recording_url": recordingURL}
		if err := s.repo.UpdateByField(ctx, callLog.ID, updates); err != nil {
			return err
		}
	}

	return nil
}

func (s *callLogService) GetAttachment(ctx context.Context, attachmentID uuid.UUID) (*models.CallLogAttachment, error) {
	return s.repo.FindAttachmentByID(ctx, attachmentID)
}
