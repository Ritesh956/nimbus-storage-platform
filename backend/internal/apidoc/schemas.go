// Package apidoc holds the request/response DTOs the OpenAPI generator
// (cmd/openapi-gen) reflects over to build docs/openapi.json's component
// schemas. These structs are deliberately not imported by any handler —
// handlers keep writing map[string]any (see internal/*/handler.go), because
// rewriting ~55 routes' response marshaling to use shared structs would be a
// large, risk-bearing change to already-audited, working code for a
// documentation-generation tool that doesn't need it.
//
// Instead, each struct here mirrors a response/request shape that was
// verified field-for-field against the real handler source (the same
// map[string]any literals frontend/lib/api.ts and frontend/lib/types.ts
// were already hand-verified against) when this package was written. That's
// a real, disclosed limitation: unlike the route paths themselves (which
// cmd/openapi-gen parses directly out of cmd/api/*.go's mux registrations
// and can never drift), a handler that changes its response shape without a
// matching edit here won't be caught by the compiler. It closes the
// audit's actual complaint, though — docs/06-api-design.md was checked
// "route-by-route against main.go by hand each session"; the routes half of
// that is now generated, and the schema half lives in one typed place
// instead of two independently hand-maintained ones (the old docs/06 prose
// and frontend/lib/types.ts), which is what makes deriving the frontend
// types from this package's schemas below meaningful.
package apidoc

import "time"

// --- auth ---

type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	OrgName  string `json:"org_name"`
}

type RegisterResponse struct {
	UserID string `json:"user_id"`
	Email  string `json:"email"`
}

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// TokenPairResponse is one of two shapes POST /v1/auth/login and POST
// /v1/auth/login/totp can return — see TOTPChallengeResponse for the other.
type TokenPairResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// TOTPChallengeResponse is POST /v1/auth/login's other possible 200 shape,
// returned instead of TokenPairResponse when the account has TOTP enabled.
type TOTPChallengeResponse struct {
	TOTPRequired   bool   `json:"totp_required"`
	ChallengeToken string `json:"challenge_token"`
}

type TOTPLoginRequest struct {
	ChallengeToken string `json:"challenge_token"`
	Code           string `json:"code"`
}

// RefreshRequest/LogoutRequest are the *Go API's* own contract — a raw
// client of nimbus-api (not the frontend) sends the refresh token directly
// in the body. The frontend never does this itself: its httpOnly-cookie BFF
// proxy (app/api/auth/{refresh,logout}/route.ts) is the only thing that
// calls these routes, reading the token from the cookie server-side. That
// proxy isn't part of this Go backend's route table, so it's out of scope
// for a spec generated from cmd/api/*.go — see docs/openapi.json's info
// description.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type LogoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type ForgotPasswordRequest struct {
	Email string `json:"email"`
}

type ResetPasswordRequest struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

type MeResponse struct {
	UserID          string `json:"user_id"`
	Email           string `json:"email"`
	IsPlatformAdmin bool   `json:"is_platform_admin"`
}

type TOTPStatusResponse struct {
	Enabled bool `json:"enabled"`
}

type TOTPSetupResponse struct {
	Secret     string `json:"secret"`
	OTPAuthURI string `json:"otpauth_uri"`
}

type TOTPCodeRequest struct {
	Code string `json:"code"`
}

// --- orgs ---

type CreateOrgRequest struct {
	Name string `json:"name"`
}

type Organization struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	OwnerUserID string `json:"owner_user_id"`
}

// OrgRole mirrors org.Role's three values — owner > admin > member.
type OrgRole string

type Member struct {
	UserID string  `json:"user_id"`
	Email  string  `json:"email"`
	Role   OrgRole `json:"role"`
}

type AddMemberRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

type AddMemberResponse struct {
	OrgID  string `json:"org_id"`
	UserID string `json:"user_id"`
	Role   string `json:"role"`
}

type UsageMember struct {
	UserID       string     `json:"user_id"`
	Email        string     `json:"email"`
	Role         OrgRole    `json:"role"`
	JoinedAt     time.Time  `json:"joined_at"`
	LastActiveAt *time.Time `json:"last_active_at"`
	Events30d    int        `json:"events_30d"`
}

