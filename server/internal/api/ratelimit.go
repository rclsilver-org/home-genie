package api

import (
	"sync"
	"time"
)

// attemptLimiter throttles the break-glass login.
//
// The endpoint is reachable from the internet and each attempt costs an
// argon2id verification, deliberately expensive: that is what makes a stolen
// hash hard to crack, and what makes an unauthenticated request an easy way
// to burn a small server's memory and CPU. The limiter turns both problems
// into a cheap counter lookup.
//
// Keyed by username and not by address: the server listens on the loopback
// behind a reverse proxy, so every request carries the same client address,
// and trusting a forwarded header would let the caller pick their own key.
// Counting per account is what an attacker cannot rename away.
type attemptLimiter struct {
	mu       sync.Mutex
	attempts map[string][]time.Time
	window   time.Duration
	max      int
	now      func() time.Time
}

// sweepThreshold is the map size past which stale keys are swept.
//
// The keys are whatever usernames the outside sends: without a sweep the map
// would grow by one entry per invented name, which on a public endpoint is a
// memory leak the attacker controls. Sweeping only past a threshold keeps
// the hot path allocation-free for the honest case of a handful of accounts.
const sweepThreshold = 1024

func newAttemptLimiter(max int, window time.Duration) *attemptLimiter {
	return &attemptLimiter{
		attempts: map[string][]time.Time{},
		window:   window,
		max:      max,
		now:      time.Now,
	}
}

// allow reports whether another attempt may be made for this key, and counts
// it. Successful logins call reset, so a working account is never locked out
// by its own use.
func (l *attemptLimiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	cutoff := l.now().Add(-l.window)

	if len(l.attempts) >= sweepThreshold {
		l.sweep(cutoff)
	}

	kept := l.attempts[key][:0]
	for _, at := range l.attempts[key] {
		if at.After(cutoff) {
			kept = append(kept, at)
		}
	}

	if len(kept) >= l.max {
		l.attempts[key] = kept
		return false
	}
	l.attempts[key] = append(kept, l.now())
	return true
}

// sweep drops every key whose newest attempt is older than the window.
// Attempts are appended in order, so the last one is the newest.
func (l *attemptLimiter) sweep(cutoff time.Time) {
	for key, attempts := range l.attempts {
		if len(attempts) == 0 || !attempts[len(attempts)-1].After(cutoff) {
			delete(l.attempts, key)
		}
	}
}

// reset forgets the failures of an account that has just proved itself.
func (l *attemptLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}
