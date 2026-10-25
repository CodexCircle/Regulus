// Package hub implements the Regulus hub: the HTTP server that serves the
// dashboard and, in later phases, the API and the agent connections.
package hub

import (
	"fmt"
	"io/fs"
	"net/http"
)

// NewHandler returns the hub HTTP handler. The dashboard is served from ui.
func NewHandler(ui fs.FS) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintln(w, "ok")
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
