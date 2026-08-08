package org

import (
	"errors"
	"testing"
)

// Audit §14: org's role-elevation rules (the whole point of the three-tier
// RBAC ladder — see docs/00-project-state.md's governance-session notes)
// had no test coverage at all. AddMemberByEmail's guard clause runs before
// it ever touches s.repo/s.users, so it's genuinely unit-testable with a
// nil Service — a real Postgres-backed test covering the success paths
// lives in repository_integration_test.go.

func TestRoleRank_OrdersOwnerAboveAdminAboveMember(t *testing.T) {
	if roleRank(RoleOwner) <= roleRank(RoleAdmin) {
		t.Fatalf("owner rank (%d) should exceed admin rank (%d)", roleRank(RoleOwner), roleRank(RoleAdmin))
	}
	if roleRank(RoleAdmin) <= roleRank(RoleMember) {
		t.Fatalf("admin rank (%d) should exceed member rank (%d)", roleRank(RoleAdmin), roleRank(RoleMember))
	}
}

func TestRoleRank_UnknownRoleRanksAsMember(t *testing.T) {
	if got, want := roleRank(Role("bogus")), roleRank(RoleMember); got != want {
		t.Fatalf("unknown role ranked %d, want it to fail closed to member's rank %d", got, want)
	}
}

func TestAddMemberByEmail_AdminGrantingElevatedRoleIsRejected(t *testing.T) {
	s := &Service{} // guard clause below never touches repo/users
	cases := []Role{RoleAdmin, RoleOwner}
	for _, role := range cases {
		_, err := s.AddMemberByEmail(t.Context(), "org-1", "someone@nimbus.test", role, RoleAdmin)
		if !errors.Is(err, ErrElevatedRoleNeedsOwner) {
			t.Fatalf("admin granting %q: got err %v, want ErrElevatedRoleNeedsOwner", role, err)
		}
	}
}

func TestAddMemberByEmail_MemberGrantingElevatedRoleIsRejected(t *testing.T) {
	s := &Service{}
	_, err := s.AddMemberByEmail(t.Context(), "org-1", "someone@nimbus.test", RoleAdmin, RoleMember)
	if !errors.Is(err, ErrElevatedRoleNeedsOwner) {
		t.Fatalf("got err %v, want ErrElevatedRoleNeedsOwner", err)
	}
}

// The one grant the guard clause allows through regardless of caller role
// — plain membership — needs a real users/repo past that point, so its
// happy path is covered in repository_integration_test.go instead.
