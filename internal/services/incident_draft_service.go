package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"math"
	"mime/multipart"
	"reflect"
	"strings"
	"time"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/i18n"
	"github.com/automax/backend/pkg/validation"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// Sentinels carry their i18n key as the message.
var (
	ErrDraftNotFound        = repository.ErrDraftNotFound
	ErrDraftVersionConflict = repository.ErrDraftVersionConflict
	ErrDraftPublished       = repository.ErrDraftPublished
	ErrDraftEmpty           = errors.New("incident_draft_empty")
	ErrDraftRequestPending  = errors.New("incident_draft_request_in_progress")
)

// DraftValidationError is returned by Publish when the draft is not yet a valid
// incident. Fields maps JSON field name to a translated message.
type DraftValidationError struct {
	Fields map[string]string
}

func (e *DraftValidationError) Error() string {
	return fmt.Sprintf("draft has %d invalid fields", len(e.Fields))
}

// draftFieldNames are the payload fields INCIDENT_DRAFT_REQUIRED_FIELDS may name
// (JSON keys of the payload).
var draftFieldNames = map[string]bool{
	"title": true, "description": true, "classification_id": true, "location_id": true,
	"latitude": true, "longitude": true, "source": true, "record_type": true, "lookup_value_ids": true,
	"caller_first_name": true, "caller_middle_name": true, "caller_last_name": true,
	"caller_phone": true, "caller_alternate_mobile_number": true,
}

// normalizeRequiredFields trims the configured names and drops unknown ones with a warning;
// an unknown name could never be filled, which would make every publish fail.
func normalizeRequiredFields(configured []string) []string {
	out := make([]string, 0, len(configured))
	seen := map[string]bool{}
	for _, f := range configured {
		f = strings.TrimSpace(f)
		switch {
		case f == "" || seen[f]:
		case !draftFieldNames[f]:
			log.Printf("[IncidentDraft] ignoring unknown INCIDENT_DRAFT_REQUIRED_FIELDS entry %q", f)
		default:
			seen[f] = true
			out = append(out, f)
		}
	}
	return out
}

const (
	draftCacheTTL      = 10 * time.Minute
	draftRequestKeyTTL = 5 * time.Minute
)

func draftCacheKey(id uuid.UUID) string { return "incident_draft:" + id.String() }
func draftRequestKey(owner uuid.UUID, clientID string) string {
	return "incident_draft:req:" + owner.String() + ":" + clientID
}

type IncidentDraftService interface {
	Create(ctx context.Context, req *models.IncidentDraftSaveRequest, ownerID uuid.UUID) (*models.IncidentDraftResponse, error)
	Update(ctx context.Context, id uuid.UUID, req *models.IncidentDraftSaveRequest, ownerID uuid.UUID) (*models.IncidentDraftResponse, error)
	Get(ctx context.Context, id, ownerID uuid.UUID) (*models.IncidentDraftResponse, error)
	List(ctx context.Context, ownerID uuid.UUID, filter *models.IncidentDraftFilter) ([]models.IncidentDraftListItem, int64, error)
	Count(ctx context.Context, ownerID uuid.UUID) (int64, error)
	Delete(ctx context.Context, id, ownerID uuid.UUID) error
	// Publish creates the incident (once) and moves the draft's attachments to it. If some
	// attachments could not be moved the draft stays unpublished with its incident id saved,
	// FailedAttachments lists them, and calling Publish again retries only those.
	Publish(ctx context.Context, id, ownerID uuid.UUID) (*PublishResult, error)
	// PruneExpired hard-deletes drafts past the configured TTL (except keep=true),
	// including their attachment rows and stored files.
	PruneExpired(ctx context.Context) (int64, error)

	AddAttachment(ctx context.Context, draftID, ownerID uuid.UUID, file multipart.File, header *multipart.FileHeader) (*models.IncidentDraftAttachmentResponse, error)
	ListAttachments(ctx context.Context, draftID, ownerID uuid.UUID) ([]models.IncidentDraftAttachmentResponse, error)
	DeleteAttachment(ctx context.Context, draftID, attachmentID, ownerID uuid.UUID) error
	// OpenAttachment returns the attachment and its file; the caller must close the reader.
	OpenAttachment(ctx context.Context, draftID, attachmentID, ownerID uuid.UUID) (*models.IncidentDraftAttachment, io.ReadCloser, error)
}

