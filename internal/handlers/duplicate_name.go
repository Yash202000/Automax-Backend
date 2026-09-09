package handlers

import (
	"context"

	"github.com/automax/backend/internal/repository"
	"github.com/automax/backend/pkg/i18n"
)

// existingNameWithAr formats an "already exists" conflict's name for display, appending
// the Arabic name in parentheses when one is set (and differs from the English name) so
// the message is useful regardless of which name the caller matched on.
func existingNameWithAr(name, nameAr string) string {
	if nameAr != "" && nameAr != name {
		return name + " (" + nameAr + ")"
	}
	return name
}

// duplicateConflictMessage builds a specific conflict message from a
// repository.CheckDuplicate result: a code collision always wins (codeKey, a plain
// message with no placeholder), otherwise the name/name_ar collision is reported using
// nameKey ("'%s' already exists") or, when a full path is available, nameAtKey ("'%s'
// already exists at '%s'") — the display name includes the Arabic name via
// existingNameWithAr when one is set.
func duplicateConflictMessage(ctx context.Context, fields repository.DuplicateFields, name, nameAr, path, codeKey, nameKey, nameAtKey string) string {
	if fields.Code {
		return i18n.T(ctx, codeKey)
	}
	display := existingNameWithAr(name, nameAr)
	if path != "" {
		return i18n.Tf(ctx, nameAtKey, display, path)
	}
	return i18n.Tf(ctx, nameKey, display)
}
