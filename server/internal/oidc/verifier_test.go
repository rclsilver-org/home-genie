package oidc_test

import (
	"context"
	"testing"
	"time"

	"github.com/rclsilver-org/home-notifications/server/internal/oidc"
	"github.com/rclsilver-org/home-notifications/server/internal/oidc/oidctest"
)

const clientID = "home-notifications"

func TestVerifyAcceptsAValidToken(t *testing.T) {
	issuer := oidctest.New(t)
	verifier := oidc.New(issuer.URL, clientID)

	identity, err := verifier.Verify(context.Background(),
		issuer.Sign(t, issuer.Valid(clientID, "sub-thomas", "thomas", "Thomas B")))
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if identity.Subject != "sub-thomas" || identity.Username != "thomas" {
		t.Fatalf("identity = %+v", identity)
	}
	if identity.DisplayName != "Thomas B" {
		t.Fatalf("display name = %q", identity.DisplayName)
	}
}

// Without this check, any client of the same provider could hand over a token
// of its own, and every application of the realm would become a way in here.
func TestVerifyRejectsAnotherAudience(t *testing.T) {
	issuer := oidctest.New(t)
	verifier := oidc.New(issuer.URL, clientID)

	if _, err := verifier.Verify(context.Background(),
		issuer.Sign(t, issuer.Valid("another-application", "sub", "thomas", ""))); err == nil {
		t.Fatal("a token addressed to another client was accepted")
	}
}

func TestVerifyRejectsAnExpiredToken(t *testing.T) {
	issuer := oidctest.New(t)
	verifier := oidc.New(issuer.URL, clientID)

	expired := issuer.Valid(clientID, "sub", "thomas", "")
	expired.Expiry = time.Now().Add(-time.Hour).Unix()
	expired.IssuedAt = time.Now().Add(-2 * time.Hour).Unix()

	if _, err := verifier.Verify(context.Background(), issuer.Sign(t, expired)); err == nil {
		t.Fatal("an expired token was accepted")
	}
}

// That is all that separates a legitimate token from a forged one.
func TestVerifyRejectsAForeignSignature(t *testing.T) {
	issuer := oidctest.New(t)
	imposter := oidctest.New(t)
	verifier := oidc.New(issuer.URL, clientID)

	forged := imposter.Valid(clientID, "sub", "thomas", "")
	forged.Issuer = issuer.URL // pretends to come from the right issuer

	if _, err := verifier.Verify(context.Background(), imposter.Sign(t, forged)); err == nil {
		t.Fatal("a token signed by another key was accepted")
	}
}

func TestVerifyRejectsGarbage(t *testing.T) {
	issuer := oidctest.New(t)
	verifier := oidc.New(issuer.URL, clientID)

	for _, raw := range []string{"", "not-a-jwt", "a.b.c"} {
		if _, err := verifier.Verify(context.Background(), raw); err == nil {
			t.Errorf("Verify(%q) succeeded", raw)
		}
	}
}

// An address can be reassigned inside an organisation; a subject cannot.
func TestUsernameFallsBackToTheSubjectNotTheEmail(t *testing.T) {
	issuer := oidctest.New(t)
	verifier := oidc.New(issuer.URL, clientID)

	anonymous := issuer.Valid(clientID, "sub-thomas", "", "")
	anonymous.Email = "thomas@example.net"

	identity, err := verifier.Verify(context.Background(), issuer.Sign(t, anonymous))
	if err != nil {
		t.Fatal(err)
	}
	if identity.Username != "sub-thomas" {
		t.Fatalf("username = %q, want the subject", identity.Username)
	}
}

func TestDisabledWithoutConfiguration(t *testing.T) {
	for _, verifier := range []*oidc.Verifier{oidc.New("", "client"), oidc.New("https://issuer", "")} {
		if verifier.Enabled() {
			t.Error("an incomplete verifier declares itself enabled")
		}
		if _, err := verifier.Verify(context.Background(), "x"); err != oidc.ErrDisabled {
			t.Errorf("err = %v, want ErrDisabled", err)
		}
	}
}

// An unreachable issuer must not turn every attempt into a long wait: the
// failure is remembered for a short while.
func TestUnreachableIssuerFailsFastOnRetry(t *testing.T) {
	verifier := oidc.New("http://127.0.0.1:1", "client")

	start := time.Now()
	for i := 0; i < 3; i++ {
		if _, err := verifier.Verify(context.Background(), "x"); err == nil {
			t.Fatal("an unreachable issuer validated a token")
		}
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("three attempts took %s — the failure is not remembered", elapsed)
	}
}
