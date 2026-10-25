package auth

import (
	"errors"
	"net/http"
)

// twoFactorPath is the prefix of the endpoints a user who must still set up
// two-factor authentication may use.
const twoFactorPath = "/api/account/two-factor"

// twoFactorRoutes are the endpoints about two-factor authentication:
//
//	GET    /api/account/two-factor                 the own state
//	POST   /api/account/two-factor/setup           start the setup (password): secret and QR code
//	POST   /api/account/two-factor/enable          confirm with a code: turns it on, returns the recovery codes
//	POST   /api/account/two-factor/disable         turn it off (password and code)
//	POST   /api/account/two-factor/recovery-codes  make new recovery codes (password and code)
//	DELETE /api/users/{username}/two-factor        remove the second factor of a user (admin)
//	GET    /api/settings/security                  who must use two-factor (admin)
//	PUT    /api/settings/security                  change that (admin)
func twoFactorRoutes(s *Service) []Route {
	admin := func(h http.HandlerFunc) http.Handler { return RequireRole(RoleAdmin, h) }
	adminJSON := func(h http.HandlerFunc) http.Handler { return RequireRole(RoleAdmin, RequireJSON(h)) }
	return []Route{
		{"GET " + twoFactorPath, http.HandlerFunc(s.handleTwoFactorStatus)},
		{"POST " + twoFactorPath + "/setup", RequireJSON(http.HandlerFunc(s.handleTwoFactorSetup))},
		{"POST " + twoFactorPath + "/enable", RequireJSON(http.HandlerFunc(s.handleTwoFactorEnable))},
		{"POST " + twoFactorPath + "/disable", RequireJSON(http.HandlerFunc(s.handleTwoFactorDisable))},
		{"POST " + twoFactorPath + "/recovery-codes", RequireJSON(http.HandlerFunc(s.handleRecoveryCodes))},
		{"DELETE /api/users/{username}/two-factor", adminJSON(s.handleResetTwoFactor)},
		{"GET /api/settings/security", admin(s.handleGetSecurity)},
		{"PUT /api/settings/security", adminJSON(s.handlePutSecurity)},
	}
}

func (s *Service) handleTwoFactorStatus(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFrom(r.Context())
	st, err := s.TwoFactorStatus(r.Context(), user)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func (s *Service) handleTwoFactorSetup(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user, _ := UserFrom(r.Context())
	setup, err := s.BeginTwoFactor(r.Context(), user, req.Password)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, setup)
}

func (s *Service) handleTwoFactorEnable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Code string `json:"code"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user, _ := UserFrom(r.Context())
	codes, err := s.EnableTwoFactor(r.Context(), user, req.Code)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

func (s *Service) handleTwoFactorDisable(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user, _ := UserFrom(r.Context())
	if err := s.DisableTwoFactor(r.Context(), user, req.Password, req.Code); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	user, _ := UserFrom(r.Context())
	codes, err := s.RenewRecoveryCodes(r.Context(), user, req.Password, req.Code)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"recovery_codes": codes})
}

func (s *Service) handleResetTwoFactor(w http.ResponseWriter, r *http.Request) {
	var req struct{}
	if !readJSON(w, r, &req) {
		return
	}
	by, _ := UserFrom(r.Context())
	if err := s.ResetTwoFactor(r.Context(), by, r.PathValue("username")); err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleGetSecurity(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"two_factor_policy":    s.Policy(),
		"two_factor_available": s.box != nil,
	})
}

func (s *Service) handlePutSecurity(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Policy Policy `json:"two_factor_policy"`
	}
	if !readJSON(w, r, &req) {
		return
	}
	by, _ := UserFrom(r.Context())
	if req.Policy != PolicyOff && s.box == nil {
		s.fail(w, ErrTwoFactorUnavailable)
		return
	}
	if err := s.SetPolicy(r.Context(), by, req.Policy); err != nil {
		s.fail(w, err)
		return
	}
	s.handleGetSecurity(w, r)
}

// failTwoFactor maps the errors of this file; it reports whether it did.
func (s *Service) failTwoFactor(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, ErrInvalidCode):
		writeError(w, http.StatusForbidden, err.Error())
	case errors.Is(err, ErrTwoFactorOn), errors.Is(err, ErrTwoFactorOff), errors.Is(err, ErrTwoFactorNotStarted), errors.Is(err, ErrTwoFactorMandatory):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, ErrTwoFactorUnavailable):
		writeError(w, http.StatusServiceUnavailable, err.Error())
	default:
		return false
	}
	return true
}
