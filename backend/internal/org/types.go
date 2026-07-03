// Package org owns organizations and membership — see docs/03-hld.md §1.
package org

import (
	"errors"
	"time"
)

type Role string

const (
	RoleOwner  Role = "owner"
	RoleMember Role = "member"
)

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
)