// PublishResult is what Publish returns.
type PublishResult struct {
	Incident          *models.IncidentResponse
	FailedAttachments []string
}

// DraftFileStorage is the part of object storage that draft attachments use.
type DraftFileStorage interface {
	UploadFile(ctx context.Context, file multipart.File, header *multipart.FileHeader, folder string) (string, error)
	DeleteFile(ctx context.Context, objectName string) error
	GetFile(ctx context.Context, objectName string) (io.ReadCloser, error)
}

type incidentDraftService struct {
	repo            repository.IncidentDraftRepository
	incidentService IncidentService
	workflowService WorkflowService
	fileStorage     DraftFileStorage
	redis           *redis.Client
	cfg             config.IncidentDraftConfig
	requiredFields  []string
}

func NewIncidentDraftService(repo repository.IncidentDraftRepository, incidentService IncidentService, workflowService WorkflowService, fileStorage DraftFileStorage, redisClient *redis.Client, cfg config.IncidentDraftConfig) IncidentDraftService {
	return &incidentDraftService{repo: repo, incidentService: incidentService, workflowService: workflowService, fileStorage: fileStorage, redis: redisClient, cfg: cfg, requiredFields: normalizeRequiredFields(cfg.RequiredFields)}
}

func (s *incidentDraftService) Create(ctx context.Context, req *models.IncidentDraftSaveRequest, ownerID uuid.UUID) (*models.IncidentDraftResponse, error) {
	if isDraftPayloadEmpty(&req.IncidentDraftPayload) {
		return nil, ErrDraftEmpty
	}

	// Idempotency: a repeated first save with the same client_request_id returns the
	// draft the first request created instead of making a second one.
	reqKey := ""
	if req.ClientRequestID != "" && s.redis != nil {
		reqKey = draftRequestKey(ownerID, req.ClientRequestID)
		ok, err := s.redis.SetNX(ctx, reqKey, "pending", draftRequestKeyTTL).Result()
		if err != nil {
			log.Printf("[IncidentDraft] request key SetNX failed: %v", err)
			reqKey = ""
		} else if !ok {
			val, gerr := s.redis.Get(ctx, reqKey).Result()
			if gerr == nil {
				if existingID, perr := uuid.Parse(val); perr == nil {
					return s.Get(ctx, existingID, ownerID)
				}
			}
			return nil, ErrDraftRequestPending
		}
	}

	draft, err := s.buildDraft(&req.IncidentDraftPayload)
	if err != nil {
		s.releaseRequestKey(ctx, reqKey)
		return nil, err
	}
	draft.CreatedBy = ownerID
	draft.Keep = req.Keep != nil && *req.Keep

	if err := s.repo.Create(ctx, draft); err != nil {
		s.releaseRequestKey(ctx, reqKey)
		return nil, err
	}
	if reqKey != "" {
		if err := s.redis.Set(ctx, reqKey, draft.ID.String(), draftRequestKeyTTL).Err(); err != nil {
			log.Printf("[IncidentDraft] request key store failed: %v", err)
		}
	}
	return s.toResponse(draft), nil
}

