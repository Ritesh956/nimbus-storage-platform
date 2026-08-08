package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"regexp"
	"strings"

	"nimbus/internal/apidoc"
)

// routeDoc is the one hand-maintained part of this generator: per-route
// summary/tags/request-and-response DTOs. Unlike routes.go's path/method
// extraction (parsed from source, can't drift), this table can go stale if
// a handler's response shape changes without a matching edit here — see
// internal/apidoc/schemas.go's package doc for why that trade-off was made
// instead of rewriting every handler to use typed structs. What it can't
// do is silently drop or misname a route: routeGen (below) fails the build
// if routes.go finds a route with no entry here, so missing coverage is a
// loud build error, not a quiet gap.
type routeDoc struct {
	Summary  string
	Tags     []string
	Request  any // nil if the route takes no body
	Response responseDoc
	Query    []queryParam
}

type responseDoc struct {
	Status      string
	Description string
	Body        any    // nil for a bodyless response (204, etc.)
	OneOf       []any  // set instead of Body for discriminated-union responses
	ContentType string // defaults to "application/json"; set for SSE/plain text
}

type queryParam struct {
	Name     string
	Type     string // "string" | "integer"
	Required bool
}

// routeTable maps "METHOD /path" (exactly as it appears in cmd/api/*.go's
// mux.Handle calls) to its documentation. Grouped to mirror wire_*.go.
var routeTable = map[string]routeDoc{
	// --- infra ---
	"GET /healthz": {Summary: "Liveness probe", Tags: []string{"infra"}, Response: responseDoc{Status: "200", Description: "process is up", ContentType: "text/plain"}},
	"GET /readyz":  {Summary: "Readiness probe (Postgres/Redis/NATS)", Tags: []string{"infra"}, Response: responseDoc{Status: "200", Description: "dependencies reachable", Body: map[string]any{}}},
	"GET /metrics": {Summary: "Prometheus scrape endpoint", Tags: []string{"infra"}, Response: responseDoc{Status: "200", Description: "Prometheus text exposition format", ContentType: "text/plain"}},

	// --- auth (wire_auth.go) ---
	"POST /v1/auth/register": {
		Summary: "Register a new user (and, unless org_name is a member-join, a default org)", Tags: []string{"auth"},
		Request: apidoc.RegisterRequest{}, Response: responseDoc{Status: "201", Description: "user created", Body: apidoc.RegisterResponse{}},
	},
	"POST /v1/auth/login": {
		Summary: "Log in with email/password", Tags: []string{"auth"},
		Request: apidoc.LoginRequest{},
		Response: responseDoc{Status: "200", Description: "token pair, or a TOTP challenge if 2FA is enabled",
			OneOf: []any{apidoc.TokenPairResponse{}, apidoc.TOTPChallengeResponse{}}},
	},
	"POST /v1/auth/login/totp": {
		Summary: "Complete a TOTP-gated login", Tags: []string{"auth"},
		Request: apidoc.TOTPLoginRequest{}, Response: responseDoc{Status: "200", Description: "token pair", Body: apidoc.TokenPairResponse{}},
	},
	"POST /v1/auth/refresh": {
		Summary: "Rotate a refresh token for a new token pair", Tags: []string{"auth"},
		Request: apidoc.RefreshRequest{}, Response: responseDoc{Status: "200", Description: "new token pair", Body: apidoc.TokenPairResponse{}},
	},
	"POST /v1/auth/logout": {
		Summary: "Revoke a refresh token", Tags: []string{"auth"},
		Request: apidoc.LogoutRequest{}, Response: responseDoc{Status: "204", Description: "logged out"},
	},
	"POST /v1/auth/password/forgot": {
		Summary: "Request a password-reset email (always 202, anti-enumeration)", Tags: []string{"auth"},
		Request: apidoc.ForgotPasswordRequest{}, Response: responseDoc{Status: "202", Description: "accepted regardless of whether the email is registered"},
	},
	"POST /v1/auth/password/reset": {
		Summary: "Reset a password with a reset token", Tags: []string{"auth"},
		Request: apidoc.ResetPasswordRequest{}, Response: responseDoc{Status: "204", Description: "password changed; every refresh-token family for the user is revoked"},
	},
	"GET /v1/auth/me": {
		Summary: "The caller's own identity", Tags: []string{"auth"}, Response: responseDoc{Status: "200", Description: "identity", Body: apidoc.MeResponse{}},
	},
	"GET /v1/auth/totp": {
		Summary: "Whether TOTP 2FA is enabled for the caller", Tags: []string{"auth"}, Response: responseDoc{Status: "200", Description: "status", Body: apidoc.TOTPStatusResponse{}},
	},
	"POST /v1/auth/totp/setup": {
		Summary: "Start TOTP enrollment", Tags: []string{"auth"}, Response: responseDoc{Status: "200", Description: "secret + otpauth URI for a QR code", Body: apidoc.TOTPSetupResponse{}},
	},
	"POST /v1/auth/totp/confirm": {
		Summary: "Confirm TOTP enrollment with a code", Tags: []string{"auth"},
		Request: apidoc.TOTPCodeRequest{}, Response: responseDoc{Status: "204", Description: "2FA enabled"},
	},
	"DELETE /v1/auth/totp": {
		Summary: "Disable TOTP 2FA", Tags: []string{"auth"},
		Request: apidoc.TOTPCodeRequest{}, Response: responseDoc{Status: "204", Description: "2FA disabled"},
	},
	"POST /v1/orgs": {
		Summary: "Create an organization", Tags: []string{"orgs"},
		Request: apidoc.CreateOrgRequest{}, Response: responseDoc{Status: "201", Description: "organization created", Body: apidoc.Organization{}},
	},
	"GET /v1/orgs": {
		Summary: "Organizations the caller belongs to", Tags: []string{"orgs"}, Response: responseDoc{Status: "200", Description: "orgs", Body: []apidoc.Organization{}},
	},
	"GET /v1/orgs/{orgId}/members": {
		Summary: "List an org's members", Tags: []string{"orgs"}, Response: responseDoc{Status: "200", Description: "members", Body: []apidoc.Member{}},
	},
	"POST /v1/orgs/{orgId}/members": {
		Summary: "Add a member by email (admin/owner only; only owner can grant admin/owner)", Tags: []string{"orgs"},
		Request: apidoc.AddMemberRequest{}, Response: responseDoc{Status: "201", Description: "member added", Body: apidoc.AddMemberResponse{}},
	},
	"DELETE /v1/orgs/{orgId}/members/{userId}": {
		Summary: "Remove a member (the owner is structurally unremovable)", Tags: []string{"orgs"}, Response: responseDoc{Status: "204", Description: "removed"},
	},
	"GET /v1/orgs/{orgId}/usage": {
		Summary: "Aggregate org usage/oversight (owner-gated)", Tags: []string{"orgs"}, Response: responseDoc{Status: "200", Description: "usage", Body: apidoc.OrgUsage{}},
	},

	// --- activity (wire_activity.go) ---
	"GET /v1/orgs/{orgId}/activity": {
		Summary: "Cursor-paginated activity feed", Tags: []string{"activity"},
		Query:    []queryParam{{Name: "cursor", Type: "string"}},
		Response: responseDoc{Status: "200", Description: "events page", Body: apidoc.ActivityResponse{}},
	},
	"GET /v1/orgs/{orgId}/events": {
		Summary: "Live activity stream (server-sent events)", Tags: []string{"activity"},
		Response: responseDoc{Status: "200", Description: "text/event-stream of activity events", ContentType: "text/event-stream"},
	},

	// --- folders/files (wire_files.go) ---
	"POST /v1/orgs/{orgId}/folders": {
		Summary: "Create a folder", Tags: []string{"folders"},
		Request: apidoc.CreateFolderRequest{}, Response: responseDoc{Status: "201", Description: "folder created", Body: apidoc.FolderNode{}},
	},
	"GET /v1/orgs/{orgId}/folders": {
		Summary: "An org's root-level folders", Tags: []string{"folders"}, Response: responseDoc{Status: "200", Description: "folders", Body: []apidoc.FolderNode{}},
	},
	"GET /v1/orgs/{orgId}/trash/folders": {
		Summary: "Soft-deleted folders", Tags: []string{"folders"}, Response: responseDoc{Status: "200", Description: "trashed folders", Body: []apidoc.FolderNode{}},
	},
	"GET /v1/orgs/{orgId}/trash/files": {
		Summary: "Soft-deleted files", Tags: []string{"files"}, Response: responseDoc{Status: "200", Description: "trashed files", Body: []apidoc.FileNode{}},
	},
	"GET /v1/folders/{folderId}/children": {
		Summary: "A folder's direct child folders and files", Tags: []string{"folders"}, Response: responseDoc{Status: "200", Description: "children", Body: apidoc.FolderChildrenResponse{}},
	},
	"GET /v1/folders/{folderId}/path": {
		Summary: "Ancestor chain from root to this folder, for breadcrumbs", Tags: []string{"folders"}, Response: responseDoc{Status: "200", Description: "path segments", Body: []apidoc.PathSegment{}},
	},
	"PATCH /v1/folders/{folderId}": {
		Summary: "Rename and/or move a folder", Tags: []string{"folders"},
		Request: apidoc.FolderUpdateRequest{}, Response: responseDoc{Status: "200", Description: "updated folder", Body: apidoc.FolderNode{}},
	},
	"DELETE /v1/folders/{folderId}": {
		Summary: "Move a folder to trash", Tags: []string{"folders"}, Response: responseDoc{Status: "204", Description: "trashed"},
	},
	"POST /v1/folders/{folderId}/restore": {
		Summary: "Restore a trashed folder", Tags: []string{"folders"}, Response: responseDoc{Status: "200", Description: "restored folder", Body: apidoc.FolderNode{}},
	},
	"PATCH /v1/files/{fileId}": {
		Summary: "Rename and/or move a file", Tags: []string{"files"},
		Request: apidoc.FileUpdateRequest{}, Response: responseDoc{Status: "200", Description: "updated file", Body: apidoc.FileNode{}},
	},
	"DELETE /v1/files/{fileId}": {
		Summary: "Move a file to trash", Tags: []string{"files"}, Response: responseDoc{Status: "204", Description: "trashed"},
	},
	"POST /v1/files/{fileId}/restore": {
		Summary: "Restore a trashed file", Tags: []string{"files"}, Response: responseDoc{Status: "200", Description: "restored file", Body: apidoc.FileNode{}},
	},
	"DELETE /v1/files/{fileId}/purge": {
		Summary: "Permanently delete a trashed file (irreversible)", Tags: []string{"files"}, Response: responseDoc{Status: "204", Description: "purged; storage reclaimed by the GC sweeper"},
	},
	"GET /v1/files/{fileId}/thumbnail": {
		Summary: "Presigned GET URLs for the file's thumbnail replicas", Tags: []string{"files"}, Response: responseDoc{Status: "200", Description: "targets", Body: apidoc.ThumbnailResponse{}},
	},
	"GET /v1/files/{fileId}/versions": {
		Summary: "Version history, newest first", Tags: []string{"files"}, Response: responseDoc{Status: "200", Description: "versions", Body: []apidoc.FileVersion{}},
	},
	"GET /v1/files/{fileId}/versions/{versionId}/download-plan": {
		Summary: "Presigned, hash-verified download plan for one version", Tags: []string{"files"}, Response: responseDoc{Status: "200", Description: "download plan", Body: apidoc.DownloadPlan{}},
	},
	"POST /v1/files/{fileId}/versions/{versionId}/restore": {
		Summary: "Make an older version the latest (a new version, not a rewrite)", Tags: []string{"files"}, Response: responseDoc{Status: "200", Description: "updated file", Body: apidoc.FileNode{}},
	},
	"GET /v1/orgs/{orgId}/search": {
		Summary: "Full-text file search with filters", Tags: []string{"search"},
		Query: []queryParam{
			{Name: "q", Type: "string"}, {Name: "type", Type: "string"}, {Name: "owner", Type: "string"},
			{Name: "cursor", Type: "string"}, {Name: "limit", Type: "integer"},
			{Name: "date_from", Type: "string"}, {Name: "date_to", Type: "string"},
			{Name: "size_min", Type: "integer"}, {Name: "size_max", Type: "integer"},
		},
		Response: responseDoc{Status: "200", Description: "results page", Body: apidoc.SearchResponse{}},
	},

	// --- cluster ops / admin (wire_storage.go) ---
	"GET /v1/admin/nodes": {
		Summary: "List storage nodes and their health", Tags: []string{"admin"}, Response: responseDoc{Status: "200", Description: "nodes", Body: []apidoc.StorageNode{}},
	},
	"GET /v1/admin/ring": {
		Summary: "Consistent-hash ring snapshot, optionally for one file's chunks", Tags: []string{"admin"},
		Query:    []queryParam{{Name: "file_id", Type: "string"}},
		Response: responseDoc{Status: "200", Description: "ring info", Body: apidoc.RingInfo{}},
	},
	"POST /v1/admin/nodes/{nodeId}/repair": {
		Summary: "Re-verify and re-replicate a node's chunks (manual trigger)", Tags: []string{"admin"}, Response: responseDoc{Status: "200", Description: "repair result", Body: apidoc.RepairResult{}},
	},
	"GET /v1/admin/dlq": {
		Summary: "List dead-lettered events", Tags: []string{"admin"}, Response: responseDoc{Status: "200", Description: "dead events", Body: apidoc.DLQListResponse{}},
	},
	"POST /v1/admin/dlq/{id}/retry": {
		Summary: "Republish a dead event", Tags: []string{"admin"}, Response: responseDoc{Status: "200", Description: "retried", Body: apidoc.RetryDeadEventResponse{}},
	},

	// --- uploads + sharing (wire_upload.go) ---
	"POST /v1/uploads": {
		Summary: "Start a chunked upload (new file or new version)", Tags: []string{"uploads"},
		Request: apidoc.InitUploadRequest{}, Response: responseDoc{Status: "201", Description: "upload session", Body: apidoc.InitUploadResponse{}},
	},
	"POST /v1/uploads/{uploadId}/chunks/check": {
		Summary: "Which of these content hashes does this org still need to upload", Tags: []string{"uploads"},
		Request: apidoc.CheckChunksRequest{}, Response: responseDoc{Status: "200", Description: "missing hashes", Body: apidoc.CheckChunksResponse{}},
	},
	"POST /v1/uploads/{uploadId}/chunks/{hash}/init": {
		Summary: "Presigned PUT URLs for one chunk's replicas", Tags: []string{"uploads"}, Response: responseDoc{Status: "200", Description: "targets", Body: apidoc.InitChunkResponse{}},
	},
	"POST /v1/uploads/{uploadId}/chunks/{hash}/commit": {
		Summary: "Confirm a chunk's replicas were written (ETag cross-check)", Tags: []string{"uploads"},
		Request: apidoc.CommitChunkRequest{}, Response: responseDoc{Status: "200", Description: "committed", Body: apidoc.StatusResponse{}},
	},
	"POST /v1/uploads/{uploadId}/complete": {
		Summary: "Finalize an upload (idempotent via Idempotency-Key header)", Tags: []string{"uploads"},
		Request: apidoc.CompleteUploadRequest{}, Response: responseDoc{Status: "201", Description: "file created/updated", Body: apidoc.CompleteUploadResponse{}},
	},
	"POST /v1/files/{fileId}/share": {
		Summary: "Create a share link for one file", Tags: []string{"sharing"},
		Request: apidoc.ShareCreateRequest{}, Response: responseDoc{Status: "201", Description: "share link", Body: apidoc.ShareLink{}},
	},
	"POST /v1/folders/{folderId}/share": {
		Summary: "Create a share link for a folder (and its descendants)", Tags: []string{"sharing"},
		Request: apidoc.ShareCreateRequest{}, Response: responseDoc{Status: "201", Description: "share link", Body: apidoc.ShareLink{}},
	},
	"POST /v1/orgs/{orgId}/shares": {
		Summary: "Create a bundle share link for several files", Tags: []string{"sharing"},
		Request: apidoc.CreateBundleShareRequest{}, Response: responseDoc{Status: "201", Description: "share link", Body: apidoc.ShareLink{}},
	},
	"GET /v1/shares/{token}": {
		Summary: "Resolve a share link (public, no auth)", Tags: []string{"sharing"},
		Response: responseDoc{Status: "200", Description: "resolved share, shape depends on kind",
			OneOf: []any{apidoc.ResolvedShareFile{}, apidoc.ResolvedShareFolder{}, apidoc.ResolvedShareBundle{}}},
	},
	"GET /v1/shares/{token}/folders/{folderId}": {
		Summary: "Navigate inside a folder share (public, no auth)", Tags: []string{"sharing"}, Response: responseDoc{Status: "200", Description: "children", Body: apidoc.ShareChildrenResponse{}},
	},
	"GET /v1/shares/{token}/files/{fileId}/download-plan": {
		Summary: "Download plan for one file inside a share (public, no auth)", Tags: []string{"sharing"}, Response: responseDoc{Status: "200", Description: "download plan", Body: apidoc.ShareDownloadPlanResponse{}},
	},
	"DELETE /v1/shares/{token}": {
		Summary: "Revoke a share link", Tags: []string{"sharing"}, Response: responseDoc{Status: "204", Description: "revoked"},
	},
}

