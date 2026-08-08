package main

// Upload + sharing wiring — both are the last concerns built because both
// depend on fileSvc/fileRepo/folderRepo from wire_files.go and, for upload,
// activitySvc/eventPublisher from wire_activity.go and the earlier NATS
// setup in run(). Grouped together because sharing.Service takes fileSvc
// (file.Service) directly as its move/copy port, the one cross-module
// dependency that isn't satisfied by an adapter in adapters.go.

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"nimbus/internal/activity"
	"nimbus/internal/events"
	"nimbus/internal/file"
	"nimbus/internal/folder"
	"nimbus/internal/platform/config"
	"nimbus/internal/sharing"
	"nimbus/internal/storage"
	"nimbus/internal/upload"
)

// wireUpload registers the chunk-check/init/commit/complete routes and the
// file/folder/bundle share routes (including the public, unauthenticated
// /v1/shares/* reads).
func wireUpload(
	mux *http.ServeMux,
	pg *pgxpool.Pool,
	cfg config.Config,
	requireAuth, requireMember, requireFileAccess, requireFolderAccess func(http.Handler) http.Handler,
	router *storage.Router,
	fileRepo *file.Repository,
	folderRepo *folder.Repository,
	sharingRepo *sharing.Repository,
	fileSvc *file.Service,
	members membershipAdapter,
	activitySvc *activity.Service,
	eventPublisher *events.Publisher,
) {
	// fileRepo.CreateWithVersion/AddVersion/GetForUpload already match
	// upload.FileCreator's signatures exactly, and activitySvc/eventPublisher
	// already match upload.ActivityRecorder/EventPublisher — all passed
	// directly, no adapters needed.
	uploadRepo := upload.NewRepository(pg)
	uploadSvc := upload.NewService(uploadRepo, router, fileRepo, folderOrgLookupAdapter{repo: folderRepo}, members, activitySvc, eventPublisher, fileRepo,
		cfg.ReplicationFactor, cfg.WriteQuorum, cfg.ChunkSizeBytes, cfg.MaxUploadBytes, cfg.OrgQuotaBytes)
	uploadHandler := upload.NewHandler(uploadSvc)
	requireUploadAccess := upload.RequireAccess(uploadRepo, members)

	mux.Handle("POST /v1/chunks/check", requireAuth(http.HandlerFunc(uploadHandler.CheckChunks)))
	mux.Handle("POST /v1/uploads", requireAuth(http.HandlerFunc(uploadHandler.InitUpload)))
	mux.Handle("POST /v1/uploads/{uploadId}/chunks/{hash}/init", requireAuth(requireUploadAccess(http.HandlerFunc(uploadHandler.InitChunk))))
	mux.Handle("POST /v1/uploads/{uploadId}/chunks/{hash}/commit", requireAuth(requireUploadAccess(http.HandlerFunc(uploadHandler.CommitChunk))))
	mux.Handle("POST /v1/uploads/{uploadId}/complete", requireAuth(requireUploadAccess(http.HandlerFunc(uploadHandler.Complete))))

	sharingSvc := sharing.NewService(sharingRepo, fileShareLookupAdapter{repo: fileRepo}, fileScopeAdapter{repo: fileRepo},
		folderShareAdapter{folders: folderRepo, files: fileRepo}, fileSvc)
	sharingHandler := sharing.NewHandler(sharingSvc, members)

	mux.Handle("POST /v1/files/{fileId}/share", requireAuth(requireFileAccess(http.HandlerFunc(sharingHandler.Create))))
	mux.Handle("POST /v1/folders/{folderId}/share", requireAuth(requireFolderAccess(http.HandlerFunc(sharingHandler.CreateFolder))))
	mux.Handle("POST /v1/orgs/{orgId}/shares", requireAuth(requireMember(http.HandlerFunc(sharingHandler.CreateBundle))))
	// The /v1/shares/* reads are public — no requireAuth; that's the point of a share link.
	mux.HandleFunc("GET /v1/shares/{token}", sharingHandler.Resolve)
	mux.HandleFunc("GET /v1/shares/{token}/folders/{folderId}", sharingHandler.Children)
	mux.HandleFunc("GET /v1/shares/{token}/files/{fileId}/download-plan", sharingHandler.DownloadPlan)
	mux.Handle("DELETE /v1/shares/{token}", requireAuth(http.HandlerFunc(sharingHandler.Delete)))
}
