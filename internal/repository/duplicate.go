package repository

// DuplicateFields reports which columns of a name/name_ar/code uniqueness check an
// existing row matched on, so the caller can build a precise conflict message
// ("this code is taken" vs "this name is taken") instead of a generic one.
type DuplicateFields struct {
	Name   bool
	NameAr bool
	Code   bool
}

// Any reports whether any field matched.
func (d DuplicateFields) Any() bool {
	return d.Name || d.NameAr || d.Code
}
