package hub

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/CodexCircle/Regulus/internal/auth"
)

// maxLayoutBytes bounds the layout a user can store.
const maxLayoutBytes = 64 << 10

// LayoutStore keeps the dashboard layout of each user. *Hub implements it.
type LayoutStore interface {
	// Layout returns the stored layout; found is false if the user has none.
	Layout(ctx context.Context, userID int64) (layout json.RawMessage, updatedAt time.Time, found bool, err error)
	SetLayout(ctx context.Context, userID int64, layout json.RawMessage) error
}

// Layout implements LayoutStore.
func (h *Hub) Layout(ctx context.Context, userID int64) (json.RawMessage, time.Time, bool, error) {
	var layout, updated string
	err := h.db.QueryRowContext(ctx,
		`SELECT layout, updated_at FROM dashboard_layouts WHERE user_id = ?`, userID).Scan(&layout, &updated)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, time.Time{}, false, nil
	}
	if err != nil {
		return nil, time.Time{}, false, fmt.Errorf("read layout: %w", err)
	}
	at, _ := time.Parse(time.RFC3339Nano, updated)
	return json.RawMessage(layout), at, true, nil
}

// SetLayout implements LayoutStore.
func (h *Hub) SetLayout(ctx context.Context, userID int64, layout json.RawMessage) error {
	_, err := h.db.ExecContext(ctx,
		`INSERT INTO dashboard_layouts (user_id, layout, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT (user_id) DO UPDATE SET layout = excluded.layout, updated_at = excluded.updated_at`,
		userID, string(layout), time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return fmt.Errorf("save layout: %w", err)
	}
	return nil
}

type layoutJSON struct {
	Layout    json.RawMessage `json:"layout"`
	UpdatedAt *time.Time      `json:"updated_at"`
}

// layoutHandlers serves GET and PUT /api/layout.
type layoutHandlers struct {
	layouts LayoutStore
	log     *slog.Logger
}

// get returns the layout of the logged-in user ({"layout": null} until one is
// saved).
func (h layoutHandlers) get(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFrom(r.Context())
	if !ok || h.layouts == nil {
		writeJSONError(w, http.StatusNotFound, "layouts are not available")
		return
	}
	layout, at, found, err := h.layouts.Layout(r.Context(), user.ID)
	if err != nil {
		h.log.Error("reading the layout failed", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := layoutJSON{Layout: json.RawMessage("null")}
	if found {
		out.Layout, out.UpdatedAt = layout, &at
	}
	writeJSONBody(w, http.StatusOK, out)
}

// put stores the layout of the logged-in user. The body is
// {"layout": <any JSON value>} up to 64 KiB; the hub does not interpret it.
func (h layoutHandlers) put(w http.ResponseWriter, r *http.Request) {
	user, ok := auth.UserFrom(r.Context())
	if !ok || h.layouts == nil {
		writeJSONError(w, http.StatusNotFound, "layouts are not available")
		return
	}
	var body struct {
		Layout json.RawMessage `json:"layout"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxLayoutBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			writeJSONError(w, http.StatusRequestEntityTooLarge, "layout is too large")
			return
		}
		writeJSONError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if len(body.Layout) == 0 || string(body.Layout) == "null" {
		writeJSONError(w, http.StatusBadRequest, `"layout" is required`)
		return
	}
	if err := h.layouts.SetLayout(r.Context(), user.ID, body.Layout); err != nil {
		h.log.Error("saving the layout failed", "err", err)
		writeJSONError(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
