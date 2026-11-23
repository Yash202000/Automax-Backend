package handlers

import (
	"context"
	"testing"

	"github.com/automax/backend/internal/models"
)

func validNazahaRequest() *models.IncidentCreateRequest {
	return &models.IncidentCreateRequest{
		CallerNumber:       "0112345678",
		ReporterNationalID: "1234567890",
	}
}

func TestValidateNazahaFields(t *testing.T) {
	ctx := context.Background()
	if errs := validateNazahaFields(ctx, validNazahaRequest()); len(errs) != 0 {
		t.Fatalf("valid request got errors: %v", errs)
	}
	// Empty optional fields are not errors: the workflow config decides what is required.
	if errs := validateNazahaFields(ctx, &models.IncidentCreateRequest{}); len(errs) != 0 {
		t.Fatalf("empty request got format errors: %v", errs)
	}

	cases := map[string]func(r *models.IncidentCreateRequest){
		"CallerNumber":       func(r *models.IncidentCreateRequest) { r.CallerNumber = "05x" },
		"ReporterNationalID": func(r *models.IncidentCreateRequest) { r.ReporterNationalID = "3234567890" },
		"ReporterPhone":      func(r *models.IncidentCreateRequest) { r.ReporterPhone = "12ab" },
	}
	for field, breakIt := range cases {
		req := validNazahaRequest()
		breakIt(req)
		if _, ok := validateNazahaFields(ctx, req)[field]; !ok {
			t.Errorf("expected an error on %s", field)
		}
	}
}
