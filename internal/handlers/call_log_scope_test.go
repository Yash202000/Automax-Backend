package handlers

import (
	"testing"

	"github.com/automax/backend/internal/models"
	"github.com/google/uuid"
)

func mkUser(super bool, perms ...string) *models.User {
	u := &models.User{ID: uuid.New(), IsSuperAdmin: super}
	if len(perms) > 0 {
		ps := make([]models.Permission, len(perms))
		for i, c := range perms {
			ps[i] = models.Permission{Code: c, IsActive: true}
		}
		u.Roles = []models.Role{{Permissions: ps}}
	}
	return u
}

func TestResolveScope(t *testing.T) {
	agent := mkUser(false, "call-logs:view")
	pid, viewAll, err := resolveScope(agent, "")
	if err != nil || viewAll || pid == nil || *pid != agent.ID {
		t.Fatalf("agent scopes to self: pid=%v viewAll=%v err=%v", pid, viewAll, err)
	}
	// agent cannot widen by passing a user_id — still scoped to self
	other := uuid.New().String()
	pid, viewAll, _ = resolveScope(agent, other)
	if viewAll || pid == nil || *pid != agent.ID {
		t.Fatalf("agent cannot view others: pid=%v viewAll=%v", pid, viewAll)
	}

	admin := mkUser(false, "call-logs:view", "call-logs:view-all")
	pid, viewAll, _ = resolveScope(admin, "")
	if !viewAll || pid != nil {
		t.Fatalf("admin no filter => all: pid=%v viewAll=%v", pid, viewAll)
	}
	pid, viewAll, err = resolveScope(admin, other)
	if err != nil || !viewAll || pid == nil || pid.String() != other {
		t.Fatalf("admin filters by agent: pid=%v viewAll=%v err=%v", pid, viewAll, err)
	}
	// bad uuid from an admin is an error
	if _, _, err := resolveScope(admin, "not-a-uuid"); err == nil {
		t.Fatalf("expected error for bad uuid")
	}
}
