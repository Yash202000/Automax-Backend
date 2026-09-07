package services

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/automax/backend/internal/config"
	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/repository"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// externalEntityDepartmentType mirrors epm_incident_handler.go's constant of the same
// name/value — External Entities are modeled as Department{Type:"external"}, not a
// separate model, so both the inbound (EPM) and outbound (MOMRA sync) integration code
// agree on what an "external" department means. Re-declared here rather than imported
// because handlers and services don't share unexported constants across files in
// different packages, but keep both in sync if this value ever changes.
const externalEntityDepartmentType = "external"

// ClassificationSyncResult reports what a MOMRA classification sync did, for admin
// visibility and demo output.
type ClassificationSyncResult struct {
	MainCreated    int `json:"main_created"`
	MainUpdated    int `json:"main_updated"`
	SubCreated     int `json:"sub_created"`
	SubUpdated     int `json:"sub_updated"`
	SpecialCreated int `json:"special_created"`
	SpecialUpdated int `json:"special_updated"`
}

// ExternalEntitySyncResult reports what a MOMRA external-entity sync did.
type ExternalEntitySyncResult struct {
	Created int `json:"created"`
	Updated int `json:"updated"`
}

// EEClassificationSyncResult reports what a MOMRA EE-classification sync did.
type EEClassificationSyncResult struct {
	EntitiesLinked int      `json:"entities_linked"`
	TotalLinks     int      `json:"total_links"`
	SkippedEEs     []string `json:"skipped_ees,omitempty"` // EECodes MOMRA returned that have no matching Department yet
}

// MOMRASyncService syncs MOMRA's classification, external-entity, and
// EE-classification master data into Automax, per
// docs/MOMRA_Outbound_Integration_Spec_v1.0.md §4-6.
type MOMRASyncService interface {
	SyncClassifications(ctx context.Context) (*ClassificationSyncResult, error)
	SyncExternalEntities(ctx context.Context) (*ExternalEntitySyncResult, error)
	SyncExternalEntityClassifications(ctx context.Context) (*EEClassificationSyncResult, error)
}

type momraSyncService struct {
	client             MOMRAClient
	classificationRepo repository.ClassificationRepository
	departmentRepo     repository.DepartmentRepository
	cfg                config.MOMRAConfig
}

func NewMOMRASyncService(
	client MOMRAClient,
	classificationRepo repository.ClassificationRepository,
	departmentRepo repository.DepartmentRepository,
	cfg config.MOMRAConfig,
) MOMRASyncService {
	return &momraSyncService{
		client:             client,
		classificationRepo: classificationRepo,
		departmentRepo:     departmentRepo,
		cfg:                cfg,
	}
}

// SyncClassifications walks MOMRA's Main -> Sub -> Special hierarchy (3.21/3.22/3.23)
// and upserts into Classification keyed on ExternalID, which is already read by the
// existing inbound EPM flow (epm_incident_handler.go's validateAndResolveClassification)
// — this sync populates a field that's already load-bearing, it doesn't repurpose one.
// Classification.Create/Update already compute Level/Path/Code automatically from
// ParentID, so this only needs to set Name/NameAr/ExternalID/ParentID/IsActive.
func (s *momraSyncService) SyncClassifications(ctx context.Context) (*ClassificationSyncResult, error) {
	result := &ClassificationSyncResult{}

	mains, err := s.client.GetMainClassifications(ctx, s.cfg.ClassificationType, s.cfg.MunicipalityID)
	if err != nil {
		return nil, fmt.Errorf("fetch main classifications: %w", err)
	}

	// Maps MOMRA's composite ID (e.g. "008_MC-116") to the local Classification UUID,
	// so child levels can resolve their parent without a repo round-trip.
	idToUUID := make(map[string]uuid.UUID, len(mains)*8)

	for _, main := range mains {
		mainUUID, created, err := s.upsertClassification(ctx, main.ID, main.Name, nil)
		if err != nil {
			return nil, fmt.Errorf("sync main classification %s: %w", main.ID, err)
		}
		idToUUID[main.ID] = mainUUID
		if created {
			result.MainCreated++
		} else {
			result.MainUpdated++
		}

		subs, err := s.client.GetSubClassifications(ctx, main.ID)
		if err != nil {
			return nil, fmt.Errorf("fetch sub classifications for %s: %w", main.ID, err)
		}
		for _, sub := range subs {
			parentUUID, ok := idToUUID[sub.ParentRef]
			if !ok {
				continue // defensive: MOMRA returned a parent ref we didn't just sync
			}
			subUUID, created, err := s.upsertClassification(ctx, sub.ID, sub.Name, &parentUUID)
			if err != nil {
				return nil, fmt.Errorf("sync sub classification %s: %w", sub.ID, err)
			}
			idToUUID[sub.ID] = subUUID
			if created {
				result.SubCreated++
			} else {
				result.SubUpdated++
			}

			specials, err := s.client.GetSpecialClassifications(ctx, sub.ID)
			if err != nil {
				return nil, fmt.Errorf("fetch special classifications for %s: %w", sub.ID, err)
			}
			for _, spc := range specials {
				spcParentUUID, ok := idToUUID[spc.ParentRef]
				if !ok {
					continue
				}
				_, created, err := s.upsertClassification(ctx, spc.ID, spc.Name, &spcParentUUID)
				if err != nil {
					return nil, fmt.Errorf("sync special classification %s: %w", spc.ID, err)
				}
				if created {
					result.SpecialCreated++
				} else {
					result.SpecialUpdated++
				}
			}
		}
	}

	return result, nil
}

