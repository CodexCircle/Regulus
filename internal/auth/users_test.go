package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"
)

const otherPassword = "another long password"

// addUser creates an account through the service, as the first admin would.
func addUser(t *testing.T, s *Service, by User, name string, role Role) User {
	t.Helper()
	u, err := s.CreateUser(context.Background(), by, name, otherPassword, role)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	return u
}

func ptr[T any](v T) *T { return &v }

func TestCreateUser(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	admin, _ := setup(t, s)

	u := addUser(t, s, admin, "  Alice ", RoleViewer)
	if u.Username != "alice" || u.Role != RoleViewer || u.Disabled {
		t.Fatalf("created user = %+v", u)
	}
	if _, _, _, err := s.Login(ctx, "1.1.1.1", "alice", otherPassword); err != nil {
		t.Fatalf("the new user cannot log in: %v", err)
	}

	if _, err := s.CreateUser(ctx, admin, "ALICE", otherPassword, RoleViewer); !errors.Is(err, ErrUserExists) {
		t.Errorf("duplicate name: err = %v, want ErrUserExists", err)
	}
	for name, tc := range map[string]struct {
		user, pass string
		role       Role
	}{
		"short name":     {"ab", otherPassword, RoleViewer},
		"short password": {"bob", "short", RoleViewer},
		"bad role":       {"bob", otherPassword, "root"},
		"empty role":     {"bob", otherPassword, ""},
	} {
		if _, err := s.CreateUser(ctx, admin, tc.user, tc.pass, tc.role); !isInputError(err) {
			t.Errorf("%s: err = %v, want an input error", name, err)
		}
	}

	users, err := s.Users(ctx)
	if err != nil || len(users) != 2 || users[0].Username != "admin" || users[1].Username != "alice" {
		t.Fatalf("Users = %+v, %v", users, err)
	}
}

func TestTheLastAdminIsProtected(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	admin, _ := setup(t, s)
	viewer := addUser(t, s, admin, "viewer1", RoleViewer)

	viewerRole := RoleViewer
	if _, err := s.UpdateUser(ctx, admin, "admin", UserChange{Role: &viewerRole}); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("demote the only admin: err = %v, want ErrLastAdmin", err)
	}
	if _, err := s.UpdateUser(ctx, admin, "admin", UserChange{Disabled: ptr(true)}); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("disable the only admin: err = %v, want ErrLastAdmin", err)
	}
	if err := s.DeleteUser(ctx, admin, "admin"); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("delete the only admin: err = %v, want ErrLastAdmin", err)
	}

	// A viewer can be promoted, after which the first admin can step down.
	adminRole := RoleAdmin
	if _, err := s.UpdateUser(ctx, admin, viewer.Username, UserChange{Role: &adminRole}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpdateUser(ctx, admin, "admin", UserChange{Role: &viewerRole}); err != nil {
		t.Fatalf("demote with another admin around: %v", err)
	}
	// ... but now the promoted one is the last, even if the demoted one is disabled and deleted.
	if _, err := s.UpdateUser(ctx, admin, viewer.Username, UserChange{Disabled: ptr(true)}); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("disable the new only admin: err = %v, want ErrLastAdmin", err)
	}
	if err := s.DeleteUser(ctx, viewer, "admin"); err != nil {
		t.Fatalf("delete a viewer: %v", err)
	}
	if err := s.DeleteUser(ctx, viewer, viewer.Username); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("delete the last admin: err = %v, want ErrLastAdmin", err)
	}
	// A disabled admin does not count as an admin to keep.
	second := addUser(t, s, viewer, "second", RoleAdmin)
	if _, err := s.UpdateUser(ctx, viewer, second.Username, UserChange{Disabled: ptr(true)}); err != nil {
		t.Fatalf("disable a second admin: %v", err)
	}
	if _, err := s.UpdateUser(ctx, viewer, viewer.Username, UserChange{Role: &viewerRole}); !errors.Is(err, ErrLastAdmin) {
		t.Errorf("demote the only enabled admin: err = %v, want ErrLastAdmin", err)
	}

	if _, err := s.UpdateUser(ctx, viewer, "ghost", UserChange{Disabled: ptr(true)}); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("unknown user: err = %v, want ErrUserNotFound", err)
	}
	if err := s.DeleteUser(ctx, viewer, "ghost"); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("delete unknown user: err = %v, want ErrUserNotFound", err)
	}
}

