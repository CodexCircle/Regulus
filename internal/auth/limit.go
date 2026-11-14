package auth

import (
	"net/netip"
	"strings"
	"time"
)

// Limits on failed attempts. Every check that takes a password or a code
// reserves its attempt BEFORE the expensive verification and settles it
// afterwards, so a burst of parallel requests cannot all pass the check before
// any failure is recorded (the old order was check, verify, record).
const (
	// maxUserFailures is the budget of one account name, over all addresses.
	// It is larger than the budget of one address (maxFailures) so that a
	// stranger from one address cannot lock a user out, but guessing one
	// password from many addresses (an IPv6 prefix, a botnet) stops too.
	maxUserFailures = 30

	// maxTracked bounds the table: keys come from outside.
	maxTracked = 10000
)

const (
	ipPrefix    = "ip:"
	resetPrefix = "reset:"
	userPrefix  = "user:"
)

// limitFor is the number of failures after which a key is limited.
func limitFor(key string) int {
	if strings.HasPrefix(key, userPrefix) {
		return maxUserFailures
	}
	return maxFailures
}

// addressKey is the part of a client address that shares one budget: an IPv4
// address as it is, an IPv6 address by its /64 (one subscriber owns that much,
// so one address per attempt would be no limit at all).
func addressKey(remote string) string {
	addr, err := netip.ParseAddr(remote)
	if err != nil {
		return remote
	}
	addr = addr.Unmap()
	if addr.Is4() {
		return addr.String()
	}
	prefix, err := addr.Prefix(64)
	if err != nil {
		return addr.String()
	}
	return prefix.String()
}

func (s *Service) ipKey(remote string) string     { return ipPrefix + addressKey(remote) }
func (s *Service) resetKey(remote string) string  { return resetPrefix + addressKey(remote) }
func (s *Service) userKey(username string) string { return userPrefix + normalizeUsername(username) }

// attempt is a reserved try. Exactly one of failed, succeeded or abandon
// settles it; abandon is meant for defer and does nothing after the others.
type attempt struct {
	s       *Service
	keys    []string
	settled bool
}

// begin reserves an attempt on every key, or returns false, reserving
// nothing, when one of them has used up its budget. The check and the
// reservation are one step under the lock.
func (s *Service) begin(keys ...string) (*attempt, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for _, k := range keys {
		if f := s.failures[k]; f != nil && now.Sub(f.since) <= failureWindow && f.count >= limitFor(k) {
			return nil, false
		}
	}
	for _, k := range keys {
		s.countLocked(k, now)
	}
	return &attempt{s: s, keys: keys}, true
}

// countLocked adds one to the key, starting a new window when the old one is over.
func (s *Service) countLocked(key string, now time.Time) {
	f := s.failures[key]
	if f == nil || now.Sub(f.since) > failureWindow {
		if f == nil && len(s.failures) >= maxTracked {
			s.makeRoomLocked(now)
		}
		f = &failure{since: now}
		s.failures[key] = f
	}
	f.count++
}

// makeRoomLocked drops the expired keys and, when the table is still full, a
// quarter of the others (arbitrary ones), so that the table never grows past
// maxTracked and the sweep is paid once per maxTracked/4 new keys, not on each.
func (s *Service) makeRoomLocked(now time.Time) {
	for k, f := range s.failures {
		if now.Sub(f.since) > failureWindow {
			delete(s.failures, k)
		}
	}
	drop := len(s.failures) - (maxTracked - maxTracked/4)
	for k := range s.failures {
		if drop <= 0 {
			break
		}
		delete(s.failures, k)
		drop--
	}
}

// failed settles the attempt as a failure: the reservation stays counted.
func (a *attempt) failed() { a.settled = true }

// succeeded settles the attempt as a success. The reservation of an address
// is given back, but the failures it already made stay: otherwise someone
// with an account of their own could reset their budget with one good login
// between guesses at another one. What belongs to one account (its name, its
// second factor) starts over.
func (a *attempt) succeeded() {
	if a.settled {
		return
	}
	a.settled = true
	a.s.mu.Lock()
	defer a.s.mu.Unlock()
	for _, k := range a.keys {
		if strings.HasPrefix(k, ipPrefix) || strings.HasPrefix(k, resetPrefix) {
			a.s.refundLocked(k)
		} else {
			delete(a.s.failures, k)
		}
	}
}

// abandon gives the reservation back when the attempt ended without a verdict
// (an error of the server, an input the user can correct). It is a no-op
// after failed or succeeded.
func (a *attempt) abandon() {
	if a == nil || a.settled {
		return
	}
	a.settled = true
	a.s.mu.Lock()
	defer a.s.mu.Unlock()
	for _, k := range a.keys {
		a.s.refundLocked(k)
	}
}

func (s *Service) refundLocked(key string) {
	f := s.failures[key]
	if f == nil {
		return
	}
	if f.count--; f.count <= 0 {
		delete(s.failures, key)
	}
}

// recordFailure counts a failure that had no reservation (a wrong second
// factor for a sensitive action, for example).
func (s *Service) recordFailure(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.countLocked(key, s.now())
}

// clearFailures forgets the failures of one key.
func (s *Service) clearFailures(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.failures, key)
}