// allMOMRAClassificationTypes covers every intake channel (matches the full
// oneof=incident request complaint query mobile ivr set validated on
// ClassificationCreateRequest.Types) so MOMRA-synced classifications are usable
// everywhere, not just the manual-create UI's incident+request default.
var allMOMRAClassificationTypes = []string{"incident", "request", "complaint", "query", "mobile", "ivr"}

func (s *momraSyncService) upsertClassification(ctx context.Context, externalID, name string, parentID *uuid.UUID) (uuid.UUID, bool, error) {
	existing, err := s.classificationRepo.FindByExternalID(ctx, externalID)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return uuid.Nil, false, err
	}
	if existing != nil {
		existing.Name = name
		existing.IsActive = true
		if err := s.classificationRepo.Update(ctx, existing); err != nil {
			return uuid.Nil, false, err
		}
		if err := s.classificationRepo.SetTypes(ctx, existing.ID, allMOMRAClassificationTypes); err != nil {
			return uuid.Nil, false, err
		}
		return existing.ID, false, nil
	}

	cls := &models.Classification{
		Name:       name,
		ExternalID: externalID,
		ParentID:   parentID,
		IsActive:   true,
	}
	if err := s.classificationRepo.Create(ctx, cls); err != nil {
		return uuid.Nil, false, err
	}
	if err := s.classificationRepo.SetTypes(ctx, cls.ID, allMOMRAClassificationTypes); err != nil {
		return uuid.Nil, false, err
	}
	return cls.ID, true, nil
}

// SyncExternalEntities upserts MOMRA's External Entities (3.35) as
// Department{Type:"external"}, matching the model epm_incident_handler.go's
// resolveEERoutingDepartment already uses for EEs today — no new model.
//
// Note: Department.Code is lowercased by the repository (shared behavior with
// internal departments' auto-generated codes), so an EECode like "AGC-0236" is stored
// as "agc-0236". FindByCode already compares case-insensitively, so lookups are
// unaffected, but code that reconstructs an outbound EECode from Department.Code
// (e.g. the future status-sync EE fields) must not assume original MOMRA casing is
// preserved — flagged as a known caveat, not fixed here, since Code normalization is
// shared with unrelated Department behavior.
func (s *momraSyncService) SyncExternalEntities(ctx context.Context) (*ExternalEntitySyncResult, error) {
	result := &ExternalEntitySyncResult{}

	requestID := fmt.Sprintf("AUTOMAX-EE-SYNC-%d", time.Now().Unix())
	entities, err := s.client.GetExternalEntities(ctx, requestID)
	if err != nil {
		return nil, fmt.Errorf("fetch external entities: %w", err)
	}

	for _, ee := range entities {
		existing, err := s.departmentRepo.FindByCode(ctx, ee.EECode)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("lookup external entity %s: %w", ee.EECode, err)
		}

		if existing != nil {
			existing.Name = ee.EEName
			existing.NameAr = ee.EENameAR
			existing.Type = externalEntityDepartmentType
			existing.IsActive = true
			if err := s.departmentRepo.Update(ctx, existing); err != nil {
				return nil, fmt.Errorf("update external entity %s: %w", ee.EECode, err)
			}
			result.Updated++
			continue
		}

		dept := &models.Department{
			Name:     ee.EEName,
			NameAr:   ee.EENameAR,
			Code:     ee.EECode,
			Type:     externalEntityDepartmentType,
			IsActive: true,
		}
		if err := s.departmentRepo.Create(ctx, dept); err != nil {
			return nil, fmt.Errorf("create external entity %s: %w", ee.EECode, err)
		}
		result.Created++
	}

	return result, nil
}

// SyncExternalEntityClassifications syncs 3.36's active EE-to-special-classification
// links by resolving each MOMRA ClassificationCode against the Classification rows
// SyncClassifications already populated, then replacing the EE Department's linked
// classifications atomically via the existing AssignClassifications (GORM association
// Replace) — matching MOMRA's "active links only" semantics: a link no longer returned
// is removed, not left stale. Depends on SyncClassifications and SyncExternalEntities
// having already run at least once.
func (s *momraSyncService) SyncExternalEntityClassifications(ctx context.Context) (*EEClassificationSyncResult, error) {
	result := &EEClassificationSyncResult{}

	requestID := fmt.Sprintf("AUTOMAX-EECLS-SYNC-%d", time.Now().Unix())
	entities, err := s.client.GetExternalEntityClassifications(ctx, requestID, s.cfg.MunicipalityID, "")
	if err != nil {
		return nil, fmt.Errorf("fetch external entity classifications: %w", err)
	}

	for _, ee := range entities {
		dept, err := s.departmentRepo.FindByCode(ctx, ee.EECode)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				// EE hasn't been synced locally yet (SyncExternalEntities should run
				// first) — skip rather than fail the whole batch.
				result.SkippedEEs = append(result.SkippedEEs, ee.EECode)
				continue
			}
			return nil, fmt.Errorf("lookup external entity %s: %w", ee.EECode, err)
		}

		classificationIDs := make([]uuid.UUID, 0, len(ee.LinkedClassifications))
		for _, link := range ee.LinkedClassifications {
			cls, err := s.classificationRepo.FindByExternalID(ctx, link.ClassificationCode)
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					// Classification hasn't been synced locally yet — skip this one
					// link rather than fail the EE.
					continue
				}
				return nil, fmt.Errorf("lookup classification %s: %w", link.ClassificationCode, err)
			}
			classificationIDs = append(classificationIDs, cls.ID)
		}

		if err := s.departmentRepo.AssignClassifications(ctx, dept.ID, classificationIDs); err != nil {
			return nil, fmt.Errorf("assign classifications for external entity %s: %w", ee.EECode, err)
		}
		result.EntitiesLinked++
		result.TotalLinks += len(classificationIDs)
	}

	return result, nil
}
