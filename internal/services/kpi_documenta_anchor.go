package services

import (
	"context"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/internal/storage"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// AnchorResult carries the resolved (and ensured — the folder is guaranteed
// to already exist in Documenta) anchor folder for a KPI's taxonomy, plus
// the human-readable root→anchor path for display.
type AnchorResult struct {
	FolderID string
	Path     []string
}

// ResolveTaxonomyAnchor computes and ensures (via EnsureFolder, so it's safe
// to call repeatedly) the Documenta anchor folder for a KPI type + its
// taxonomy IDs, without requiring a saved KPI row — the taxonomy IDs alone
// (already present in the create form before the KPI is saved) are enough.
//
//   - Strategic & Operational: unchanged single-level Pillar root, resolved
//     the same way ResolvePillarForKPI resolves it for a saved KPI (direct
//     PillarID for Strategic; Objective's PillarID then Process's PillarID
//     fallback for Operational) — this preserves the existing "root is
//     always Pillar" invariant for these two types.
//   - Award: NEW — a 2-level AwardCriterion → AwardSubCriterion chain, since
//     Award KPIs have no Pillar concept but do have real Criteria/
//     Sub-Criteria data, unlike Strategic/Operational's Domain (which has no
//     parent/child relation to Pillar and would require inventing structure
//     not backed by the data model).
//
// Returns (nil, nil) — not an error — when the anchor can't yet be resolved
// (e.g. an Operational combination whose Objective/Process have no PillarID
// set), mirroring ResolvePillarForKPI's existing contract so callers can
// show "not resolvable yet" instead of a hard error.
func ResolveTaxonomyAnchor(
	ctx context.Context,
	db *gorm.DB,
	documentaClient storage.DocumentaClient,
	workspaceName string,
	kpiType string,
	pillarID *uuid.UUID,
	objectiveID *uuid.UUID,
	processID *uuid.UUID,
	awardSubCriterionID *uuid.UUID,
) (*AnchorResult, error) {
	switch kpiType {
	case models.KPITypeStrategic, "":
		if pillarID == nil {
			return nil, nil
		}
		pillar, err := loadPillar(db, *pillarID)
		if err != nil {
			return nil, nil
		}
		folderID, err := documentaClient.EnsureFolder(ctx, workspaceName, "", pillar.NameEn)
		if err != nil {
			return nil, err
		}
		return &AnchorResult{FolderID: folderID, Path: []string{pillar.NameEn}}, nil

	case models.KPITypeOperational:
		pillar, err := resolvePillarByTaxonomy(db, objectiveID, processID)
		if err != nil {
			return nil, err
		}
		if pillar == nil {
			return nil, nil
		}
		folderID, err := documentaClient.EnsureFolder(ctx, workspaceName, "", pillar.NameEn)
		if err != nil {
			return nil, err
		}
		return &AnchorResult{FolderID: folderID, Path: []string{pillar.NameEn}}, nil

	case models.KPITypeAward:
		if awardSubCriterionID == nil {
			return nil, nil
		}
		var subCriterion models.AwardSubCriterion
		if err := db.Preload("AwardCriterion").Where("id = ?", *awardSubCriterionID).First(&subCriterion).Error; err != nil {
			return nil, nil
		}
		if subCriterion.AwardCriterion == nil {
			return nil, nil
		}
		criterionFolderID, err := documentaClient.EnsureFolder(ctx, workspaceName, "", subCriterion.AwardCriterion.NameEn)
		if err != nil {
			return nil, err
		}
		subFolderID, err := documentaClient.EnsureFolder(ctx, workspaceName, criterionFolderID, subCriterion.NameEn)
		if err != nil {
			return nil, err
		}
		return &AnchorResult{FolderID: subFolderID, Path: []string{subCriterion.AwardCriterion.NameEn, subCriterion.NameEn}}, nil

	default:
		return nil, nil
	}
}

// MasterDataCategoryLabels maps each root-folder category key to the
// Documenta folder name it becomes. These ARE the root folders — one static,
// flat, top-level folder per KPI master-data category (Pillars, Enablers,
// Objectives Hierarchy, Initiatives, Domains, Award Criteria, Award
// Sub-Criteria) — not a folder per individual entity instance. The user
// picks one of these seven as their KPI's root, then browses/creates
// whatever sub-folders they want underneath it freehand.
var MasterDataCategoryLabels = map[string]string{
	"pillar":              "Pillars",
	"enabler":             "Enablers",
	"objective":           "Objectives Hierarchy",
	"initiative":          "Initiatives",
	"domain":              "Domains",
	"award_criterion":     "Award Criteria",
	"award_sub_criterion": "Award Sub-Criteria",
}

// ResolveMasterDataCategoryAnchor computes and ensures the Documenta root
// folder for one of the seven fixed KPI master-data categories, picked
// freely by the user in the Evidence Folder picker, independent of which
// taxonomy the KPI being configured actually belongs to.
func ResolveMasterDataCategoryAnchor(
	ctx context.Context,
	documentaClient storage.DocumentaClient,
	workspaceName string,
	category string,
) (*AnchorResult, error) {
	name, ok := MasterDataCategoryLabels[category]
	if !ok {
		return nil, nil
	}
	folderID, err := documentaClient.EnsureFolder(ctx, workspaceName, "", name)
	if err != nil {
		return nil, err
	}
	return &AnchorResult{FolderID: folderID, Path: []string{name}}, nil
}

// resolvePillarByTaxonomy is the raw-ID equivalent of ResolvePillarForKPI's
// Operational branch — same objective-then-process fallback rule, but
// taking IDs directly instead of loading a saved OperationalKPI row, so it
// can be used before the KPI exists.
func resolvePillarByTaxonomy(db *gorm.DB, objectiveID, processID *uuid.UUID) (*models.Pillar, error) {
	if objectiveID != nil {
		var objective models.OperationalObjective
		if err := db.Select("pillar_id").Where("id = ?", *objectiveID).First(&objective).Error; err == nil && objective.PillarID != nil {
			return loadPillar(db, *objective.PillarID)
		}
	}
	if processID != nil {
		var process models.Process
		if err := db.Select("pillar_id").Where("id = ?", *processID).First(&process).Error; err == nil && process.PillarID != nil {
			return loadPillar(db, *process.PillarID)
		}
	}
	return nil, nil
}