// SetQuotaRequest is PATCH /v1/orgs/{orgId}/quota's body (platform-admin
// only) — nil/omitted/null clears the org's per-tenant override, falling
// back to the configured default (org/usage.go, audit §06).
type SetQuotaRequest struct {
	QuotaBytes *int64 `json:"quota_bytes"`
}

// OrgUsage is GET /v1/orgs/{orgId}/usage's response — owner-gated org
// oversight (org/usage.go), aggregate metadata only.
type OrgUsage struct {
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

// --- folders / files ---

type CreateFolderRequest struct {
	ParentID *string `json:"parent_id"`
	Name     string  `json:"name"`
}

// FolderUpdateRequest — PATCH /v1/folders/{folderId} decodes into a raw map
// server-side (to distinguish "field omitted" from "field explicitly null"
// for parent_id), but this is the shape a client actually sends.
type FolderUpdateRequest struct {
	Name     *string `json:"name,omitempty"`
	ParentID *string `json:"parent_id,omitempty"`
}

type FileUpdateRequest struct {
	Name     *string `json:"name,omitempty"`
	FolderID *string `json:"folder_id,omitempty"`
}

type FolderNode struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	ParentID  *string   `json:"parent_id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type FileSummary struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	SizeBytes    *int64  `json:"size_bytes"`
	MimeType     *string `json:"mime_type"`
	HasThumbnail bool    `json:"has_thumbnail"`
}

type FileNode struct {
	ID              string    `json:"id"`
	OrgID           string    `json:"org_id"`
	FolderID        string    `json:"folder_id"`
	Name            string    `json:"name"`
	LatestVersionID *string   `json:"latest_version_id"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type FolderChildrenResponse struct {
	Folders []FolderNode  `json:"folders"`
	Files   []FileSummary `json:"files"`
}

type PathSegment struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type FileVersion struct {
	ID             string    `json:"id"`
	SizeBytes      int64     `json:"size_bytes"`
	ChecksumSHA256 string    `json:"checksum_sha256"`
	MimeType       string    `json:"mime_type"`
	CreatedAt      time.Time `json:"created_at"`
}

type ThumbnailResponse struct {
	Targets []string `json:"targets"`
}

type DownloadPlanChunk struct {
	Sequence int      `json:"sequence"`
	Hash     string   `json:"hash"`
	Targets  []string `json:"targets"`
}

type DownloadPlan struct {
	Chunks []DownloadPlanChunk `json:"chunks"`
}

// --- search / activity ---

type SearchResult struct {
	FileID    string    `json:"file_id"`
	Name      string    `json:"name"`
	FolderID  string    `json:"folder_id"`
	OwnerID   string    `json:"owner_id"`
	CreatedAt time.Time `json:"created_at"`
	SizeBytes *int64    `json:"size_bytes"`
	MimeType  *string   `json:"mime_type"`
}

type SearchResponse struct {
	Results    []SearchResult `json:"results"`
	NextCursor string         `json:"next_cursor"`
}

type ActivityEvent struct {
	Verb       string    `json:"verb"`
	TargetType string    `json:"target_type"`
	TargetID   string    `json:"target_id"`
	Actor      *string   `json:"actor"`
	CreatedAt  time.Time `json:"created_at"`
}

type ActivityResponse struct {
	Events     []ActivityEvent `json:"events"`
	NextCursor string          `json:"next_cursor"`
}

// --- sharing ---

type ShareCreateRequest struct {
	ExpiresAt *string `json:"expires_at,omitempty"`
}

type CreateBundleShareRequest struct {
	FileIDs   []string `json:"file_ids"`
	ExpiresAt *string  `json:"expires_at,omitempty"`
}

type ShareLink struct {
	Token string `json:"token"`
	URL   string `json:"url"`
}

type ShareFileInfo struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	SizeBytes      int64  `json:"size_bytes"`
	MimeType       string `json:"mime_type"`
	ChecksumSHA256 string `json:"checksum_sha256"`
}

type ShareFolderInfo struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ResolvedShare is GET /v1/shares/{token}'s response — one of three shapes
// discriminated by "kind", matching frontend/lib/types.ts's ResolvedShare
// union exactly (a "file" share embeds its download plan directly; "folder"
// and "bundle" list entries and fetch each file's plan on demand).
type ResolvedShareFile struct {
	Kind         string        `json:"kind"` // "file"
	File         ShareFileInfo `json:"file"`
	DownloadPlan DownloadPlan  `json:"download_plan"`
}

