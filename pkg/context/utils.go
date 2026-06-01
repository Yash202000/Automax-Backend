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

// WithReportDataSource returns a derived context that carries the active
// data source name (e.g. "incidents", "users") so applyFilters in the
// repository can select the correct allowed-fields map.
func WithReportDataSource(ctx context.Context, dataSource string) context.Context {
	return context.WithValue(ctx, constants.ContextKeys.REPORT_DATA_SOURCE, dataSource)
}

// WithReportTimezone returns a derived context that carries the user's IANA
// timezone (e.g. "Asia/Kolkata") so applyFilters can interpret datetime-local
// filter values in the correct timezone.
func WithReportTimezone(ctx context.Context, timezone string) context.Context {
	return context.WithValue(ctx, constants.ContextKeys.REPORT_TIMEZONE, timezone)
}
