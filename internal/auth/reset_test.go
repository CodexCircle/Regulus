package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"
)

const resetPassword = "a brand new password"

func TestAdminResetsAUser(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	admin, _ := setup(t, s)
	addUser(t, s, admin, "dana", RoleViewer)
	_, session, _, err := s.Login(ctx, "1.1.1.1", "dana", otherPassword)
	if err != nil {
		t.Fatal(err)
	}

	token, expires, err := s.CreateReset(ctx, admin.Username, "Dana")
	if err != nil {
		t.Fatal(err)
	}
	if want := s.now().Add(ResetTTL); expires.Before(want.Add(-time.Minute)) || expires.After(want.Add(time.Minute)) {
		t.Errorf("expires = %v, want about %v", expires, want)
	}
	if _, err := s.Authenticate(ctx, session); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("creating a reset must end the sessions: err = %v", err)
	}
	// Until the link is used the old password still works.
	if _, _, _, err := s.Login(ctx, "1.1.1.1", "dana", otherPassword); err != nil {
		t.Errorf("old password before the reset: %v", err)
	}

	_, again, _, _ := s.Login(ctx, "1.1.1.1", "dana", otherPassword)
	if err := s.ResetPassword(ctx, "2.2.2.2", token, resetPassword); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, err := s.Authenticate(ctx, again); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("using the link must end the sessions: err = %v", err)
	}
	if _, _, _, err := s.Login(ctx, "1.1.1.1", "dana", otherPassword); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("old password after the reset: err = %v", err)
	}
	if _, _, _, err := s.Login(ctx, "1.1.1.1", "dana", resetPassword); err != nil {
		t.Errorf("new password: %v", err)
	}

	// Single use.
	if err := s.ResetPassword(ctx, "2.2.2.2", token, "yet another password"); !errors.Is(err, ErrInvalidReset) {
		t.Errorf("second use: err = %v, want ErrInvalidReset", err)
	}
}

