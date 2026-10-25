package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestTOTPMatchesRFC6238(t *testing.T) {
	secret := []byte("12345678901234567890") // the RFC's test key
	for _, tc := range []struct {
		unix int64
		want string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
		{20000000000, "353130"},
	} {
		if got := totpCode(secret, totpStepOf(tc.unix)); got != tc.want {
			t.Errorf("code at %d = %s, want %s", tc.unix, got, tc.want)
		}
	}
}

func TestTOTPWindow(t *testing.T) {
	secret := []byte("12345678901234567890")
	const now = 1_000_000_000
	step := totpStepOf(now)
	for name, tc := range map[string]struct {
		step int64
		ok   bool
	}{
		"the current step":  {step, true},
		"the previous step": {step - 1, true},
		"the next step":     {step + 1, true},
		"two steps back":    {step - 2, false},
		"two steps ahead":   {step + 2, false},
	} {
		got, ok := matchTOTP(secret, totpCode(secret, tc.step), now)
		if ok != tc.ok || (ok && got != tc.step) {
			t.Errorf("%s: matched step %d, ok = %v; want ok = %v", name, got, ok, tc.ok)
		}
	}
	if _, ok := matchTOTP(secret, "000000", now); ok && totpCode(secret, step) != "000000" {
		t.Error("a wrong code matched")
	}
}

func TestTOTPURI(t *testing.T) {
	uri := totpURI([]byte("12345678901234567890"), "alice")
	for _, want := range []string{
		"otpauth://totp/Regulus:alice?",
		"secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ",
		"issuer=Regulus",
		"period=30",
		"digits=6",
	} {
		if !strings.Contains(uri, want) {
			t.Errorf("uri %q lacks %q", uri, want)
		}
	}
}