func TestDisablingEndsSessionsAndBlocksLogin(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	admin, _ := setup(t, s)
	addUser(t, s, admin, "carol", RoleViewer)
	_, token, _, err := s.Login(ctx, "1.1.1.1", "carol", otherPassword)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := s.UpdateUser(ctx, admin, "carol", UserChange{Disabled: ptr(true)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("session of a disabled user: err = %v, want ErrUnauthenticated", err)
	}
	if _, _, _, err := s.Login(ctx, "1.1.1.1", "carol", otherPassword); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("login of a disabled user: err = %v, want ErrInvalidCredentials (no hint)", err)
	}

	if _, err := s.UpdateUser(ctx, admin, "carol", UserChange{Disabled: ptr(false)}); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.Login(ctx, "1.1.1.1", "carol", otherPassword); err != nil {
		t.Errorf("login after enabling: %v", err)
	}
}

func TestDeletingAUserEndsTheirSessions(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	admin, _ := setup(t, s)
	addUser(t, s, admin, "dave", RoleViewer)
	_, token, _, _ := s.Login(ctx, "1.1.1.1", "dave", otherPassword)
	if err := s.DeleteUser(ctx, admin, "dave"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("session of a deleted user: err = %v", err)
	}
	// The name can be used again, by a different account.
	addUser(t, s, admin, "dave", RoleViewer)
}

func TestChangePassword(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	admin, current := setup(t, s)
	_, other, _, err := s.Login(ctx, "1.1.1.1", "admin", goodPassword)
	if err != nil {
		t.Fatal(err)
	}

	if err := s.ChangePassword(ctx, admin, current, "not the password", otherPassword); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("wrong current password: err = %v", err)
	}
	if err := s.ChangePassword(ctx, admin, current, goodPassword, "short"); !isInputError(err) {
		t.Fatalf("short new password: err = %v", err)
	}
	if err := s.ChangePassword(ctx, admin, current, goodPassword, otherPassword); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Authenticate(ctx, current); err != nil {
		t.Errorf("the session that changed the password must stay: %v", err)
	}
	if _, err := s.Authenticate(ctx, other); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("other sessions must end: err = %v", err)
	}
	if _, _, _, err := s.Login(ctx, "1.1.1.1", "admin", goodPassword); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("the old password must stop working: err = %v", err)
	}
	if _, _, _, err := s.Login(ctx, "1.1.1.1", "admin", otherPassword); err != nil {
		t.Errorf("the new password must work: %v", err)
	}
}

func TestChangePasswordIsRateLimited(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	admin, token := setup(t, s)
	for range maxFailures {
		if err := s.ChangePassword(ctx, admin, token, "wrong password!", otherPassword); !errors.Is(err, ErrWrongPassword) {
			t.Fatalf("err = %v", err)
		}
	}
	if err := s.ChangePassword(ctx, admin, token, goodPassword, otherPassword); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("after too many failures: err = %v, want ErrRateLimited", err)
	}
}

func TestSessionsCanBeListedAndRevoked(t *testing.T) {
	s := newService(t)
	ctx := WithClient(context.Background(), Client{Remote: "203.0.113.5", UserAgent: "TestBrowser/1.0"})
	admin, first := setup(t, s)
	_, second, _, err := s.Login(ctx, "203.0.113.5", "admin", goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	addUser(t, s, admin, "erin", RoleViewer)
	erin, erinToken, _, _ := s.Login(ctx, "203.0.113.5", "erin", otherPassword)

	list, err := s.Sessions(ctx, admin, second)
	if err != nil || len(list) != 2 {
		t.Fatalf("Sessions = %+v, %v", list, err)
	}
	var current, old SessionInfo
	for _, si := range list {
		if si.Current {
			current = si
		} else {
			old = si
		}
	}
	if current.ID == "" || old.ID == "" || current.ID == old.ID {
		t.Fatalf("expected one current and one other session: %+v", list)
	}
	if current.RemoteAddr != "203.0.113.5" || current.UserAgent != "TestBrowser/1.0" {
		t.Errorf("the client was not recorded: %+v", current)
	}
	if current.ID == hashToken(second) || len(current.ID) >= len(hashToken(second)) {
		t.Errorf("the id must not be the whole token hash")
	}

	// Another user's session cannot be revoked with a valid id of the first.
	if err := s.RevokeSession(ctx, erin, old.ID); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("revoking someone else's session: err = %v", err)
	}
	if _, err := s.Authenticate(ctx, first); err != nil {
		t.Errorf("the session must still exist: %v", err)
	}
	if err := s.RevokeSession(ctx, admin, "short"); !errors.Is(err, ErrSessionNotFound) {
		t.Errorf("bad id: err = %v", err)
	}
	if err := s.RevokeSession(ctx, admin, old.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, first); !errors.Is(err, ErrUnauthenticated) {
		t.Errorf("revoked session still works: err = %v", err)
	}
	if _, err := s.Authenticate(ctx, erinToken); err != nil {
		t.Errorf("the other user's session must stay: %v", err)
	}
}

