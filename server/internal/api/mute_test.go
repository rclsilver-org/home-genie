package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"
)

// A mute belongs to whoever sets it: nobody needs a right to fall silent
// themselves, and nobody can silence somebody else.
func TestAMuteSilencesOnlyItsAuthor(t *testing.T) {
	httpServer, server, alice := liveServer(t)
	repository := server.store
	withLocalAccount(t, repository, "bob", testPassword)

	channel := decode[channelPayload](t, call(t, server, http.MethodPost, "/api/v1/channels",
		alice, createChannelRequest{Slug: "alerts", Name: "Alertes"}))
	publishToken := decode[publishTokenPayload](t, call(t, server, http.MethodPost,
		fmt.Sprintf("/api/v1/channels/%d/tokens", channel.ID), alice,
		createTokenRequest{Name: "alertmanager"}))
	shareChannel(t, repository, channel.ID, "bob", "reader")

	// Bob, a mere reader, needs no right at all to fall silent.
	bob := session(t, server, "bob", testPassword)
	until := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	if r := call(t, server, http.MethodPut, "/api/v1/mute", bob,
		setMuteRequest{MutedUntil: until}); r.Code != http.StatusOK {
		t.Fatalf("status = %d: %s", r.Code, r.Body)
	}

	// Alice asked for nothing.
	conn := dial(t, httpServer.URL, alice, 0)
	readUntil(t, conn, frameReady)

	webhook(t, server, "alerts", publishToken.Token, alertmanagerWebhook{
		Version: "4", Alerts: []alertmanagerAlert{firing("c", "critical")}})

	frame := readUntil(t, conn, eventMessageNew)
	var forAlice messagePayload
	if err := json.Unmarshal(frame.Payload, &forAlice); err != nil {
		t.Fatal(err)
	}
	if forAlice.Silent {
		t.Fatalf("bob's mute silenced alice: %+v", forAlice)
	}

	// And Bob received it silent.
	bobConn := dial(t, httpServer.URL, bob, 0)
	bobFrame := readUntil(t, bobConn, eventMessageNew)
	var forBob messagePayload
	if err := json.Unmarshal(bobFrame.Payload, &forBob); err != nil {
		t.Fatal(err)
	}
	if !forBob.Silent {
		t.Fatalf("bob muted himself and still gets noise: %+v", forBob)
	}

	// Silent does not mean lost: the alert is there, open, for both of them.
	for name, token := range map[string]string{"alice": alice, "bob": bob} {
		alerts := decode[[]alertPayload](t, call(t, server, http.MethodGet,
			"/api/v1/alerts?unacked=1", token, nil))
		if len(alerts) != 1 {
			t.Fatalf("%s: the alert must stay to be dealt with, %+v", name, alerts)
		}
	}

	// Each person's mute is readable by that person alone.
	mine := decode[mutePayload](t, call(t, server, http.MethodGet, "/api/v1/mute", bob, nil))
	if mine.MutedUntil == nil {
		t.Fatal("bob should see his own mute")
	}
	hers := decode[mutePayload](t, call(t, server, http.MethodGet, "/api/v1/mute", alice, nil))
	if hers.MutedUntil != nil {
		t.Fatalf("alice sees bob's mute: %+v", hers)
	}
}