func TestSecretFileAndSealer(t *testing.T) {
	dir := t.TempDir()
	secret, err := LoadOrCreateSecret(dir)
	if err != nil || len(secret) != secretLen {
		t.Fatalf("create: %d bytes, %v", len(secret), err)
	}
	if fi, err := os.Stat(filepath.Join(dir, secretFile)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("the secret must be private: %v, %v", fi, err)
	}
	again, err := LoadOrCreateSecret(dir)
	if err != nil || string(again) != string(secret) {
		t.Fatalf("reload gave a different secret: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, secretFile), []byte("short"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateSecret(dir); err == nil {
		t.Error("a truncated secret file was accepted")
	}

	box, _ := newSealer(secret)
	sealed, err := box.seal([]byte("totp secret"), []byte("user:1"))
	if err != nil {
		t.Fatal(err)
	}
	if got, err := box.open(sealed, []byte("user:1")); err != nil || string(got) != "totp secret" {
		t.Fatalf("open = %q, %v", got, err)
	}
	if _, err := box.open(sealed, []byte("user:2")); err == nil {
		t.Error("a secret copied to another user decrypted")
	}
	other, _ := newSealer([]byte("a completely different hub secret!!"))
	if _, err := other.open(sealed, []byte("user:1")); err == nil {
		t.Error("a secret decrypted with another key")
	}
}

// enrolled sets up an admin with two-factor on and returns the TOTP secret,
// the recovery codes and the session of the setup.
type enrolled struct {
	user     User
	session  string
	secret   []byte
	recovery []string
}

func (e enrolled) code(s *Service) string { return totpCode(e.secret, totpStepOf(s.now().Unix())) }

func enroll(t *testing.T, s *Service) enrolled {
	t.Helper()
	ctx := context.Background()
	u, session := setup(t, s)
	setupInfo, err := s.BeginTwoFactor(ctx, u, goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := totpEncoding.DecodeString(strings.ReplaceAll(setupInfo.Secret, " ", ""))
	if err != nil {
		t.Fatalf("the shown secret is not base32: %v", err)
	}
	e := enrolled{user: u, session: session, secret: secret}
	e.recovery, err = s.EnableTwoFactor(ctx, u, totpCode(secret, totpStepOf(s.now().Unix())))
	if err != nil {
		t.Fatal(err)
	}
	e.user.TwoFactor = true
	return e
}

func TestEnrollment(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	u, _ := setup(t, s)

	if _, err := s.BeginTwoFactor(ctx, u, "wrong password!"); !errors.Is(err, ErrWrongPassword) {
		t.Fatalf("setup with a wrong password: %v", err)
	}
	if _, err := s.EnableTwoFactor(ctx, u, "123456"); !errors.Is(err, ErrTwoFactorNotStarted) {
		t.Fatalf("enable before setup: %v", err)
	}
	setupInfo, err := s.BeginTwoFactor(ctx, u, goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(setupInfo.URI, "otpauth://totp/Regulus:admin?") || !strings.HasPrefix(setupInfo.QR, "data:image/png;base64,") || len(setupInfo.QR) < 500 {
		t.Fatalf("setup = %+v", setupInfo)
	}
	if st, _ := s.TwoFactorStatus(ctx, u); st.Enabled {
		t.Fatal("two-factor must not be on before it is confirmed")
	}
	if got, _ := s.Users(ctx); got[0].TwoFactor {
		t.Fatal("an unconfirmed enrollment shows as on")
	}

	// Without confirming, a login still needs only the password.
	if _, _, _, err := s.Login(ctx, "r", "admin", goodPassword); err != nil {
		t.Fatalf("login with an unconfirmed enrollment: %v", err)
	}

	if _, err := s.EnableTwoFactor(ctx, u, "000000"); !errors.Is(err, ErrInvalidCode) && totpCode(mustSecret(t, s, u), totpStepOf(s.now().Unix())) != "000000" {
		t.Fatalf("enable with a wrong code: %v", err)
	}
	e := enrollFrom(t, s, u, setupInfo)
	if len(e.recovery) != recoveryCodeCount {
		t.Fatalf("recovery codes = %v", e.recovery)
	}
	seen := map[string]bool{}
	for _, c := range e.recovery {
		if len(c) != recoveryCodeLen+1 || c[recoveryCodeLen/2] != '-' || seen[c] {
			t.Fatalf("bad or repeated recovery code %q", c)
		}
		seen[c] = true
	}
	st, err := s.TwoFactorStatus(ctx, e.user)
	if err != nil || !st.Enabled || st.RecoveryCodesLeft != recoveryCodeCount || st.Mandatory {
		t.Fatalf("status = %+v, %v", st, err)
	}
	if _, err := s.BeginTwoFactor(ctx, e.user, goodPassword); !errors.Is(err, ErrTwoFactorOn) {
		t.Errorf("setup while on: %v", err)
	}
	if _, err := s.EnableTwoFactor(ctx, e.user, e.code(s)); !errors.Is(err, ErrTwoFactorOn) {
		t.Errorf("enable while on: %v", err)
	}

	// The audit log says so, and the secret is not in the clear in the database.
	entries, _ := s.Audit(ctx, 10)
	found := false
	for _, en := range entries {
		found = found || en.Event == EventTwoFactorEnabled
	}
	if !found {
		t.Errorf("no audit entry for enabling: %+v", entries)
	}
	var enc string
	if err := s.store.db.QueryRow(`SELECT secret_enc FROM user_totp`).Scan(&enc); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(enc, strings.ReplaceAll(setupInfo.Secret, " ", "")) {
		t.Error("the TOTP secret is stored in the clear")
	}
}

// mustSecret reads back the TOTP secret of a user.
func mustSecret(t *testing.T, s *Service, u User) []byte {
	t.Helper()
	row, err := s.store.totpOf(context.Background(), u.ID)
	if err != nil {
		t.Fatal(err)
	}
	secret, err := s.secretOf(row, u.ID)
	if err != nil {
		t.Fatal(err)
	}
	return secret
}

func enrollFrom(t *testing.T, s *Service, u User, setupInfo TwoFactorSetup) enrolled {
	t.Helper()
	secret, _ := totpEncoding.DecodeString(strings.ReplaceAll(setupInfo.Secret, " ", ""))
	codes, err := s.EnableTwoFactor(context.Background(), u, totpCode(secret, totpStepOf(s.now().Unix())))
	if err != nil {
		t.Fatal(err)
	}
	u.TwoFactor = true
	return enrolled{user: u, secret: secret, recovery: codes}
}

func TestLoginNeedsTheSecondFactor(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	now := time.Now()
	s.now = func() time.Time { return now }
	e := enroll(t, s)

	_, token, _, err := s.Login(ctx, "r", "admin", goodPassword)
	var need *TwoFactorRequiredError
	if !errors.As(err, &need) || !errors.Is(err, ErrTwoFactorRequired) || token != "" || need.Pending == "" {
		t.Fatalf("login = token %q, err %v; want a pending login", token, err)
	}
	if n := countSessions(t, s); n != 1 { // only the one of the setup
		t.Fatalf("a pending login created a session: %d sessions", n)
	}

	// The pending token is not a session.
	if _, err := s.Authenticate(ctx, need.Pending); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("the pending token worked as a session: %v", err)
	}

	if _, err := s.CompleteLogin(ctx, "r", need.Pending, "000000"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("wrong code: %v", err)
	}
	// A second step later, the code of the app is valid and works.
	now = now.Add(totpPeriod * time.Second)
	res, err := s.CompleteLogin(ctx, "r", need.Pending, e.code(s))
	if err != nil || res.Token == "" || res.User.Username != "admin" || res.RecoveryUsed {
		t.Fatalf("complete = %+v, %v", res, err)
	}
	if _, err := s.Authenticate(ctx, res.Token); err != nil {
		t.Fatalf("the session does not work: %v", err)
	}
	// The pending login is used up.
	if _, err := s.CompleteLogin(ctx, "r", need.Pending, e.code(s)); !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("reusing a pending login: %v", err)
	}
}

func countSessions(t *testing.T, s *Service) int {
	t.Helper()
	var n int
	if err := s.store.db.QueryRow(`SELECT COUNT(*) FROM sessions`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestACodeWorksOnlyOnce(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	now := time.Now()
	s.now = func() time.Time { return now }
	e := enroll(t, s)
	now = now.Add(totpPeriod * time.Second) // the code of the setup is behind us

	login := func() (LoginResult, error) {
		_, _, _, err := s.Login(ctx, "r", "admin", goodPassword)
		var need *TwoFactorRequiredError
		if !errors.As(err, &need) {
			t.Fatalf("login: %v", err)
		}
		return s.CompleteLogin(ctx, "r", need.Pending, e.code(s))
	}
	if _, err := login(); err != nil {
		t.Fatalf("first use: %v", err)
	}
	if _, err := login(); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("the same code twice: %v, want ErrInvalidCode (replay)", err)
	}
	// An older code (one step back) is a replay too, even though it is in the window.
	_, _, _, err := s.Login(ctx, "r", "admin", goodPassword)
	var need *TwoFactorRequiredError
	errors.As(err, &need)
	if _, err := s.CompleteLogin(ctx, "r", need.Pending, totpCode(e.secret, totpStepOf(now.Unix())-1)); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("an earlier step: %v", err)
	}
	// The next step is fine.
	now = now.Add(totpPeriod * time.Second)
	_, _, _, err = s.Login(ctx, "r2", "admin", goodPassword)
	errors.As(err, &need)
	if _, err := s.CompleteLogin(ctx, "r2", need.Pending, e.code(s)); err != nil {
		t.Fatalf("the next step: %v", err)
	}
}

func TestPendingLoginLimits(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	now := time.Now()
	s.now = func() time.Time { return now }
	e := enroll(t, s)
	now = now.Add(totpPeriod * time.Second)

	pending := func(remote string) string {
		_, _, _, err := s.Login(ctx, remote, "admin", goodPassword)
		var need *TwoFactorRequiredError
		if !errors.As(err, &need) {
			t.Fatalf("login: %v", err)
		}
		return need.Pending
	}

	// Five wrong codes use up a pending login, even if the sixth is right.
	p := pending("a")
	for i := range maxPendingAttempts {
		if _, err := s.CompleteLogin(ctx, "a", p, "000000"); !errors.Is(err, ErrInvalidCode) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if _, err := s.CompleteLogin(ctx, "a", p, e.code(s)); !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("after too many tries: %v", err)
	}

	// A pending login expires.
	p = pending("b")
	now = now.Add(PendingTTL + time.Second)
	if _, err := s.CompleteLogin(ctx, "b", p, e.code(s)); !errors.Is(err, ErrLoginExpired) {
		t.Fatalf("expired pending login: %v", err)
	}

	// Wrong codes count toward the rate limit of the address, across logins,
	// and the password stage does not reset it.
	for range maxFailures {
		p = pending("c")
		_, _ = s.CompleteLogin(ctx, "c", p, "000000")
		if s.limited("c") {
			break
		}
	}
	if _, _, _, err := s.Login(ctx, "c", "admin", goodPassword); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("after many wrong codes: %v, want ErrRateLimited", err)
	}
	if _, err := s.CompleteLogin(ctx, "c", "anything", e.code(s)); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("complete while limited: %v", err)
	}
}