func (s *incidentDraftService) Update(ctx context.Context, id uuid.UUID, req *models.IncidentDraftSaveRequest, ownerID uuid.UUID) (*models.IncidentDraftResponse, error) {
	if isDraftPayloadEmpty(&req.IncidentDraftPayload) {
		return nil, ErrDraftEmpty
	}
	if req.Version < 1 {
		return nil, ErrDraftVersionConflict
	}

	draft, err := s.buildDraft(&req.IncidentDraftPayload)
	if err != nil {
		return nil, err
	}
	draft.ID = id
	draft.CreatedBy = ownerID

	if err := s.repo.UpdateWithVersion(ctx, draft, req.Version, req.Keep); err != nil {
		return nil, err
	}
	s.invalidate(ctx, id)

	// Reload for created_at / keep, which the partial update does not carry.
	return s.Get(ctx, id, ownerID)
}

func (s *incidentDraftService) Get(ctx context.Context, id, ownerID uuid.UUID) (*models.IncidentDraftResponse, error) {
	if s.redis != nil {
		if raw, err := s.redis.Get(ctx, draftCacheKey(id)).Bytes(); err == nil {
			var cached models.IncidentDraft
			if json.Unmarshal(raw, &cached) == nil && cached.CreatedBy == ownerID {
				return s.toResponse(&cached), nil
			}
		}
	}

	draft, err := s.repo.FindByID(ctx, id, ownerID)
	if err != nil {
		return nil, err
	}
	if s.redis != nil {
		if raw, merr := json.Marshal(draft); merr == nil {
			if serr := s.redis.Set(ctx, draftCacheKey(id), raw, draftCacheTTL).Err(); serr != nil {
				log.Printf("[IncidentDraft] cache set failed: %v", serr)
			}
		}
	}
	return s.toResponse(draft), nil
}

func (s *incidentDraftService) List(ctx context.Context, ownerID uuid.UUID, filter *models.IncidentDraftFilter) ([]models.IncidentDraftListItem, int64, error) {
	drafts, total, err := s.repo.List(ctx, ownerID, filter)
	if err != nil {
		return nil, 0, err
	}
	items := make([]models.IncidentDraftListItem, 0, len(drafts))
	for i := range drafts {
		d := &drafts[i]
		filled, required := s.countRequired(d)
		var caller models.CallerInfo
		_ = json.Unmarshal(d.CallerInfo, &caller)
		item := models.IncidentDraftListItem{
			ID:               d.ID,
			DraftNumber:      d.DraftNumber,
			RecordType:       d.RecordType,
			ClassificationID: d.ClassificationID,
			LocationID:       d.LocationID,
			Description:      d.Description,
			Latitude:         d.Latitude,
			Longitude:        d.Longitude,
			CallerName:       caller.FullName(),
			CallerPhone:      models.MaskPhone(caller.Phone),
			Keep:             d.Keep,
			Version:          d.Version,
			UpdatedAt:        d.UpdatedAt,
			FilledFields:     filled,
			RequiredFields:   required,
			DaysToExpiry:     s.daysToExpiry(d),
		}
		if d.Classification != nil {
			item.ClassName, item.ClassNameAr = d.Classification.Name, d.Classification.NameAr
		}
		if d.Location != nil {
			item.LocationName, item.LocationNameAr = d.Location.Name, d.Location.NameAr
		}
		items = append(items, item)
	}
	return items, total, nil
}

func (s *incidentDraftService) Count(ctx context.Context, ownerID uuid.UUID) (int64, error) {
	return s.repo.Count(ctx, ownerID)
}

func (s *incidentDraftService) Delete(ctx context.Context, id, ownerID uuid.UUID) error {
	paths, err := s.repo.Delete(ctx, id, ownerID)
	if err != nil {
		return err
	}
	s.removeObjects(ctx, paths)
	s.invalidate(ctx, id)
	return nil
}

