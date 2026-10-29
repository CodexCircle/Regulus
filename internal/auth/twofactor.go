// twofactor.go is the two-factor (TOTP) part of Service: the policy (who must use
// it), enrollment (BeginTwoFactor, EnableTwoFactor), recovery codes, turning it
// off, and the second step of sign-in (CompleteLogin). The TOTP arithmetic is in
// totp.go, the storage in store_twofactor.go, the routes in twofactor_http.go.
// See docs/code-map.md.

package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"rsc.io/qr"
)

// Two-factor limits.
const (
	// PendingTTL is how long a login waits for the second factor.
	PendingTTL = 5 * time.Minute
	// maxPendingAttempts is how many codes a pending login accepts.
	maxPendingAttempts = 5
	// recoveryCodeCount is how many recovery codes an enrollment makes.
	recoveryCodeCount = 10
	recoveryCodeLen   = 10 // characters, without the dash
)

// Policy says who must use two-factor authentication.
type Policy string

// The policies an admin can choose.
const (
	PolicyOff    Policy = "off"
	PolicyAdmins Policy = "admins"
	PolicyAll    Policy = "all"
)

const policySetting = "two_factor_policy"

// Audit events of two-factor authentication.
const (
	EventTwoFactorEnabled      = "two_factor_enabled"
	EventTwoFactorDisabled     = "two_factor_disabled"
	EventTwoFactorReset        = "two_factor_reset"
	EventRecoveryCodeUsed      = "recovery_code_used"
	EventRecoveryCodesRenewed  = "recovery_codes_renewed"
	EventTwoFactorPolicyChange = "two_factor_policy_changed"
)

// Errors of two-factor authentication.
var (
	ErrTwoFactorUnavailable = errors.New("two-factor authentication is not available: the hub has no secret key")
	ErrTwoFactorOn          = errors.New("two-factor authentication is already on")
	ErrTwoFactorOff         = errors.New("two-factor authentication is not on")
	ErrTwoFactorNotStarted  = errors.New("start the setup first")
	ErrTwoFactorMandatory   = errors.New("the hub requires two-factor authentication for your account, so it cannot be turned off")
	ErrInvalidCode          = errors.New("the code is not valid")
	ErrLoginExpired         = errors.New("this sign-in expired: sign in again")
	// ErrTwoFactorRequired is wrapped by a TwoFactorRequiredError.
	ErrTwoFactorRequired = errors.New("a second factor is required")
)

// TwoFactorRequiredError is what Login returns when the password was right but
// the account has two-factor on: the login goes on with CompleteLogin.
type TwoFactorRequiredError struct {
	// Pending is the token to present, with a code, to CompleteLogin.
	Pending string
	Expires time.Time
}

func (e *TwoFactorRequiredError) Error() string { return ErrTwoFactorRequired.Error() }

// Is lets errors.Is(err, ErrTwoFactorRequired) match.
func (e *TwoFactorRequiredError) Is(target error) bool { return target == ErrTwoFactorRequired }

// Policy returns who must use two-factor authentication.
func (s *Service) Policy() Policy {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.policy
}

// SetPolicy changes who must use two-factor authentication. Users it now
// applies to are asked to set it up at their next request.
func (s *Service) SetPolicy(ctx context.Context, by User, p Policy) error {
	if p != PolicyOff && p != PolicyAdmins && p != PolicyAll {
		return &inputError{`policy must be "off", "admins" or "all"`}
	}
	if err := s.store.putSetting(ctx, policySetting, string(p)); err != nil {
		return err
	}
	s.mu.Lock()
	s.policy = p
	s.mu.Unlock()
	s.audit(ctx, by.Username, EventTwoFactorPolicyChange, "", string(p))
	return nil
}

func (s *Service) loadPolicy(ctx context.Context) error {
	v, ok, err := s.store.setting(ctx, policySetting)
	if err != nil {
		return fmt.Errorf("read two-factor policy: %w", err)
	}
	s.policy = PolicyOff
	if ok {
		switch p := Policy(v); p {
		case PolicyAdmins, PolicyAll:
			s.policy = p
		}
	}
	return nil
}

// Mandatory reports whether the policy requires the user to have two-factor.
// "admins" means whoever can manage users or roles.
func (s *Service) Mandatory(ctx context.Context, u User) bool {
	switch s.Policy() {
	case PolicyAll:
		return true
	case PolicyAdmins:
		p := s.permsOfUser(ctx, u)
		return p.has(PermUsersManage) || p.has(PermRolesManage)
	}
	return false
}

// MustEnroll reports whether the user has to set up two-factor before doing
// anything else.
func (s *Service) MustEnroll(ctx context.Context, u User) bool {
	return !u.TwoFactor && s.Mandatory(ctx, u)
}

