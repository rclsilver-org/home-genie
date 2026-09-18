package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rclsilver-org/home-genie/server/internal/auth"
	"github.com/rclsilver-org/home-genie/server/internal/store"
)

// The ingest routes land in the next slice; the middleware they will use is
// exercised here through a probe handler, so the guarantee is verified
// rather than assumed.
func publishProbe(server *Server) *http.ServeMux {
	mux := http.NewServeMux()
	mux.Handle("POST /probe/{slug}", server.requirePublishToken(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, channel, ok := PublishTargetFrom(r.Context())
			if !ok {
				http.Error(w, "no target", http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(channel.Slug))
		})))
	mux.Handle("POST /probe", server.requirePublishToken(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})))
	return mux
}

func probe(t *testing.T, server *Server, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, path, nil)
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	recorder := httptest.NewRecorder()
	publishProbe(server).ServeHTTP(recorder, request)
	return recorder
}

// issueChannelAndToken creates a channel and a publish token on it.
func issueChannelAndToken(t *testing.T, s *store.Store, slug string) (store.Channel, string) {
	t.Helper()
	owner, err := s.UserByUsername("thomas")
	if err != nil {
		t.Fatal(err)
	}
	channel, err := s.CreateChannel(slug, slug, "", owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	plain, hashed, err := auth.NewToken(auth.PublishTokenPrefix, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreatePublishToken(channel.ID, "producteur", hashed); err != nil {
		t.Fatal(err)
	}
	return channel, plain
}

func TestPublishTokenResolvesItsChannel(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	_, token := issueChannelAndToken(t, repository, "mediacenter")

	recorder := probe(t, server, "/probe/mediacenter", token)
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", recorder.Code, recorder.Body)
	}
	if recorder.Body.String() != "mediacenter" {
		t.Fatalf("channel = %q", recorder.Body.String())
	}
}

// A mistyped URL must fail loudly rather than publish somewhere else.
func TestPublishTokenRefusesAnotherChannel(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	issueChannelAndToken(t, repository, "mediacenter")
	_, otherToken := issueChannelAndToken(t, repository, "alerts")

	recorder := probe(t, server, "/probe/mediacenter", otherToken)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", recorder.Code)
	}
}

// The two credential kinds must not be interchangeable, in either direction.
func TestADeviceTokenCannotPublish(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	issueChannelAndToken(t, repository, "mediacenter")

	deviceToken := session(t, server, "thomas", testPassword)

	recorder := probe(t, server, "/probe/mediacenter", deviceToken)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("a device token published: status = %d", recorder.Code)
	}
}

func TestAPublishTokenCannotRead(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	_, publishToken := issueChannelAndToken(t, repository, "mediacenter")

	for _, path := range []string{"/api/v1/me", "/api/v1/channels"} {
		recorder := call(t, server, http.MethodGet, path, publishToken, nil)
		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s: a publish token read, status = %d", path, recorder.Code)
		}
	}
}

func TestRevokedPublishTokenIsRejected(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, token := issueChannelAndToken(t, repository, "mediacenter")

	tokens, err := repository.PublishTokensOf(channel.ID)
	if err != nil || len(tokens) != 1 {
		t.Fatalf("tokens = %+v err = %v", tokens, err)
	}
	if err := repository.RevokePublishToken(channel.ID, tokens[0].ID); err != nil {
		t.Fatal(err)
	}

	if recorder := probe(t, server, "/probe/mediacenter", token); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("a revoked token published: status = %d", recorder.Code)
	}
}

func TestMissingOrUnknownPublishTokenIsRejected(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	issueChannelAndToken(t, repository, "mediacenter")

	for name, token := range map[string]string{
		"absent":  "",
		"unknown": "hnp_nonexistent",
	} {
		if recorder := probe(t, server, "/probe/mediacenter", token); recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s: status = %d, want 401", name, recorder.Code)
		}
	}
}

// Using a token records when the producer last published, which is what
// makes a forgotten one visible in the channel's token list.
func TestPublishRecordsLastUse(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)
	channel, token := issueChannelAndToken(t, repository, "mediacenter")

	if recorder := probe(t, server, "/probe/mediacenter", token); recorder.Code != http.StatusOK {
		t.Fatalf("status = %d", recorder.Code)
	}

	tokens, err := repository.PublishTokensOf(channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if tokens[0].LastUsedAt == nil {
		t.Fatal("the use was not recorded")
	}
}