func (s *incidentDraftService) Publish(ctx context.Context, id, ownerID uuid.UUID) (*PublishResult, error) {
	var result PublishResult

	err := s.repo.Transaction(ctx, func(tx repository.IncidentDraftRepository) error {
		// The row lock serialises concurrent publishes and uploads: the second publish waits,
		// then sees the draft published (or its incident id) and does not create another.
		draft, err := tx.FindByIDForUpdate(ctx, id, ownerID)
		if err != nil {
			return err
		}
		if draft.PublishedAt != nil {
			return ErrDraftPublished
		}

		if draft.PublishedIncidentID != nil {
			// Retry: the incident already exists, only attachments are left to move.
			detail, gerr := s.incidentService.GetIncident(ctx, *draft.PublishedIncidentID)
			if gerr != nil {
				return gerr
			}
			result.Incident = &detail.IncidentResponse
		} else {
			createReq, cerr := draftToCreateRequest(draft.ToPayload())
			if cerr != nil {
				return cerr
			}
			if verr := s.validateForPublish(ctx, draft, createReq); verr != nil {
				return verr
			}

			// Drafts carry no workflow; pick it the way the portal create path does.
			workflowID, werr := s.resolveWorkflow(ctx, createReq)
			if werr != nil {
				return werr
			}
			createReq.WorkflowID = workflowID.String()

			incident, ierr := s.incidentService.CreateIncident(ctx, createReq, ownerID)
			if ierr != nil {
				return ierr
			}
			result.Incident = incident

			// CreateIncident is not part of this transaction. If the process dies or the commit
			// fails before the id below is saved, the incident exists but the draft does not
			// know it. That window is accepted; it cannot be closed without a tx-aware create.
			if serr := tx.SetPublishedIncident(ctx, id, ownerID, incident.ID); serr != nil {
				return serr
			}
			// From here the incident exists, so a failure must not undo it: log and carry on.
			if rerr := s.incidentService.CreateRevision(ctx, incident.ID, models.RevisionActionCreated,
				fmt.Sprintf("Created from draft %s (version %d, last updated %s)",
					draft.DraftNumber, draft.Version, draft.UpdatedAt.UTC().Format("2006-01-02 15:04:05 UTC")),
				draftRevisionChanges(draft), ownerID); rerr != nil {
				log.Printf("[IncidentDraft] revision for draft %s failed: %v", draft.DraftNumber, rerr)
			}
		}

		result.FailedAttachments = s.moveAttachments(ctx, tx, draft, result.Incident.ID)
		if len(result.FailedAttachments) > 0 {
			// Keep the draft open (unpublished) so Publish can be called again for the rest.
			return nil
		}
		return tx.MarkPublished(ctx, id, ownerID)
	})
	if err != nil {
		return nil, err
	}
	s.invalidate(ctx, id)
	return &result, nil
}

func (s *incidentDraftService) PruneExpired(ctx context.Context) (int64, error) {
	cutoff := time.Now().Add(-time.Duration(s.cfg.TTLDays) * 24 * time.Hour)
	n, ids, paths, err := s.repo.PruneExpired(ctx, cutoff)
	if err != nil {
		return 0, err
	}
	for _, id := range ids {
		s.invalidate(ctx, id)
	}
	s.removeObjects(ctx, paths)
	return n, nil
}

// moveAttachments hands the draft's files to the incident: each becomes an
// IncidentAttachment pointing at the same stored object, then the draft row goes. Only rows
// that moved are removed, so a failure never orphans a file that is still wanted. It returns
// the file names that could not be moved (empty means everything moved).
func (s *incidentDraftService) moveAttachments(ctx context.Context, tx repository.IncidentDraftRepository, draft *models.IncidentDraft, incidentID uuid.UUID) []string {
	atts, err := tx.ListAttachments(ctx, draft.ID)
	if err != nil {
		log.Printf("[IncidentDraft] list attachments of %s failed: %v", draft.DraftNumber, err)
		return []string{"unknown"} // names are unknown, but a retry will list them again
	}
	var failed []string
	moved := make([]uuid.UUID, 0, len(atts))
	for i := range atts {
		a := &atts[i]
		_, aerr := s.incidentService.AddAttachment(ctx, incidentID, &models.IncidentAttachment{
			FileName:     a.FileName,
			FileSize:     a.FileSize,
			MimeType:     a.MimeType,
			FilePath:     a.FilePath,
			UploadedByID: a.UploadedByID,
		})
		if aerr != nil {
			log.Printf("[IncidentDraft] move attachment %s of %s failed: %v", a.ID, draft.DraftNumber, aerr)
			failed = append(failed, a.FileName)
			continue
		}
		moved = append(moved, a.ID)
	}
	if err := tx.DeleteAttachmentsByID(ctx, moved); err != nil {
		log.Printf("[IncidentDraft] remove moved attachment rows of %s failed: %v", draft.DraftNumber, err)
	}
	return failed
}

