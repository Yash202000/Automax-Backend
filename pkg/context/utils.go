package cstmContext

import (
	"context"

	"github.com/automax/backend/internal/models"
	"github.com/automax/backend/pkg/constants"
)

// WithReportColumns returns a derived context that carries the requested
// column slice so the repository can selectively run enrichment queries
// and build rows dynamically from field→label mappings.
func WithReportColumns(ctx context.Context, columns []models.ColumnField) context.Context {
	return context.WithValue(ctx, constants.ContextKeys.REPORT_COLUMNS, columns)
}
