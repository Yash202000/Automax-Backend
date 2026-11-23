package models

import "testing"

func uncooperativeIncident() *Incident {
	return &Incident{
		CooperationStatus: CooperationStatusUncooperative,
		ReporterName:      "Ali",
		ReporterPhone:     "+966501234567",
		Reporter:          &User{FirstName: "Ali", NationalID: "1234567890", Address: "Riyadh"},
	}
}

func TestReporterHiddenOnlyForNazahaUncooperative(t *testing.T) {
	defer SetHideUncooperativeReporter(false)

	// Other clients: nothing is hidden, even for an uncooperative reporter.
	SetHideUncooperativeReporter(false)
	resp := ToIncidentResponse(uncooperativeIncident())
	if resp.ReporterHidden || resp.ReporterName != "Ali" || resp.Reporter == nil {
		t.Fatalf("reporter must stay visible for other clients, got %+v", resp)
	}

	// Nazaha, uncooperative: hidden.
	SetHideUncooperativeReporter(true)
	resp = ToIncidentResponse(uncooperativeIncident())
	if !resp.ReporterHidden || resp.ReporterName != "" || resp.ReporterPhone != "" || resp.Reporter != nil {
		t.Fatalf("reporter must be hidden, got %+v", resp)
	}

	// Nazaha, cooperative: visible.
	inc := uncooperativeIncident()
	inc.CooperationStatus = CooperationStatusCooperative
	resp = ToIncidentResponse(inc)
	if resp.ReporterHidden || resp.ReporterName != "Ali" {
		t.Fatalf("cooperative reporter must stay visible, got %+v", resp)
	}
}

func TestHideReporter(t *testing.T) {
	inc := uncooperativeIncident()
	HideReporter(inc)
	if inc.ReporterName != "" || inc.ReporterPhone != "" || inc.Reporter != nil {
		t.Fatalf("HideReporter left data behind: %+v", inc)
	}
}

func TestClearNazahaFields(t *testing.T) {
	id := "6b0e2a4e-8f3c-4f5e-9d3a-1c2b3d4e5f62"
	req := &IncidentCreateRequest{
		Title: "title", CallerNumber: "x", LinkedIncidentNumber: "x", ConcernedAuthorityID: &id,
		PreviousReportIDs: []string{id}, Content: "x", Notes: "x", Street: "x",
		ReporterNationalID: "x", ReporterNationalityID: &id, ReporterGender: "x", ReporterAddress: "x",
	}
	req.ClearNazahaFields()
	for _, field := range NazahaWorkflowFields {
		if field != "reporter_mobile" && req.NazahaFieldValue(field) != "" {
			t.Errorf("%q must be cleared", field)
		}
	}
	if req.Title != "title" {
		t.Fatal("generic fields must be kept")
	}
}

func TestTrimNazahaFields(t *testing.T) {
	blank := "  "
	req := &IncidentCreateRequest{Content: " c ", ReporterGender: " Male ", ConcernedAuthorityID: &blank}
	req.TrimNazahaFields()
	if req.Content != "c" || req.ReporterGender != "male" || req.ConcernedAuthorityID != nil {
		t.Fatalf("unexpected trim result: %+v", req)
	}
}

func TestToReporterDetailsResponse(t *testing.T) {
	inc := &Incident{Reporter: &User{
		FirstName: "Ali", LastName: "Saleh", Phone: "+966501234567", Email: "ali@example.com",
		NationalID: "1234567890", Gender: GenderMale, Address: "Riyadh",
		Nationality: &LookupValue{Code: "SA", Name: "Saudi Arabia"},
	}}
	got := ToReporterDetailsResponse(inc)
	if got.ReporterName != "Ali Saleh" || got.ReporterEmail != "ali@example.com" ||
		got.ReporterNationalID != "1234567890" || got.ReporterAddress != "Riyadh" ||
		got.ReporterNationality == nil || got.ReporterNationality.Code != "SA" {
		t.Fatalf("details must come from the contact, got %+v", got)
	}

	// Generated login email and placeholder national ID are not real details.
	inc.Reporter.Email = "web_966501234567_1700000000@automax.com"
	inc.Reporter.NationalID = "8f0c6f2e-0000-0000-0000-000000000000"
	got = ToReporterDetailsResponse(inc)
	if got.ReporterEmail != "" || got.ReporterNationalID != "" {
		t.Fatalf("generated values must be left out, got %+v", got)
	}

	if got := ToReporterDetailsResponse(&Incident{}); got.ReporterName != "" || got.ReporterNationality != nil {
		t.Fatal("no contact means no details")
	}
}

func TestNazahaFieldValueCoversEveryWorkflowField(t *testing.T) {
	x := "x"
	req := &IncidentCreateRequest{
		CallerNumber: "x", ReporterPhone: "x", PreviousReportIDs: []string{"x"}, LinkedIncidentNumber: "x",
		ConcernedAuthorityID: &x, ReporterNationalityID: &x, ReporterNationalID: "x",
		ReporterAddress: "x", ReporterGender: "x", Content: "x", Notes: "x", Street: "x",
	}
	for _, field := range NazahaWorkflowFields {
		if req.NazahaFieldValue(field) == "" {
			t.Errorf("NazahaFieldValue does not handle %q", field)
		}
		if (&IncidentCreateRequest{}).NazahaFieldValue(field) != "" {
			t.Errorf("%q must be empty on an empty request", field)
		}
	}
}