type ResolvedShareFolder struct {
	Kind    string            `json:"kind"` // "folder"
	Folder  ShareFolderInfo   `json:"folder"`
	Folders []ShareFolderInfo `json:"folders"`
	Files   []ShareFileInfo   `json:"files"`
}

type ResolvedShareBundle struct {
	Kind  string          `json:"kind"` // "bundle"
	Files []ShareFileInfo `json:"files"`
}

type ShareChildrenResponse struct {
	Folder  ShareFolderInfo   `json:"folder"`
	Folders []ShareFolderInfo `json:"folders"`
	Files   []ShareFileInfo   `json:"files"`
}

type ShareDownloadPlanResponse struct {
	File         ShareFileInfo `json:"file"`
	DownloadPlan DownloadPlan  `json:"download_plan"`
}

// --- uploads ---

type InitUploadRequest struct {
	FolderID  string `json:"folder_id,omitempty"`
	FileID    string `json:"file_id,omitempty"`
	Name      string `json:"name,omitempty"`
	SizeBytes int64  `json:"size_bytes"`
	MimeType  string `json:"mime_type"`
}

type InitUploadResponse struct {
	UploadID string `json:"upload_id"`
}

type CheckChunksRequest struct {
	Hashes []string `json:"hashes"`
}

type CheckChunksResponse struct {
	Missing []string `json:"missing"`
}

type ChunkTarget struct {
	NodeID string `json:"node_id"`
	PutURL string `json:"put_url"`
}

type InitChunkResponse struct {
	Targets   []ChunkTarget `json:"targets"`
	ExpiresAt string        `json:"expires_at"`
}

type CommitChunkRequest struct {
	SizeBytes int64             `json:"size_bytes"`
	Etags     map[string]string `json:"etags"`
}

type CommitChunkResponse struct {
	Status string `json:"status"`
}

type CompleteUploadRequest struct {
	ChunkOrder     []string `json:"chunk_order"`
	SizeBytes      int64    `json:"size_bytes"`
	ChecksumSHA256 string   `json:"checksum_sha256"`
}

type CompleteUploadResponse struct {
	FileID    string `json:"file_id"`
	VersionID string `json:"version_id"`
}

// --- admin / cluster ops ---

type StorageNode struct {
	ID              string     `json:"id"`
	Endpoint        string     `json:"endpoint"`
	Status          string     `json:"status"`
	LastHeartbeatAt *time.Time `json:"last_heartbeat_at"`
}

type RingVNode struct {
	Position uint32 `json:"position"`
	Node     string `json:"node"`
}

type RingChunk struct {
	Sequence   int      `json:"sequence"`
	Hash       string   `json:"hash"`
	Position   uint32   `json:"position"`
	Preference []string `json:"preference"`
	Locations  []string `json:"locations"`
}

type RingInfo struct {
	VNodes            []RingVNode `json:"vnodes"`
	ReplicationFactor int         `json:"replication_factor"`
	Chunks            []RingChunk `json:"chunks,omitempty"`
}

type RepairResult struct {
	Checked      int `json:"checked"`
	Restored     int `json:"restored"`
	Unrepairable int `json:"unrepairable"`
}

type DeadEvent struct {
	ID         string         `json:"id"`
	Subject    string         `json:"subject"`
	Payload    map[string]any `json:"payload"`
	Error      string         `json:"error"`
	Deliveries int            `json:"deliveries"`
	Status     string         `json:"status"`
	CreatedAt  time.Time      `json:"created_at"`
	RetriedAt  *time.Time     `json:"retried_at,omitempty"`
}

type DLQListResponse struct {
	Events []DeadEvent `json:"events"`
}

type RetryDeadEventResponse struct {
	Status string `json:"status"`
}

// --- shared / generic ---

// StatusResponse covers the several small {"status": "..."} acknowledgement
// bodies (chunk commit, DLQ retry) that don't warrant their own named type.
type StatusResponse struct {
	Status string `json:"status"`
}

// ErrorResponse mirrors httpserver.errorEnvelope — every non-2xx JSON
// response across all ~55 routes uses this one shape.
type ErrorResponse struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id,omitempty"`
	} `json:"error"`
}