// removeObjects deletes stored files best-effort; a leftover object is only wasted space.
// A key an incident attachment still references is never deleted: publish hands the same
// object to the incident, so a draft row that outlived a failed hand-over (or a rolled-back
// publish) must not take the incident's file with it. If the lookup fails nothing is
// deleted, because a stray file costs less than a lost one.
func (s *incidentDraftService) removeObjects(ctx context.Context, paths []string) {
	if s.fileStorage == nil || len(paths) == 0 {
		return
	}
	referenced, err := s.repo.ReferencedPaths(ctx, paths)
	if err != nil {
		log.Printf("[IncidentDraft] skipping delete of %d objects, reference check failed: %v", len(paths), err)
		return
	}
	for _, p := range paths {
		if referenced[p] {
			continue
		}
		if err := s.fileStorage.DeleteFile(ctx, p); err != nil {
			log.Printf("[IncidentDraft] delete object %q failed: %v", p, err)
		}
	}
}

func (s *incidentDraftService) AddAttachment(ctx context.Context, draftID, ownerID uuid.UUID, file multipart.File, header *multipart.FileHeader) (*models.IncidentDraftAttachmentResponse, error) {
	draft, err := s.repo.FindByID(ctx, draftID, ownerID)
	if err != nil {
		return nil, err
	}
	if draft.PublishedAt != nil || draft.PublishedIncidentID != nil {
		return nil, ErrDraftPublished
	}
	key, err := s.fileStorage.UploadFile(ctx, file, header, "incident-drafts/"+draftID.String())
	if err != nil {
		log.Printf("[IncidentDraft] attachment upload for %s failed: %v", draftID, err)
		return nil, err
	}
	att := &models.IncidentDraftAttachment{
		DraftID:      draftID,
		FileName:     header.Filename,
		FileSize:     header.Size,
		MimeType:     header.Header.Get("Content-Type"),
		FilePath:     key,
		UploadedByID: ownerID,
	}
	// Insert under the draft's row lock. Publish and delete take the same lock, so a draft
	// that was published or removed while the file uploaded is caught here instead of
	// receiving an attachment nobody will move or clean up.
	err = s.repo.Transaction(ctx, func(tx repository.IncidentDraftRepository) error {
		current, ferr := tx.FindByIDForUpdate(ctx, draftID, ownerID)
		if ferr != nil {
			return ferr
		}
		if current.PublishedAt != nil || current.PublishedIncidentID != nil {
			return ErrDraftPublished
		}
		return tx.CreateAttachment(ctx, att)
	})
	if err != nil {
		s.removeObjects(ctx, []string{key})
		return nil, err
	}
	s.touch(ctx, draftID, ownerID)
	return toDraftAttachmentResponse(att), nil
}

func (s *incidentDraftService) ListAttachments(ctx context.Context, draftID, ownerID uuid.UUID) ([]models.IncidentDraftAttachmentResponse, error) {
	if _, err := s.repo.FindByID(ctx, draftID, ownerID); err != nil {
		return nil, err
	}
	atts, err := s.repo.ListAttachments(ctx, draftID)
	if err != nil {
		return nil, err
	}
	out := make([]models.IncidentDraftAttachmentResponse, 0, len(atts))
	for i := range atts {
		out = append(out, *toDraftAttachmentResponse(&atts[i]))
	}
	return out, nil
}