// TwoFactorStatus is what the user sees about their own two-factor state.
type TwoFactorStatus struct {
	Enabled           bool   `json:"enabled"`
	Mandatory         bool   `json:"mandatory"`
	Policy            Policy `json:"policy"`
	RecoveryCodesLeft int    `json:"recovery_codes_left"`
}

// TwoFactorStatus returns the two-factor state of the user.
func (s *Service) TwoFactorStatus(ctx context.Context, u User) (TwoFactorStatus, error) {
	st := TwoFactorStatus{Enabled: u.TwoFactor, Mandatory: s.Mandatory(ctx, u), Policy: s.Policy()}
	if u.TwoFactor {
		n, err := s.store.recoveryCodesLeft(ctx, u.ID)
		if err != nil {
			return TwoFactorStatus{}, err
		}
		st.RecoveryCodesLeft = n
	}
	return st, nil
}

// TwoFactorSetup is what the user needs to add the account to an app.
type TwoFactorSetup struct {
	// Secret is the key to type into the app, in groups for reading.
	Secret string `json:"secret"`
	// URI is the otpauth:// address the QR code holds.
	URI string `json:"uri"`
	// QR is the QR code as a PNG data URL.
	QR string `json:"qr"`
}

func aadOf(userID int64) []byte { return []byte("user:" + strconv.FormatInt(userID, 10)) }

func (s *Service) twoFactorKey(userID int64) string { return fmt.Sprintf("2fa:%d", userID) }

// checkPassword verifies the password of a logged-in user for a sensitive
// action. Wrong passwords count toward the rate limit.
func (s *Service) checkPassword(ctx context.Context, u User, password string) error {
	key := s.twoFactorKey(u.ID)
	if s.limited(key) {
		return ErrRateLimited
	}
	hash, err := s.store.passwordHash(ctx, u.ID)
	if err != nil {
		return err
	}
	if ok, _ := VerifyPassword(hash, password); !ok {
		s.recordFailure(key)
		s.audit(ctx, u.Username, EventPasswordFailed, u.Username, "two-factor settings")
		return ErrWrongPassword
	}
	return nil
}

// BeginTwoFactor starts the enrollment: it makes a secret for the user, who
// must confirm it with EnableTwoFactor before it counts. The password is
// needed so that a stolen session cannot add a second factor of its own.
func (s *Service) BeginTwoFactor(ctx context.Context, u User, password string) (TwoFactorSetup, error) {
	if s.box == nil {
		return TwoFactorSetup{}, ErrTwoFactorUnavailable
	}
	if u.TwoFactor {
		return TwoFactorSetup{}, ErrTwoFactorOn
	}
	if err := s.checkPassword(ctx, u, password); err != nil {
		return TwoFactorSetup{}, err
	}
	secret, err := newTOTPSecret()
	if err != nil {
		return TwoFactorSetup{}, err
	}
	sealed, err := s.box.seal(secret, aadOf(u.ID))
	if err != nil {
		return TwoFactorSetup{}, err
	}
	ok, err := s.store.putTOTP(ctx, u.ID, sealed, s.now())
	if err != nil {
		return TwoFactorSetup{}, err
	}
	if !ok {
		return TwoFactorSetup{}, ErrTwoFactorOn
	}
	uri := totpURI(secret, u.Username)
	code, err := qr.Encode(uri, qr.M)
	if err != nil {
		return TwoFactorSetup{}, fmt.Errorf("make QR code: %w", err)
	}
	code.Scale = 6
	return TwoFactorSetup{
		Secret: groupSecret(totpEncoding.EncodeToString(secret)),
		URI:    uri,
		QR:     "data:image/png;base64," + base64.StdEncoding.EncodeToString(code.PNG()),
	}, nil
}

