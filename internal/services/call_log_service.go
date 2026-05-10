package services

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/internal/storage"
	pkgUtils "github.com/automax/backend/pkg/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type CallLogService interface {
	CreateCallLog(ctx context.Context, req *models.CallLogCreateRequest, createdBy uuid.UUID) (*models.CallLogResponse, error)
	GetCallLog(ctx context.Context, id uuid.UUID) (*models.CallLogResponse, error)
	UpdateCallLog(ctx context.Context, id uuid.UUID, req *models.CallLogUpdateRequest) (*models.CallLogResponse, error)
	DeleteCallLog(ctx context.Context, id uuid.UUID) error
	ListCallLogs(ctx context.Context, filter *models.CallLogFilter) ([]models.CallLogResponse, int64, error)
	ListCallLogsSummary(ctx context.Context, filter *models.CallLogFilter, currentUserID uuid.UUID) ([]models.CallLogListItem, int64, error)
	GetStats(ctx context.Context) (*models.CallLogStats, error)
	StartCall(ctx context.Context, callUUID string, createdBy uuid.UUID, participants []uuid.UUID) (*models.CallLogResponse, error)
	EndCall(ctx context.Context, callUUID string, endAt *time.Time, status string) (*models.CallLogResponse, error)
	JoinCall(ctx context.Context, callUUID string, userID uuid.UUID) error
	GetCallLogsByUserID(ctx context.Context, userID uuid.UUID, page, limit int) ([]models.CallLogResponse, int64, error)
	GetSipInfo(ctx context.Context) (map[string]interface{}, error)

	// Attachments
	AddAttachment(ctx context.Context, callUUID uuid.UUID, attachment *models.CallLogAttachment) error
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

func (s *callLogService) CreateCallLog(ctx context.Context, req *models.CallLogCreateRequest, createdBy uuid.UUID) (*models.CallLogResponse, error) {
	meta := req.Meta
	if meta == "" {
		meta = "{}"
	}

	callLog := &models.CallLog{
		CallUuid:     req.CallUuid,
		CreatedBy:    createdBy,
		StartAt:      req.StartAt,
		EndAt:        req.EndAt,
		Status:       req.Status,
		Participants: req.Participants,
		InvitedUsers: req.InvitedUsers,
		RecordingUrl: req.RecordingUrl,
		Meta:         meta,
		CreatedAt:    time.Now(),
	}

	if err := s.repo.Create(ctx, callLog); err != nil {
		return nil, err
	}

	return s.getCallLogResponse(ctx, callLog.ID)
}

func (s *callLogService) GetCallLog(ctx context.Context, id uuid.UUID) (*models.CallLogResponse, error) {
	return s.getCallLogResponse(ctx, id)
}

func (s *callLogService) UpdateCallLog(ctx context.Context, id uuid.UUID, req *models.CallLogUpdateRequest) (*models.CallLogResponse, error) {
	callLog, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	if req.StartAt != nil {
		callLog.StartAt = req.StartAt
	}
	if req.EndAt != nil {
		callLog.EndAt = req.EndAt
	}
	if req.Status != "" {
		callLog.Status = req.Status
	}
	if req.Participants != nil {
		callLog.Participants = req.Participants
	}
	if req.JoinedUsers != nil {
		callLog.JoinedUsers = req.JoinedUsers
	}
	if req.InvitedUsers != nil {
		callLog.InvitedUsers = req.InvitedUsers
	}
	if req.RecordingUrl != "" {
		callLog.RecordingUrl = req.RecordingUrl
	}
	if req.Meta != "" {
		if req.Meta == "" {
			callLog.Meta = "{}"
		} else {
			callLog.Meta = req.Meta
		}
	}

	callLog.UpdatedAt = &time.Time{}
	*callLog.UpdatedAt = time.Now()

	if err := s.repo.Update(ctx, callLog); err != nil {
		return nil, err
	}

	return s.getCallLogResponse(ctx, id)
}

func (s *callLogService) DeleteCallLog(ctx context.Context, id uuid.UUID) error {
	return s.repo.Delete(ctx, id)
}

func (s *callLogService) ListCallLogs(ctx context.Context, filter *models.CallLogFilter) ([]models.CallLogResponse, int64, error) {
	// Set defaults
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
		// Populate user details for participants, joined users, and invited users
		resp, err := s.toCallLogResponseWithUsers(ctx, &callLog)
		if err != nil {
			return nil, 0, err
		}
		responses[i] = resp
	}

	return responses, total, nil
}

