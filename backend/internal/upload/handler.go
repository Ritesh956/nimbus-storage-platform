package upload

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"nimbus/internal/auth"
	"nimbus/internal/platform/httpserver"
	"nimbus/internal/storage"
)

type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

type checkChunksRequest struct {
	Hashes []string `json:"hashes"`
}

func (h *Handler) CheckChunks(w http.ResponseWriter, r *http.Request) {
	var req checkChunksRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "malformed JSON body")
		return
	}
	missing, err := h.svc.CheckChunks(r.Context(), req.Hashes)
	if err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInternal, "failed to check chunks")
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{"missing": missing})
}

type initUploadRequest struct {
	FolderID string `json:"folder_id"`
	// FileID, if set, uploads a new version of an existing file instead of
	// creating one — folder_id/name are then optional (docs/09-roadmap.md
	// Day 6).
	FileID    string `json:"file_id"`
	Name      string `json:"name"`
	SizeBytes int64  `json:"size_bytes"`
	MimeType  string `json:"mime_type"`
}

func (h *Handler) InitUpload(w http.ResponseWriter, r *http.Request) {
	var req initUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "malformed JSON body")
		return
	}
	if req.FileID == "" && (req.FolderID == "" || req.Name == "") {
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "either file_id, or folder_id and name, are required")
		return
	}
	userID := auth.UserIDFromContext(r.Context())
	u, err := h.svc.InitUpload(r.Context(), userID, req.FolderID, req.FileID, req.Name, req.SizeBytes, req.MimeType)
	if err != nil {
		writeUploadError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusCreated, map[string]string{"upload_id": u.ID})
}

func (h *Handler) InitChunk(w http.ResponseWriter, r *http.Request) {
	u, _ := UploadFromContext(r.Context())
	hash := r.PathValue("hash")

	targets, expiresAt, err := h.svc.InitChunk(r.Context(), u, hash)
	if err != nil {
		writeUploadError(w, r, err)
		return
	}
	resp := make([]map[string]string, len(targets))
	for i, t := range targets {
		resp[i] = map[string]string{"node_id": t.NodeID, "put_url": t.PutURL}
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]any{
		"targets":    resp,
		"expires_at": expiresAt.Format(time.RFC3339),
	})
}

type commitChunkRequest struct {
	SizeBytes int64             `json:"size_bytes"`
	Etags     map[string]string `json:"etags"`
}

func (h *Handler) CommitChunk(w http.ResponseWriter, r *http.Request) {
	u, _ := UploadFromContext(r.Context())
	hash := r.PathValue("hash")

	var req commitChunkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "malformed JSON body")
		return
	}
	if err := h.svc.CommitChunk(r.Context(), u, hash, req.SizeBytes, req.Etags); err != nil {
		writeUploadError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusOK, map[string]string{"status": "committed"})
}

type completeRequest struct {
	ChunkOrder     []string `json:"chunk_order"`
	SizeBytes      int64    `json:"size_bytes"`
	ChecksumSHA256 string   `json:"checksum_sha256"`
}

func (h *Handler) Complete(w http.ResponseWriter, r *http.Request) {
	u, _ := UploadFromContext(r.Context())

	var req completeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "malformed JSON body")
		return
	}
	idempotencyKey := r.Header.Get("Idempotency-Key")

	fileID, versionID, err := h.svc.CompleteUpload(r.Context(), u, idempotencyKey, req.ChunkOrder, req.SizeBytes, req.ChecksumSHA256)
	if err != nil {
		writeUploadError(w, r, err)
		return
	}
	httpserver.WriteJSON(w, http.StatusCreated, map[string]string{"file_id": fileID, "version_id": versionID})
}

func writeUploadError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrUploadNotFound):
		httpserver.WriteError(w, r, httpserver.ErrNotFound, "upload not found")
	case errors.Is(err, ErrInvalidFolder):
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "invalid folder")
	case errors.Is(err, ErrTargetFileNotFound):
		httpserver.WriteError(w, r, httpserver.ErrNotFound, "target file not found")
	case errors.Is(err, ErrForbidden):
		httpserver.WriteError(w, r, httpserver.ErrForbidden, "not a member of this organization")
	case errors.Is(err, ErrChunkNotFound):
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "chunk was never initialized - call init first")
	case errors.Is(err, ErrInvalidState):
		httpserver.WriteError(w, r, httpserver.ErrConflict, "chunk is not awaiting commit")
	case errors.Is(err, ErrChecksumMismatch):
		httpserver.WriteError(w, r, httpserver.ErrConflict, "replica checksums do not agree")
	case errors.Is(err, ErrUploadNotInProgress):
		httpserver.WriteError(w, r, httpserver.ErrConflict, "upload is not in progress")
	case errors.Is(err, ErrEmptyChunkOrder):
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "chunk_order must not be empty")
	case errors.Is(err, ErrMissingChunks):
		httpserver.WriteError(w, r, httpserver.ErrInvalid, "one or more chunks in chunk_order were never committed")
	case errors.Is(err, ErrFileTooLarge):
		httpserver.WriteError(w, r, httpserver.ErrTooLarge, "file exceeds the maximum upload size")
	case errors.Is(err, ErrQuotaExceeded):
		httpserver.WriteError(w, r, httpserver.ErrQuotaFull, "organization storage quota exceeded")
	case errors.Is(err, storage.ErrInsufficientHealthyNodes):
		httpserver.WriteError(w, r, httpserver.ErrInternal, "not enough healthy storage nodes to place this chunk")
	case errors.Is(err, storage.ErrNoNodes):
		httpserver.WriteError(w, r, httpserver.ErrInternal, "no storage nodes configured")
	default:
		slog.Default().Error("upload handler: unexpected error", "error", err)
		httpserver.WriteError(w, r, httpserver.ErrInternal, "upload operation failed")
	}
}
