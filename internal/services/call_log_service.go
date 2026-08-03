package services

import (
	"context"
	"fmt"
	"log"
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
	JoinCall(ctx context.Context, callUUID string, phone string) error
	GetCallLogsByPhone(ctx context.Context, phone string, page, limit int) ([]models.CallLogResponse, int64, error)
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
		CallUuid:  req.CallUuid,
		CallType:  req.CallType,
		StartAt:   req.StartAt,
		EndAt:     req.EndAt,
		Status:    req.Status,
		Meta:      req.Meta,
		CreatedAt: time.Now(),
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
			cp.PhoneNumber = participants[i].Phone
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

	// Bulk-fetch users by participant phone numbers across all calls.
	phoneSet := make(map[string]struct{})
	for _, cl := range callLogs {
		for _, p := range cl.Participants {
			if p.PhoneNumber != "" {
				phoneSet[p.PhoneNumber] = struct{}{}
			}
		}
	}
	phones := make([]string, 0, len(phoneSet))
	for ph := range phoneSet {
		phones = append(phones, ph)
	}
	nameMap := make(map[string]string) // phone/ext → full name
	extMap := make(map[string]string)  // phone/ext → extension
	if users, err := s.userRepo.FindByExtOrPhoneList(ctx, phones); err == nil {
		for _, u := range users {
			fullName := strings.TrimSpace(u.FirstName + " " + u.LastName)
			if u.Extension != "" {
				nameMap[u.Extension] = fullName
				extMap[u.Extension] = u.Extension
			}
			if u.Phone != "" {
				nameMap[u.Phone] = fullName
				extMap[u.Phone] = u.Extension
			}
		}
	}

	// Resolve the current user's phone/extension for direction detection.
	currentUserPhone := ""
	currentUserExt := ""
	if u, err := s.userRepo.FindByID(ctx, currentUserID); err == nil {
		currentUserPhone = u.Phone
		currentUserExt = u.Extension
	}

	items := make([]models.CallLogListItem, len(callLogs))
	for i, cl := range callLogs {
		var recordingURL string
		if len(cl.Attachments) > 0 {
			recordingURL = pkgUtils.GenerateAttachmentAppURL(ctx, cl.Attachments[0].ID)
		} else if cl.CallUuid != "" && models.RecordingURLFromMeta(cl.Meta) != "" {
			// Cintrix-originated call with a recording — proxy through the CTI
			// endpoint rather than exposing the raw (HMAC-gated) Cintrix URL.
			recordingURL = pkgUtils.GenerateCTIRecordingAppURL(ctx, cl.CallUuid)
		}
		item := models.CallLogListItem{
			ID:           cl.ID,
			CallUuid:     cl.CallUuid,
			CallType:     cl.CallType,
			Status:       cl.Status,
			RecordingUrl: recordingURL,
			CreatedAt:    cl.CreatedAt,
		}

		// Identify initiator, first non-initiator, and current user's own participant.
		var initiatorParticipant *models.CallParticipant
		var otherParticipant *models.CallParticipant
		var myParticipant *models.CallParticipant
		for j := range cl.Participants {
			p := &cl.Participants[j]
			if p.Role == "initiator" {
				initiatorParticipant = p
			} else if otherParticipant == nil {
				otherParticipant = p
			}
			if p.PhoneNumber == currentUserExt || p.PhoneNumber == currentUserPhone {
				myParticipant = p
			}
		}

		// Duration = how long the current user was on the call (joined_at → left_at).
		// Falls back to end_at when left_at is not recorded, and to total call duration
		// when no participant record is matched.
		duration := 0
		if myParticipant != nil && myParticipant.JoinedAt != nil {
			endTime := myParticipant.LeftAt
			if endTime == nil {
				endTime = cl.EndAt
			}
			if endTime != nil {
				if secs := int(endTime.Sub(*myParticipant.JoinedAt).Seconds()); secs > 0 {
					duration = secs
				}
			}
		} else if cl.StartAt != nil && cl.EndAt != nil {
			if secs := int(cl.EndAt.Sub(*cl.StartAt).Seconds()); secs > 0 {
				duration = secs
			}
		}
		item.Duration = duration

		// Direction: outgoing if the current user is the initiator.
		isInitiator := initiatorParticipant != nil &&
			(initiatorParticipant.PhoneNumber == currentUserExt || initiatorParticipant.PhoneNumber == currentUserPhone)
		if isInitiator {
			item.Direction = "outgoing"
		} else {
			item.Direction = "incoming"
		}

		otherP := otherParticipant
		if !isInitiator {
			otherP = initiatorParticipant
		}
		if otherP != nil {
			item.OtherPartyPhone = otherP.PhoneNumber
			item.OtherPartyExtension = extMap[otherP.PhoneNumber]
			item.OtherPartyName = otherPartyName(otherP.DisplayName, nameMap[otherP.PhoneNumber], otherP.PhoneNumber)
		}

		items[i] = item
	}

	return items, total, nil
}

