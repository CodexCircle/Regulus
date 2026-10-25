package auth

import (
	"errors"
	"net/http"
	"strconv"
)

// Route is an API endpoint that needs a login.
type Route struct {
	Pattern string
	Handler http.Handler
}

// RequireRole lets a request through only if the logged-in user has the role,
// otherwise it answers 403. Use it inside Service.Require.
func RequireRole(role Role, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if u, ok := UserFrom(r.Context()); !ok || u.Role != role {
			writeError(w, http.StatusForbidden, ErrForbidden.Error())
			return
		}
		next.ServeHTTP(w, r)
	})
}

// APIRoutes are the account endpoints. They all need a login (wrap them with
// Require); the ones about other users and the audit log need an admin, and
// the ones under /api/account are for the logged-in user's own account.
//
//	GET    /api/users                  list the accounts (admin)
//	POST   /api/users                  create an account (admin)
//	PATCH  /api/users/{username}       change role or enable/disable (admin)
//	DELETE /api/users/{username}       delete an account (admin)
//	POST   /api/users/{username}/reset make a one-time password reset token (admin)
//	GET    /api/audit                  recent account events (admin)
//	POST   /api/account/password       change the own password
//	GET    /api/account/sessions       list the own sessions
//	DELETE /api/account/sessions/{id}  end one of the own sessions
//
// The two-factor endpoints are listed at twoFactorRoutes.
//
// The Service may be nil to list the routes only.
func APIRoutes(s *Service) []Route {
	admin := func(h http.HandlerFunc) http.Handler { return RequireRole(RoleAdmin, h) }
	adminJSON := func(h http.HandlerFunc) http.Handler { return RequireRole(RoleAdmin, RequireJSON(h)) }
	routes := []Route{
		{"GET /api/users", admin(s.handleListUsers)},
		{"POST /api/users", adminJSON(s.handleCreateUser)},
		{"PATCH /api/users/{username}", adminJSON(s.handleUpdateUser)},
		{"DELETE /api/users/{username}", adminJSON(s.handleDeleteUser)},
		{"POST /api/users/{username}/reset", adminJSON(s.handleCreateReset)},
		{"GET /api/audit", admin(s.handleAudit)},
		{"POST /api/account/password", RequireJSON(http.HandlerFunc(s.handleChangePassword))},
		{"GET /api/account/sessions", http.HandlerFunc(s.handleListSessions)},
		{"DELETE /api/account/sessions/{id}", RequireJSON(http.HandlerFunc(s.handleRevokeSession))},
	}
	return append(routes, twoFactorRoutes(s)...)
}

func (s *Service) fail(w http.ResponseWriter, err error) {
	if s.failTwoFactor(w, err) {
		return
	}
	switch {
	case isInputError(err):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrUserNotFound), errors.Is(err, ErrSessionNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, ErrUserExists), errors.Is(err, ErrLastAdmin):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrInvalidReset):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, ErrWrongPassword), errors.Is(err, ErrForbidden):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrRateLimited):
		w.Header().Set("Retry-After", "900")
		writeError(w, http.StatusTooManyRequests, err.Error())
	default:
		s.log.Error("request failed", "err", err)
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func (s *Service) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.Users(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, users)
}

func (s *Service) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     Role   `json:"role"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	by, _ := UserFrom(r.Context())
	u, err := s.CreateUser(r.Context(), by, req.Username, req.Password, req.Role)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, u)
}

func (s *Service) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Role     *Role `json:"role"`
		Disabled *bool `json:"disabled"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	by, _ := UserFrom(r.Context())
	u, err := s.UpdateUser(r.Context(), by, r.PathValue("username"), UserChange{Role: req.Role, Disabled: req.Disabled})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Service) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	var req struct{}
	if !readJSON(w, r, &req) {
		return
	}
	by, _ := UserFrom(r.Context())
	if err := s.DeleteUser(r.Context(), by, r.PathValue("username")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleCreateReset(w http.ResponseWriter, r *http.Request) {
	var req struct{}
	if !readJSON(w, r, &req) {
		return
	}
	by, _ := UserFrom(r.Context())
	username := r.PathValue("username")
	token, expires, err := s.CreateReset(r.Context(), by.Username, username)
	if err != nil {
		s.fail(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusCreated, map[string]any{
		"username":   normalizeUsername(username),
		"token":      token,
		"expires_at": expires,
	})
}

func (s *Service) handleResetPassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Token    string `json:"token"`
		Password string `json:"new_password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	if err := s.ResetPassword(withRequestClient(r), remoteIP(r), req.Token, req.Password); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleAudit(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeError(w, http.StatusBadRequest, "limit must be a positive number")
			return
		}
		limit = n
	}
	entries, err := s.Audit(r.Context(), limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, entries)
}

func sessionToken(r *http.Request) string {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

func (s *Service) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Current string `json:"current_password"`
		New     string `json:"new_password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user, _ := UserFrom(r.Context())
	if err := s.ChangePassword(r.Context(), user, sessionToken(r), req.Current, req.New); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleListSessions(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	list, err := s.Sessions(r.Context(), user, sessionToken(r))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Service) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	var req struct{}
	if !readJSON(w, r, &req) {
		return
	}
	user, _ := UserFrom(r.Context())
	if err := s.RevokeSession(r.Context(), user, r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
