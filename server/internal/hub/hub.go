// Package hub fans events out to the sockets a user currently holds.
//
// It knows nothing about WebSockets: it hands events to subscribers over
// channels, which keeps it testable without a network and lets the
// transport change without touching the fanout.
package hub

import (
	"sync"

	"github.com/rclsilver-org/home-genie/server/internal/store"
)

// bufferSize is how far a slow socket may lag before it is dropped.
//
// Dropping is the right failure: the client reconnects with its last seq
// and replays what it missed, so a slow consumer costs a reconnection, not
// a lost event. Blocking the publisher instead would let one stalled phone
// hold up the whole server.
const bufferSize = 64

// Subscription is one socket's feed.
type Subscription struct {
	// Events carries the fanout. It is closed when the subscription is
	// dropped, either by the subscriber or because it fell behind.
	Events chan store.Event

	hub      *Hub
	userID   int64
	deviceID int64
	once     sync.Once
}

// Hub tracks who is listening.
type Hub struct {
	mu     sync.RWMutex
	byUser map[int64]map[*Subscription]struct{}
	// byDevice counts live sockets per device. A client can hold two for a
	// moment — it reconnects before the server notices the old one died —
	// and the caller needs to know that before declaring the device offline.
	byDevice map[int64]int
}

// New returns an empty hub.
func New() *Hub {
	return &Hub{
		byUser:   make(map[int64]map[*Subscription]struct{}),
		byDevice: make(map[int64]int),
	}
}

// Subscribe registers a socket for a user's events.
func (h *Hub) Subscribe(userID, deviceID int64) *Subscription {
	subscription := &Subscription{
		Events:   make(chan store.Event, bufferSize),
		hub:      h,
		userID:   userID,
		deviceID: deviceID,
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	if _, ok := h.byUser[userID]; !ok {
		h.byUser[userID] = make(map[*Subscription]struct{})
	}
	h.byUser[userID][subscription] = struct{}{}
	h.byDevice[deviceID]++

	return subscription
}

// Close unregisters the subscription. It is safe to call twice, which
// matters because both the reader and the writer side may end a socket.
func (s *Subscription) Close() {
	s.once.Do(func() {
		s.hub.remove(s)
		close(s.Events)
	})
}

// DeviceID is the device this subscription belongs to.
func (s *Subscription) DeviceID() int64 { return s.deviceID }

func (h *Hub) remove(subscription *Subscription) {
	h.mu.Lock()
	defer h.mu.Unlock()

	subscriptions, ok := h.byUser[subscription.userID]
	if !ok {
		return
	}
	if _, present := subscriptions[subscription]; !present {
		// Close is idempotent, so remove can be reached twice.
		return
	}
	delete(subscriptions, subscription)
	if len(subscriptions) == 0 {
		delete(h.byUser, subscription.userID)
	}

	if h.byDevice[subscription.deviceID] <= 1 {
		delete(h.byDevice, subscription.deviceID)
	} else {
		h.byDevice[subscription.deviceID]--
	}
}

// DeviceSubscribers reports how many live sockets a device holds.
//
// Zero is what makes a device genuinely offline. Anything else means a
// reconnection overlapped, and clearing the connected marker then would
// declare a perfectly healthy phone unreachable — which in turn would
// miscount the reliability figures and fire the wrong monitoring alert.
func (h *Hub) DeviceSubscribers(deviceID int64) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.byDevice[deviceID]
}

// Publish hands an event to every socket the user holds. A subscriber whose
// buffer is full is dropped rather than waited on.
func (h *Hub) Publish(event store.Event) {
	h.mu.RLock()
	subscriptions := make([]*Subscription, 0, len(h.byUser[event.UserID]))
	for subscription := range h.byUser[event.UserID] {
		subscriptions = append(subscriptions, subscription)
	}
	h.mu.RUnlock()

	for _, subscription := range subscriptions {
		select {
		case subscription.Events <- event:
		default:
			// Full: the socket is not keeping up. Drop it; the client will
			// reconnect and replay from its last seq.
			subscription.Close()
		}
	}
}

// PublishAll fans out a batch, typically the same change addressed to every
// member of a channel.
func (h *Hub) PublishAll(events []store.Event) {
	for _, event := range events {
		h.Publish(event)
	}
}

// Subscribers reports how many sockets a user holds, for the diagnostics.
func (h *Hub) Subscribers(userID int64) int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.byUser[userID])
}

// Total reports how many sockets are held overall.
func (h *Hub) Total() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	total := 0
	for _, subscriptions := range h.byUser {
		total += len(subscriptions)
	}
	return total
}