func TestExpiredSessionsAreNotListed(t *testing.T) {
	s := newService(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	admin, token := setup(t, s)
	now = now.Add(SessionTTL + time.Minute)
	list, err := s.Sessions(context.Background(), admin, token)
	if err != nil || len(list) != 0 {
		t.Fatalf("Sessions = %+v, %v", list, err)
	}
}

func TestAuditLog(t *testing.T) {
	s := newService(t)
	ctx := WithClient(context.Background(), Client{Remote: "198.51.100.7"})
	admin, token := setup(t, s)
	_, _, _, _ = s.Login(ctx, "198.51.100.7", "Admin", "wrong password!")
	_, _, _, _ = s.Login(ctx, "198.51.100.7", "admin", goodPassword)
	addUser(t, s, admin, "frank", RoleViewer)
	_, _ = s.UpdateUser(ctx, admin, "frank", UserChange{Role: ptr(RoleAdmin)})
	_, _ = s.UpdateUser(ctx, admin, "frank", UserChange{Disabled: ptr(true)})
	_ = s.ChangePassword(ctx, admin, token, goodPassword, otherPassword)
	_ = s.DeleteUser(ctx, admin, "frank")

	entries, err := s.Audit(ctx, 100)
	if err != nil {
		t.Fatal(err)
	}
	var events []string
	for _, e := range entries { // newest first
		events = append([]string{e.Event}, events...)
	}
	want := []string{EventSetup, EventLoginFailed, EventLogin, EventUserCreated, EventRoleChanged, EventUserDisabled, EventPasswordChange, EventUserDeleted}
	if len(events) != len(want) {
		t.Fatalf("events = %v, want %v", events, want)
	}
	for i := range want {
		if events[i] != want[i] {
			t.Fatalf("events = %v, want %v", events, want)
		}
	}
	failed := entries[len(entries)-2]
	if failed.Actor != "admin" || failed.Remote != "198.51.100.7" {
		t.Errorf("failed login entry = %+v", failed)
	}
	if got, _ := s.Audit(ctx, 3); len(got) != 3 || got[0].Event != EventUserDeleted {
		t.Errorf("limit not applied or not newest first: %+v", got)
	}
	// The audit log must never hold a password.
	raw, _ := json.Marshal(entries)
	for _, secret := range []string{goodPassword, otherPassword, "wrong password!"} {
		if contains(string(raw), secret) {
			t.Errorf("the audit log contains a password: %s", raw)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// --- HTTP ---

func loginAs(t *testing.T, c *client, user, pass string) {
	t.Helper()
	c.cookie = nil
	if rec := c.do("POST", "/api/auth/login", map[string]string{"username": user, "password": pass}); rec.Code != http.StatusOK {
		t.Fatalf("login as %s: status = %d, body = %s", user, rec.Code, rec.Body)
	}
}

func TestHTTPRolesAreEnforced(t *testing.T) {
	s := newService(t)
	c := newClient(t, s)
	admin, _ := setup(t, s)
	addUser(t, s, admin, "viewer1", RoleViewer)

	loginAs(t, c, "viewer1", otherPassword)
	for _, tc := range []struct {
		method, path string
		body         any
	}{
		{"GET", "/api/users", nil},
		{"POST", "/api/users", map[string]string{"username": "x1x", "password": otherPassword, "role": "viewer"}},
		{"PATCH", "/api/users/viewer1", map[string]any{"role": "admin"}},
		{"DELETE", "/api/users/admin", struct{}{}},
		{"GET", "/api/audit", nil},
	} {
		if rec := c.do(tc.method, tc.path, tc.body); rec.Code != http.StatusForbidden {
			t.Errorf("viewer %s %s: status = %d, want 403", tc.method, tc.path, rec.Code)
		}
	}
	// A viewer still manages their own account.
	if rec := c.do("GET", "/api/account/sessions", nil); rec.Code != http.StatusOK {
		t.Errorf("viewer lists own sessions: status = %d", rec.Code)
	}

	// Nothing without a login.
	c.cookie = nil
	if rec := c.do("GET", "/api/users", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("no login: status = %d, want 401", rec.Code)
	}
}

func TestHTTPUserManagement(t *testing.T) {
	s := newService(t)
	c := newClient(t, s)
	setup(t, s)
	loginAs(t, c, "admin", goodPassword)

	rec := c.do("POST", "/api/users", map[string]string{"username": "Gina", "password": otherPassword, "role": "viewer"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, body = %s", rec.Code, rec.Body)
	}
	if rec := c.do("POST", "/api/users", map[string]string{"username": "gina", "password": otherPassword, "role": "viewer"}); rec.Code != http.StatusConflict {
		t.Errorf("duplicate: status = %d, want 409", rec.Code)
	}
	if rec := c.do("POST", "/api/users", map[string]string{"username": "hal", "password": "x", "role": "viewer"}); rec.Code != http.StatusBadRequest {
		t.Errorf("bad password: status = %d, want 400", rec.Code)
	}
	if rec := c.do("POST", "/api/users", map[string]any{"username": "hal", "password": otherPassword, "role": "viewer", "extra": 1}); rec.Code != http.StatusBadRequest {
		t.Errorf("unknown field: status = %d, want 400", rec.Code)
	}

	rec = c.do("GET", "/api/users", nil)
	var users []User
	if err := json.Unmarshal(rec.Body.Bytes(), &users); err != nil || len(users) != 2 {
		t.Fatalf("list: %s (%v)", rec.Body, err)
	}
	if contains(rec.Body.String(), "hash") || contains(rec.Body.String(), "password") {
		t.Errorf("the list must not expose secrets: %s", rec.Body)
	}

	rec = c.do("PATCH", "/api/users/gina", map[string]any{"disabled": true})
	var updated User
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil || rec.Code != http.StatusOK || !updated.Disabled {
		t.Fatalf("disable: status = %d, body = %s", rec.Code, rec.Body)
	}
	if rec := c.do("PATCH", "/api/users/admin", map[string]any{"role": "viewer"}); rec.Code != http.StatusConflict {
		t.Errorf("demote last admin: status = %d, want 409", rec.Code)
	}
	if rec := c.do("PATCH", "/api/users/ghost", map[string]any{"disabled": true}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown user: status = %d, want 404", rec.Code)
	}
	if rec := c.do("PATCH", "/api/users/gina", map[string]any{"role": "root"}); rec.Code != http.StatusBadRequest {
		t.Errorf("bad role: status = %d, want 400", rec.Code)
	}

	// A DELETE is refused without the JSON content type, like any change.
	if rec := c.do("DELETE", "/api/users/gina", nil); rec.Code != http.StatusUnsupportedMediaType {
		t.Errorf("delete without JSON: status = %d, want 415", rec.Code)
	}
	if rec := c.do("DELETE", "/api/users/gina", struct{}{}); rec.Code != http.StatusNoContent {
		t.Errorf("delete: status = %d, want 204", rec.Code)
	}
	if rec := c.do("DELETE", "/api/users/admin", struct{}{}); rec.Code != http.StatusConflict {
		t.Errorf("delete last admin: status = %d, want 409", rec.Code)
	}

	rec = c.do("GET", "/api/audit?limit=2", nil)
	var entries []AuditEntry
	if err := json.Unmarshal(rec.Body.Bytes(), &entries); err != nil || len(entries) != 2 || entries[0].Event != EventUserDeleted {
		t.Fatalf("audit: %s (%v)", rec.Body, err)
	}
	if rec := c.do("GET", "/api/audit?limit=x", nil); rec.Code != http.StatusBadRequest {
		t.Errorf("bad limit: status = %d, want 400", rec.Code)
	}
}

func TestHTTPOwnAccount(t *testing.T) {
	s := newService(t)
	c := newClient(t, s)
	setup(t, s)
	loginAs(t, c, "admin", goodPassword)

	if rec := c.do("POST", "/api/account/password", map[string]string{"current_password": "wrong password!", "new_password": otherPassword}); rec.Code != http.StatusForbidden {
		t.Errorf("wrong current password: status = %d, want 403", rec.Code)
	}
	if rec := c.do("POST", "/api/account/password", map[string]string{"current_password": goodPassword, "new_password": "short"}); rec.Code != http.StatusBadRequest {
		t.Errorf("short password: status = %d, want 400", rec.Code)
	}
	if rec := c.do("POST", "/api/account/password", map[string]string{"current_password": goodPassword, "new_password": otherPassword}); rec.Code != http.StatusNoContent {
		t.Fatalf("change password: status = %d", rec.Code)
	}
	if rec := c.do("GET", "/api/secret", nil); rec.Code != http.StatusOK {
		t.Errorf("the session must survive the password change: status = %d", rec.Code)
	}

	rec := c.do("GET", "/api/account/sessions", nil)
	var list []SessionInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list) != 1 || !list[0].Current {
		t.Fatalf("sessions: %s (%v)", rec.Body, err)
	}
	if rec := c.do("DELETE", "/api/account/sessions/0000000000000000", struct{}{}); rec.Code != http.StatusNotFound {
		t.Errorf("unknown session: status = %d, want 404", rec.Code)
	}
	if rec := c.do("DELETE", "/api/account/sessions/"+list[0].ID, struct{}{}); rec.Code != http.StatusNoContent {
		t.Fatalf("revoke own session: status = %d", rec.Code)
	}
	if rec := c.do("GET", "/api/secret", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("after revoking the current session: status = %d, want 401", rec.Code)
	}
}
