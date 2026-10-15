package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/automax/backend/internal/models"
	"github.com/gofiber/fiber/v2"
)

func TestExistingMomraTicketNumber(t *testing.T) {
	momra := func(incidentNumber, momraNo string) models.Incident {
		return models.Incident{
			IncidentNumber: incidentNumber,
			CustomFields:   `{"momra_incident_no":"` + momraNo + `","source":"MOMRA"}`,
		}
	}

	t.Run("returns the AutoMax ticket number, not the MOMRA number", func(t *testing.T) {
		got := existingMomraTicketNumber([]models.Incident{momra("INC-2026-000123", "INC-DEMO-TEST-101")}, "INC-DEMO-TEST-101")
		if got != "INC-2026-000123" {
			t.Errorf("ticket = %q, want %q", got, "INC-2026-000123")
		}
	})

	t.Run("first exact match wins (candidates are oldest first)", func(t *testing.T) {
		got := existingMomraTicketNumber([]models.Incident{
			momra("INC-2026-000001", "INC-DEMO-TEST-101"),
			momra("INC-2026-000002", "INC-DEMO-TEST-101"),
		}, "INC-DEMO-TEST-101")
		if got != "INC-2026-000001" {
			t.Errorf("ticket = %q, want the original incident's %q", got, "INC-2026-000001")
		}
	})

	t.Run("LIKE wildcard false positive is skipped", func(t *testing.T) {
		// "_" in the incoming number matched "X" in SQL LIKE.
		got := existingMomraTicketNumber([]models.Incident{
			momra("INC-2026-000009", "INCX1"),
			momra("INC-2026-000010", "INC_1"),
		}, "INC_1")
		if got != "INC-2026-000010" {
			t.Errorf("ticket = %q, want the exact match %q", got, "INC-2026-000010")
		}
	})

	t.Run("no exact match: empty, never a wrong identifier", func(t *testing.T) {
		got := existingMomraTicketNumber([]models.Incident{momra("INC-2026-000009", "INCX1")}, "INC_1")
		if got != "" {
			t.Errorf("ticket = %q, want empty", got)
		}
	})

	t.Run("exact match without an incident number: empty", func(t *testing.T) {
		got := existingMomraTicketNumber([]models.Incident{momra("", "INC-DEMO-TEST-101")}, "INC-DEMO-TEST-101")
		if got != "" {
			t.Errorf("ticket = %q, want empty", got)
		}
	})

	t.Run("unparseable custom_fields: skipped", func(t *testing.T) {
		got := existingMomraTicketNumber([]models.Incident{
			{IncidentNumber: "INC-2026-000005", CustomFields: `{"momra_incident_no":"INC-DEMO-TEST-101"`},
		}, "INC-DEMO-TEST-101")
		if got != "" {
			t.Errorf("ticket = %q, want empty", got)
		}
	})
}

func TestEPMInsertDuplicateResponse(t *testing.T) {
	cases := []struct {
		name, ticket string
	}{
		{"existing ticket found", "INC-2026-000123"},
		{"existing ticket unknown", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := fiber.New()
			app.Post("/", func(c *fiber.Ctx) error {
				return epmInsertDuplicateResponse(c, "INC-DEMO-TEST-101", tc.ticket)
			})
			resp, err := app.Test(httptest.NewRequest("POST", "/", nil))
			if err != nil {
				t.Fatal(err)
			}
			if resp.StatusCode != fiber.StatusConflict {
				t.Errorf("HTTP status = %d, want 409", resp.StatusCode)
			}
			var body EPMInsertIncidentResponse
			if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			want := EPMInsertIncidentResponse{
				Ex:             "",
				HTTPStatusCode: 409,
				Message:        "Duplicate incident number: INC-DEMO-TEST-101",
				Result:         false,
				TicketNumber:   tc.ticket,
			}
			if body != want {
				t.Errorf("body = %+v, want %+v", body, want)
			}
		})
	}
}
