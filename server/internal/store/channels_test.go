package store

import (
	"errors"
	"testing"
	"time"
)

func TestValidateSlug(t *testing.T) {
	valid := []string{"a", "alerts", "alerts-critical", "homelab-info", "arr2"}
	for _, slug := range valid {
		if err := ValidateSlug(slug); err != nil {
			t.Errorf("ValidateSlug(%q) = %v, want nil", slug, err)
		}
	}

	invalid := []string{
		"",            // empty
		"Alerts",      // uppercase
		"-alerts",     // starts with a dash
		"alerts-",     // ends with a dash
		"alerts/prod", // slash: it would break the publish path
		"alerts prod", // space
		"api",         // reserved: it would shadow the whole API
		"healthz",     // reserved
	}
	for _, slug := range invalid {
		if err := ValidateSlug(slug); err == nil {
			t.Errorf("ValidateSlug(%q) accepted an unusable slug", slug)
		}
	}
}

func TestCreateChannelMakesTheCreatorOwner(t *testing.T) {
	s := newTestStore(t)
	user, _ := s.CreateLocalUser("thomas", "", "hash", true)

	channel, err := s.CreateChannel("alerts-critical", "Critical alerts", "", user.ID)
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	role, err := s.RoleOn(channel.ID, user.ID)
	if err != nil {
		t.Fatalf("RoleOn: %v", err)
	}
	if role != RoleOwner {
		t.Fatalf("role = %q, want owner", role)
	}
	if !role.CanPublish() || !role.CanAdminister() {
		t.Fatal("an owner can neither publish nor administer")
	}
}

