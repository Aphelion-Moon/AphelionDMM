package server

import (
	"sdmm/internal/aphelion/collab/model"
	"testing"
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
	owner.publishDurable(value)
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