// otherPartyName picks the best display name for the non-current participant:
// the CTI-provided contact/display name, else a name resolved from the users
// table, else the raw phone number (frontend renders "Unknown" only when all
// are empty).
func otherPartyName(displayName, nameFromUsers, phone string) string {
	if displayName != "" {
		return displayName
	}
	if nameFromUsers != "" {
		return nameFromUsers
	}
	return phone
}

func (s *callLogService) GetStats(ctx context.Context) (*models.CallLogStats, error) {
	return s.repo.GetStats(ctx)
}

// setUserCallStatus looks up a user by phone/extension and updates their call_status.
// Uses UpdateProfile (targeted map update) because the generic Update method does not
// include call_status in its field list.
// Silently skips if the user is not found in the users table.
func (s *callLogService) setUserCallStatus(ctx context.Context, phone, status string) {
	user, err := s.userRepo.FindByExtOrPhone(ctx, phone)
	if err != nil {
		return // not a system user — skip
	}
	if string(user.CallStatus) == status {
		return
	}
	if err := s.userRepo.UpdateProfile(ctx, map[string]interface{}{
		"id":          user.ID,
		"call_status": status,
	}); err != nil {
		log.Printf("[CallLog] setUserCallStatus: failed to update user %s: %v", user.ID, err)
	}
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
		participants = append(participants, &models.CallParticipant{
			PhoneNumber: r.Phone,
			Role:        recipientRole,
			JoinStatus:  "invited",
		})
	}

	if err := s.repo.CreateWithParticipants(ctx, callLog, participants); err != nil {
		return nil, err
	}

	// Initiator is immediately joined — mark them in_call.
	s.setUserCallStatus(ctx, initiator.Phone, "in_call")

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

	// If start_at was never set (recipient never joined), derive it from the
	// earliest joined_at among participants so duration can be computed.
	if callLog.StartAt == nil {
		for _, p := range callLog.Participants {
			if p.JoinedAt == nil {
				continue
			}
			existing, ok := updates["start_at"].(time.Time)
			if !ok || p.JoinedAt.Before(existing) {
				updates["start_at"] = *p.JoinedAt
			}
		}
	}

	if err := s.repo.UpdateByField(ctx, callLog.ID, updates); err != nil {
		return nil, err
	}

	if err := s.repo.UpdateParticipantsLeftAt(ctx, callLog.ID, *endAt); err != nil {
		return nil, err
	}

	// Revert in_call → online for all participants found in the users table.
	phones := make([]string, 0, len(callLog.Participants))
	for _, p := range callLog.Participants {
		phones = append(phones, p.PhoneNumber)
	}
	if len(phones) > 0 {
		users, err := s.userRepo.FindByExtOrPhoneList(ctx, phones)
		if err == nil {
			for i := range users {
				if users[i].CallStatus == models.CallStatusInCall {
					s.userRepo.UpdateProfile(ctx, map[string]interface{}{
						"id":          users[i].ID,
						"call_status": string(models.CallStatusOnline),
					})
				}
			}
		}
	}

	return s.getCallLogResponse(ctx, callLog.ID)
}