func TestDuplicateSlugIsAConflict(t *testing.T) {
	s := newTestStore(t)
	user, _ := s.CreateLocalUser("thomas", "", "hash", true)

	if _, err := s.CreateChannel("alerts", "", "", user.ID); err != nil {
		t.Fatal(err)
	}
	_, err := s.CreateChannel("alerts", "", "", user.ID)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

// A rejected creation must leave nothing behind, since the channel and its
// first owner are written together.
func TestFailedCreationLeavesNoChannel(t *testing.T) {
	s := newTestStore(t)

	// User 999 does not exist: the membership insert violates the foreign key.
	if _, err := s.CreateChannel("alerts", "", "", 999); err == nil {
		t.Fatal("a channel with a non-existent owner was created")
	}
	if _, err := s.ChannelBySlug("alerts"); !errors.Is(err, ErrNotFound) {
		t.Fatal("the channel survived the failed transaction")
	}
}

func TestRoleOnIsNotFoundForANonMember(t *testing.T) {
	s := newTestStore(t)
	owner, _ := s.CreateLocalUser("thomas", "", "hash", true)
	stranger, _ := s.CreateLocalUser("other", "", "hash", false)
	channel, _ := s.CreateChannel("alerts", "", "", owner.ID)

	if _, err := s.RoleOn(channel.ID, stranger.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSetMemberAddsThenChangesTheRole(t *testing.T) {
	s := newTestStore(t)
	owner, _ := s.CreateLocalUser("thomas", "", "hash", true)
	other, _ := s.CreateLocalUser("other", "", "hash", false)
	channel, _ := s.CreateChannel("alerts", "", "", owner.ID)

	if err := s.SetMember(channel.ID, other.ID, RoleReader); err != nil {
		t.Fatalf("SetMember: %v", err)
	}
	role, _ := s.RoleOn(channel.ID, other.ID)
	if role != RoleReader {
		t.Fatalf("role = %q, want reader", role)
	}
	if role.CanPublish() {
		t.Fatal("a reader can publish")
	}

	if err := s.SetMember(channel.ID, other.ID, RoleWriter); err != nil {
		t.Fatalf("SetMember (update): %v", err)
	}
	role, _ = s.RoleOn(channel.ID, other.ID)
	if role != RoleWriter {
		t.Fatalf("role = %q, want writer", role)
	}
	if !role.CanPublish() || role.CanAdminister() {
		t.Fatal("a writer should publish but not administer")
	}

	members, err := s.MembersOf(channel.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(members) != 2 {
		t.Fatalf("%d members, want 2 (the update duplicated the row)", len(members))
	}
}

func TestSetMemberRejectsAnUnknownRole(t *testing.T) {
	s := newTestStore(t)
	owner, _ := s.CreateLocalUser("thomas", "", "hash", true)
	channel, _ := s.CreateChannel("alerts", "", "", owner.ID)

	if err := s.SetMember(channel.ID, owner.ID, Role("admin")); err == nil {
		t.Fatal("an unknown role was accepted")
	}
}

// A channel with no owner could only be repaired by hand on the database.
func TestRemovingTheLastOwnerIsRefused(t *testing.T) {
	s := newTestStore(t)
	owner, _ := s.CreateLocalUser("thomas", "", "hash", true)
	channel, _ := s.CreateChannel("alerts", "", "", owner.ID)

	err := s.RemoveMember(channel.ID, owner.ID)
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}

	// With a second owner, the first can go.
	second, _ := s.CreateLocalUser("other", "", "hash", false)
	if err := s.SetMember(channel.ID, second.ID, RoleOwner); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveMember(channel.ID, owner.ID); err != nil {
		t.Fatalf("RemoveMember: %v", err)
	}
}

func TestMembershipsOfListsOnlyOwnChannels(t *testing.T) {
	s := newTestStore(t)
	thomas, _ := s.CreateLocalUser("thomas", "", "hash", true)
	other, _ := s.CreateLocalUser("other", "", "hash", false)

	mine, _ := s.CreateChannel("alerts", "", "", thomas.ID)
	s.CreateChannel("prive", "", "", other.ID)

	memberships, err := s.MembershipsOf(thomas.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(memberships) != 1 || memberships[0].Channel.ID != mine.ID {
		t.Fatalf("memberships = %+v", memberships)
	}
	if memberships[0].Role != RoleOwner {
		t.Fatalf("role = %q", memberships[0].Role)
	}
}

func TestUpdateChannelSetsAndClearsMute(t *testing.T) {
	s := newTestStore(t)
	owner, _ := s.CreateLocalUser("thomas", "", "hash", true)
	channel, _ := s.CreateChannel("alerts", "", "", owner.ID)

	until := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	muted := &until
	if err := s.UpdateChannel(channel.ID, nil, nil, &muted); err != nil {
		t.Fatalf("UpdateChannel: %v", err)
	}

	refreshed, _ := s.ChannelByID(channel.ID)
	if refreshed.MutedUntil == nil {
		t.Fatal("the mute was not recorded")
	}
	if !refreshed.IsMuted(time.Now().UTC()) {
		t.Fatal("the channel is not reported as muted")
	}
	if refreshed.IsMuted(until.Add(time.Minute)) {
		t.Fatal("the channel is still muted past its deadline")
	}

	// A pointer to nil clears the field; a nil pointer would leave it alone.
	var cleared *time.Time
	if err := s.UpdateChannel(channel.ID, nil, nil, &cleared); err != nil {
		t.Fatal(err)
	}
	refreshed, _ = s.ChannelByID(channel.ID)
	if refreshed.MutedUntil != nil {
		t.Fatal("the mute was not cleared")
	}
}

func TestDeleteChannelCascades(t *testing.T) {
	s := newTestStore(t)
	owner, _ := s.CreateLocalUser("thomas", "", "hash", true)
	channel, _ := s.CreateChannel("alerts", "", "", owner.ID)
	if _, err := s.CreatePublishToken(channel.ID, "diun", "hash-token"); err != nil {
		t.Fatal(err)
	}

	if err := s.DeleteChannel(channel.ID); err != nil {
		t.Fatalf("DeleteChannel: %v", err)
	}
	if _, err := s.ChannelByID(channel.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("the channel survived")
	}
	if _, _, err := s.PublishTokenByHash("hash-token"); !errors.Is(err, ErrNotFound) {
		t.Fatal("the token survived its channel")
	}
	if err := s.DeleteChannel(channel.ID); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleting twice did not report the absence")
	}
}