// ListCallLogsSummary returns the slim list view for call logs.
// Direction is "outgoing" when the current user created the call, "incoming" otherwise.
// Other party is the creator (for incoming) or the first non-self participant (for outgoing).
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

	// Collect other-party user IDs to fetch in bulk.
	otherPartyIDs := make([]uuid.UUID, 0, len(callLogs))
	directions := make([]string, len(callLogs))

	for i, cl := range callLogs {
		if cl.CreatedBy == currentUserID {
			directions[i] = "outgoing"
			// Other party = first participant who is not the current user.
			for _, pid := range cl.Participants {
				if pid != currentUserID {
					otherPartyIDs = append(otherPartyIDs, pid)
					break
				}
			}
			// Fall back to invited users if participants list is empty.
			if len(otherPartyIDs) == i {
				for _, pid := range cl.InvitedUsers {
					if pid != currentUserID {
						otherPartyIDs = append(otherPartyIDs, pid)
						break
					}
				}
			}
		} else {
			directions[i] = "incoming"
			// Other party = the creator.
			otherPartyIDs = append(otherPartyIDs, cl.CreatedBy)
		}
	}

	// Bulk-fetch other-party users.
	userMap := make(map[uuid.UUID]*models.User)
	if len(otherPartyIDs) > 0 {
		users, err := s.userRepo.FindByIDs(ctx, otherPartyIDs)
		if err == nil {
			for i := range users {
				userMap[users[i].ID] = &users[i]
			}
		}
	}

	items := make([]models.CallLogListItem, len(callLogs))
	otherPartyIdx := 0
	for i, cl := range callLogs {
		duration := 0
		if cl.StartAt != nil && cl.EndAt != nil {
			duration = int(cl.EndAt.Sub(*cl.StartAt).Seconds())
		}

		item := models.CallLogListItem{
			ID:        cl.ID,
			CallUuid:  cl.CallUuid,
			Status:    cl.Status,
			Direction: directions[i],
			Duration:  duration,
			CreatedAt: cl.CreatedAt,
		}

		if otherPartyIdx < len(otherPartyIDs) {
			if u, ok := userMap[otherPartyIDs[otherPartyIdx]]; ok {
				item.OtherPartyName = u.FirstName + " " + u.LastName
				item.OtherPartyExtension = u.Extension
			}
			otherPartyIdx++
		}

		items[i] = item
	}

	return items, total, nil
}

func (s *callLogService) GetStats(ctx context.Context) (*models.CallLogStats, error) {
	return s.repo.GetStats(ctx)
}

func (s *callLogService) StartCall(ctx context.Context, callUUID string, createdBy uuid.UUID, participants []uuid.UUID) (*models.CallLogResponse, error) {
	callLog, err := s.repo.FindByCallUUID(ctx, callUUID)
	if err != nil && err != gorm.ErrRecordNotFound {
		return nil, err
	}

	if callLog != nil {
		return nil, fmt.Errorf("call already exists") // Call already exists, do not create a duplicate
	}

	// now := time.Now()
	req := &models.CallLogCreateRequest{
		CallUuid: callUUID,
		// StartAt:      &now,
		Status:       "ongoing",
		Participants: participants,
		InvitedUsers: participants,
	}

	return s.CreateCallLog(ctx, req, createdBy)
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

	updateReq := &models.CallLogUpdateRequest{
		EndAt:  endAt,
		Status: status,
	}

	return s.UpdateCallLog(ctx, callLog.ID, updateReq)
}

func (s *callLogService) JoinCall(ctx context.Context, callUUID string, userID uuid.UUID) error {
	callLog, err := s.repo.FindByCallUUID(ctx, callUUID)
	if err != nil {
		return err
	}

	// Add user to joined users if not already present
	found := false
	for _, id := range callLog.JoinedUsers {
		if id == userID {
			found = true
			break
		}
	}
	if !found {
		log.Println("Found User: ", userID)
		startAt := time.Now()
		callLog.JoinedUsers = append(callLog.JoinedUsers, userID)
		callLog.StartAt = &startAt
		callLog.UpdatedAt = &time.Time{}
		*callLog.UpdatedAt = time.Now()
		return s.repo.Update(ctx, callLog)
	}

	return nil
}

func (s *callLogService) getCallLogResponse(ctx context.Context, id uuid.UUID) (*models.CallLogResponse, error) {
	callLog, err := s.repo.FindByID(ctx, id)
	if err != nil {
		return nil, err
	}

	// Populate user details for participants, joined users, and invited users
	response, err := s.toCallLogResponseWithUsers(ctx, callLog)
	if err != nil {
		return nil, err
	}
	return &response, nil
}

