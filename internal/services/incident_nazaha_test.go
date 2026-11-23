package services

import (
	"testing"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
)

func TestNazahaReporterUserUpdates(t *testing.T) {
	nationality := uuid.New()
	nationalityID := nationality.String()
	req := &models.IncidentCreateRequest{
		ReporterNationalID:    "1234567890",
		ReporterNationalityID: &nationalityID,
		ReporterGender:        "male",
		ReporterAddress:       "Riyadh",
		ReporterPhone:         "+966501234567",
		ReporterEmail:         "ali@example.com",
	}

	// New contact: nationality, gender and address are saved (ID and mobile were set on register).
	got := nazahaReporterUserUpdates(nil, req)
	if got["nationality_id"] != nationality || got["gender"] != "male" || got["address"] != "Riyadh" ||
		got["national_id"] != nil || got["phone"] != nil {
		t.Fatalf("new contact: unexpected updates %v", got)
	}

	// Existing contact with a generated placeholder ID: the real ID is saved.
	got = nazahaReporterUserUpdates(&models.User{NationalID: "8f0c6f2e-0000-0000-0000-000000000000", Phone: "+966501234567"}, req)
	if got["national_id"] != "1234567890" {
		t.Fatalf("placeholder ID should be replaced, got %v", got)
	}

	// Contact matched by ID with a new mobile: the mobile is updated.
	got = nazahaReporterUserUpdates(&models.User{NationalID: "1234567890", Phone: "+966509999999"}, req)
	if got["phone"] == nil {
		t.Fatalf("new mobile should be saved, got %v", got)
	}

	// Contact with the generated login email: the real email is saved.
	got = nazahaReporterUserUpdates(&models.User{Email: "web_1_1@automax.com", Phone: "+966501234567"}, req)
	if got["email"] != "ali@example.com" {
		t.Fatalf("real email should be saved, got %v", got)
	}

	// Nothing changed: no updates.
	same := &models.User{NationalID: "1234567890", NationalityID: &nationality, Gender: "male", Address: "Riyadh", Phone: "+966501234567", Email: "ali@example.com"}
	if got = nazahaReporterUserUpdates(same, req); len(got) != 0 {
		t.Fatalf("expected no updates, got %v", got)
	}
}

func TestNazahaReporterUserUpdatesNeverClears(t *testing.T) {
	nationality := uuid.New()
	contact := &models.User{NationalityID: &nationality, Gender: "male", Address: "Riyadh", Phone: "+966501234567"}
	if got := nazahaReporterUserUpdates(contact, &models.IncidentCreateRequest{}); len(got) != 0 {
		t.Fatalf("empty fields must not clear the contact, got %v", got)
	}
}
