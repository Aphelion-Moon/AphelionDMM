package editor

import (
	"context"
	"testing"
	"time"

	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/engine"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
	"sdmm/internal/aphelion/maplint"
	"sdmm/internal/dmapi/dmenv"
	"sdmm/internal/dmapi/dmvars"
)

// TestSessionFixtureLightFixSettlesThenRescans runs "Move light edits on
// fixtures to bulb variables" as the submitting host of a session: the fix is
// one operation, the map stays synchronizing until the server acknowledges it
// (so Map Lint must defer its rescan), and the acknowledged map rescans clean.
func TestSessionFixtureLightFixSettlesThenRescans(t *testing.T) {
	activateLint(t)
	e := selectionEditor(t)
	fixture := &dmvars.MutableVariables{}
	for name, value := range map[string]string{"brightness": "8", "bulb_power": "1", "bulb_colour": `"#f3fffa"`, "dir": "2", "light_color": "null", "light_power": "1"} {
		fixture.Put(name, value)
	}
	e.app.LoadedEnvironment().Objects["/obj/machinery/light"] = &dmenv.Object{Path: "/obj/machinery/light", Vars: fixture.ToImmutable()}
	e.dmm.Tiles[0].InstancesAdd(lintPrefab("/obj/machinery/light", "light_color", `"#d1dfff"`, "light_power", "2", "light_on", "1"))
	e.dmm.Tiles[0].InstancesRegenerate()
	e.initializeCollaboration()
	e.pMap.Snapshot().Sync()
	app := e.app.(*editorTestApp)
	app.runLater = make(chan func(), 32)
	transport := newEditorNetworkTransport()
	network, err := client.NewNetworkExecutor(transport, e.authoritative, e.actorID, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AttachCollaborationExecutor(network); err != nil {
		t.Fatal(err)
	}
	server, err := engine.NewDocument(e.authoritative)
	if err != nil {
		t.Fatal(err)
	}

	var result *LintFixResult
	if err := e.ApplyLintFixes(maplint.DefaultFixes.With(maplint.FixMoveFixtureLight), func(r LintFixResult) { result = &r }); err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Err != nil || result.Tiles != 1 || result.Fixes != 4 {
		t.Fatalf("result = %+v", result)
	}
	if !e.CollaborationSynchronizing() {
		t.Fatal("an unacknowledged fix must report synchronizing so the rescan waits")
	}
	if _, err := e.PrepareLintScan(10); err == nil {
		t.Fatal("scan captured an unacknowledged edit")
	}

	decoded, err := protocol.DecodeClient(mustEditorJSON(t, transport.next(t)))
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := server.Apply(decoded.Payload.(*protocol.OperationSubmitPayload).Operation, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	hash, _ := server.CachedHash()
	if err := network.Receive(protocol.ServerEnvelope{ProtocolVersion: model.ProtocolVersion, MessageID: "accepted", SessionID: "session-1", Type: protocol.ServerOperationAccepted, Payload: mustEditorJSON(t, protocol.OperationAcceptedPayload{Operation: accepted, MapHash: hash})}); err != nil {
		t.Fatal(err)
	}
	app.runScheduled(t)
	for len(app.runLater) > 0 {
		app.runScheduled(t)
	}
	e.ProcessCollaborationUpdates()
	if e.CollaborationSynchronizing() {
		t.Fatal("acknowledged fix still synchronizing")
	}

	var light *dmvars.Variables
	for _, instance := range e.dmm.Tiles[0].Instances() {
		if instance.Prefab().Path() == "/obj/machinery/light" {
			light = instance.Prefab().Vars()
		}
	}
	if light == nil || light.ValueV("bulb_colour", "") != `"#d1dfff"` || light.ValueV("bulb_power", "") != "2" {
		t.Fatal("fixture light edits did not move to bulb variables")
	}
	for _, name := range []string{"light_color", "light_power", "light_on"} {
		if _, explicit := light.ExplicitValue(name); explicit {
			t.Fatalf("fixture kept %s", name)
		}
	}
	run, err := e.PrepareLintScan(10)
	if err != nil {
		t.Fatal(err)
	}
	rescan, err := run(context.Background())
	if err != nil || rescan.Violations != 0 {
		t.Fatalf("rescan = %+v, %v", rescan, err)
	}
}
