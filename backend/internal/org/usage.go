package org

import (
	"context"
	"time"
)

// Org usage/oversight (post-Tier-4 governance session): one owner-gated
// read summarizing the organization — storage against quota, member
// activity, share exposure. Deliberately aggregate action *metadata* only:
// everything here is derived from data any member can already see
// (activity feed, members list, children listings) — no file names, no
// content, and NOT auth_audit_log (login history spans orgs and is
// user-private, not org governance).

// The ports below are satisfied by adapters over file/sharing/activity
// repositories in cmd/api/main.go — org never touches their tables (HLD §1).

type StorageStats struct {
	UsedBytes    int64
	LiveFiles    int
	TrashedFiles int
}

type StorageStatsReader interface {
	OrgStorageStats(ctx context.Context, orgID string) (StorageStats, error)
}

type ShareLinkCounter interface {
	ActiveLinkCount(ctx context.Context, orgID string) (int, error)
}

type MemberActivityStat struct {
	Events       int // since the window start
	LastActiveAt *time.Time
}

type ActivityStatsReader interface {
	// ActorStats returns per-actor event counts since `since` plus each
	// actor's all-time last activity in this org, keyed by user ID.
	ActorStats(ctx context.Context, orgID string, since time.Time) (map[string]MemberActivityStat, error)
	// VerbCounts returns org-wide event counts by verb since `since`.
	VerbCounts(ctx context.Context, orgID string, since time.Time) (map[string]int, error)
}

type UsageSources struct {
	Storage  StorageStatsReader
	Shares   ShareLinkCounter
	Activity ActivityStatsReader
}

// usageWindow is the "recent activity" horizon for per-member counts and
// the verb breakdown. Fixed, not configurable — it's a dashboard framing
// choice, not an operational knob.
const usageWindow = 30 * 24 * time.Hour

type UsageMember struct {
	UserID       string     `json:"user_id"`
	Email        string     `json:"email"`
	Role         Role       `json:"role"`
	JoinedAt     time.Time  `json:"joined_at"`
	LastActiveAt *time.Time `json:"last_active_at"`
	Events30d    int        `json:"events_30d"`
}

type Usage struct {
	Storage struct {
		UsedBytes    int64 `json:"used_bytes"`
		QuotaBytes   int64 `json:"quota_bytes"`
		LiveFiles    int   `json:"live_files"`
		TrashedFiles int   `json:"trashed_files"`
	} `json:"storage"`
	ActiveShareLinks int            `json:"active_share_links"`
	Members          []UsageMember  `json:"members"`
	Activity30d      map[string]int `json:"activity_30d"`
}

func (s *Service) Usage(ctx context.Context, orgID string) (Usage, error) {
	var u Usage

	storage, err := s.usage.Storage.OrgStorageStats(ctx, orgID)
	if err != nil {
		return Usage{}, err
	}
	u.Storage.UsedBytes = storage.UsedBytes
	u.Storage.QuotaBytes = s.quotaBytes
	u.Storage.LiveFiles = storage.LiveFiles
	u.Storage.TrashedFiles = storage.TrashedFiles

	if u.ActiveShareLinks, err = s.usage.Shares.ActiveLinkCount(ctx, orgID); err != nil {
		return Usage{}, err
	}

	since := time.Now().Add(-usageWindow)
	actorStats, err := s.usage.Activity.ActorStats(ctx, orgID, since)
	if err != nil {
		return Usage{}, err
	}
	if u.Activity30d, err = s.usage.Activity.VerbCounts(ctx, orgID, since); err != nil {
		return Usage{}, err
	}

	members, err := s.repo.ListMembers(ctx, orgID)
	if err != nil {
		return Usage{}, err
	}
	u.Members = make([]UsageMember, len(members))
	for i, m := range members {
		um := UsageMember{UserID: m.UserID, Email: m.Email, Role: m.Role, JoinedAt: m.CreatedAt}
		if st, ok := actorStats[m.UserID]; ok {
			um.LastActiveAt = st.LastActiveAt
			um.Events30d = st.Events
		}
		u.Members[i] = um
	}
	return u, nil
}
