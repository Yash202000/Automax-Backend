package cstmContext

import (
	"context"

	"github.com/automax/backend/pkg/constants"
)

// columnsSet converts a slice of column names into a set (map[string]bool)
// that the repository layer can use to skip enrichment queries for columns
// that are not requested in this call.
func ColumnsSet(columns []string) map[string]bool {
	s := make(map[string]bool, len(columns))
	for _, col := range columns {
		s[col] = true
	}
	return s
}

// withReportColumns returns a derived context that carries the column set so
// the repository can selectively run enrichment queries.
func WithReportColumns(ctx context.Context, columns []string) context.Context {
	return context.WithValue(ctx, constants.ContextKeys.REPORT_COLUMNS, ColumnsSet(columns))
}
