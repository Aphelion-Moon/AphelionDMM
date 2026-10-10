package server

import (
	"bytes"
	"strings"
	"testing"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func TestSharedPublicationDetachesCallerAndPublicSubscriber(t *testing.T) {
	owner := &DocumentOwner{}
	shared, cancel, err := owner.subscribeSharedDurable(2)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()
	public, cancelPublic, err := owner.subscribeDurable(2)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelPublic()
	value := model.AcceptedOperation{Operation: model.Operation{Changes: []model.TileChange{{After: model.TileState{Prefabs: []model.PrefabState{{Path: "/obj/unknown", Vars: map[string]string{"raw": "captured"}}}}}}}, Revision: 1}
	owner.publishDurable(value, "")
	value.Changes[0].After.Prefabs[0].Vars["raw"] = "caller mutation"
	external := <-public
	external.Changes[0].After.Prefabs[0].Vars["raw"] = "external subscriber mutation"
	internal := <-shared
	if internal.accepted.Changes[0].After.Prefabs[0].Vars["raw"] != "captured" {
		t.Fatal("network publication aliases mutable caller")
	}
	owner.closeDurable()
	if _, open := <-shared; open {
		t.Fatal("shared subscriber survived owner close")
	}
}

// Every connection of a session receives the committed hash with the event
// and the same encoded acceptance, built once.
func TestSharedPublicationCarriesHashAndEncodesOnce(t *testing.T) {
	owner := &DocumentOwner{}
	first, cancelFirst, _ := owner.subscribeSharedDurable(1)
	defer cancelFirst()
	second, cancelSecond, _ := owner.subscribeSharedDurable(1)
	defer cancelSecond()
	hash := strings.Repeat("b", 64)
	owner.publishDurable(model.AcceptedOperation{Operation: model.Operation{OperationID: "01890f3e-7b5c-7abc-8def-0123456789ab"}, Revision: 1}, hash)
	a, b := <-first, <-second
	if a != b || a.mapHash != hash {
		t.Fatal("connections did not share one event carrying the committed hash")
	}
	envelope := protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "accepted-x", SessionID: "session", Type: protocol.ServerOperationAccepted}
	payload := protocol.OperationAcceptedPayload{Operation: a.accepted, MapHash: hash}
	encoded, shared, err := a.sharedJSON("session", envelope, payload)
	want, _ := marshalServerEnvelope(envelope, payload)
	if err != nil || !shared || !bytes.Equal(encoded, want) {
		t.Fatalf("shared encoding differs from the per-connection encoding: %v", err)
	}
	again, _, _ := b.sharedJSON("session", envelope, payload)
	if &again[0] != &encoded[0] {
		t.Fatal("acceptance was encoded more than once")
	}
	if _, shared, _ := a.sharedJSON("other-session", envelope, payload); shared {
		t.Fatal("encoding shared across sessions")
	}
}
