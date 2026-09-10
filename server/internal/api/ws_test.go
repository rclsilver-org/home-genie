package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// dial opens an authenticated socket against a live test server.
func dial(t *testing.T, base, token string, sinceSeq int64) *websocket.Conn {
	t.Helper()

	url := strings.Replace(base, "http://", "ws://", 1) + "/api/v1/ws"
	if sinceSeq > 0 {
		url += fmt.Sprintf("?since_seq=%d", sinceSeq)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	conn, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}},
	})
	if err != nil {
		t.Fatalf("dialling: %v", err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

// readFrame reads one frame, failing the test on timeout.
func readFrame(t *testing.T, conn *websocket.Conn) frame {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	var f frame
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("decoding %s: %v", data, err)
	}
	return f
}

// readUntil reads frames until one of kind is seen, skipping heartbeats.
func readUntil(t *testing.T, conn *websocket.Conn, kind string) frame {
	t.Helper()
	for i := 0; i < 20; i++ {
		f := readFrame(t, conn)
		if f.Kind == kind {
			return f
		}
	}
	t.Fatalf("frame %q never came", kind)
	return frame{}
}

func liveServer(t *testing.T) (*httptest.Server, *Server, string) {
	t.Helper()
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)

	httpServer := httptest.NewServer(server.Routes())
	t.Cleanup(httpServer.Close)

	token := session(t, server, "thomas", testPassword)
	return httpServer, server, token
}

func TestSocketRequiresAValidToken(t *testing.T) {
	httpServer, _, _ := liveServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := strings.Replace(httpServer.URL, "http://", "ws://", 1) + "/api/v1/ws"
	if _, _, err := websocket.Dial(ctx, url, nil); err == nil {
		t.Fatal("a socket opened without a token")
	}
}

func TestSocketAnnouncesReady(t *testing.T) {
	httpServer, _, token := liveServer(t)
	conn := dial(t, httpServer.URL, token, 0)

	f := readFrame(t, conn)
	if f.Kind != frameReady {
		t.Fatalf("first frame = %q, want %q", f.Kind, frameReady)
	}
}

