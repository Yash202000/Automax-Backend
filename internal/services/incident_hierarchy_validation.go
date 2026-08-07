package services

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// An incident records one specific place and one specific category, so its location
// and classification must be leaves of their hierarchies. Selecting an umbrella node
// like a district or a top-level category files the incident at a level the hierarchy
// was never meant to hold, and reports grouped by location or classification then
// count it separately from everything beneath it.
//
// "Leaf" here means no children that are active and not soft-deleted, which is what
// LocationRepository.HasActiveChildren and ClassificationRepository.HasActiveChildren
// already answer. A node whose children have all been deactivated therefore becomes
// selectable again — deliberately, since that is the tree the client renders.

// validateHierarchyNode checks that raw names an existing node that is not a parent of
// any active node, and returns the parsed id so callers need not parse it again.
//
// It takes closures rather than a repository so the rule can be tested without faking
// a 20-method repository interface for the two queries it actually makes.
// hasActiveChildren is only consulted once the node is known to exist.
func validateHierarchyNode(
	raw string,
	exists func(uuid.UUID) (bool, error),
	hasActiveChildren func(uuid.UUID) (bool, error),
	notFound, hasChildren error,
) (uuid.UUID, error) {
	// A malformed id is reported as not-found rather than as a separate error: to the
	// caller it names nothing either way. Note this is a behaviour change — the create
	// path used to swallow the parse error and silently drop the field.
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, notFound
	}

	ok, err := exists(id)
	if err != nil {
		return uuid.Nil, err
	}
	if !ok {
		return uuid.Nil, notFound
	}

	parent, err := hasActiveChildren(id)
	if err != nil {
		return uuid.Nil, err
	}
	if parent {
		return uuid.Nil, hasChildren
	}
	return id, nil
}

// validateSelectableLocation resolves raw and rejects it unless it is an existing
// location with no active sub-locations.
func (s *incidentService) validateSelectableLocation(ctx context.Context, raw string) (uuid.UUID, error) {
	return validateHierarchyNode(raw,
		func(id uuid.UUID) (bool, error) {
			loc, err := s.locationRepo.FindByID(ctx, id)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return false, nil // absent, not a failure to look it up
			}
			if err != nil {
				return false, err
			}
			return loc != nil, nil
		},
		func(id uuid.UUID) (bool, error) {
			return s.locationRepo.HasActiveChildren(ctx, id)
		},
		ErrLocationNotFound, ErrLocationNotSelectable,
	)
}

// validateSelectableClassification resolves raw and rejects it unless it is an existing
// classification with no active sub-classifications.
func (s *incidentService) validateSelectableClassification(ctx context.Context, raw string) (uuid.UUID, error) {
	return validateHierarchyNode(raw,
		func(id uuid.UUID) (bool, error) {
			cls, err := s.classificationRepo.FindByID(ctx, id)
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return false, nil // absent, not a failure to look it up
			}
			if err != nil {
				return false, err
			}
			return cls != nil, nil
		},
		func(id uuid.UUID) (bool, error) {
			return s.classificationRepo.HasActiveChildren(ctx, id)
		},
		ErrClassificationNotFound, ErrClassificationNotSelectable,
	)
}

// validateIncidentHierarchySelection validates whichever of the two fields is present.
// Empty and nil are left alone: both are optional on create and mean "clear" on update.
func (s *incidentService) validateIncidentHierarchySelection(ctx context.Context, locationID, classificationID *string) error {
	if locationID != nil && *locationID != "" {
		if _, err := s.validateSelectableLocation(ctx, *locationID); err != nil {
			return err
		}
	}
	if classificationID != nil && *classificationID != "" {
		if _, err := s.validateSelectableClassification(ctx, *classificationID); err != nil {
			return err
		}
	}
	return nil
}
