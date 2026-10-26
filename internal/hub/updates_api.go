package hub

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/CodexCircle/Regulus/internal/auth"
	"github.com/CodexCircle/Regulus/internal/update"
)

// UpdateControl is what the HTTP API needs to report and steer the updates of
// the hub. *update.Manager implements it.
type UpdateControl interface {
	Status(ctx context.Context) (update.Status, error)
	SetSettings(ctx context.Context, by string, s update.Settings) (update.Status, error)
	Check(ctx context.Context) (update.Status, error)
	Download(ctx context.Context, by string) (update.Status, error)
	Apply(ctx context.Context, by string) (update.Status, error)
}

type updateHandlers struct {
	updates UpdateControl
}

func (u updateHandlers) status(w http.ResponseWriter, r *http.Request) {
	if u.updates == nil {
		http.NotFound(w, r)
		return
	}
	st, err := u.updates.Status(r.Context())
	u.reply(w, http.StatusOK, st, err)
}

func (u updateHandlers) settings(w http.ResponseWriter, r *http.Request) {
	if u.updates == nil {
		http.NotFound(w, r)
		return
	}
	var req update.Settings
	if !decodeBody(w, r, &req) {
		return
	}
	by, _ := auth.UserFrom(r.Context())
	st, err := u.updates.SetSettings(r.Context(), by.Username, req)
	u.reply(w, http.StatusOK, st, err)
}

func (u updateHandlers) check(w http.ResponseWriter, r *http.Request) {
	if u.updates == nil {
		http.NotFound(w, r)
		return
	}
	var req struct{}
	if !decodeBody(w, r, &req) {
		return
	}
	st, err := u.updates.Check(r.Context())
	u.reply(w, http.StatusOK, st, err)
}

func (u updateHandlers) download(w http.ResponseWriter, r *http.Request) {
	if u.updates == nil {
		http.NotFound(w, r)
		return
	}
	var req struct{}
	if !decodeBody(w, r, &req) {
		return
	}
	by, _ := auth.UserFrom(r.Context())
	st, err := u.updates.Download(r.Context(), by.Username)
	u.reply(w, http.StatusAccepted, st, err)
}

func (u updateHandlers) apply(w http.ResponseWriter, r *http.Request) {
	if u.updates == nil {
		http.NotFound(w, r)
		return
	}
	var req struct{}
	if !decodeBody(w, r, &req) {
		return
	}
	by, _ := auth.UserFrom(r.Context())
	st, err := u.updates.Apply(r.Context(), by.Username)
	u.reply(w, http.StatusAccepted, st, err)
}

// reply writes the status, or the error as the status code that fits it.
func (u updateHandlers) reply(w http.ResponseWriter, ok int, st update.Status, err error) {
	switch {
	case err == nil:
		writeJSONBody(w, ok, st)
	case errors.Is(err, update.ErrTooSoon):
		w.Header().Set("Retry-After", strconv.Itoa(30))
		writeJSONError(w, http.StatusTooManyRequests, err.Error())
	case errors.Is(err, update.ErrChecksOff), errors.Is(err, update.ErrBusy),
		errors.Is(err, update.ErrNothingToFetch), errors.Is(err, update.ErrNotReleaseBuild),
		errors.Is(err, update.ErrApplyDisabled), errors.Is(err, update.ErrNothingToApply),
		errors.Is(err, update.ErrNotVerified), errors.Is(err, update.ErrApplying):
		writeJSONError(w, http.StatusConflict, err.Error())
	case errors.Is(err, update.ErrInvalidSettings):
		writeJSONError(w, http.StatusBadRequest, err.Error())
	case update.IsUpstream(err):
		writeJSONError(w, http.StatusBadGateway, err.Error())
	default:
		writeJSONError(w, http.StatusInternalServerError, "internal error")
	}
}