func (s *callLogService) JoinCall(ctx context.Context, callUUID string, phone string) error {
	callLog, err := s.repo.FindByCallUUID(ctx, callUUID)
	if err != nil {
		return err
	}

	now := time.Now()

	participant, err := s.repo.FindParticipant(ctx, callLog.ID, phone)
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
			PhoneNumber: phone,
			Role:        "participant",
			JoinStatus:  "joined",
			JoinedAt:    &now,
		}); err != nil {
			return err
		}
	}

	// Mark the joining user in_call if they exist in the users table.
	s.setUserCallStatus(ctx, phone, "in_call")

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

func (s *callLogService) GetCallLogsByPhone(ctx context.Context, phone string, page, limit int) ([]models.CallLogResponse, int64, error) {
	if page <= 0 {
		page = 1
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}

	callLogs, total, err := s.repo.FindByPhone(ctx, phone, page, limit)
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
// bulk-fetching user names for participants by phone/extension.
func (s *callLogService) toCallLogResponse(ctx context.Context, callLog *models.CallLog) (models.CallLogResponse, error) {
	var recordingURL string
	if len(callLog.Attachments) > 0 {
		recordingURL = pkgUtils.GenerateAttachmentAppURL(ctx, callLog.Attachments[0].ID)
	} else if callLog.CallUuid != "" && models.RecordingURLFromMeta(callLog.Meta) != "" {
		recordingURL = pkgUtils.GenerateCTIRecordingAppURL(ctx, callLog.CallUuid)
	}
	resp := models.CallLogResponse{
		ID:           callLog.ID,
		CallUuid:     callLog.CallUuid,
		CallType:     callLog.CallType,
		Status:       callLog.Status,
		StartAt:      callLog.StartAt,
		EndAt:        callLog.EndAt,
		RecordingUrl: recordingURL,
		Meta:         callLog.Meta,
		CreatedAt:    callLog.CreatedAt,
		UpdatedAt:    callLog.UpdatedAt,
		Participants: []models.CallParticipantResponse{},
	}

	if len(callLog.Participants) == 0 {
		return resp, nil
	}

	// Bulk-fetch users matching participant phone numbers/extensions.
	phones := make([]string, 0, len(callLog.Participants))
	for _, p := range callLog.Participants {
		if p.PhoneNumber != "" {
			phones = append(phones, p.PhoneNumber)
		}
	}
	nameMap := make(map[string]string) // phone/ext → full name
	if users, err := s.userRepo.FindByExtOrPhoneList(ctx, phones); err == nil {
		for _, u := range users {
			fullName := strings.TrimSpace(u.FirstName + " " + u.LastName)
			if u.Extension != "" {
				nameMap[u.Extension] = fullName
			}
			if u.Phone != "" {
				nameMap[u.Phone] = fullName
			}
		}
	}

	participantResponses := make([]models.CallParticipantResponse, len(callLog.Participants))
	for i, p := range callLog.Participants {
		name := nameMap[p.PhoneNumber]
		if name == "" {
			name = p.PhoneNumber
		}
		participantResponses[i] = models.CallParticipantResponse{
			ID:          p.ID,
			PhoneNumber: p.PhoneNumber,
			Name:        name,
			Role:        p.Role,
			JoinStatus:  p.JoinStatus,
			JoinedAt:    p.JoinedAt,
			LeftAt:      p.LeftAt,
		}
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

	return s.repo.CreateAttachment(ctx, attachment)
}

func (s *callLogService) GetAttachment(ctx context.Context, attachmentID uuid.UUID) (*models.CallLogAttachment, error) {
	return s.repo.FindAttachmentByID(ctx, attachmentID)
}
