package httpserver

import (
	"net/http"
	"strconv"
	"time"

	"nimbus/internal/platform/metrics"
)

// Metrics records nimbus_http_request_duration_seconds for every request.
// It takes the *http.ServeMux itself (rather than deriving a label from
// r.URL.Path) so the route label is the registered pattern — e.g.
// "GET /v1/files/{fileId}" — not the literal path, keeping cardinality
// bounded no matter how many distinct IDs are ever requested.
func Metrics(mux *http.ServeMux) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)

			_, pattern := mux.Handler(r)
			if pattern == "" {
				pattern = "unmatched"
			}
			metrics.HTTPRequestDuration.
				WithLabelValues(r.Method, pattern, strconv.Itoa(sw.status)).
				Observe(time.Since(start).Seconds())
		})
	}
}
