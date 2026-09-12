package api

import (
	"fmt"
	"testing"
	"time"
)

// The login endpoint is reachable from the internet, so the limiter must not
// keep a row per username anyone ever invents: that would hand an attacker a
// memory leak. Stale names are swept once the map grows past the threshold.
func TestLimiterSweepsInventedAccounts(t *testing.T) {
	limiter := newAttemptLimiter(5, 15*time.Minute)
	now := time.Now()
	limiter.now = func() time.Time { return now }

	for i := 0; i < sweepThreshold; i++ {
		limiter.allow(fmt.Sprintf("invented-%d", i))
	}
	if got := len(limiter.attempts); got < sweepThreshold {
		t.Fatalf("before the window closes, %d keys, want at least %d", got, sweepThreshold)
	}

	// The window passes; the very next attempt pays for the sweep.
	now = now.Add(16 * time.Minute)
	limiter.allow("fresh")

	if got := len(limiter.attempts); got != 1 {
		t.Fatalf("after the sweep, %d keys, want only the live one", got)
	}
}

// A key still inside its window must survive the sweep: forgetting live
// failures would reopen the door the limiter closes.
func TestLimiterSweepKeepsLiveAccounts(t *testing.T) {
	limiter := newAttemptLimiter(5, 15*time.Minute)
	now := time.Now()
	limiter.now = func() time.Time { return now }

	for i := 0; i < 5; i++ {
		limiter.allow("target")
	}

	for i := 0; i < sweepThreshold; i++ {
		limiter.allow(fmt.Sprintf("noise-%d", i))
	}
	now = now.Add(10 * time.Minute) // inside the window
	limiter.allow("fresh")          // triggers a sweep attempt

	if limiter.allow("target") {
		t.Fatal("the throttled account was forgotten by the sweep")
	}
}
