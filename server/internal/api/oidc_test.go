package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/rclsilver-org/home-notifications/server/internal/db"
	"github.com/rclsilver-org/home-notifications/server/internal/hub"
	"github.com/rclsilver-org/home-notifications/server/internal/oidc"
	"github.com/rclsilver-org/home-notifications/server/internal/oidc/oidctest"
	"github.com/rclsilver-org/home-notifications/server/internal/store"
)

const oidcClientID = "home-notifications"

// newOIDCServer builds a server wired to a throwaway issuer.
func newOIDCServer(t *testing.T) (*Server, *store.Store, *oidctest.Issuer) {
	t.Helper()

	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { handle.Close() })

	issuer := oidctest.New(t)
	repository := store.New(handle)
	server := New(repository, hub.New(), oidc.New(issuer.URL, oidcClientID),
		slog.New(slog.NewTextHandler(io.Discard, nil)))

	return server, repository, issuer
}

func oidcLogin(t *testing.T, server *Server, idToken, deviceName string) (int, loginResponse) {
	t.Helper()
	recorder := post(t, server, "/api/v1/auth/oidc", oidcLoginRequest{
		IDToken: idToken, DeviceName: deviceName, Platform: "android",
	})
	var response loginResponse
	if recorder.Code == http.StatusCreated {
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
	}
	return recorder.Code, response
}

func TestOIDCLoginCreatesTheAccountAndEnrolsTheDevice(t *testing.T) {
	server, repository, issuer := newOIDCServer(t)

	code, response := oidcLogin(t, server,
		issuer.Sign(t, issuer.Valid(oidcClientID, "sub-thomas", "thomas", "Thomas B")), "phone")
	if code != http.StatusCreated {
		t.Fatalf("status = %d", code)
	}
	if response.Token == "" {
		t.Fatal("no device token")
	}
	if response.User.Username != "thomas" || response.User.IsLocal {
		t.Fatalf("account = %+v — an OIDC account must not be local", response.User)
	}

	user, err := repository.UserByOIDCSubject("sub-thomas")
	if err != nil {
		t.Fatalf("the account was not created: %v", err)
	}
	if user.PasswordHash != "" {
		t.Fatal("an OIDC account was given a password")
	}
}

// Matching happens on the subject: a rename at the provider finds the same
// account again and refreshes the name.
func TestARenameAtTheProviderKeepsTheSameAccount(t *testing.T) {
	server, repository, issuer := newOIDCServer(t)

	oidcLogin(t, server, issuer.Sign(t,
		issuer.Valid(oidcClientID, "sub-thomas", "thomas", "Thomas B")), "phone")

	_, response := oidcLogin(t, server, issuer.Sign(t,
		issuer.Valid(oidcClientID, "sub-thomas", "thomas.b", "Thomas B")), "phone")

	if response.User.Username != "thomas.b" {
		t.Fatalf("name = %q — the rename was not picked up", response.User.Username)
	}

	count, err := repository.CountUsers()
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("%d accounts — the rename created a second one", count)
	}
}

// The security point: a different subject carrying the same name must never
// take over the existing account.
func TestADifferentSubjectNeverInheritsTheAccount(t *testing.T) {
	server, repository, issuer := newOIDCServer(t)

	oidcLogin(t, server, issuer.Sign(t,
		issuer.Valid(oidcClientID, "sub-thomas", "thomas", "Thomas")), "phone")
	first, _ := repository.UserByOIDCSubject("sub-thomas")

	_, response := oidcLogin(t, server, issuer.Sign(t,
		issuer.Valid(oidcClientID, "sub-impostor", "thomas", "Thomas")), "other")

	if response.User.ID == first.ID {
		t.Fatal("another subject took over the existing account")
	}
	if response.User.Username == "thomas" {
		t.Fatalf("name = %q — the collision was not avoided", response.User.Username)
	}
}

// An OIDC account must never absorb the local break-glass one, which exists
// precisely to work when the provider does not.
func TestOIDCNeverTakesOverTheBreakGlassAccount(t *testing.T) {
	server, repository, issuer := newOIDCServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	local, _ := repository.UserByUsername("thomas")

	_, response := oidcLogin(t, server, issuer.Sign(t,
		issuer.Valid(oidcClientID, "sub-thomas", "thomas", "Thomas")), "phone")

	if response.User.ID == local.ID {
		t.Fatal("the OIDC sign-in took over the break-glass account")
	}

	// And the local account keeps its password intact.
	refreshed, err := repository.UserByUsername("thomas")
	if err != nil || refreshed.PasswordHash != local.PasswordHash {
		t.Fatal("the break-glass account was altered")
	}
	if !refreshed.IsLocal() {
		t.Fatal("the break-glass account is no longer local")
	}
}

func TestOIDCRejectsInvalidTokens(t *testing.T) {
	server, _, issuer := newOIDCServer(t)

	wrongAudience := issuer.Sign(t, issuer.Valid("another-client", "sub", "thomas", ""))
	for name, token := range map[string]string{
		"empty":          "",
		"gibberish":      "not-a-jwt",
		"wrong audience": wrongAudience,
	} {
		code, _ := oidcLogin(t, server, token, "phone")
		want := http.StatusUnauthorized
		if token == "" {
			want = http.StatusBadRequest
		}
		if code != want {
			t.Errorf("%s: status = %d, want %d", name, code, want)
		}
	}
}

func TestOIDCRequiresADeviceName(t *testing.T) {
	server, _, issuer := newOIDCServer(t)
	code, _ := oidcLogin(t, server,
		issuer.Sign(t, issuer.Valid(oidcClientID, "sub", "thomas", "")), "")
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", code)
	}
}

// A deployment without an identity provider must fail clearly, not mysteriously.
func TestOIDCDisabledSaysSo(t *testing.T) {
	server, _ := newTestServer(t) // built without a verifier
	recorder := post(t, server, "/api/v1/auth/oidc", oidcLoginRequest{
		IDToken: "no matter what", DeviceName: "phone",
	})
	if recorder.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501", recorder.Code)
	}
}

// The device token handed back by OIDC is worth the local one: that is what
// keeps the provider out of the path of every request.
func TestTheDeviceTokenWorksLikeAnyOther(t *testing.T) {
	server, _, issuer := newOIDCServer(t)

	_, response := oidcLogin(t, server,
		issuer.Sign(t, issuer.Valid(oidcClientID, "sub-thomas", "thomas", "Thomas")), "phone")

	me := decode[meResponse](t, call(t, server, http.MethodGet, "/api/v1/me", response.Token, nil))
	if me.User.Username != "thomas" {
		t.Fatalf("me = %+v", me.User)
	}
	if len(me.Devices) != 1 || !me.Devices[0].Current {
		t.Fatalf("devices = %+v", me.Devices)
	}
}
