package store

import (
	"errors"
	"testing"
)

func TestPublishTokenResolvesToItsChannel(t *testing.T) {
	s := newTestStore(t)
	owner, _ := s.CreateLocalUser("thomas", "", "hash", true)
	channel, _ := s.CreateChannel("mediacenter", "Mediacenter", "", owner.ID)

	created, err := s.CreatePublishToken(channel.ID, "sonarr", "hash-token")
	if err != nil {
		t.Fatalf("CreatePublishToken: %v", err)
	}
	if created.IsRevoked() {
		t.Fatal("a fresh token is reported as revoked")
	}

	token, target, err := s.PublishTokenByHash("hash-token")
	if err != nil {
		t.Fatalf("PublishTokenByHash: %v", err)
	}
	if token.ID != created.ID || target.ID != channel.ID || target.Slug != "mediacenter" {
		t.Fatalf("wrong resolution: token=%+v channel=%+v", token, target)
	}
}

// Revocation has to be immediate, not a flag callers must remember to test.
func TestRevokedTokenNoLongerResolves(t *testing.T) {
	s := newTestStore(t)
	owner, _ := s.CreateLocalUser("thomas", "", "hash", true)
	channel, _ := s.CreateChannel("mediacenter", "", "", owner.ID)
	token, _ := s.CreatePublishToken(channel.ID, "sonarr", "hash-token")

	if err := s.RevokePublishToken(channel.ID, token.ID); err != nil {
		t.Fatalf("RevokePublishToken: %v", err)
	}
	if _, _, err := s.PublishTokenByHash("hash-token"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a revoked token still resolves: %v", err)
	}

	// Revoking twice reports the absence rather than pretending to succeed.
	if err := s.RevokePublishToken(channel.ID, token.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

// A token belongs to one channel: revoking it from another must not work,
// or an owner could disable a producer on a channel they do not administer.
func TestRevokeIsScopedToTheChannel(t *testing.T) {
	s := newTestStore(t)
	owner, _ := s.CreateLocalUser("thomas", "", "hash", true)
	mine, _ := s.CreateChannel("mediacenter", "", "", owner.ID)
	other, _ := s.CreateChannel("alerts", "", "", owner.ID)
	token, _ := s.CreatePublishToken(mine.ID, "sonarr", "hash-token")

	if err := s.RevokePublishToken(other.ID, token.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the token was revoked from the wrong channel: %v", err)
	}
	if _, _, err := s.PublishTokenByHash("hash-token"); err != nil {
		t.Fatal("the token was revoked despite the wrong channel")
	}
}

// A revoked token stays listed: knowing who could publish, and when they
// last did, is part of the audit trail.
func TestRevokedTokensStayListed(t *testing.T) {
	s := newTestStore(t)
	owner, _ := s.CreateLocalUser("thomas", "", "hash", true)
	channel, _ := s.CreateChannel("mediacenter", "", "", owner.ID)
	live, _ := s.CreatePublishToken(channel.ID, "sonarr", "hash-1")
	dead, _ := s.CreatePublishToken(channel.ID, "radarr", "hash-2")
	if err := s.RevokePublishToken(channel.ID, dead.ID); err != nil {
		t.Fatal(err)
	}

	tokens, err := s.PublishTokensOf(channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 2 {
		t.Fatalf("%d tokens, want 2", len(tokens))
	}
	// Live tokens come first.
	if tokens[0].ID != live.ID || tokens[0].IsRevoked() {
		t.Fatalf("the first token should be the live one: %+v", tokens[0])
	}
	if !tokens[1].IsRevoked() {
		t.Fatal("the revoked token is not flagged")
	}
}

func TestTouchPublishTokenRecordsUse(t *testing.T) {
	s := newTestStore(t)
	owner, _ := s.CreateLocalUser("thomas", "", "hash", true)
	channel, _ := s.CreateChannel("mediacenter", "", "", owner.ID)
	token, _ := s.CreatePublishToken(channel.ID, "sonarr", "hash-token")

	if token.LastUsedAt != nil {
		t.Fatal("a fresh token has a last-used date")
	}
	if err := s.TouchPublishToken(token.ID); err != nil {
		t.Fatalf("TouchPublishToken: %v", err)
	}

	refreshed, _, err := s.PublishTokenByHash("hash-token")
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.LastUsedAt == nil {
		t.Fatal("the use was not recorded")
	}
}
