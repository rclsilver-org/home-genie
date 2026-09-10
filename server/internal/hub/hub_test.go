package hub

import (
	"testing"
	"time"

	"github.com/rclsilver-org/home-notifications/server/internal/store"
)

func event(userID int64, kind string) store.Event {
	return store.Event{UserID: userID, Kind: kind}
}

func TestPublishReachesEverySocketOfTheUser(t *testing.T) {
	h := New()
	phone := h.Subscribe(1, 10)
	tablet := h.Subscribe(1, 11)
	defer phone.Close()
	defer tablet.Close()

	h.Publish(event(1, "channel.created"))

	for name, subscription := range map[string]*Subscription{"phone": phone, "tablet": tablet} {
		select {
		case received := <-subscription.Events:
			if received.Kind != "channel.created" {
				t.Errorf("%s: kind = %q", name, received.Kind)
			}
		case <-time.After(time.Second):
			t.Errorf("%s: received nothing", name)
		}
	}
}

// A user's events must not leak to another user's sockets.
func TestPublishIsScopedToTheUser(t *testing.T) {
	h := New()
	mine := h.Subscribe(1, 10)
	other := h.Subscribe(2, 20)
	defer mine.Close()
	defer other.Close()

	h.Publish(event(1, "channel.created"))

	select {
	case received := <-other.Events:
		t.Fatalf("another user received %q", received.Kind)
	case <-time.After(50 * time.Millisecond):
	}
}

// A socket that stops reading must be dropped, not block the publisher: one
// stalled phone cannot be allowed to hold up the server.
func TestASlowSubscriberIsDroppedNotWaitedOn(t *testing.T) {
	h := New()
	slow := h.Subscribe(1, 10)

	done := make(chan struct{})
	go func() {
		for i := 0; i < bufferSize*2; i++ {
			h.Publish(event(1, "channel.created"))
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on a subscriber that stopped reading")
	}

	// The subscription was closed, so its channel drains then reports closed.
	for range slow.Events { //nolint:revive // draining the buffer
	}

	if h.Subscribers(1) != 0 {
		t.Fatalf("the dropped subscription is still registered (%d)", h.Subscribers(1))
	}
}

// Both the reader and the writer side of a socket may end it.
func TestCloseIsIdempotent(t *testing.T) {
	h := New()
	subscription := h.Subscribe(1, 10)

	subscription.Close()
	subscription.Close() // must not panic on a double close

	if h.Total() != 0 {
		t.Fatalf("Total = %d, want 0", h.Total())
	}
}

func TestCountsFollowSubscriptions(t *testing.T) {
	h := New()
	if h.Total() != 0 {
		t.Fatal("a fresh hub is not empty")
	}

	first := h.Subscribe(1, 10)
	second := h.Subscribe(1, 11)
	third := h.Subscribe(2, 20)

	if h.Subscribers(1) != 2 || h.Subscribers(2) != 1 || h.Total() != 3 {
		t.Fatalf("counts wrong: %d %d %d", h.Subscribers(1), h.Subscribers(2), h.Total())
	}

	first.Close()
	second.Close()
	third.Close()

	if h.Total() != 0 {
		t.Fatalf("Total = %d after closing everything", h.Total())
	}
}

// Publishing to a user with no socket is a no-op, not a panic: it is the
// normal case when the phone is off.
func TestPublishWithoutSubscribers(t *testing.T) {
	New().Publish(event(42, "channel.created"))
}

// A client reconnects before the server has noticed the old socket died:
// the device stays reachable through the new one, and only goes offline
// once the last one is gone.
func TestDeviceSubscribersCountsOverlappingSockets(t *testing.T) {
	h := New()
	if h.DeviceSubscribers(10) != 0 {
		t.Fatal("a device with no socket counts one")
	}

	old := h.Subscribe(1, 10)
	fresh := h.Subscribe(1, 10)
	if got := h.DeviceSubscribers(10); got != 2 {
		t.Fatalf("DeviceSubscribers = %d, want 2", got)
	}

	// The dying one goes; the device stays reachable through the fresh one.
	old.Close()
	if got := h.DeviceSubscribers(10); got != 1 {
		t.Fatalf("DeviceSubscribers = %d after closing the old one, want 1", got)
	}

	fresh.Close()
	if got := h.DeviceSubscribers(10); got != 0 {
		t.Fatalf("DeviceSubscribers = %d after closing everything, want 0", got)
	}
}

// Close is idempotent; the counter must not go down twice.
func TestDeviceSubscribersSurvivesADoubleClose(t *testing.T) {
	h := New()
	first := h.Subscribe(1, 10)
	h.Subscribe(1, 10)

	first.Close()
	first.Close()

	if got := h.DeviceSubscribers(10); got != 1 {
		t.Fatalf("DeviceSubscribers = %d, want 1 — the double Close counted twice", got)
	}
}

// Two devices of the same user count separately: reading on the phone does
// not make the tablet reachable.
func TestDeviceSubscribersAreCountedPerDevice(t *testing.T) {
	h := New()
	telephone := h.Subscribe(1, 10)
	h.Subscribe(1, 11)

	telephone.Close()

	if got := h.DeviceSubscribers(10); got != 0 {
		t.Fatalf("phone: %d, want 0", got)
	}
	if got := h.DeviceSubscribers(11); got != 1 {
		t.Fatalf("tablet: %d, want 1", got)
	}
}
