package httpserver

import "net/http"

// CORS allows a browser-based client (the Next.js frontend, a different
// origin in dev) to call this API — a real, necessary gap: without it,
// every browser fetch from the frontend fails preflight before nimbus-api
// ever sees the request. Default is permissive ("*") because this API
// authenticates via a Bearer token in the Authorization header, not
// cookies — there's no ambient credential for a wildcard origin to leak,
// unlike cookie-based auth where "*" would be a real CSRF-adjacent risk.
// A production deployment should still pin this to the real frontend
// origin via NIMBUS_CORS_ORIGIN.
func CORS(allowedOrigin string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Idempotency-Key")
			w.Header().Set("Access-Control-Max-Age", "600")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