func (s *incidentDraftService) DeleteAttachment(ctx context.Context, draftID, attachmentID, ownerID uuid.UUID) error {
	var att *models.IncidentDraftAttachment
	// Delete the row under the draft's lock. Publish holds the same lock while it lists and
	// moves the attachments, so a delete can no longer slip in between its read and the
	// hand-over, and the file is only removed once the row deletion is committed.
	err := s.repo.Transaction(ctx, func(tx repository.IncidentDraftRepository) error {
		draft, ferr := tx.FindByIDForUpdate(ctx, draftID, ownerID)
		if ferr != nil {
			return ferr
		}
		if draft.PublishedAt != nil {
			return ErrDraftPublished
		}
		found, aerr := tx.FindAttachment(ctx, draftID, attachmentID)
		if aerr != nil {
			return aerr
		}
		att = found
		return tx.DeleteAttachment(ctx, draftID, attachmentID)
	})
	if err != nil {
		return err
	}
	s.removeObjects(ctx, []string{att.FilePath})
	s.touch(ctx, draftID, ownerID)
	return nil
}

func (s *incidentDraftService) OpenAttachment(ctx context.Context, draftID, attachmentID, ownerID uuid.UUID) (*models.IncidentDraftAttachment, io.ReadCloser, error) {
	if _, err := s.repo.FindByID(ctx, draftID, ownerID); err != nil {
		return nil, nil, err
	}
	att, err := s.repo.FindAttachment(ctx, draftID, attachmentID)
	if err != nil {
		return nil, nil, err
	}
	f, err := s.fileStorage.GetFile(ctx, att.FilePath)
	if err != nil {
		return nil, nil, err
	}
	return att, f, nil
}

// touch resets the expiry clock after an attachment change without bumping version, so
// it cannot make the UI's next auto-save fail with 409.
func (s *incidentDraftService) touch(ctx context.Context, draftID, ownerID uuid.UUID) {
	if err := s.repo.Touch(ctx, draftID, ownerID); err != nil {
		log.Printf("[IncidentDraft] touch %s failed: %v", draftID, err)
	}
	s.invalidate(ctx, draftID)
}

func toDraftAttachmentResponse(a *models.IncidentDraftAttachment) *models.IncidentDraftAttachmentResponse {
	return &models.IncidentDraftAttachmentResponse{
		ID:        a.ID,
		DraftID:   a.DraftID,
		FileName:  a.FileName,
		FileSize:  a.FileSize,
		MimeType:  a.MimeType,
		URL:       fmt.Sprintf("/api/v1/incident-drafts/%s/attachments/%s", a.DraftID, a.ID),
		CreatedAt: a.CreatedAt,
	}
}

func (s *incidentDraftService) resolveWorkflow(ctx context.Context, req *models.IncidentCreateRequest) (uuid.UUID, error) {
	var locID, classID *uuid.UUID
	if req.LocationID != nil {
		if id, err := uuid.Parse(*req.LocationID); err == nil {
			locID = &id
		}
	}
	if req.ClassificationID != nil {
		if id, err := uuid.Parse(*req.ClassificationID); err == nil {
			classID = &id
		}
	}
	recordType := req.RecordType
	if recordType == "" {
		recordType = "incident"
	}
	id, err := s.workflowService.ResolveWorkflow(ctx, locID, classID, recordType)
	if err != nil {
		log.Printf("[IncidentDraft] resolve workflow failed: %v", err)
		return uuid.Nil, ErrEmptyWorkflow
	}
	return id, nil
}