// The live path: an event produced while the socket is open must arrive.
func TestSocketDeliversLiveEvents(t *testing.T) {
	httpServer, server, token := liveServer(t)
	conn := dial(t, httpServer.URL, token, 0)
	readUntil(t, conn, frameReady)

	call(t, server, http.MethodPost, "/api/v1/channels", token,
		createChannelRequest{Slug: "alerts", Name: "Alertes"})

	f := readUntil(t, conn, "channel.created")
	if f.Seq == 0 {
		t.Fatal("the event carries no seq")
	}

	var payload channelPayload
	if err := json.Unmarshal(f.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.Slug != "alerts" {
		t.Fatalf("payload = %+v", payload)
	}
}

// The contract that makes an aggressive power manager survivable: a socket killed while events
// are produced loses nothing, it replays them on reconnection.
func TestAKilledSocketLosesNothing(t *testing.T) {
	httpServer, server, token := liveServer(t)

	conn := dial(t, httpServer.URL, token, 0)
	ready := readUntil(t, conn, frameReady)

	// The socket dies without a clean close, as a task killer would do.
	conn.CloseNow()

	// Three channels are created while nobody is listening.
	for _, slug := range []string{"alerts", "mediacenter", "homelab"} {
		if r := call(t, server, http.MethodPost, "/api/v1/channels", token,
			createChannelRequest{Slug: slug}); r.Code != http.StatusCreated {
			t.Fatalf("creating %s: %d %s", slug, r.Code, r.Body)
		}
	}

	// Reconnecting from the last seq replays exactly what was missed.
	again := dial(t, httpServer.URL, token, ready.Seq)

	seen := []string{}
	for {
		f := readFrame(t, again)
		if f.Kind == frameReady {
			break
		}
		if f.Kind == frameHeartbeat {
			continue
		}
		var payload channelPayload
		json.Unmarshal(f.Payload, &payload)
		seen = append(seen, payload.Slug)
	}

	if len(seen) != 3 {
		t.Fatalf("replayed %v, want the three missed channels", seen)
	}
}

// Replaying must not hand the same event twice, or the app would show
// duplicates after every reconnection.
func TestReplayDoesNotDuplicate(t *testing.T) {
	httpServer, server, token := liveServer(t)

	conn := dial(t, httpServer.URL, token, 0)
	readUntil(t, conn, frameReady)
	call(t, server, http.MethodPost, "/api/v1/channels", token,
		createChannelRequest{Slug: "alerts"})
	first := readUntil(t, conn, "channel.created")
	conn.CloseNow()

	// Reconnecting from that seq must replay nothing.
	again := dial(t, httpServer.URL, token, first.Seq)
	f := readFrame(t, again)
	if f.Kind != frameReady {
		t.Fatalf("an already-seen event was replayed: %q seq=%d", f.Kind, f.Seq)
	}
	if f.Seq != first.Seq {
		t.Fatalf("ready reports seq %d, want %d", f.Seq, first.Seq)
	}
}

// Every device of a user gets the event: reading on the phone must not
// leave the tablet stale.
func TestEveryDeviceOfTheUserReceives(t *testing.T) {
	httpServer, server, token := liveServer(t)
	secondToken := session(t, server, "thomas", testPassword)

	phone := dial(t, httpServer.URL, token, 0)
	tablet := dial(t, httpServer.URL, secondToken, 0)
	readUntil(t, phone, frameReady)
	readUntil(t, tablet, frameReady)

	call(t, server, http.MethodPost, "/api/v1/channels", token,
		createChannelRequest{Slug: "alerts"})

	for name, conn := range map[string]*websocket.Conn{"phone": phone, "tablet": tablet} {
		if f := readUntil(t, conn, "channel.created"); f.Seq == 0 {
			t.Errorf("%s: event without seq", name)
		}
	}
}

// A socket marks its device connected, which is what turns a delivery to a
// device without a socket into a countable miss.
func TestSocketMarksTheDeviceConnected(t *testing.T) {
	httpServer, server, token := liveServer(t)
	_, repository := server, server.store

	conn := dial(t, httpServer.URL, token, 0)
	readUntil(t, conn, frameReady)

	waitFor(t, func() bool {
		count, _ := repository.ConnectedDeviceCount()
		return count == 1
	}, "the device was never marked connected")

	conn.Close(websocket.StatusNormalClosure, "")

	waitFor(t, func() bool {
		count, _ := repository.ConnectedDeviceCount()
		return count == 0
	}, "the device stayed marked connected after the close")
}

func TestSinceSeqMustBeAPositiveInteger(t *testing.T) {
	httpServer, _, token := liveServer(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	url := strings.Replace(httpServer.URL, "http://", "ws://", 1) + "/api/v1/ws?since_seq=demain"
	if _, _, err := websocket.Dial(ctx, url, &websocket.DialOptions{
		HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}},
	}); err == nil {
		t.Fatal("a malformed since_seq was accepted")
	}
}

// waitFor polls a condition, since the socket bookkeeping happens on the
// server's own goroutine.
func waitFor(t *testing.T, condition func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(message)
}

// The bug seen after a night of running: the client reconnects before the
// server has noticed the old socket died, and closing the dying one erased
// the marker its replacement had just set. The device then looked offline
// while answering every thirty seconds — which falsifies both the metric
// under watch and the count of misses.
func TestAReplacedSocketDoesNotMarkTheDeviceOffline(t *testing.T) {
	server, repository := newTestServer(t)
	withLocalAccount(t, repository, "thomas", testPassword)

	httpServer := httptest.NewServer(server.Routes())
	t.Cleanup(httpServer.Close)

	token := session(t, server, "thomas", testPassword)

	previous := dial(t, httpServer.URL, token, 0)
	readUntil(t, previous, frameReady)
	waitFor(t, func() bool {
		count, _ := repository.ConnectedDeviceCount()
		return count == 1
	}, "the first socket did not mark the device connected")

	// The same device opens a second socket: that is what a client does when
	// it reconnects before the first one was detected closed.
	fresh := dial(t, httpServer.URL, token, 0)
	readUntil(t, fresh, frameReady)

	// Then the dying one closes.
	previous.CloseNow()

	// The device must stay connected: the new socket is alive.
	time.Sleep(300 * time.Millisecond)
	if count, _ := repository.ConnectedDeviceCount(); count != 1 {
		t.Fatalf("connected devices = %d, want 1 — the dead socket silenced the live one", count)
	}

	// And it only goes offline once the last one is gone.
	fresh.CloseNow()
	waitFor(t, func() bool {
		count, _ := repository.ConnectedDeviceCount()
		return count == 0
	}, "the device stayed connected after its last socket closed")
}
