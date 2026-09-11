package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/rclsilver-org/home-genie/server/internal/auth"
	"github.com/rclsilver-org/home-genie/server/internal/db"
	"github.com/rclsilver-org/home-genie/server/internal/hub"
	"github.com/rclsilver-org/home-genie/server/internal/store"
)

func newTestServer(t *testing.T) (*Server, *store.Store) {
	server, repository, _ := newTestServerWithHub(t)
	return server, repository
}

// newTestServerWithHub also hands back the fanout, for the socket tests.
func newTestServerWithHub(t *testing.T) (*Server, *store.Store, *hub.Hub) {
	t.Helper()
	handle, err := db.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("opening the database: %v", err)
	}
	t.Cleanup(func() { handle.Close() })

	repository := store.New(handle)
	fanout := hub.New()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return New(repository, fanout, nil, logger), repository, fanout
}

// withLocalAccount creates the break-glass account with a known password.
func withLocalAccount(t *testing.T, s *store.Store, username, password string) store.User {
	t.Helper()
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	user, err := s.CreateLocalUser(username, username, hash, true)
	if err != nil {
		t.Fatal(err)
	}
	return user
}

func post(t *testing.T, server *Server, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(encoded))
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, request)
	return recorder
}

func TestLoginEnrolsADeviceAndReturnsAToken(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", "a-solid-password")

	recorder := post(t, server, "/api/v1/auth/login", loginRequest{
		Username:   "thomas",
		Password:   "a-solid-password",
		DeviceName: "phone",
		Platform:   "android",
	})
	if recorder.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201: %s", recorder.Code, recorder.Body)
	}

	var response loginResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Token == "" {
		t.Fatal("no token was returned")
	}
	if response.Device.Transport != store.TransportWebSocket {
		t.Fatalf("transport = %q", response.Device.Transport)
	}
	if !response.User.IsLocal || !response.User.IsAdmin {
		t.Fatalf("the account came back wrong: %+v", response.User)
	}

	// The clear token must never be stored: only its hash resolves.
	if _, _, err := repository.DeviceByTokenHash(response.Token); err == nil {
		t.Fatal("the token resolves in clear, so it was stored unhashed")
	}
	if _, _, err := repository.DeviceByTokenHash(auth.HashToken(response.Token)); err != nil {
		t.Fatalf("the token does not resolve through its hash: %v", err)
	}
}

func TestLoginRejectsAWrongPassword(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", "a-solid-password")

	recorder := post(t, server, "/api/v1/auth/login", loginRequest{
		Username: "thomas", Password: "wrong", DeviceName: "phone",
	})
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", recorder.Code)
	}
}

// An unknown account and a wrong password must be indistinguishable from
// outside: same status, same body.
func TestUnknownAccountLooksLikeAWrongPassword(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", "a-solid-password")

	wrongPassword := post(t, server, "/api/v1/auth/login", loginRequest{
		Username: "thomas", Password: "wrong", DeviceName: "phone",
	})
	unknownUser := post(t, server, "/api/v1/auth/login", loginRequest{
		Username: "nobody", Password: "wrong", DeviceName: "phone",
	})

	if wrongPassword.Code != unknownUser.Code {
		t.Fatalf("statuses differ: %d and %d", wrongPassword.Code, unknownUser.Code)
	}
	if wrongPassword.Body.String() != unknownUser.Body.String() {
		t.Fatalf("bodies differ: %q and %q", wrongPassword.Body, unknownUser.Body)
	}
}

func TestLoginRequiresADeviceName(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", "a-solid-password")

	recorder := post(t, server, "/api/v1/auth/login", loginRequest{
		Username: "thomas", Password: "a-solid-password",
	})
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

// An unknown field is a client typo, not a setting to ignore silently.
func TestLoginRejectsUnknownFields(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", "a-solid-password")

	request := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login",
		bytes.NewReader([]byte(`{"username":"thomas","password":"x","device_name":"phone","typo":1}`)))
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", recorder.Code)
	}
}

func TestMeRequiresAValidToken(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", "a-solid-password")

	for name, header := range map[string]string{
		"absent":       "",
		"malformed":    "hnd_something",
		"wrong scheme": "Basic hnd_something",
		"unknown":      "Bearer hnd_nonexistent",
	} {
		request := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
		if header != "" {
			request.Header.Set("Authorization", header)
		}
		recorder := httptest.NewRecorder()
		server.Routes().ServeHTTP(recorder, request)

		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", name, recorder.Code)
		}
	}
}

func TestMeReturnsTheCallerAndItsDevices(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", "a-solid-password")

	login := post(t, server, "/api/v1/auth/login", loginRequest{
		Username: "thomas", Password: "a-solid-password",
		DeviceName: "phone", Platform: "android",
	})
	var session loginResponse
	if err := json.Unmarshal(login.Body.Bytes(), &session); err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	request.Header.Set("Authorization", "Bearer "+session.Token)
	recorder := httptest.NewRecorder()
	server.Routes().ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", recorder.Code, recorder.Body)
	}

	var me meResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &me); err != nil {
		t.Fatal(err)
	}
	if me.User.Username != "thomas" {
		t.Fatalf("user = %+v", me.User)
	}
	if len(me.Devices) != 1 {
		t.Fatalf("%d devices, want 1", len(me.Devices))
	}
	if !me.Devices[0].Current {
		t.Fatal("the calling device is not flagged as current")
	}
	if me.Devices[0].LastSeenAt == nil {
		t.Fatal("the authenticated request did not record a last-seen date")
	}
	if me.Devices[0].Connected {
		t.Fatal("a device with no live socket is reported as connected")
	}
}

// Two enrolments of the same account are two distinct devices with distinct
// tokens: revoking the tablet must not sign the phone out.
func TestEachEnrolmentIsADistinctDevice(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", "a-solid-password")

	tokens := make(map[string]bool)
	for _, name := range []string{"phone", "tablet"} {
		recorder := post(t, server, "/api/v1/auth/login", loginRequest{
			Username: "thomas", Password: "a-solid-password",
			DeviceName: name, Platform: "android",
		})
		var response loginResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		tokens[response.Token] = true
	}

	if len(tokens) != 2 {
		t.Fatal("the two enrolments share a token")
	}
}