func (s *callLogService) GetCallLogsByUserID(ctx context.Context, userID uuid.UUID, page, limit int) ([]models.CallLogResponse, int64, error) {
	// Set defaults
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
		// Populate user details for participants, joined users, and invited users
		resp, err := s.toCallLogResponseWithUsers(ctx, &callLog)
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

	sipInfo := map[string]interface{}{
		"enabled":   true,
		"socketURL": socketURL,
		"domain":    domain,
	}

	return sipInfo, nil
}

// populateUserDetails fetches user details for given UUIDs and returns UserMinimalResponse array
func (s *callLogService) populateUserDetails(ctx context.Context, userIDs models.UUIDArray) ([]models.UserMinimalResponse, error) {
	if len(userIDs) == 0 {
		return []models.UserMinimalResponse{}, nil
	}

	users, err := s.userRepo.FindByIDs(ctx, userIDs)
	if err != nil {
		return nil, err
	}

	// Convert to UserMinimalResponse with only ID and Extension
	userResponses := make([]models.UserMinimalResponse, len(users))
	for i, user := range users {
		userResponses[i] = models.UserMinimalResponse{
			ID:        user.ID,
			Extension: user.Extension,
		}
	}

	return userResponses, nil
}

// toCallLogResponseWithUsers converts CallLog to CallLogResponse and populates user details
func (s *callLogService) toCallLogResponseWithUsers(ctx context.Context, callLog *models.CallLog) (models.CallLogResponse, error) {
	resp := models.CallLogResponse{
		ID:           callLog.ID,
		CallUuid:     callLog.CallUuid,
		CreatedBy:    callLog.CreatedBy,
		StartAt:      callLog.StartAt,
		EndAt:        callLog.EndAt,
		Status:       callLog.Status,
		RecordingUrl: callLog.RecordingUrl,
		Meta:         callLog.Meta,
		CreatedAt:    callLog.CreatedAt,
		UpdatedAt:    callLog.UpdatedAt,
	}

	// Populate creator if available
	if callLog.Creator != nil {
		creatorResp := models.ToUserResponse(callLog.Creator)
		resp.Creator = &creatorResp
	}

	// Populate participants
	if len(callLog.Participants) > 0 {
		participants, err := s.populateUserDetails(ctx, callLog.Participants)
		if err != nil {
			return resp, err
		}
		resp.Participants = participants
	} else {
		resp.Participants = []models.UserMinimalResponse{}
	}

	// Populate joined users
	if len(callLog.JoinedUsers) > 0 {
		joinedUsers, err := s.populateUserDetails(ctx, callLog.JoinedUsers)
		if err != nil {
			return resp, err
		}
		resp.JoinedUsers = joinedUsers
	} else {
		resp.JoinedUsers = []models.UserMinimalResponse{}
	}

	// Populate invited users
	if len(callLog.InvitedUsers) > 0 {
		invitedUsers, err := s.populateUserDetails(ctx, callLog.InvitedUsers)
		if err != nil {
			return resp, err
		}
		resp.InvitedUsers = invitedUsers
	} else {
		resp.InvitedUsers = []models.UserMinimalResponse{}
	}

	return resp, nil
}

// Attachments

func (s *callLogService) AddAttachment(ctx context.Context, callLogID uuid.UUID, attachment *models.CallLogAttachment) error {
	attachment.CallLogID = callLogID

	if err := s.repo.CreateAttachment(ctx, attachment); err != nil {
		return err
	}

	// created, err := s.repo.FindAttachmentByID(ctx, attachment.ID)
	// if err != nil {
	// 	return nil, err
	// }

	// @Todo need to add a log to record for the call attachment maybe maybe not
	// url, err := s.storage.GetFileURL(ctx, attachment.FilePath)
	// if err != nil {
	// 	// Log the error but don't fail the operation
	// 	fmt.Printf("Warning: failed to get presigned URL for attachment %s: %v\n", attachment.ID, err)
	// }

	if attachment.ID != uuid.Nil {
		recordingURL := pkgUtils.GenerateAttachmentAppURL(ctx, attachment.ID)
		updates := map[string]interface{}{"recording_url": recordingURL}
		if err := s.repo.UpdateByField(ctx, callLogID, updates); err != nil {
			return err
		}

	}

	// resp := models.ToCallLogAttachmentResponse(attachment, url)
	return nil
}

func (s *callLogService) GetAttachment(ctx context.Context, attachmentID uuid.UUID) (*models.CallLogAttachment, error) {
	return s.repo.FindAttachmentByID(ctx, attachmentID)
}