func TestRecoveryCodes(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	e := enroll(t, s)

	login := func(code string) (LoginResult, error) {
		_, _, _, err := s.Login(ctx, "r", "admin", goodPassword)
		var need *TwoFactorRequiredError
		if !errors.As(err, &need) {
			t.Fatalf("login: %v", err)
		}
		return s.CompleteLogin(ctx, "r", need.Pending, code)
	}
	res, err := login(strings.ToUpper(e.recovery[0])) // typed in capitals, with the dash
	if err != nil || !res.RecoveryUsed || res.RecoveryLeft != recoveryCodeCount-1 {
		t.Fatalf("recovery login = %+v, %v", res, err)
	}
	if _, err := login(e.recovery[0]); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("a recovery code worked twice: %v", err)
	}
	if res, err := login(strings.ReplaceAll(e.recovery[1], "-", " ")); err != nil || res.RecoveryLeft != recoveryCodeCount-2 {
		t.Fatalf("recovery code typed with a space: %+v, %v", res, err)
	}
	if _, err := login("abcde-fghij"); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("an invented recovery code: %v", err)
	}

	entries, _ := s.Audit(ctx, 20)
	used := 0
	for _, en := range entries {
		if en.Event == EventRecoveryCodeUsed {
			used++
		}
	}
	if used != 2 {
		t.Errorf("recovery code uses in the audit log = %d, want 2: %+v", used, entries)
	}
	var stored string
	_ = s.store.db.QueryRow(`SELECT code_hash FROM recovery_codes LIMIT 1`).Scan(&stored)
	for _, c := range e.recovery {
		if strings.Contains(stored, strings.ReplaceAll(c, "-", "")) {
			t.Error("a recovery code is stored in the clear")
		}
	}

	// New codes replace all the old ones.
	now := time.Now().Add(time.Minute)
	s.now = func() time.Time { return now }
	fresh, err := s.RenewRecoveryCodes(ctx, e.user, goodPassword, e.code(s))
	if err != nil || len(fresh) != recoveryCodeCount {
		t.Fatalf("renew: %v, %v", fresh, err)
	}
	if _, err := login(e.recovery[2]); !errors.Is(err, ErrInvalidCode) {
		t.Fatalf("an old recovery code worked after renewing: %v", err)
	}
	if _, err := login(fresh[0]); err != nil {
		t.Fatalf("a new recovery code: %v", err)
	}
}

