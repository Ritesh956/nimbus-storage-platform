package httpserver

import (
	"encoding/json"
	"net/http"

	"nimbus/internal/platform/logging"
)

// ErrorCode is the fixed, small set of API error codes from
// docs/06-api-design.md §1 — domain code returns these, never raw HTTP
// status codes (docs/03-hld.md §2).
type ErrorCode string

const (
	ErrInvalid      ErrorCode = "invalid"
	ErrUnauthorized ErrorCode = "unauthorized"
	ErrForbidden    ErrorCode = "forbidden"
	ErrNotFound     ErrorCode = "not_found"
	ErrConflict     ErrorCode = "conflict"
	ErrTooLarge     ErrorCode = "too_large"
	ErrQuotaFull    ErrorCode = "quota_exceeded"
	ErrRateLimited  ErrorCode = "rate_limited"
	ErrInternal     ErrorCode = "internal"
)

var statusForCode = map[ErrorCode]int{
	ErrInvalid:      http.StatusBadRequest,
	ErrUnauthorized: http.StatusUnauthorized,
	ErrForbidden:    http.StatusForbidden,
	ErrNotFound:     http.StatusNotFound,
	ErrConflict:     http.StatusConflict,
	ErrTooLarge:     http.StatusRequestEntityTooLarge,
	ErrQuotaFull:    http.StatusInsufficientStorage,
	ErrRateLimited:  http.StatusTooManyRequests,
	ErrInternal:     http.StatusInternalServerError,
}

type errorEnvelope struct {
	Error struct {
		Code      ErrorCode `json:"code"`
		Message   string    `json:"message"`
		RequestID string    `json:"request_id,omitempty"`
	} `json:"error"`
}

// WriteError writes the standard error envelope (docs/06-api-design.md §1).
func WriteError(w http.ResponseWriter, r *http.Request, code ErrorCode, message string) {
	status, ok := statusForCode[code]
	if !ok {
		status = http.StatusInternalServerError
	}
	var body errorEnvelope
	body.Error.Code = code
	body.Error.Message = message
	body.Error.RequestID = logging.RequestIDFromContext(r.Context())

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// WriteJSON writes v as a JSON response body with the given status.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
