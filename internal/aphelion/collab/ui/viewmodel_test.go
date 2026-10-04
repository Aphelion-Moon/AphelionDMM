package ui

import (
	"errors"
	"testing"

	"sdmm/internal/aphelion/collab/client"
	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/aphelion/collab/protocol"
)

func TestBuildViewModelCapabilitiesOrderingAndAccessibleStatus(t *testing.T) {
	t.Parallel()

	view := BuildViewModel(SessionStatus{
		SessionID:      "session-1",
		Role:           "owner",
		State:          client.StateCaughtUp,
		Revision:       12,
		InviteReady:    true,
		ReconnectReady: true,
		Participants: []protocol.ParticipantPresence{
			{ActorID: model.ActorID("01890f3e-7b5c-7abc-8def-0123456789bb"), DisplayName: "Zed"},
			{ActorID: model.ActorID("01890f3e-7b5c-7abc-8def-0123456789bc"), DisplayName: "Alpha"},
			{ActorID: model.ActorID("01890f3e-7b5c-7abc-8def-0123456789bd")},
		},
	})
	if view.SyncLabel != "Caught up" || view.RevisionLabel != "Revision 12" {
		t.Fatalf("status labels = %q, %q", view.SyncLabel, view.RevisionLabel)
	}
	if !view.CanEdit || !view.CanAdminister || !view.CanCopyInvite || !view.CanLeave || view.CanReconnect {
		t.Fatalf("capabilities = %#v", view)
	}
	if len(view.Participants) != 3 || view.Participants[0].Label != "Alpha" || view.Participants[1].Label != "Zed" || view.Participants[2].Label == "" {
		t.Fatalf("participants = %#v", view.Participants)
	}
}

func TestBuildViewModelBoundsConflictsAndRedactsErrors(t *testing.T) {
	t.Parallel()

	conflicts := make([]client.Conflict, maxVisibleConflicts+2)
	for index := range conflicts {
		conflicts[index] = client.Conflict{OperationID: model.OperationID("01890f3e-7b5c-7abc-8def-0123456789bb"), Code: "precondition_failed", Message: "authoritative value changed"}
	}
	conflicts[0].AuthoritativeValues = []model.Tile{{Coord: model.Coord{X: 1, Y: 1, Z: 1}, State: model.TileState{Prefabs: []model.PrefabState{{Path: "/obj/secret-token", Vars: map[string]string{"value": "secret-token"}}}}}}
	previews := make([]client.ConflictPreview, maxVisibleConflicts)
	for index := range previews {
		previews[index] = client.PreviewConflict(conflicts[index], conflictPreviewLimits())
	}
	view := BuildViewModel(SessionStatus{State: client.StateConflict, Role: "viewer", ConflictPreviews: previews, ConflictCount: len(conflicts), Err: errors.New("request used secret-token"), SensitiveValues: []string{"secret-token"}})
	if len(view.ConflictSummaries) != maxVisibleConflicts || len(view.Conflicts) != maxVisibleConflicts || view.HiddenConflictCount != 2 {
		t.Fatalf("conflict bounds = %d summaries, %d actionable, %d hidden", len(view.ConflictSummaries), len(view.Conflicts), view.HiddenConflictCount)
	}
	if view.ErrorText != "request used [redacted]" || view.CanEdit || view.CanAdminister {
		t.Fatalf("error/capabilities = %q %#v", view.ErrorText, view)
	}
	redactedPrefab := view.Conflicts[0].Values[0].Prefabs[0]
	if redactedPrefab.Path != "/obj/[redacted]" || redactedPrefab.Variables[0].Value != "[redacted]" {
		t.Fatalf("authoritative conflict values were not redacted: %#v", redactedPrefab)
	}
	if previews[0].Values[0].Prefabs[0].Path != "/obj/secret-token" || previews[0].Values[0].Prefabs[0].Variables[0].Value != "secret-token" {
		t.Fatal("view redaction mutated reusable status")
	}
}

func TestBuildViewModelShowsReconnectControlWhileReconnecting(t *testing.T) {
	t.Parallel()

	view := BuildViewModel(SessionStatus{
		State:          client.StateReconnecting,
		ReconnectReady: false,
	})
	if !view.ShowReconnect || view.CanReconnect {
		t.Fatalf("reconnect capabilities during automatic retry = %#v", view)
	}

	view = BuildViewModel(SessionStatus{
		State:          client.StateReconnecting,
		ReconnectReady: true,
	})
	if !view.ShowReconnect || !view.CanReconnect {
		t.Fatalf("reconnect capabilities after exhausted retries = %#v", view)
	}
}