func TestSensitiveActionsNeedPasswordAndCode(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	now := time.Now()
	s.now = func() time.Time { return now }
	e := enroll(t, s)
	now = now.Add(totpPeriod * time.Second)

	if err := s.DisableTwoFactor(ctx, e.user, "wrong password!", e.code(s)); !errors.Is(err, ErrWrongPassword) {
		t.Errorf("disable with a wrong password: %v", err)
	}
	if err := s.DisableTwoFactor(ctx, e.user, goodPassword, "000000"); !errors.Is(err, ErrInvalidCode) {
		t.Errorf("disable with a wrong code: %v", err)
	}
	if _, err := s.RenewRecoveryCodes(ctx, e.user, goodPassword, "000000"); !errors.Is(err, ErrInvalidCode) {
		t.Errorf("renew with a wrong code: %v", err)
	}
	if err := s.DisableTwoFactor(ctx, e.user, goodPassword, e.recovery[0]); err != nil {
		t.Fatalf("disable with a recovery code: %v", err)
	}
	if got, _ := s.Users(ctx); got[0].TwoFactor {
		t.Error("two-factor is still on")
	}
	if _, _, _, err := s.Login(ctx, "r", "admin", goodPassword); err != nil {
		t.Errorf("login after disabling: %v", err)
	}
	e.user.TwoFactor = false // what the next request would load
	if err := s.DisableTwoFactor(ctx, e.user, goodPassword, "123456"); !errors.Is(err, ErrTwoFactorOff) {
		t.Errorf("disable while off: %v", err)
	}
	var n int
	_ = s.store.db.QueryRow(`SELECT COUNT(*) FROM recovery_codes`).Scan(&n)
	if n != 0 {
		t.Errorf("%d recovery codes left after disabling", n)
	}
}

