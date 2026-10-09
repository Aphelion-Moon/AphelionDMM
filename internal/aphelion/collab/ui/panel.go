package ui

import (
	"fmt"
	"strings"

	"sdmm/internal/aphelion/collab/model"
	"sdmm/internal/app/ui/component"
	"sdmm/internal/app/ui/uikit"
	w "sdmm/internal/imguiext/widget"

	"github.com/SpaiR/imgui-go"
)

const (
	maxPanelConflictTiles     = 20
	maxPanelConflictPrefabs   = 8
	maxPanelConflictVariables = 8
)

type PanelApp interface {
	CollaborationViewModel(int) ViewModel
	HasActiveCollaboration() bool
	DoLeaveCollaborationSession()
	DoRetryCollaborationSession()
	DoUpdateCollaborationDisplayName(string)
	DoCopyCollaborationInvitation(InvitationRole, string)
	DoResolveCollaborationConflict(model.OperationID, ConflictAction)
	DoResolveAllCollaborationConflicts(BulkDraftAction, bool)
}

type Panel struct {
	component.Component

	app          PanelApp
	inviteeName  string
	displayName  string
	conflictPage int
	sessionID    string
	bulk         draftBulkControls
	kept         bool
}

func (panel *Panel) Init(app PanelApp) {
	panel.app = app
	panel.inviteeName = "Collaborator"
	panel.displayName = "Mapper"
}

func (panel *Panel) Process(int32) {
	if !panel.app.HasActiveCollaboration() {
		panel.conflictPage, panel.sessionID = 0, ""
		uikit.EmptyState("No active collaboration session. Host or join one from the Collaboration menu.")
		return
	}
	view := panel.app.CollaborationViewModel(panel.conflictPage)
	if view.SessionLabel != panel.sessionID {
		panel.sessionID = view.SessionLabel
		if panel.conflictPage != 0 {
			view = panel.app.CollaborationViewModel(0)
		}
	}
	panel.conflictPage = view.ConflictPage
	sessionLabel := view.SessionLabel
	if sessionLabel == "" {
		sessionLabel = "Pending"
	}
	imgui.Text("Session: " + sessionLabel)
	imgui.Text("Role: " + view.RoleLabel)
	imgui.Text(view.RevisionLabel)
	imgui.Text("Status: " + view.SyncLabel)
	if view.StatusBanner != "" {
		imgui.TextWrapped(view.StatusBanner)
	}
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
	panel.showCursorColorPicker() // APHELION EDIT ADDITION - COLLABORATION CURSOR COLOR

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
		panel.renderBulkActions(view)
		panel.renderConflictNavigation(view)
		for index, conflict := range view.Conflicts {
			imgui.PushID(string(conflict.OperationID))
			imgui.TextWrapped(view.ConflictSummaries[index])
			imgui.TextDisabled(fmt.Sprintf("Recorded at revision %d", conflict.Revision))
			panel.renderConflictValues(conflict)
			w.Button("Refresh", func() { panel.app.DoResolveCollaborationConflict(conflict.OperationID, ConflictActionRefresh) }).Build()
			imgui.SameLine()
			w.Button("Discard", func() { panel.app.DoResolveCollaborationConflict(conflict.OperationID, ConflictActionDiscard) }).Build()
			imgui.SameLine()
			w.Button("Rebuild", func() { panel.app.DoResolveCollaborationConflict(conflict.OperationID, ConflictActionRebuild) }).Build()
			imgui.SameLine()
			w.Button("Export Draft", func() { panel.app.DoResolveCollaborationConflict(conflict.OperationID, ConflictActionExport) }).Build()
			imgui.PopID()
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

func (panel *Panel) renderBulkActions(view ViewModel) {
	imgui.TextDisabled(draftCountText(view.DraftCount))
	panel.bulk.render(panel.app, view, func() { panel.kept = true })
	if panel.kept {
		imgui.TextDisabled("Drafts are kept. They stay here until you resolve them.")
	}
}

func (panel *Panel) renderConflictNavigation(view ViewModel) {
	if view.ConflictPageCount <= 1 {
		return
	}
	imgui.TextDisabled(fmt.Sprintf("Drafts %d–%d of %d", view.ConflictPage*maxVisibleConflicts+1, view.ConflictPage*maxVisibleConflicts+len(view.Conflicts), view.ConflictCount))
	w.Disabled(view.ConflictPage == 0, w.Button("Previous##conflict-page", func() { panel.conflictPage = view.ConflictPage - 1 })).Build()
	imgui.SameLine()
	w.Disabled(view.ConflictPage+1 >= view.ConflictPageCount, w.Button("Next##conflict-page", func() { panel.conflictPage = view.ConflictPage + 1 })).Build()
}

func (panel *Panel) renderConflictValues(conflict ConflictView) {
	panel.renderTileValues("Draft before", conflict.DraftBefore, conflict.DraftTileCount)
	panel.renderTileValues("Draft intended values", conflict.DraftAfter, conflict.DraftTileCount)
	panel.renderTileValues("Authoritative values", conflict.Values, conflict.AuthoritativeTileCount)
}

func (panel *Panel) renderTileValues(title string, values []AuthoritativeTileView, totalTiles int) {
	label := fmt.Sprintf("%s (%d tiles)##conflict-values-%s", title, totalTiles, title)
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
			if hidden := prefab.VariableCount - visibleVariables; hidden != 0 {
				imgui.TextDisabled(fmt.Sprintf("    %d additional variables hidden", hidden))
			}
		}
		if hidden := tile.PrefabCount - visiblePrefabs; hidden != 0 {
			imgui.TextDisabled(fmt.Sprintf("  %d additional prefabs hidden", hidden))
		}
	}
	if hidden := totalTiles - visibleTiles; hidden != 0 {
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
