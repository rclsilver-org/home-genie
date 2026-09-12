package api

import (
	"net/http"
	"testing"
)

// The break-glass login is exposed to the internet and every attempt costs an
// argon2id: without a cap, spelling an account name saturates a small server.
func TestLoginAttemptsAreThrottled(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)

	body := loginRequest{Username: "thomas", Password: "wrong-password", DeviceName: "desk"}
	for i := 0; i < 5; i++ {
		if r := call(t, server, http.MethodPost, "/api/v1/auth/login", "", body); r.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: status = %d, want 401", i+1, r.Code)
		}
	}

	r := call(t, server, http.MethodPost, "/api/v1/auth/login", "", body)
	if r.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", r.Code)
	}
	if r.Header().Get("Retry-After") == "" {
		t.Error("a 429 must say when to come back")
	}

	// And the right password is refused too: the cap is on the account, not
	// on whether the attempt was correct.
	good := loginRequest{Username: "thomas", Password: testPassword, DeviceName: "poste"}
	if r := call(t, server, http.MethodPost, "/api/v1/auth/login", "", good); r.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", r.Code)
	}
}

// A successful login forgets the failures: otherwise a few typos would lock
// the account out after the fact.
func TestASuccessfulLoginForgetsTheFailures(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)

	bad := loginRequest{Username: "thomas", Password: "wrong-password", DeviceName: "desk"}
	for i := 0; i < 4; i++ {
		call(t, server, http.MethodPost, "/api/v1/auth/login", "", bad)
	}

	good := loginRequest{Username: "thomas", Password: testPassword, DeviceName: "poste"}
	if r := call(t, server, http.MethodPost, "/api/v1/auth/login", "", good); r.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 200", r.Code)
	}
	for i := 0; i < 5; i++ {
		if r := call(t, server, http.MethodPost, "/api/v1/auth/login", "", bad); r.Code == http.StatusTooManyRequests {
			t.Fatalf("the counter was not reset (attempt %d)", i+1)
		}
	}
}