func TestPolicy(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	admin, _ := setup(t, s)
	viewer := addUser(t, s, admin, "eve", RoleViewer)

	if s.Policy() != PolicyOff || s.MustEnroll(ctx, admin) {
		t.Fatal("two-factor must be optional by default")
	}
	if err := s.SetPolicy(ctx, admin, "sometimes"); !isInputError(err) {
		t.Fatalf("bad policy: %v", err)
	}
	if err := s.SetPolicy(ctx, admin, PolicyAdmins); err != nil {
		t.Fatal(err)
	}
	if !s.MustEnroll(ctx, admin) || s.MustEnroll(ctx, viewer) {
		t.Fatalf("admins policy: admin %v, viewer %v", s.MustEnroll(ctx, admin), s.MustEnroll(ctx, viewer))
	}
	if err := s.SetPolicy(ctx, admin, PolicyAll); err != nil {
		t.Fatal(err)
	}
	if !s.MustEnroll(ctx, viewer) {
		t.Fatal("the all policy must reach viewers")
	}

	// The policy survives a restart.
	s2, err := New(ctx, s.store.db, quietLog(), WithSecret(testSecret))
	if err != nil || s2.Policy() != PolicyAll {
		t.Fatalf("after restart: %v, %v", s2.Policy(), err)
	}

	// Required, the second factor cannot be turned off.
	if err := s.SetPolicy(ctx, admin, PolicyOff); err != nil {
		t.Fatal(err)
	}
	info, err := s.BeginTwoFactor(ctx, admin, goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	e := enrollFrom(t, s, admin, info)
	if err := s.SetPolicy(ctx, admin, PolicyAdmins); err != nil {
		t.Fatal(err)
	}
	if err := s.DisableTwoFactor(ctx, e.user, goodPassword, e.recovery[0]); !errors.Is(err, ErrTwoFactorMandatory) {
		t.Fatalf("disable while required: %v", err)
	}
	entries, _ := s.Audit(ctx, 30)
	changes := 0
	for _, en := range entries {
		if en.Event == EventTwoFactorPolicyChange {
			changes++
		}
	}
	if changes != 4 {
		t.Errorf("policy changes in the audit log = %d, want 4", changes)
	}
}

func TestAdminResetsTwoFactor(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	e := enroll(t, s)
	other := addUser(t, s, e.user, "eve", RoleViewer)
	_ = other

	if err := s.ResetTwoFactor(ctx, e.user, "nobody"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("unknown user: %v", err)
	}
	if err := s.ResetTwoFactor(ctx, other, e.user.Username); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a viewer reset the two-factor of an admin: %v", err)
	}
	if err := s.ResetTwoFactor(ctx, e.user, e.user.Username); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Authenticate(ctx, e.session); !errors.Is(err, ErrUnauthenticated) {
		t.Error("resetting two-factor must end the sessions")
	}
	if _, _, _, err := s.Login(ctx, "r", "admin", goodPassword); err != nil {
		t.Fatalf("login with the password alone after the reset: %v", err)
	}
	entries, _ := s.Audit(ctx, 10)
	if !hasEvent(entries, EventTwoFactorReset, "admin", "admin") {
		t.Errorf("audit = %+v", entries)
	}
}

func hasEvent(entries []AuditEntry, event, actor, target string) bool {
	for _, e := range entries {
		if e.Event == event && e.Actor == actor && e.Target == target {
			return true
		}
	}
	return false
}

func TestRecoverAdminClearsTwoFactor(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	e := enroll(t, s)

	rec, err := RecoverAdmin(ctx, s.store.db, "", quietLog())
	if err != nil || !rec.TwoFactorCleared {
		t.Fatalf("recovery = %+v, %v", rec, err)
	}
	if err := s.ResetPassword(ctx, "r", rec.Token, resetPassword); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := s.Login(ctx, "r", "admin", resetPassword); err != nil {
		t.Fatalf("an admin recovered from the console must not need the lost second factor: %v", err)
	}
	_ = e
}

