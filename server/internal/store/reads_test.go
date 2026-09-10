package store

import "testing"

func publishTo(t *testing.T, s *Store, channelID int64, title string) Message {
	t.Helper()
	message, err := s.CreateMessage(NewMessage{ChannelID: channelID, Title: title})
	if err != nil {
		t.Fatal(err)
	}
	return message
}

// The unread count must not leak channels the user does not belong to —
// the join is where that kind of mistake hides.
func TestUnreadCountsOnlyCoverOwnChannels(t *testing.T) {
	s := newTestStore(t)
	thomas, _ := s.CreateLocalUser("thomas", "", "hash", true)
	other, _ := s.CreateLocalUser("other", "", "hash", false)

	mine, _ := s.CreateChannel("mediacenter", "", "", thomas.ID)
	theirs, _ := s.CreateChannel("prive", "", "", other.ID)

	publishTo(t, s, mine.ID, "mine")
	publishTo(t, s, theirs.ID, "not mine")
	publishTo(t, s, theirs.ID, "not mine either")

	counts, err := s.UnreadCounts(thomas.ID)
	if err != nil {
		t.Fatal(err)
	}
	if counts[mine.ID] != 1 {
		t.Fatalf("my channel: %d unread, want 1", counts[mine.ID])
	}
	if _, present := counts[theirs.ID]; present {
		t.Fatal("a channel I am not a member of shows up in my unread")
	}
}

func TestReadMessageIDsBatch(t *testing.T) {
	s := newTestStore(t)
	thomas, _ := s.CreateLocalUser("thomas", "", "hash", true)
	channel, _ := s.CreateChannel("mediacenter", "", "", thomas.ID)

	first := publishTo(t, s, channel.ID, "one")
	second := publishTo(t, s, channel.ID, "two")
	third := publishTo(t, s, channel.ID, "three")

	if _, err := s.MarkRead(second.ID, thomas.ID, nil); err != nil {
		t.Fatal(err)
	}

	read, err := s.ReadMessageIDs(thomas.ID, []int64{first.ID, second.ID, third.ID})
	if err != nil {
		t.Fatal(err)
	}
	if read[first.ID] || !read[second.ID] || read[third.ID] {
		t.Fatalf("wrong read state: %+v", read)
	}

	// An empty batch must not produce invalid SQL.
	if empty, err := s.ReadMessageIDs(thomas.ID, nil); err != nil || len(empty) != 0 {
		t.Fatalf("empty batch: %+v %v", empty, err)
	}
}

// The acknowledgement cursor never goes backwards: an older acknowledgement
// must not redeliver what was already delivered.
func TestRecordDeliveredNeverGoesBackwards(t *testing.T) {
	s := newTestStore(t)
	thomas, _ := s.CreateLocalUser("thomas", "", "hash", true)
	channel, _ := s.CreateChannel("mediacenter", "", "", thomas.ID)
	device, _ := s.CreateDevice(thomas.ID, "phone", "android", "hash-token")

	message := publishTo(t, s, channel.ID, "one")
	event, err := s.AppendEvent(thomas.ID, "message.new",
		map[string]any{"id": message.ID, "title": message.Title})
	if err != nil {
		t.Fatal(err)
	}

	count, err := s.RecordDelivered(thomas.ID, device.ID, event.Seq)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("count = %d, want 1", count)
	}

	// Replay the same acknowledgement, then an older one: nothing more.
	for _, seq := range []int64{event.Seq, event.Seq - 1} {
		again, err := s.RecordDelivered(thomas.ID, device.ID, seq)
		if err != nil {
			t.Fatal(err)
		}
		if again != 0 {
			t.Fatalf("acknowledgement at %d: %d more deliveries", seq, again)
		}
	}

	if got := timelineKindCount(t, s, message.ID, MessageDelivered); got != 1 {
		t.Fatalf("%d \"delivered\" entries, want 1", got)
	}
}

func timelineKindCount(t *testing.T, s *Store, messageID int64, kind string) int {
	t.Helper()
	entries, err := s.TimelineOf(messageID)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, entry := range entries {
		if entry.Kind == kind {
			count++
		}
	}
	return count
}