func TestResetTokenRules(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	admin, _ := setup(t, s)
	addUser(t, s, admin, "dana", RoleViewer)
	addUser(t, s, admin, "erin", RoleViewer)

	// A new token replaces the earlier one of the same account.
	first, _, _ := s.CreateReset(ctx, admin.Username, "dana")
	second, _, _ := s.CreateReset(ctx, admin.Username, "dana")
	if err := s.ResetPassword(ctx, "r", first, resetPassword); !errors.Is(err, ErrInvalidReset) {
		t.Errorf("replaced token: err = %v, want ErrInvalidReset", err)
	}

	// A bad password does not use the token up.
	if err := s.ResetPassword(ctx, "r", second, "short"); !isInputError(err) {
		t.Errorf("short password: err = %v, want an input error", err)
	}

	// The token is only stored hashed.
	var n int
	if err := s.store.db.QueryRow(`SELECT COUNT(*) FROM password_resets WHERE token_hash = ?`, second).Scan(&n); err != nil || n != 0 {
		t.Errorf("the raw token is stored: n = %d, err = %v", n, err)
	}

	// Expired.
	now := s.now()
	s.now = func() time.Time { return now.Add(ResetTTL + time.Minute) }
	if err := s.ResetPassword(ctx, "r", second, resetPassword); !errors.Is(err, ErrInvalidReset) {
		t.Errorf("expired token: err = %v, want ErrInvalidReset", err)
	}
	s.now = func() time.Time { return now }
	if err := s.ResetPassword(ctx, "r", second, resetPassword); err != nil {
		t.Errorf("valid token after the failed ones: %v", err)
	}

	// Unknown accounts and disabled accounts.
	if _, _, err := s.CreateReset(ctx, admin.Username, "nobody"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("unknown account: err = %v, want ErrUserNotFound", err)
	}
	if _, err := s.UpdateUser(ctx, admin, "erin", UserChange{Disabled: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.CreateReset(ctx, admin.Username, "erin"); !isInputError(err) {
		t.Errorf("disabled account: err = %v, want an input error", err)
	}
}

func TestDeletingAnAccountDropsItsReset(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	admin, _ := setup(t, s)
	addUser(t, s, admin, "dana", RoleViewer)
	token, _, _ := s.CreateReset(ctx, admin.Username, "dana")
	if err := s.DeleteUser(ctx, admin, "dana"); err != nil {
		t.Fatal(err)
	}
	if err := s.ResetPassword(ctx, "r", token, resetPassword); !errors.Is(err, ErrInvalidReset) {
		t.Errorf("token of a deleted account: err = %v, want ErrInvalidReset", err)
	}
}

func TestGuessingResetTokensIsRateLimited(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	admin, _ := setup(t, s)
	addUser(t, s, admin, "dana", RoleViewer)
	token, _, _ := s.CreateReset(ctx, admin.Username, "dana")

	for range maxFailures {
		if err := s.ResetPassword(ctx, "9.9.9.9", "guess", resetPassword); !errors.Is(err, ErrInvalidReset) {
			t.Fatalf("guess: err = %v, want ErrInvalidReset", err)
		}
	}
	if err := s.ResetPassword(ctx, "9.9.9.9", token, resetPassword); !errors.Is(err, ErrRateLimited) {
		t.Errorf("after too many guesses even the right token waits: err = %v, want ErrRateLimited", err)
	}
	if err := s.ResetPassword(ctx, "8.8.8.8", token, resetPassword); err != nil {
		t.Errorf("another client: %v", err)
	}
}

func TestResetsAreAudited(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	admin, _ := setup(t, s)
	addUser(t, s, admin, "dana", RoleViewer)
	token, _, _ := s.CreateReset(ctx, admin.Username, "dana")
	_ = s.ResetPassword(ctx, "r", token, resetPassword)

	entries, _ := s.Audit(ctx, 3)
	if len(entries) != 3 || entries[0].Event != EventPasswordReset || entries[0].Actor != "dana" ||
		entries[1].Event != EventResetCreated || entries[1].Actor != "admin" || entries[1].Target != "dana" {
		t.Fatalf("audit = %+v", entries)
	}
	raw, _ := json.Marshal(entries)
	for _, secret := range []string{token, resetPassword} {
		if strings.Contains(string(raw), secret) {
			t.Errorf("the audit log holds a secret: %s", raw)
		}
	}
}

func TestHTTPReset(t *testing.T) {
	s := newService(t)
	c := newClient(t, s)
	admin, _ := setup(t, s)
	addUser(t, s, admin, "dana", RoleViewer)

	// Only admins create resets.
	loginAs(t, c, "dana", otherPassword)
	if rec := c.do("POST", "/api/users/dana/reset", struct{}{}); rec.Code != http.StatusForbidden {
		t.Errorf("viewer creating a reset: status = %d, want 403", rec.Code)
	}
	loginAs(t, c, "admin", goodPassword)
	if rec := c.do("POST", "/api/users/nobody/reset", struct{}{}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown user: status = %d, want 404", rec.Code)
	}
	rec := c.do("POST", "/api/users/dana/reset", struct{}{})
	if rec.Code != http.StatusCreated || rec.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("create: status = %d, headers = %v", rec.Code, rec.Header())
	}
	var got struct {
		Username  string    `json:"username"`
		Token     string    `json:"token"`
		ExpiresAt time.Time `json:"expires_at"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Username != "dana" || got.Token == "" || got.ExpiresAt.IsZero() {
		t.Fatalf("body = %s (%v)", rec.Body, err)
	}

	// The reset endpoint is public: no login, JSON only.
	c.cookie = nil
	if rec := c.do("POST", "/api/auth/reset", map[string]string{"token": "nope", "new_password": resetPassword}); rec.Code != http.StatusBadRequest {
		t.Errorf("bad token: status = %d, want 400", rec.Code)
	}
	if rec := c.do("POST", "/api/auth/reset", map[string]string{"token": got.Token, "new_password": "short"}); rec.Code != http.StatusBadRequest {
		t.Errorf("short password: status = %d, want 400", rec.Code)
	}
	if rec := c.do("POST", "/api/auth/reset", map[string]string{"token": got.Token, "new_password": resetPassword}); rec.Code != http.StatusNoContent {
		t.Fatalf("reset: status = %d, body = %s", rec.Code, rec.Body)
	}
	if rec := c.do("POST", "/api/auth/reset", map[string]string{"token": got.Token, "new_password": resetPassword}); rec.Code != http.StatusBadRequest {
		t.Errorf("reuse: status = %d, want 400", rec.Code)
	}
	loginAs(t, c, "dana", resetPassword)

	for range maxFailures {
		c.do("POST", "/api/auth/reset", map[string]string{"token": "guess", "new_password": resetPassword})
	}
	if rec := c.do("POST", "/api/auth/reset", map[string]string{"token": "guess", "new_password": resetPassword}); rec.Code != http.StatusTooManyRequests {
		t.Errorf("guessing: status = %d, want 429", rec.Code)
	}
}

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestRecoverAdmin(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	admin, session := setup(t, s)
	db := s.store.db

	rec, err := RecoverAdmin(ctx, db, "", quietLog())
	if err != nil {
		t.Fatal(err)
	}
	if rec.Username != "admin" || rec.Token == "" || rec.Reenabled {
		t.Fatalf("recovery = %+v", rec)
	}
	if _, err := s.Authenticate(ctx, session); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("recovery must end the sessions: err = %v", err)
	}
	if err := s.ResetPassword(ctx, "r", rec.Token, resetPassword); err != nil {
		t.Fatalf("using the recovery token: %v", err)
	}
	if _, _, _, err := s.Login(ctx, "r", "admin", resetPassword); err != nil {
		t.Errorf("login with the new password: %v", err)
	}

	entries, _ := s.Audit(ctx, 3)
	if entries[2].Event != EventResetCreated || entries[2].Actor != ConsoleActor || entries[2].Target != admin.Username {
		t.Errorf("audit = %+v", entries)
	}
}

func TestRecoverAdminChoosesTheAccount(t *testing.T) {
	ctx := context.Background()

	// No accounts: the setup token is the way in.
	empty := newService(t)
	if _, err := RecoverAdmin(ctx, empty.store.db, "", quietLog()); err == nil || !strings.Contains(err.Error(), "setup token") {
		t.Errorf("no accounts: err = %v", err)
	}

	s := newService(t)
	admin, _ := setup(t, s)
	addUser(t, s, admin, "viola", RoleViewer)
	addUser(t, s, admin, "boss2", RoleAdmin)
	db := s.store.db

	if _, err := RecoverAdmin(ctx, db, "", quietLog()); err == nil || !strings.Contains(err.Error(), "admin, boss2") {
		t.Errorf("several admins: err = %v, want a list", err)
	}
	if _, err := RecoverAdmin(ctx, db, "viola", quietLog()); err == nil || !strings.Contains(err.Error(), "not an admin") {
		t.Errorf("a viewer: err = %v", err)
	}
	if _, err := RecoverAdmin(ctx, db, "ghost", quietLog()); err == nil {
		t.Error("an unknown account must fail")
	}

	// A disabled admin is enabled again.
	if _, err := s.UpdateUser(ctx, admin, "boss2", UserChange{Disabled: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	rec, err := RecoverAdmin(ctx, db, "Boss2", quietLog())
	if err != nil || !rec.Reenabled || rec.Username != "boss2" {
		t.Fatalf("recovery = %+v, %v", rec, err)
	}
	if err := s.ResetPassword(ctx, "r", rec.Token, resetPassword); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.Login(ctx, "r", "boss2", resetPassword); err != nil {
		t.Errorf("login after recovery: %v", err)
	}
}