// draftRevisionChanges records the draft's identity on the incident's history entry.
func draftRevisionChanges(d *models.IncidentDraft) []models.IncidentFieldChange {
	str := func(v string) *string { return &v }
	return []models.IncidentFieldChange{
		{FieldName: "draft_number", FieldLabel: "Draft Number", NewValue: str(d.DraftNumber)},
		{FieldName: "draft_version", FieldLabel: "Draft Version", NewValue: str(fmt.Sprintf("%d", d.Version))},
		{FieldName: "draft_last_updated", FieldLabel: "Draft Last Updated", NewValue: str(d.UpdatedAt.UTC().Format(time.RFC3339))},
	}
}

// payloadCallerInfo groups the flat caller_* payload fields into the stored object.
func payloadCallerInfo(p models.IncidentDraftPayload) models.CallerInfo {
	return models.CallerInfo{
		FirstName:             strings.TrimSpace(p.CallerFirstName),
		MiddleName:            strings.TrimSpace(p.CallerMiddleName),
		LastName:              strings.TrimSpace(p.CallerLastName),
		Phone:                 strings.TrimSpace(p.CallerPhone),
		AlternateMobileNumber: strings.TrimSpace(p.AlternateMobileNumber),
	}
}

// draftToCreateRequest turns a draft payload into a Create Incident request. Fields shared
// with the incident form copy over through JSON; the caller block is mapped explicitly.
func draftToCreateRequest(payload models.IncidentDraftPayload) (*models.IncidentCreateRequest, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var req models.IncidentCreateRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("decode draft payload: %w", err)
	}
	req.ReporterName = payloadCallerInfo(payload).FullName()
	req.ReporterPhone = strings.TrimSpace(payload.CallerPhone)
	req.AlternateMobile = strings.TrimSpace(payload.AlternateMobileNumber)
	return &req, nil
}

// validateForPublish runs the same struct validation as the Create Incident handler,
// plus the static required fields, and collects every problem at once.
func (s *incidentDraftService) validateForPublish(ctx context.Context, d *models.IncidentDraft, req *models.IncidentCreateRequest) error {
	fields := validation.ValidateStruct(ctx, req)
	if fields == nil {
		fields = map[string]string{}
	}
	for f, ok := range s.requiredFilled(d) {
		if ok {
			continue
		}
		if _, exists := fields[f]; !exists {
			fields[f] = i18n.T(ctx, "field_required")
		}
	}
	if len(fields) == 0 {
		return nil
	}
	return &DraftValidationError{Fields: fields}
}

func (s *incidentDraftService) buildDraft(p *models.IncidentDraftPayload) (*models.IncidentDraft, error) {
	callerInfo, err := json.Marshal(payloadCallerInfo(*p))
	if err != nil {
		return nil, err
	}
	d := &models.IncidentDraft{
		Title:       strings.TrimSpace(p.Title),
		Description: p.Description,
		Source:      p.Source,
		RecordType:  strings.TrimSpace(p.RecordType),
		Latitude:    p.Latitude,
		Longitude:   p.Longitude,
		CallerInfo:  callerInfo,
	}
	if d.RecordType == "" {
		d.RecordType = "incident"
	}

	if len(p.LookupValueIDs) > 0 {
		ids, merr := json.Marshal(p.LookupValueIDs)
		if merr != nil {
			return nil, merr
		}
		d.LookupValueIDs = ids
	}
	parseID := func(v *string) *uuid.UUID {
		if v == nil {
			return nil
		}
		if u, perr := uuid.Parse(*v); perr == nil {
			return &u
		}
		return nil
	}
	d.ClassificationID = parseID(p.ClassificationID)
	d.LocationID = parseID(p.LocationID)
	return d, nil
}

