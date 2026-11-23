package services

import "testing"

func TestKpiSubNoPrefixError(t *testing.T) {
	cases := []struct {
		no    int
		sub   string
		valid bool
	}{
		{3, "3.1", true},
		{3, "3.10", true},
		{950, "950.2a", true},
		{3, "3.", false},
		{3, "3", false},
		{3, "31.1", false},
		{3, "5.1", false},
		{31, "3.1", false},
		{3, "", false},
	}
	for _, c := range cases {
		got := KpiSubNoPrefixError(c.no, c.sub) == ""
		if got != c.valid {
			t.Errorf("KpiSubNoPrefixError(%d, %q) valid=%v, want %v", c.no, c.sub, got, c.valid)
		}
	}
}

func TestValidateKpiMasterDataImportOptions(t *testing.T) {
	ok := []KpiImportOptions{
		{},
		{ExistingMode: KpiImportExistingSkip, ErrorMode: KpiImportErrorsSkipInvalid},
		{ExistingMode: KpiImportExistingUpdate, ErrorMode: KpiImportErrorsAbort},
	}
	for _, o := range ok {
		if err := ValidateKpiMasterDataImportOptions(o); err != nil {
			t.Errorf("options %+v rejected: %v", o, err)
		}
	}
	bad := []KpiImportOptions{
		{ExistingMode: "bogus"},
		{ErrorMode: "ignore"},
		{ExistingMode: "Update"},
	}
	for _, o := range bad {
		if err := ValidateKpiMasterDataImportOptions(o); err == nil {
			t.Errorf("options %+v accepted, want error", o)
		}
	}
}
