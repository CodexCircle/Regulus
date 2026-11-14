package auth

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func (s *Service) failureCount(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	if f := s.failures[key]; f != nil {
		return f.count
	}
	return 0
}

func TestAddressKey(t *testing.T) {
	for in, want := range map[string]string{
		"203.0.113.7":               "203.0.113.7",
		"::ffff:203.0.113.7":        "203.0.113.7",
		"2001:db8:1:2:aaaa::1":      "2001:db8:1:2::/64",
		"2001:db8:1:2:bbbb:cccc::9": "2001:db8:1:2::/64",
		"2001:db8:1:3::1":           "2001:db8:1:3::/64",
		"not an address":            "not an address",
		"":                          "",
	} {
		if got := addressKey(in); got != want {
			t.Errorf("addressKey(%q) = %q, want %q", in, got, want)
		}
	}
}

// 200 parallel wrong passwords from one address must not all reach the
// password check: the limit is taken before the check, not after it.
func TestParallelWrongLoginsAreLimited(t *testing.T) {
	s := newService(t)
	setup(t, s)
	var mu sync.Mutex
	counts := map[string]int{}
	var wg sync.WaitGroup
	for range 200 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _, err := s.Login(context.Background(), "198.51.100.1", "admin", "wrong password!")
			mu.Lock()
			defer mu.Unlock()
			switch {
			case errors.Is(err, ErrRateLimited):
				counts["limited"]++
			case errors.Is(err, ErrInvalidCredentials):
				counts["checked"]++
			default:
				counts[fmt.Sprint(err)]++
			}
		}()
	}
	wg.Wait()
	if counts["checked"] > maxFailures || counts["limited"] < 200-maxFailures || len(counts) != 2 {
		t.Errorf("results %v: at most %d may reach the check and the rest must be limited", counts, maxFailures)
	}
}

func TestAddressesOfOnePrefixShareABudget(t *testing.T) {
	s := newService(t)
	setup(t, s)
	for i := range maxFailures {
		addr := fmt.Sprintf("2001:db8:7:7:%x::1", i+1) // ten different addresses, one /64
		if _, _, _, err := s.Login(context.Background(), addr, "admin", "wrong password!"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if _, _, _, err := s.Login(context.Background(), "2001:db8:7:7:ffff::1", "admin", goodPassword); !errors.Is(err, ErrRateLimited) {
		t.Errorf("an eleventh address of the same /64 even with the right password: %v, want ErrRateLimited", err)
	}
	if _, _, _, err := s.Login(context.Background(), "2001:db8:7:8::1", "admin", goodPassword); err != nil {
		t.Errorf("another /64: %v", err)
	}
}

// One account guessed from many addresses stops too.
func TestOneAccountIsLimitedAcrossAddresses(t *testing.T) {
	s := newService(t)
	setup(t, s)
	for i := range maxUserFailures {
		addr := fmt.Sprintf("192.0.2.%d", i+1)
		if _, _, _, err := s.Login(context.Background(), addr, "Admin", "wrong password!"); !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("attempt %d: %v", i, err)
		}
	}
	if _, _, _, err := s.Login(context.Background(), "192.0.2.200", "admin", "wrong password!"); !errors.Is(err, ErrRateLimited) {
		t.Errorf("after %d failures from %d addresses: %v, want ErrRateLimited", maxUserFailures, maxUserFailures, err)
	}
	// Other accounts are not affected, and the table does not tell which
	// names exist: an unknown name is counted the same way.
	if _, _, _, err := s.Login(context.Background(), "192.0.2.201", "nobody", "wrong password!"); !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("another name: %v", err)
	}
}

// A good login must not hand back the failures of the address: someone with an
// account of their own could otherwise reset the budget between guesses.
func TestSuccessDoesNotResetTheAddressBudget(t *testing.T) {
	s := newService(t)
	setup(t, s)
	key := s.ipKey("198.51.100.9")
	for range maxFailures - 1 {
		s.recordFailure(key)
	}
	if _, _, _, err := s.Login(context.Background(), "198.51.100.9", "admin", goodPassword); err != nil {
		t.Fatal(err)
	}
	if n := s.failureCount(key); n != maxFailures-1 {
		t.Errorf("failures of the address after a good login = %d, want %d", n, maxFailures-1)
	}
}

func TestAbandonGivesTheReservationBack(t *testing.T) {
	s := newService(t)
	att, ok := s.begin("ip:x")
	if !ok || s.failureCount("ip:x") != 1 {
		t.Fatalf("begin: ok=%v count=%d", ok, s.failureCount("ip:x"))
	}
	att.abandon()
	if n := s.failureCount("ip:x"); n != 0 {
		t.Errorf("count after abandon = %d", n)
	}
	att, _ = s.begin("ip:x")
	att.failed()
	att.abandon() // no-op after a verdict
	if n := s.failureCount("ip:x"); n != 1 {
		t.Errorf("count after failed+abandon = %d, want 1", n)
	}
}

func TestFailureTableIsBounded(t *testing.T) {
	s := newService(t)
	for i := range 3 * maxTracked {
		s.recordFailure(fmt.Sprintf("ip:%d", i))
	}
	s.mu.Lock()
	n := len(s.failures)
	s.mu.Unlock()
	if n > maxTracked {
		t.Errorf("the table holds %d keys, want at most %d", n, maxTracked)
	}
}

func TestExpiredWindowStartsOver(t *testing.T) {
	s := newService(t)
	now := time.Now()
	s.now = func() time.Time { return now }
	for range maxFailures {
		att, ok := s.begin("ip:y")
		if !ok {
			t.Fatal("limited too early")
		}
		att.failed()
	}
	if _, ok := s.begin("ip:y"); ok {
		t.Fatal("not limited")
	}
	now = now.Add(failureWindow + time.Second)
	if _, ok := s.begin("ip:y"); !ok {
		t.Error("still limited after the window")
	}
}

func TestHashingIsBounded(t *testing.T) {
	hash, err := HashPassword(goodPassword)
	if err != nil {
		t.Fatal(err)
	}
	hashPeak.Store(0)
	var wg sync.WaitGroup
	for range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, err := VerifyPassword(hash, goodPassword); err != nil || !ok {
				t.Errorf("verify: %v %v", ok, err)
			}
		}()
	}
	wg.Wait()
	if p := hashPeak.Load(); p > maxConcurrentHashes || p < 1 {
		t.Errorf("peak of hashes at the same time = %d, want 1 to %d", p, maxConcurrentHashes)
	}
}

func TestBusyWhenNoSlotIsFree(t *testing.T) {
	hash, _ := HashPassword(goodPassword)
	old := hashWait
	hashWait = 20 * time.Millisecond
	defer func() { hashWait = old }()
	for range maxConcurrentHashes {
		hashSlots <- struct{}{}
	}
	defer func() {
		for range maxConcurrentHashes {
			<-hashSlots
		}
	}()
	if _, err := VerifyPassword(hash, goodPassword); !errors.Is(err, ErrBusy) {
		t.Errorf("verify with no free slot: %v, want ErrBusy", err)
	}
	if _, err := HashPassword(goodPassword); !errors.Is(err, ErrBusy) {
		t.Errorf("hash with no free slot: %v, want ErrBusy", err)
	}
}
