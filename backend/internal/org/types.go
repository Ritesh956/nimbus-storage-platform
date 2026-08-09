// Package org owns organizations and membership — see docs/03-hld.md §1.
package org

import (
	"errors"
	"time"
)

type Role string

// Three-tier org RBAC (governance session): owner > admin > member. Admin
// is delegated governance — oversight (usage view) plus member management
// bounded by the rules in Service.AddMemberByEmail/RemoveMember — while
// day-to-day file/folder/share access stays identical for every role.
const (
	RoleOwner  Role = "owner"
	RoleAdmin  Role = "admin"
	RoleMember Role = "member"
)

// roleRank orders roles for RequireRole's minimum-role checks.
func roleRank(r Role) int {
	switch r {
	case RoleOwner:
		return 3
	case RoleAdmin:
		return 2
	default:
		return 1
	}
}

type Organization struct {
	ID          string
	Name        string
	OwnerUserID string
	CreatedAt   time.Time
}

type Member struct {
	OrgID     string
	UserID    string
	Email     string
	Role      Role
	CreatedAt time.Time
}

var (
	ErrNotMember          = errors.New("not a member of this organization")
	ErrOwnerRequired      = errors.New("owner role required")
	ErrAlreadyMember      = errors.New("user is already a member")
	ErrCannotRemoveOwner  = errors.New("cannot remove the organization owner")
	ErrTargetUserNotFound = errors.New("user not found")
	ErrNotFound           = errors.New("organization not found")
	// Admin-tier bounds (see Role docs above).
	ErrElevatedRoleNeedsOwner  = errors.New("only owners can grant elevated roles")
	ErrAdminRemovesMembersOnly = errors.New("admins can only remove plain members")
)
