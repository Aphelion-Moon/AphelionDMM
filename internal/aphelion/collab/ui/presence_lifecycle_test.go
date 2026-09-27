package ui

import (
	"fmt"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func TestSessionClientExpiresPresenceForBothViews(t *testing.T) {
	for _, readStatusFirst := range []bool{true, false} {
		t.Run(fmt.Sprint(readStatusFirst), func(t *testing.T) {
			now := time.Unix(100, 0)
			client := NewSessionClient(SessionClientConfig{Now: func() time.Time { return now }})
			const oldActors = 4096
			participants := make([]protocol.ParticipantPresence, oldActors)
			for i := range participants {
				participants[i] = protocol.ParticipantPresence{
					ActorID: model.ActorID(fmt.Sprintf("01890f3e-7b5c-7abc-8def-%012d", i)),
					Cursor:  coord(1, 1, 1), Status: "active",
				}
			}
			client.recordPresenceSnapshot(&protocol.PresenceSnapshotPayload{Participants: participants})
			now = now.Add(30 * time.Second)
			if got := len(client.Status().Participants); got != oldActors {
				t.Fatalf("presence expired before the overlay boundary: %d", got)
			}
			// A fresh update keeps this actor visible while all other actors age out.
			actor := participants[0].ActorID
			client.recordPresenceUpdate(&protocol.ServerPresenceUpdatePayload{ActorID: actor, Sequence: 2, Cursor: coord(2, 1, 1), Status: "active"})
			now = now.Add(time.Nanosecond)
			if readStatusFirst {
				if got := client.Status().Participants; len(got) != 1 || got[0].ActorID != actor {
					t.Fatalf("status retained stale actors: %d", len(got))
				}
			}
			observed := client.ObservedPresence()
			if len(observed) != 1 || observed[0].Presence.ActorID != actor {
				t.Fatalf("overlay source retained stale actors: %d", len(observed))
			}
			if got := client.Status().Participants; len(got) != 1 || got[0].ActorID != actor {
				t.Fatalf("status retained stale actors: %d", len(got))
			}
			if len(client.participants) != 1 {
				t.Fatal("expired presence was hidden but retained in memory")
			}
			// Expiry is ephemeral: even an equal sequence can restore presence
			// after a lossy interval or reconnect; no durable state is involved.
			restored := participants[1].ActorID
			client.recordPresenceUpdate(&protocol.ServerPresenceUpdatePayload{ActorID: restored, Cursor: coord(3, 1, 1), Status: "active"})
			if got := len(client.Status().Participants); got != 2 {
				t.Fatalf("fresh presence did not restore the actor: %d", got)
			}
			now = now.Add(31 * time.Second)
			if len(client.ObservedPresence()) != 0 || len(client.Status().Participants) != 0 || len(client.participants) != 0 {
				t.Fatal("idle presence did not expire")
			}
		})
	}
}

func TestSessionClientStatusDetachesPresence(t *testing.T) {
	client := NewSessionClient(SessionClientConfig{})
	actor := model.ActorID("01890f3e-7b5c-7abc-8def-0123456789ad")
	client.recordPresenceUpdate(&protocol.ServerPresenceUpdatePayload{ActorID: actor, Cursor: coord(1, 2, 1),
		Selection: &protocol.PresenceSelection{Min: model.Coord{X: 1, Y: 1, Z: 1}, Max: model.Coord{X: 2, Y: 2, Z: 1}}, Status: "active"})
	status := client.Status()
	status.Participants[0].Cursor.X = 99
	status.Participants[0].Selection.Min.X = 99
	observed := client.ObservedPresence()
	if observed[0].Presence.Cursor.X != 1 || observed[0].Presence.Selection.Min.X != 1 {
		t.Fatal("status exposed mutable internal presence")
	}
}
