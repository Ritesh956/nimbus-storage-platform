package httpserver

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// New builds an *http.Server with sane timeouts for the given handler.
func New(addr string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second, // generous: chunk upload commit bodies can be slow on a loaded box
		IdleTimeout:       120 * time.Second,
	}
}

// Run starts srv and blocks until ctx is cancelled (e.g. on SIGTERM), then
// drains in-flight requests within gracePeriod before returning — see
// docs/03-hld.md §2 ("Graceful shutdown").
func Run(ctx context.Context, srv *http.Server, logger *slog.Logger, gracePeriod time.Duration) error {
	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server starting", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	logger.Info("shutting down", "grace_period", gracePeriod)
	shutdownCtx, cancel := context.WithTimeout(context.Background(), gracePeriod)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
