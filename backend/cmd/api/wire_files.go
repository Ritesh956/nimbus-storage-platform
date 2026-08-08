package main

// Folder + file + search wiring — the org-scoped filesystem itself: browse,
// CRUD, trash/restore, versions, thumbnails, and full-text search over it.
// Grouped together because file.Service depends directly on folder's org
// lookup and storage's router, and search reads the same file/folder rows
// through its own dedicated tsvector-backed repository.

import (
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"nimbus/internal/file"
	"nimbus/internal/folder"
	"nimbus/internal/search"
	"nimbus/internal/storage"
)

// wireFiles registers every /v1/orgs/{orgId}/folders, /v1/folders/*,
// /v1/files/*, trash, and search route, and returns file.Service plus
// fileRepo — upload and sharing wiring both build directly on top of them.
func wireFiles(
	mux *http.ServeMux,
	pg *pgxpool.Pool,
	requireAuth, requireMember func(http.Handler) http.Handler,
	folderRepo *folder.Repository,
	fileRepo *file.Repository,
	storageRepo *storage.Repository,
	router *storage.Router,
	members membershipAdapter,
) (fileSvc *file.Service, requireFolderAccess, requireFileAccess func(http.Handler) http.Handler) {

	folderSvc := folder.NewService(folderRepo)
	folderHandler := folder.NewHandler(folderSvc, fileListerAdapter{repo: fileRepo}, members)
	requireFolderAccess = folder.RequireAccess(folderRepo, members)

	fileSvc = file.NewService(fileRepo, folderOrgLookupAdapter{repo: folderRepo}, storageRepo, router)
	fileHandler := file.NewHandler(fileSvc, members)
	requireFileAccess = file.RequireAccess(fileRepo, members)

	mux.Handle("POST /v1/orgs/{orgId}/folders", requireAuth(requireMember(http.HandlerFunc(folderHandler.Create))))
	mux.Handle("GET /v1/orgs/{orgId}/folders", requireAuth(requireMember(http.HandlerFunc(folderHandler.ListRoot))))
	mux.Handle("GET /v1/orgs/{orgId}/trash/folders", requireAuth(requireMember(http.HandlerFunc(folderHandler.ListTrashed))))
	mux.Handle("GET /v1/orgs/{orgId}/trash/files", requireAuth(requireMember(http.HandlerFunc(fileHandler.ListTrashed))))
	mux.Handle("GET /v1/folders/{folderId}/children", requireAuth(requireFolderAccess(http.HandlerFunc(folderHandler.ListChildren))))
	mux.Handle("GET /v1/folders/{folderId}/path", requireAuth(requireFolderAccess(http.HandlerFunc(folderHandler.Path))))
	mux.Handle("PATCH /v1/folders/{folderId}", requireAuth(requireFolderAccess(http.HandlerFunc(folderHandler.Update))))
	mux.Handle("DELETE /v1/folders/{folderId}", requireAuth(requireFolderAccess(http.HandlerFunc(folderHandler.Delete))))
	mux.Handle("POST /v1/folders/{folderId}/restore", requireAuth(http.HandlerFunc(folderHandler.Restore)))

	mux.Handle("PATCH /v1/files/{fileId}", requireAuth(requireFileAccess(http.HandlerFunc(fileHandler.Update))))
	mux.Handle("DELETE /v1/files/{fileId}", requireAuth(requireFileAccess(http.HandlerFunc(fileHandler.Delete))))
	mux.Handle("POST /v1/files/{fileId}/restore", requireAuth(http.HandlerFunc(fileHandler.Restore)))
	mux.Handle("DELETE /v1/files/{fileId}/purge", requireAuth(http.HandlerFunc(fileHandler.Purge)))
	mux.Handle("GET /v1/files/{fileId}/thumbnail", requireAuth(requireFileAccess(http.HandlerFunc(fileHandler.Thumbnail))))
	mux.Handle("GET /v1/files/{fileId}/versions", requireAuth(requireFileAccess(http.HandlerFunc(fileHandler.ListVersions))))
	mux.Handle("GET /v1/files/{fileId}/versions/{versionId}/download-plan", requireAuth(requireFileAccess(http.HandlerFunc(fileHandler.DownloadPlan))))
	mux.Handle("POST /v1/files/{fileId}/versions/{versionId}/restore", requireAuth(requireFileAccess(http.HandlerFunc(fileHandler.RestoreVersion))))

	searchRepo := search.NewRepository(pg)
	searchSvc := search.NewService(searchRepo)
	searchHandler := search.NewHandler(searchSvc)
	mux.Handle("GET /v1/orgs/{orgId}/search", requireAuth(requireMember(http.HandlerFunc(searchHandler.Search))))

	return fileSvc, requireFolderAccess, requireFileAccess
}