// pathParamPattern extracts {name} placeholders from a Go 1.22 mux pattern.
var pathParamPattern = regexp.MustCompile(`\{([a-zA-Z0-9_]+)\}`)

func main() {
	dir := flag.String("dir", "cmd/api", "directory to parse route registrations from")
	out := flag.String("out", "", "output file (default: stdout)")
	flag.Parse()

	routes, err := parseRoutes(*dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "openapi-gen:", err)
		os.Exit(1)
	}

	sb := newSchemaBuilder()
	doc := Document{
		OpenAPI: "3.0.3",
		Info: Info{
			Title: "Nimbus Storage API",
			Description: "Generated from cmd/api/*.go's real mux.Handle registrations " +
				"(cmd/openapi-gen) — paths/methods/auth requirements can't drift from " +
				"what's actually registered; request/response schemas come from a " +
				"hand-maintained mapping table verified against the handlers " +
				"(internal/apidoc). Describes nimbus-api's own /v1/* routes only — " +
				"not the frontend's Next.js BFF auth proxy (app/api/auth/*), which " +
				"is a separate, unversioned surface.",
			Version: "1",
		},
		Servers: []Server{{URL: "http://localhost:8080", Description: "local dev (Compose or kind)"}},
		Paths:   map[string]PathItem{},
		Components: Components{
			Schemas: sb.components,
			SecuritySchemes: map[string]SecurityScheme{
				"bearerAuth": {Type: "http", Scheme: "bearer", BearerFormat: "JWT"},
			},
		},
	}

	var missing []string
	for _, route := range routes {
		key := route.Method + " " + route.Path
		rd, ok := routeTable[key]
		if !ok {
			missing = append(missing, key)
			continue
		}

		op := Operation{
			Summary:   rd.Summary,
			Tags:      rd.Tags,
			Responses: map[string]Response{},
		}
		if len(route.Auth) > 0 {
			op.Security = []map[string][]string{{"bearerAuth": {}}}
		}

		for _, name := range pathParamPattern.FindAllStringSubmatch(route.Path, -1) {
			op.Parameters = append(op.Parameters, Parameter{Name: name[1], In: "path", Required: true, Schema: &Schema{Type: "string"}})
		}
		for _, q := range rd.Query {
			t := q.Type
			if t == "" {
				t = "string"
			}
			op.Parameters = append(op.Parameters, Parameter{Name: q.Name, In: "query", Required: q.Required, Schema: &Schema{Type: t}})
		}

		if rd.Request != nil {
			op.RequestBody = &RequestBody{
				Required: true,
				Content:  map[string]MediaType{"application/json": {Schema: sb.refOf(rd.Request)}},
			}
		}

		resp := Response{Description: rd.Response.Description}
		ct := rd.Response.ContentType
		if ct == "" {
			ct = "application/json"
		}
		switch {
		case len(rd.Response.OneOf) > 0:
			resp.Content = map[string]MediaType{ct: {Schema: sb.oneOfRefs(rd.Response.OneOf...)}}
		case rd.Response.Body != nil:
			resp.Content = map[string]MediaType{ct: {Schema: sb.refOf(rd.Response.Body)}}
		case ct != "application/json":
			resp.Content = map[string]MediaType{ct: {}}
		}
		status := rd.Response.Status
		if status == "" {
			status = "200"
		}
		op.Responses[status] = resp
		op.Responses["default"] = Response{
			Description: "error",
			Content:     map[string]MediaType{"application/json": {Schema: sb.refOf(apidoc.ErrorResponse{})}},
		}

		if doc.Paths[route.Path] == nil {
			doc.Paths[route.Path] = PathItem{}
		}
		doc.Paths[route.Path][strings.ToLower(route.Method)] = op
	}

	if len(missing) > 0 {
		fmt.Fprintln(os.Stderr, "openapi-gen: routes registered in cmd/api but missing from routeTable in cmd/openapi-gen/main.go:")
		for _, m := range missing {
			fmt.Fprintln(os.Stderr, "  -", m)
		}
		os.Exit(1)
	}

	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "openapi-gen: marshal:", err)
		os.Exit(1)
	}

	if *out == "" {
		fmt.Println(string(data))
		return
	}
	if err := os.WriteFile(*out, append(data, '\n'), 0o644); err != nil {
		fmt.Fprintln(os.Stderr, "openapi-gen:", err)
		os.Exit(1)
	}
}
