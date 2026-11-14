package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters for new hashes (OWASP recommendation: 19 MiB, 2
// iterations, 1 lane). The parameters are stored with every hash, so they can
// be raised later without invalidating existing passwords.
const (
	argonMemory  = 19 * 1024 // KiB
	argonTime    = 2
	argonThreads = 1
	argonKeyLen  = 32
	argonSaltLen = 16

	// Upper bounds accepted when reading a stored hash.
	maxArgonMemory  = 1024 * 1024 // KiB
	maxArgonTime    = 16
	maxArgonThreads = 16
)

var b64 = base64.RawStdEncoding

const (
	// maxConcurrentHashes bounds the argon2 runs at the same moment. Each one
	// takes 19 MiB, and logins come from anyone who can reach the hub, so
	// without a bound a few thousand parallel requests would use gigabytes.
	maxConcurrentHashes = 4
)

// hashWait is how long a request waits for a free slot before it is told the
// server is busy (a variable so that a test need not wait that long).
var hashWait = 5 * time.Second

// ErrBusy means too many passwords are being checked at once.
var ErrBusy = errors.New("the server is busy checking passwords, try again in a moment")

var (
	hashSlots   = make(chan struct{}, maxConcurrentHashes)
	hashRunning atomic.Int32 // for tests: how many run now
	hashPeak    atomic.Int32 // for tests: the most that ran together
)

// acquireHash takes one of the slots, or fails with ErrBusy after hashWait.
func acquireHash() (release func(), err error) {
	timer := time.NewTimer(hashWait)
	defer timer.Stop()
	select {
	case hashSlots <- struct{}{}:
	case <-timer.C:
		return nil, ErrBusy
	}
	n := hashRunning.Add(1)
	for {
		peak := hashPeak.Load()
		if n <= peak || hashPeak.CompareAndSwap(peak, n) {
			break
		}
	}
	return func() {
		hashRunning.Add(-1)
		<-hashSlots
	}, nil
}

// HashPassword returns the argon2id hash of password in the PHC string format,
// for example $argon2id$v=19$m=19456,t=2,p=1$<salt>$<hash>.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate salt: %w", err)
	}
	release, err := acquireHash()
	if err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	release()
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemory, argonTime, argonThreads, b64.EncodeToString(salt), b64.EncodeToString(key)), nil
}

// VerifyPassword reports whether password matches the encoded hash. It returns
// an error only when the stored hash is malformed.
func VerifyPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" {
		return false, errors.New("unsupported password hash format")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return false, errors.New("unsupported argon2 version")
	}
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, errors.New("invalid argon2 parameters")
	}
	if memory == 0 || memory > maxArgonMemory || time == 0 || time > maxArgonTime || threads == 0 || threads > maxArgonThreads {
		return false, errors.New("argon2 parameters out of range")
	}
	salt, err := b64.DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return false, errors.New("invalid salt")
	}
	want, err := b64.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false, errors.New("invalid hash")
	}
	release, err := acquireHash()
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	release()
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
