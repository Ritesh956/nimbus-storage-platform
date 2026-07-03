package httpserver

import (
	"context"
	"net/http"
	"time"
)

// Checker probes one dependency (Postgres, Redis, NATS, ...) and returns a
// non-nil error if it's not reachable.
type Checker func(ctx context.Context) error

// ReadinessChecker aggregates dependency checks behind /readyz, per
// docs/03-hld.md §2 ("Health/readiness").
type ReadinessChecker struct {
	checks map[string]Checker
}

func NewReadinessChecker() *ReadinessChecker {
	return &ReadinessChecker{checks: map[string]Checker{}}
}

func (rc *ReadinessChecker) Register(name string, c Checker) {
	rc.checks[name] = c
}

func (rc *ReadinessChecker) Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()

		results := make(map[string]string, len(rc.checks))
		allOK := true
		for name, check := range rc.checks {
			if err := check(ctx); err != nil {
				results[name] = err.Error()
				allOK = false
			} else {
				results[name] = "ok"
			}
		}

		status := http.StatusOK
		if !allOK {
			status = http.StatusServiceUnavailable
		}
		WriteJSON(w, status, map[string]any{"status": statusLabel(allOK), "checks": results})
	}
}

func statusLabel(ok bool) string {
	if ok {
		return "ready"
	}
	return "not_ready"
}

// Liveness reports the process is up and serving — no dependency checks.
// Kubernetes uses this to decide whether to restart the container.
func Liveness(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
