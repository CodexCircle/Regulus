// Package hub implements the Regulus hub: the HTTP server that serves the
// dashboard and, in later phases, the API and the agent connections.
package hub

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/CodexCircle/Regulus/internal/config"
	"github.com/CodexCircle/Regulus/internal/storage"
)

const shutdownTimeout = 10 * time.Second

// Hub is a running-capable hub instance: its configuration, database and UI.
type Hub struct {
	cfg config.Config
	log *slog.Logger
	db  *sql.DB
	ui  fs.FS
}

// New opens the database (applying migrations) and returns a Hub that serves
// the dashboard from ui. Call Close when done.
func New(ctx context.Context, cfg config.Config, log *slog.Logger, ui fs.FS) (*Hub, error) {
	db, err := storage.Open(ctx, cfg.DataDir)
	if err != nil {
		return nil, fmt.Errorf("storage: %w", err)
	}
	log.Info("database ready", "dir", cfg.DataDir)
	return &Hub{cfg: cfg, log: log, db: db, ui: ui}, nil
}

// Close releases the hub resources.
func (h *Hub) Close() error {
	return h.db.Close()
}

// Serve serves HTTP on ln until ctx is cancelled, then shuts down gracefully.
func (h *Hub) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{
		Handler:           NewHandler(h.ui),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	h.log.Info("hub listening", "addr", ln.Addr().String())

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	h.log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// NewHandler returns the hub HTTP handler. The dashboard is served from ui.
func NewHandler(ui fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, "ok")
	})

	files := http.FileServerFS(ui)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if _, err := fs.Stat(ui, "index.html"); err != nil {
			http.Error(w, "dashboard not built: run `make dashboard` before `make build`", http.StatusServiceUnavailable)
			return
		}
		files.ServeHTTP(w, r)
	})
	return mux
}
