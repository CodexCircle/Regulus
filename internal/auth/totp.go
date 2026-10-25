package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1" //nolint:gosec // RFC 6238 authenticator apps use HMAC-SHA1
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
)

// TOTP parameters (RFC 6238). They are the defaults every authenticator app
// understands, so the otpauth URI does not need to spell them out.
const (
	totpPeriod = 30 // seconds per time step
	totpDigits = 6
	totpIssuer = "Regulus"
)

// totpEncoding is how secrets are shown to users: base32 without padding.
var totpEncoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// newTOTPSecret returns 20 random bytes, the size RFC 4226 recommends.
func newTOTPSecret() ([]byte, error) {
	secret := make([]byte, 20)
	if _, err := rand.Read(secret); err != nil {
		return nil, fmt.Errorf("generate secret: %w", err)
	}
	return secret, nil
}

// totpCode computes the code of a time step.
func totpCode(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step)) //nolint:gosec // steps are positive
	mac := hmac.New(sha1.New, secret)
	mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	bin := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	mod := uint32(1)
	for range totpDigits {
		mod *= 10
	}
	return fmt.Sprintf("%0*d", totpDigits, bin%mod)
}

// totpStepOf returns the time step of a Unix time.
func totpStepOf(unix int64) int64 { return unix / totpPeriod }

// matchTOTP returns the time step whose code equals code, looking at the
// current step and one step either side (clock drift), or false. All three
// codes are always compared, in constant time.
func matchTOTP(secret []byte, code string, unix int64) (int64, bool) {
	cur := totpStepOf(unix)
	matched := int64(-1)
	for _, step := range []int64{cur, cur - 1, cur + 1} {
		if step < 0 {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(code), []byte(totpCode(secret, step))) == 1 && matched < 0 {
			matched = step
		}
	}
	return matched, matched >= 0
}

// totpURI is what authenticator apps read from the QR code.
func totpURI(secret []byte, account string) string {
	label := url.PathEscape(totpIssuer + ":" + account)
	q := url.Values{}
	q.Set("secret", totpEncoding.EncodeToString(secret))
	q.Set("issuer", totpIssuer)
	q.Set("algorithm", "SHA1")
	q.Set("digits", fmt.Sprint(totpDigits))
	q.Set("period", fmt.Sprint(totpPeriod))
	return "otpauth://totp/" + label + "?" + q.Encode()
}

// normalizeCode removes the spaces and dashes people type or paste into a
// code, and lowercases it.
func normalizeCode(code string) string {
	code = strings.ToLower(strings.TrimSpace(code))
	return strings.NewReplacer(" ", "", "-", "").Replace(code)
}

// looksLikeTOTP reports whether a normalized code has the shape of a TOTP
// code (six digits) rather than a recovery code.
func looksLikeTOTP(code string) bool {
	if len(code) != totpDigits {
		return false
	}
	for _, c := range code {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