func groupSecret(s string) string {
	var b strings.Builder
	for i, c := range s {
		if i > 0 && i%4 == 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// secretOf decrypts the TOTP secret of the user.
func (s *Service) secretOf(row totpRow, userID int64) ([]byte, error) {
	if s.box == nil {
		return nil, ErrTwoFactorUnavailable
	}
	return s.box.open(row.secretEnc, aadOf(userID))
}

// EnableTwoFactor confirms the enrollment with a code from the app and turns
// two-factor on. It returns the recovery codes, which are never shown again.
func (s *Service) EnableTwoFactor(ctx context.Context, u User, code string) ([]string, error) {
	key := s.twoFactorKey(u.ID)
	if s.limited(key) {
		return nil, ErrRateLimited
	}
	row, err := s.store.totpOf(ctx, u.ID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTwoFactorNotStarted
	}
	if err != nil {
		return nil, err
	}
	if row.confirmed {
		return nil, ErrTwoFactorOn
	}
	secret, err := s.secretOf(row, u.ID)
	if err != nil {
		return nil, err
	}
	step, ok := matchTOTP(secret, normalizeCode(code), s.now().Unix())
	if !ok {
		s.recordFailure(key)
		return nil, ErrInvalidCode
	}
	if ok, err := s.store.confirmTOTP(ctx, u.ID, step, s.now()); err != nil {
		return nil, err
	} else if !ok {
		return nil, ErrTwoFactorOn
	}
	s.clearFailures(key)
	codes, err := s.newRecoveryCodes(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, u.Username, EventTwoFactorEnabled, u.Username, "")
	return codes, nil
}

// newRecoveryCodes makes a fresh set, replacing any older one, and returns
// the codes in the form shown to the user.
func (s *Service) newRecoveryCodes(ctx context.Context, userID int64) ([]string, error) {
	codes := make([]string, recoveryCodeCount)
	hashes := make([]string, recoveryCodeCount)
	for i := range codes {
		raw := make([]byte, 7) // 56 bits: ten base32 characters, 50 bits used
		if _, err := rand.Read(raw); err != nil {
			return nil, fmt.Errorf("generate recovery code: %w", err)
		}
		plain := strings.ToLower(totpEncoding.EncodeToString(raw))[:recoveryCodeLen]
		codes[i] = plain[:recoveryCodeLen/2] + "-" + plain[recoveryCodeLen/2:]
		hashes[i] = hashToken(plain)
	}
	if err := s.store.replaceRecoveryCodes(ctx, userID, hashes); err != nil {
		return nil, err
	}
	return codes, nil
}

// secondFactor is how a code was accepted.
type secondFactor int

const (
	factorNone secondFactor = iota
	factorTOTP
	factorRecovery
)

// checkSecondFactor accepts a TOTP code, or a recovery code, for a user with
// two-factor on. A TOTP code works once; a recovery code works once and is
// then gone.
func (s *Service) checkSecondFactor(ctx context.Context, userID int64, code string) (secondFactor, error) {
	code = normalizeCode(code)
	if code == "" || len(code) > 32 {
		return factorNone, nil
	}
	row, err := s.store.totpOf(ctx, userID)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !row.confirmed) {
		return factorNone, nil
	}
	if err != nil {
		return factorNone, err
	}
	if looksLikeTOTP(code) {
		secret, err := s.secretOf(row, userID)
		if err != nil {
			return factorNone, err
		}
		step, ok := matchTOTP(secret, code, s.now().Unix())
		if !ok || step <= row.lastStep {
			return factorNone, nil
		}
		fresh, err := s.store.useStep(ctx, userID, step)
		if err != nil || !fresh {
			return factorNone, err
		}
		return factorTOTP, nil
	}
	used, err := s.store.useRecoveryCode(ctx, userID, hashToken(code), s.now())
	if err != nil || !used {
		return factorNone, err
	}
	return factorRecovery, nil
}

// verifyRecoverable checks a code for a sensitive action by a logged-in user.
// A recovery code works too, and is used up.
func (s *Service) verifyRecoverable(ctx context.Context, u User, code string) error {
	key := s.twoFactorKey(u.ID)
	factor, err := s.checkSecondFactor(ctx, u.ID, code)
	if err != nil {
		return err
	}
	if factor == factorNone {
		s.recordFailure(key)
		return ErrInvalidCode
	}
	if factor == factorRecovery {
		s.noteRecoveryUsed(ctx, u.Username)
	}
	return nil
}

func (s *Service) noteRecoveryUsed(ctx context.Context, username string) {
	s.log.Warn("a recovery code was used", "user", username)
	s.audit(ctx, username, EventRecoveryCodeUsed, username, "")
}

// DisableTwoFactor turns two-factor off after the password and a valid code
// (or recovery code). It is refused when the policy requires it.
func (s *Service) DisableTwoFactor(ctx context.Context, u User, password, code string) error {
	if !u.TwoFactor {
		return ErrTwoFactorOff
	}
	if s.Mandatory(ctx, u) {
		return ErrTwoFactorMandatory
	}
	if err := s.checkPassword(ctx, u, password); err != nil {
		return err
	}
	if err := s.verifyRecoverable(ctx, u, code); err != nil {
		return err
	}
	s.clearFailures(s.twoFactorKey(u.ID))
	if _, err := s.store.clearTwoFactor(ctx, u.ID); err != nil {
		return err
	}
	s.audit(ctx, u.Username, EventTwoFactorDisabled, u.Username, "")
	return nil
}

// RenewRecoveryCodes replaces the recovery codes after the password and a
// valid code, and returns the new ones.
func (s *Service) RenewRecoveryCodes(ctx context.Context, u User, password, code string) ([]string, error) {
	if !u.TwoFactor {
		return nil, ErrTwoFactorOff
	}
	if err := s.checkPassword(ctx, u, password); err != nil {
		return nil, err
	}
	if err := s.verifyRecoverable(ctx, u, code); err != nil {
		return nil, err
	}
	s.clearFailures(s.twoFactorKey(u.ID))
	codes, err := s.newRecoveryCodes(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	s.audit(ctx, u.Username, EventRecoveryCodesRenewed, u.Username, "")
	return codes, nil
}

// ResetTwoFactor removes the second factor of another user, for someone who
// lost the app and the recovery codes. The caller must be an admin (checked by
// the HTTP layer). The sessions of the user end.
func (s *Service) ResetTwoFactor(ctx context.Context, by User, username string) error {
	target, err := s.userByName(ctx, username)
	if err != nil {
		return err
	}
	if err := s.mayManage(ctx, by, target); err != nil {
		return err
	}
	if _, err := s.store.clearTwoFactor(ctx, target.ID); err != nil {
		return err
	}
	if err := s.store.deleteSessionsOf(ctx, target.ID, ""); err != nil {
		return err
	}
	s.audit(ctx, by.Username, EventTwoFactorReset, target.Username, "sessions ended")
	return nil
}

// LoginResult is the outcome of a completed login.
type LoginResult struct {
	User    User
	Token   string
	Expires time.Time
	// RecoveryUsed is true when a recovery code, not an app code, was used.
	RecoveryUsed bool
	// RecoveryLeft is how many recovery codes remain; set with RecoveryUsed.
	RecoveryLeft int
}

// beginPending makes the pending login of a user who passed the password.
func (s *Service) beginPending(ctx context.Context, userID int64) (*TwoFactorRequiredError, error) {
	token, err := randomToken(32)
	if err != nil {
		return nil, err
	}
	now := s.now()
	s.store.deleteExpiredPending(ctx, now)
	expires := now.Add(PendingTTL)
	if err := s.store.putPending(ctx, hashToken(token), userID, now, expires); err != nil {
		return nil, err
	}
	return &TwoFactorRequiredError{Pending: token, Expires: expires}, nil
}

// CompleteLogin finishes a login that waits for the second factor: with a
// valid code the session starts. remote identifies the client for the limit on
// failed attempts, which wrong codes count toward.
func (s *Service) CompleteLogin(ctx context.Context, remote, pending, code string) (LoginResult, error) {
	if s.limited(remote) {
		return LoginResult{}, ErrRateLimited
	}
	if pending == "" || len(pending) > 128 {
		s.recordFailure(remote)
		return LoginResult{}, ErrLoginExpired
	}
	hash := hashToken(pending)
	userID, ok, err := s.store.tryPending(ctx, hash, s.now(), maxPendingAttempts)
	if err != nil {
		return LoginResult{}, err
	}
	if !ok {
		s.recordFailure(remote)
		return LoginResult{}, ErrLoginExpired
	}
	user, err := s.userByID(ctx, userID)
	if err != nil || user.Disabled {
		_ = s.store.deletePending(ctx, hash)
		return LoginResult{}, ErrLoginExpired
	}
	factor, err := s.checkSecondFactor(ctx, userID, code)
	if err != nil {
		return LoginResult{}, err
	}
	if factor == factorNone {
		s.recordFailure(remote)
		s.audit(ctx, user.Username, EventLoginFailed, "", "wrong second factor")
		return LoginResult{}, ErrInvalidCode
	}
	_ = s.store.deletePending(ctx, hash)
	s.clearFailures(remote)

	now := s.now()
	_ = s.store.deleteExpiredSessions(ctx, now)
	token, expires, err := s.newSession(ctx, user.ID, now)
	if err != nil {
		return LoginResult{}, err
	}
	res := LoginResult{User: user, Token: token, Expires: expires}
	detail := ""
	if factor == factorRecovery {
		res.RecoveryUsed = true
		res.RecoveryLeft, _ = s.store.recoveryCodesLeft(ctx, user.ID)
		s.noteRecoveryUsed(ctx, user.Username)
		detail = "recovery code"
	}
	s.audit(ctx, user.Username, EventLogin, "", detail)
	return res, nil
}

func (s *Service) userByID(ctx context.Context, id int64) (User, error) {
	u, err := s.store.userByID(ctx, id)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	return u, err
}

// newSealerFor is used by New; kept here with the other two-factor code.
func sealerFor(secret []byte) (*sealer, error) {
	if len(secret) == 0 {
		return nil, nil //nolint:nilnil // no secret: two-factor is unavailable
	}
	return newSealer(secret)
}