func TestWithoutASecretTwoFactorIsUnavailable(t *testing.T) {
	s := newService(t)
	s.box = nil
	u, _ := setup(t, s)
	if _, err := s.BeginTwoFactor(context.Background(), u, goodPassword); !errors.Is(err, ErrTwoFactorUnavailable) {
		t.Fatalf("err = %v", err)
	}
}

func TestHTTPTwoFactor(t *testing.T) {
	s := newService(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	c := newClient(t, s)
	setupBody := map[string]string{"setup_token": s.SetupToken(), "username": "admin", "password": goodPassword}
	if rec := c.do("POST", "/api/auth/setup", setupBody); rec.Code != http.StatusOK {
		t.Fatalf("setup: %d %s", rec.Code, rec.Body)
	}

	// Setup and confirm.
	rec := c.do("POST", "/api/account/two-factor/setup", map[string]string{"password": goodPassword})
	if rec.Code != http.StatusOK {
		t.Fatalf("2fa setup: %d %s", rec.Code, rec.Body)
	}
	var setupInfo TwoFactorSetup
	_ = json.Unmarshal(rec.Body.Bytes(), &setupInfo)
	secret, _ := totpEncoding.DecodeString(strings.ReplaceAll(setupInfo.Secret, " ", ""))
	if rec := c.do("POST", "/api/account/two-factor/enable", map[string]string{"code": "bad"}); rec.Code != http.StatusForbidden {
		t.Fatalf("enable with a bad code: %d", rec.Code)
	}
	rec = c.do("POST", "/api/account/two-factor/enable", map[string]string{"code": totpCode(secret, totpStepOf(now.Unix()))})
	var codes struct {
		RecoveryCodes []string `json:"recovery_codes"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &codes)
	if rec.Code != http.StatusOK || len(codes.RecoveryCodes) != 10 {
		t.Fatalf("enable: %d %s", rec.Code, rec.Body)
	}
	if rec := c.do("GET", "/api/account/two-factor", nil); !strings.Contains(rec.Body.String(), `"enabled":true`) || !strings.Contains(rec.Body.String(), `"recovery_codes_left":10`) {
		t.Fatalf("status: %s", rec.Body)
	}

	// Log in again: the password alone gives no cookie.
	c.do("POST", "/api/auth/logout", map[string]string{})
	rec = c.do("POST", "/api/auth/login", map[string]string{"username": "admin", "password": goodPassword})
	var pending struct {
		Required bool   `json:"two_factor_required"`
		Token    string `json:"pending_token"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &pending)
	if rec.Code != http.StatusOK || !pending.Required || pending.Token == "" || c.cookie != nil {
		t.Fatalf("login = %d %s, cookie %v", rec.Code, rec.Body, c.cookie)
	}
	if rec := c.do("GET", "/api/secret", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("the API is open after the password alone: %d", rec.Code)
	}
	now = now.Add(totpPeriod * time.Second)
	if rec := c.do("POST", "/api/auth/login/two-factor", map[string]string{"pending_token": pending.Token, "code": "000000"}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong code: %d", rec.Code)
	}
	rec = c.do("POST", "/api/auth/login/two-factor", map[string]string{"pending_token": pending.Token, "code": totpCode(secret, totpStepOf(now.Unix()))})
	if rec.Code != http.StatusOK || c.cookie == nil {
		t.Fatalf("second step: %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), `"two_factor":true`) {
		t.Errorf("the user does not say it has two-factor: %s", rec.Body)
	}
	if rec := c.do("GET", "/api/secret", nil); rec.Code != http.StatusOK {
		t.Fatalf("the API is closed after login: %d", rec.Code)
	}

	// An admin policy now lets this admin through, a new viewer is not asked.
	if rec := c.do("PUT", "/api/settings/security", map[string]string{"two_factor_policy": "bogus"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("bad policy: %d", rec.Code)
	}
	if rec := c.do("PUT", "/api/settings/security", map[string]string{"two_factor_policy": "all"}); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"two_factor_policy":"all"`) {
		t.Fatalf("set policy: %d %s", rec.Code, rec.Body)
	}
	if rec := c.do("POST", "/api/account/two-factor/disable", map[string]string{"password": goodPassword, "code": codes.RecoveryCodes[0]}); rec.Code != http.StatusConflict {
		t.Fatalf("disable while required: %d %s", rec.Code, rec.Body)
	}
}

func TestHTTPForcedEnrollment(t *testing.T) {
	s := newService(t)
	c := newClient(t, s)
	c.do("POST", "/api/auth/setup", map[string]string{"setup_token": s.SetupToken(), "username": "admin", "password": goodPassword})
	if rec := c.do("PUT", "/api/settings/security", map[string]string{"two_factor_policy": "admins"}); rec.Code != http.StatusOK {
		t.Fatalf("set policy: %d %s", rec.Code, rec.Body)
	}

	// Required and not set up: everything but the two-factor endpoints is closed.
	rec := c.do("GET", "/api/secret", nil)
	if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "two_factor_setup_required") {
		t.Fatalf("API while enrollment is pending: %d %s", rec.Code, rec.Body)
	}
	if rec := c.do("GET", "/api/users", nil); rec.Code != http.StatusForbidden {
		t.Fatalf("users while enrollment is pending: %d", rec.Code)
	}
	if rec := c.do("GET", "/api/auth/me", nil); !strings.Contains(rec.Body.String(), `"two_factor_setup_required":true`) {
		t.Fatalf("me = %s", rec.Body)
	}
	rec = c.do("POST", "/api/account/two-factor/setup", map[string]string{"password": goodPassword})
	if rec.Code != http.StatusOK {
		t.Fatalf("setup while enrollment is pending: %d %s", rec.Code, rec.Body)
	}
	var setupInfo TwoFactorSetup
	_ = json.Unmarshal(rec.Body.Bytes(), &setupInfo)
	secret, _ := totpEncoding.DecodeString(strings.ReplaceAll(setupInfo.Secret, " ", ""))
	if rec := c.do("POST", "/api/account/two-factor/enable", map[string]string{"code": totpCode(secret, totpStepOf(s.now().Unix()))}); rec.Code != http.StatusOK {
		t.Fatalf("enable: %d %s", rec.Code, rec.Body)
	}
	if rec := c.do("GET", "/api/secret", nil); rec.Code != http.StatusOK {
		t.Fatalf("API after enrolling: %d %s", rec.Code, rec.Body)
	}
}

func TestHTTPTwoFactorAdminActions(t *testing.T) {
	s := newService(t)
	c := newClient(t, s)
	admin, _ := setup(t, s)
	loginAs(t, c, "admin", goodPassword)
	eve := addUser(t, s, admin, "eve", RoleViewer)

	// Only admins reach the admin endpoints.
	viewer := newClient(t, s)
	loginAs(t, viewer, "eve", otherPassword)
	for _, tc := range []struct{ method, path string }{
		{"GET", "/api/settings/security"},
		{"PUT", "/api/settings/security"},
		{"DELETE", "/api/users/admin/two-factor"},
	} {
		if rec := viewer.do(tc.method, tc.path, map[string]string{}); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s as a viewer = %d, want 403", tc.method, tc.path, rec.Code)
		}
	}

	// Give eve a second factor, then an admin removes it.
	info, err := s.BeginTwoFactor(context.Background(), eve, otherPassword)
	if err != nil {
		t.Fatal(err)
	}
	enrollFrom(t, s, eve, info)
	if _, _, _, err := s.Login(context.Background(), "x", "eve", otherPassword); !errors.Is(err, ErrTwoFactorRequired) {
		t.Fatalf("eve has two-factor on: login = %v", err)
	}
	if rec := c.do("DELETE", "/api/users/eve/two-factor", map[string]string{}); rec.Code != http.StatusNoContent {
		t.Fatalf("reset: %d %s", rec.Code, rec.Body)
	}
	if rec := c.do("DELETE", "/api/users/nobody/two-factor", map[string]string{}); rec.Code != http.StatusNotFound {
		t.Fatalf("reset of nobody: %d", rec.Code)
	}
	if _, _, _, err := s.Login(context.Background(), "x", "eve", otherPassword); err != nil {
		t.Fatalf("eve needs only the password after the reset: %v", err)
	}
}
