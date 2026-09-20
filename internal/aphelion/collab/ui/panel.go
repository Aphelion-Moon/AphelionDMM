package ui

import (
	"fmt"
	"strconv"
	"strings"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/app/ui/component"
	w "sdmm/internal/imguiext/widget"

	"github.com/SpaiR/imgui-go"
)

const (
	maxPanelConflictTiles     = 20
	maxPanelConflictPrefabs   = 8
	maxPanelConflictVariables = 8
)

type PanelApp interface {
	CollaborationViewModel() ViewModel
	HasActiveCollaboration() bool
	DoLeaveCollaborationSession()
	DoRetryCollaborationSession()
	DoUpdateCollaborationDisplayName(string)
	DoCopyCollaborationInvitation(InvitationRole, string)
	DoResolveCollaborationConflict(model.OperationID, ConflictAction)
}

type Panel struct {
	component.Component

	app         PanelApp
	inviteeName string
	displayName string
}

func (panel *Panel) Init(app PanelApp) {
	panel.app = app
	panel.inviteeName = "Collaborator"
	panel.displayName = "Mapper"
}

func (panel *Panel) Process(int32) {
	if !panel.app.HasActiveCollaboration() {
		imgui.TextDisabled("No active collaboration session")
		return
	}
	view := panel.app.CollaborationViewModel()
	sessionLabel := view.SessionLabel
	if sessionLabel == "" {
		sessionLabel = "Pending"
	}
	imgui.Text("Session: " + sessionLabel)
	imgui.Text("Role: " + view.RoleLabel)
	imgui.Text(view.RevisionLabel)
	imgui.Text("Status: " + view.SyncLabel)
	if view.ErrorText != "" {
		imgui.Separator()
		imgui.TextWrapped("Error: " + view.ErrorText)
	}
	imgui.Separator()
	imgui.Text("Your display name")
	w.InputTextWithHint("##collaboration-display-name", "Display name", &panel.displayName).Width(-1).Build()
	displayName := strings.TrimSpace(panel.displayName)
	w.Disabled(displayName == "", w.Button("Update Name", func() {
		panel.app.DoUpdateCollaborationDisplayName(displayName)
	})).Build()

	imgui.Separator()
	imgui.Text("Participants")
	if len(view.Participants) == 0 {
		imgui.TextDisabled("No participant details available")
	}
	for _, participant := range view.Participants {
		label := participant.Label
		if participant.Status != "" {
			label += " - " + participant.Status
		}
		imgui.BulletText(label)
	}

	if view.CanAdminister {
		imgui.Separator()
		imgui.Text("Invite a participant")
		w.InputTextWithHint("##collaboration-invitee-name", "Display name", &panel.inviteeName).Width(-1).Build()
		inviteDisabled := !view.CanCopyInvite || strings.TrimSpace(panel.inviteeName) == ""
		w.Disabled(inviteDisabled, w.Button("Copy Editor Invite", func() {
			panel.app.DoCopyCollaborationInvitation(InvitationRoleEditor, strings.TrimSpace(panel.inviteeName))
		})).Build()
		w.Disabled(inviteDisabled, w.Button("Copy Viewer Invite", func() {
			panel.app.DoCopyCollaborationInvitation(InvitationRoleViewer, strings.TrimSpace(panel.inviteeName))
		})).Build()
		imgui.TextWrapped("Invites are short-lived and can be used once.")
	}

	if len(view.ConflictSummaries) != 0 || view.HiddenConflictCount != 0 {
		imgui.Separator()
		imgui.Text("Conflicts")
		imgui.TextWrapped("Inspect or export retained drafts before rebuilding or discarding them. Export saves a recovery reference; it does not apply the edit.")
		for index, conflict := range view.Conflicts {
			imgui.TextWrapped(view.ConflictSummaries[index])
			imgui.TextDisabled(fmt.Sprintf("Recorded at revision %d", conflict.Revision))
			panel.renderConflictValues(conflict, index)
			buttonSuffix := "##conflict-" + strconv.Itoa(index)
			w.Button("Refresh"+buttonSuffix, func() { panel.app.DoResolveCollaborationConflict(conflict.OperationID, ConflictActionRefresh) }).Build()
			imgui.SameLine()
			w.Button("Discard"+buttonSuffix, func() { panel.app.DoResolveCollaborationConflict(conflict.OperationID, ConflictActionDiscard) }).Build()
			imgui.SameLine()
			w.Button("Rebuild"+buttonSuffix, func() { panel.app.DoResolveCollaborationConflict(conflict.OperationID, ConflictActionRebuild) }).Build()
			imgui.SameLine()
			w.Button("Export Draft"+buttonSuffix, func() { panel.app.DoResolveCollaborationConflict(conflict.OperationID, ConflictActionExport) }).Build()
		}
		if view.HiddenConflictCount != 0 {
			imgui.TextDisabled(fmt.Sprintf("%d additional conflicts hidden", view.HiddenConflictCount))
		}
	}

	imgui.Separator()
	if view.ShowReconnect {
		w.Disabled(!view.CanReconnect, w.Button("Retry Reconnect", panel.app.DoRetryCollaborationSession)).Build()
		imgui.SameLine()
	}
	w.Disabled(!view.CanLeave, w.Button("Leave Session", panel.app.DoLeaveCollaborationSession)).Build()
	if len(view.Conflicts) != 0 {
		imgui.TextWrapped("Resolve or explicitly discard retained drafts before leaving or closing the project.")
	}
}

