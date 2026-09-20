package ui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	collabclient "sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func TestSessionDraftExportPreservesIntentAndGuardsClose(t *testing.T) {
	snapshot := controllerSnapshot(t)
	actorID, err := model.NewActorID()
	if err != nil {
		t.Fatal(err)
	}
	transport := collabclient.NewWebSocketTransport(collabclient.TransportConfig{})
	network, err := collabclient.NewNetworkExecutor(transport, snapshot, actorID, "session-1")
	if err != nil {
		t.Fatal(err)
	}
	client := NewSessionClient(SessionClientConfig{})
	client.network = network
	client.administrationToken = "private-admin-token"
	client.resumptionToken = "private-resume-token"
	controller := NewController(nil, client)
	controller.active = true
	permit, err := controller.BeginProjectReplacement()
	if err != nil {
		t.Fatal(err)
	}
	draft := sessionConflictOperation(t, snapshot, actorID)
	draft.Changes[0].After.Prefabs[0].Path = "/obj/unknown_preserved_type"
	draft.Changes[0].After.Prefabs[0].Vars["large"] = strings.Repeat("雪", protocol.MaxMessageBytes)
	// A locally rejected action must remain exportable even beyond wire limits.
	if _, err := network.Execute(context.Background(), draft); err == nil || errors.Is(err, collabclient.ErrTransportNotConnected) {
		t.Fatalf("oversized operation not rejected before send: %v", err)
	}
	if _, err := controller.BeginProjectReplacement(); !errors.Is(err, ErrRetainedDrafts) {
		t.Fatalf("begin close = %v", err)
	}
	if err := controller.CompleteProjectReplacement(context.Background(), permit); !errors.Is(err, ErrRetainedDrafts) {
		t.Fatalf("delayed close = %v", err)
	}
	if !controller.Active() || client.network != network {
		t.Fatal("blocked close destroyed the session")
	}
	path := filepath.Join(t.TempDir(), "draft.json")
	if err := os.WriteFile(path, []byte("previous export"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := client.ExportConflict(draft.OperationID, path); err != nil {
		t.Fatal(err)
	}
	encoded, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var exported struct {
		FormatVersion    int             `json:"format_version"`
		Operation        model.Operation `json:"operation"`
		RecordedRevision model.Revision  `json:"recorded_revision"`
		RecordedMapHash  string          `json:"recorded_map_hash"`
	}
	if err := json.Unmarshal(encoded, &exported); err != nil {
		t.Fatal(err)
	}
	if exported.FormatVersion != 1 || !reflect.DeepEqual(exported.Operation, draft) || exported.RecordedRevision != snapshot.Revision || exported.RecordedMapHash != mustSnapshotHash(t, snapshot) {
		t.Fatal("export changed draft data or omitted authority metadata")
	}
	for _, secret := range []string{client.administrationToken, client.resumptionToken} {
		if strings.Contains(string(encoded), secret) {
			t.Fatal("export exposed session credentials")
		}
	}
	if err := client.ExportConflict("missing", path); err == nil {
		t.Fatal("missing draft exported")
	}
	if after, err := os.ReadFile(path); err != nil || string(after) != string(encoded) {
		t.Fatal("failed export replaced prior file")
	}
	if err := client.ExportConflict(draft.OperationID, filepath.Join(path, "invalid.json")); err == nil {
		t.Fatal("invalid destination accepted")
	}
	if len(network.Conflicts()) != 1 || network.HasUnacknowledgedOperations() {
		t.Fatal("export discarded or queued intent")
	}
	if _, err := client.DiscardConflict(context.Background(), draft.OperationID); err != nil {
		t.Fatal(err)
	}
	if err := controller.CompleteProjectReplacement(context.Background(), permit); err != nil {
		t.Fatal(err)
	}
	if controller.Active() {
		t.Fatal("explicit discard did not unblock close")
	}
}

func TestDraftPreviewPreservesStackOrderAndRedactsValues(t *testing.T) {
	state := model.TileState{Prefabs: []model.PrefabState{
		{StableID: "z", Path: "/obj/secret", Vars: map[string]string{"name": "secret"}},
		{StableID: "a", Path: "/turf/example"},
	}}
	conflict := collabclient.Conflict{Draft: model.Operation{Changes: []model.TileChange{{Coord: model.Coord{X: 1, Y: 1, Z: 1}, Before: state, After: state}}}}
	view := BuildViewModel(SessionStatus{SessionID: "session", State: collabclient.StateDisconnected, Conflicts: []collabclient.Conflict{conflict}, SensitiveValues: []string{"secret"}})
	if view.CanLeave {
		t.Fatal("leave offered with retained draft")
	}
	for _, values := range [][]AuthoritativeTileView{view.Conflicts[0].DraftBefore, view.Conflicts[0].DraftAfter} {
		if len(values) != 1 || len(values[0].Prefabs) != 2 || values[0].Prefabs[0].StableID != "z" || values[0].Prefabs[0].Path != "/obj/[redacted]" || values[0].Prefabs[0].Variables[0].Value != "[redacted]" {
			t.Fatal("draft preview reordered the stack or failed to redact")
		}
	}
	if state.Prefabs[0].Path != "/obj/secret" {
		t.Fatal("preview mutated retained data")
	}
	if !BuildViewModel(SessionStatus{SessionID: "session", State: collabclient.StateDisconnected}).CanLeave {
		t.Fatal("disconnected session cannot be left after resolving drafts")
	}
}