func (s *incidentDraftService) toResponse(d *models.IncidentDraft) *models.IncidentDraftResponse {
	filled, required := s.countRequired(d)
	return &models.IncidentDraftResponse{
		ID:                   d.ID,
		DraftNumber:          d.DraftNumber,
		IncidentDraftPayload: d.ToPayload(),
		Version:              d.Version,
		Keep:                 d.Keep,
		CreatedAt:            d.CreatedAt,
		UpdatedAt:            d.UpdatedAt,
		PublishedAt:          d.PublishedAt,
		PublishedIncidentID:  d.PublishedIncidentID,
		FilledFields:         filled,
		RequiredFields:       required,
		DaysToExpiry:         s.daysToExpiry(d),
	}
}

// daysToExpiry is set only when the draft will expire and is within the warning window.
func (s *incidentDraftService) daysToExpiry(d *models.IncidentDraft) *int {
	if d.Keep || s.cfg.TTLDays <= 0 {
		return nil
	}
	remaining := d.UpdatedAt.Add(time.Duration(s.cfg.TTLDays) * 24 * time.Hour).Sub(time.Now())
	days := int(math.Ceil(remaining.Hours() / 24))
	if days < 0 {
		days = 0
	}
	if days > s.cfg.ExpiryWarnDays {
		return nil
	}
	return &days
}

func (s *incidentDraftService) invalidate(ctx context.Context, id uuid.UUID) {
	if s.redis == nil {
		return
	}
	if err := s.redis.Del(ctx, draftCacheKey(id)).Err(); err != nil {
		log.Printf("[IncidentDraft] cache invalidate failed: %v", err)
	}
}

func (s *incidentDraftService) releaseRequestKey(ctx context.Context, key string) {
	if key != "" {
		_ = s.redis.Del(ctx, key).Err()
	}
}

// requiredFilled reports, per configured required field, whether the draft has a value.
func (s *incidentDraftService) requiredFilled(d *models.IncidentDraft) map[string]bool {
	var raw map[string]interface{}
	if b, err := json.Marshal(d.ToPayload()); err == nil {
		_ = json.Unmarshal(b, &raw)
	}
	out := make(map[string]bool, len(s.requiredFields))
	for _, f := range s.requiredFields {
		out[f] = isFilled(lookupPath(raw, f))
	}
	return out
}

// countRequired returns how many required fields the draft has filled, and how many exist.
func (s *incidentDraftService) countRequired(d *models.IncidentDraft) (filled, required int) {
	for _, ok := range s.requiredFilled(d) {
		if ok {
			filled++
		}
	}
	return filled, len(s.requiredFields)
}

// lookupPath reads a field, or a dotted path into nested objects, from decoded JSON.
func lookupPath(raw map[string]interface{}, path string) interface{} {
	var cur interface{} = raw
	for _, part := range strings.Split(path, ".") {
		m, ok := cur.(map[string]interface{})
		if !ok {
			return nil
		}
		cur = m[part]
	}
	return cur
}

func isFilled(v interface{}) bool {
	switch t := v.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(t) != ""
	case []interface{}:
		return len(t) > 0
	default:
		return true
	}
}

// isDraftPayloadEmpty reports whether no field carries a value. record_type and source
// are ignored because the UI pre-fills them, which would make every form "non-empty".
func isDraftPayloadEmpty(p *models.IncidentDraftPayload) bool {
	v := reflect.ValueOf(p).Elem()
	t := v.Type()
	for i := 0; i < v.NumField(); i++ {
		switch t.Field(i).Name {
		case "RecordType", "Source":
			continue
		}
		f := v.Field(i)
		switch f.Kind() {
		case reflect.String:
			if strings.TrimSpace(f.String()) != "" {
				return false
			}
		case reflect.Ptr:
			if f.IsNil() {
				continue
			}
			if e := f.Elem(); e.Kind() == reflect.String && strings.TrimSpace(e.String()) == "" {
				continue
			}
			return false
		case reflect.Slice, reflect.Map:
			if f.Len() > 0 {
				return false
			}
		default:
			if !f.IsZero() {
				return false
			}
		}
	}
	return true
}