func (panel *Panel) renderConflictValues(conflict ConflictView, conflictIndex int) {
	panel.renderTileValues("Draft before", conflict.DraftBefore, conflictIndex)
	panel.renderTileValues("Draft intended values", conflict.DraftAfter, conflictIndex)
	panel.renderTileValues("Authoritative values", conflict.Values, conflictIndex)
}

func (panel *Panel) renderTileValues(title string, values []AuthoritativeTileView, conflictIndex int) {
	label := fmt.Sprintf("%s (%d tiles)##conflict-values-%d-%s", title, len(values), conflictIndex, title)
	if !imgui.CollapsingHeader(label) {
		return
	}
	visibleTiles := len(values)
	if visibleTiles > maxPanelConflictTiles {
		visibleTiles = maxPanelConflictTiles
	}
	for tileIndex := 0; tileIndex < visibleTiles; tileIndex++ {
		tile := values[tileIndex]
		imgui.BulletText(fmt.Sprintf("(%d, %d, %d)", tile.Coord.X, tile.Coord.Y, tile.Coord.Z))
		if len(tile.Prefabs) == 0 {
			imgui.TextDisabled("  Empty tile")
		}
		visiblePrefabs := len(tile.Prefabs)
		if visiblePrefabs > maxPanelConflictPrefabs {
			visiblePrefabs = maxPanelConflictPrefabs
		}
		for prefabIndex := 0; prefabIndex < visiblePrefabs; prefabIndex++ {
			prefab := tile.Prefabs[prefabIndex]
			imgui.Text("  " + conflictPreviewText(prefab.Path))
			visibleVariables := len(prefab.Variables)
			if visibleVariables > maxPanelConflictVariables {
				visibleVariables = maxPanelConflictVariables
			}
			for variableIndex := 0; variableIndex < visibleVariables; variableIndex++ {
				variable := prefab.Variables[variableIndex]
				imgui.TextWrapped(fmt.Sprintf("    %s = %s", conflictPreviewText(variable.Name), conflictPreviewText(variable.Value)))
			}
			if hidden := len(prefab.Variables) - visibleVariables; hidden != 0 {
				imgui.TextDisabled(fmt.Sprintf("    %d additional variables hidden", hidden))
			}
		}
		if hidden := len(tile.Prefabs) - visiblePrefabs; hidden != 0 {
			imgui.TextDisabled(fmt.Sprintf("  %d additional prefabs hidden", hidden))
		}
	}
	if hidden := len(values) - visibleTiles; hidden != 0 {
		imgui.TextDisabled(fmt.Sprintf("%d additional tiles hidden; export the draft for complete before and intended values", hidden))
	}
}

func conflictPreviewText(value string) string {
	count := 0
	for index := range value {
		if count == 1024 {
			return value[:index] + "… (preview truncated)"
		}
		count++
	}
	return value
}
